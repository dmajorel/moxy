package detail

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testClock is a clock the tests move by hand, so a TTL can be exercised
// without a single sleep.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestCacheServesTheSameValueWithoutCallingLoaderAgain(t *testing.T) {
	clock := newTestClock()
	c := newCache[string, int](5*time.Second, time.Second, clock.Now)

	var calls int32
	load := func(context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		return 42, nil
	}

	for i := 0; i < 3; i++ {
		got, err := c.get(context.Background(), "k", load)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got != 42 {
			t.Fatalf("got %d, want 42", got)
		}
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("loader called %d times, want 1", n)
	}
}

func TestCacheReloadsAfterTTL(t *testing.T) {
	clock := newTestClock()
	c := newCache[string, int](5*time.Second, time.Second, clock.Now)

	var calls int32
	load := func(context.Context) (int, error) {
		return int(atomic.AddInt32(&calls, 1)), nil
	}

	if got, _ := c.get(context.Background(), "k", load); got != 1 {
		t.Fatalf("first get returned %d, want 1", got)
	}

	clock.advance(4 * time.Second)
	if got, _ := c.get(context.Background(), "k", load); got != 1 {
		t.Fatalf("get before the ttl elapsed returned %d, want the cached 1", got)
	}

	clock.advance(time.Second)
	if got, _ := c.get(context.Background(), "k", load); got != 2 {
		t.Fatalf("get after the ttl elapsed returned %d, want a fresh 2", got)
	}
}

func TestCacheRemembersFailures(t *testing.T) {
	clock := newTestClock()
	c := newCache[string, int](5*time.Second, time.Second, clock.Now)

	want := errors.New("cluster is down")
	var calls int32
	load := func(context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		return 0, want
	}

	for i := 0; i < 3; i++ {
		if _, err := c.get(context.Background(), "k", load); !errors.Is(err, want) {
			t.Fatalf("get returned %v, want %v", err, want)
		}
	}
	// An unreachable cluster must not be asked again by every request that
	// comes in during the same five seconds.
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("loader called %d times for a failing key, want 1", n)
	}
}

func TestCacheCollapsesConcurrentCallsOnTheSameKey(t *testing.T) {
	const callers = 10

	clock := newTestClock()
	c := newCache[string, int](5*time.Second, time.Second, clock.Now)

	var (
		calls   int32
		arrived int32
	)
	load := func(context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		// Stay in flight until every caller is on its way, so this really is
		// the stampede the cache exists to absorb and not a sequence of hits
		// on an already-stored value.
		for atomic.LoadInt32(&arrived) < callers {
			runtime.Gosched()
		}
		return 7, nil
	}

	var wg sync.WaitGroup
	results := make([]int, callers)
	for i := 0; i < callers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt32(&arrived, 1)
			got, err := c.get(context.Background(), "k", load)
			if err != nil {
				t.Errorf("caller %d: %v", i, err)
			}
			results[i] = got
		}()
	}
	wg.Wait()

	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("%d callers triggered %d upstream calls, want 1", callers, n)
	}
	for i, got := range results {
		if got != 7 {
			t.Fatalf("caller %d got %d, want 7", i, got)
		}
	}
}

func TestCacheDoesNotBlockOtherKeys(t *testing.T) {
	clock := newTestClock()
	c := newCache[string, int](5*time.Second, time.Second, clock.Now)

	stuck := make(chan struct{})
	defer close(stuck)

	go func() {
		_, _ = c.get(context.Background(), "slow", func(context.Context) (int, error) {
			<-stuck
			return 1, nil
		})
	}()

	// No synchronisation with the goroutine above: whether it has started or
	// not, a different key must be answerable. A cache holding its lock
	// across the loader would deadlock here.
	got, err := c.get(context.Background(), "fast", func(context.Context) (int, error) {
		return 2, nil
	})
	if err != nil {
		t.Fatalf("get on the other key: %v", err)
	}
	if got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
}

func TestCacheCancellingOneCallerLeavesTheSharedWorkAlone(t *testing.T) {
	clock := newTestClock()
	c := newCache[string, int](5*time.Second, time.Second, clock.Now)

	var calls int32
	entered := make(chan struct{})
	release := make(chan struct{})
	load := func(ctx context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		close(entered)
		<-release
		// The loader must not see the cancellation of the caller that
		// happened to arrive first.
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return 9, nil
	}

	leaving, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := c.get(leaving, "k", load)
		first <- err
	}()

	<-entered

	second := make(chan int, 1)
	go func() {
		got, err := c.get(context.Background(), "k", load)
		if err != nil {
			t.Errorf("second caller: %v", err)
		}
		second <- got
	}()

	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("the caller that went away got %v, want context.Canceled", err)
	}

	close(release)
	if got := <-second; got != 9 {
		t.Fatalf("the caller that stayed got %d, want 9", got)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("loader called %d times, want 1", n)
	}
}

func TestDetachKeepsValuesAndDropsCancellation(t *testing.T) {
	type ctxKey string

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey("k"), "v"))
	cancel()

	d := detach(ctx)
	if got := d.Value(ctxKey("k")); got != "v" {
		t.Fatalf("value is %v, want v", got)
	}
	if err := d.Err(); err != nil {
		t.Fatalf("detached context reports %v, want no error", err)
	}
	if d.Done() != nil {
		t.Fatal("detached context is cancellable")
	}
	if _, ok := d.Deadline(); ok {
		t.Fatal("detached context carries a deadline")
	}
}

// TestCacheForgetsATransientFailureSooner: a value and a failure age
// differently. On the long-lived caches -- pending updates, guest
// configuration -- remembering one slow answer for as long as a good one left
// the node page saying "unknown" for minutes after the cluster had recovered.
func TestCacheForgetsATransientFailureSooner(t *testing.T) {
	clock := newTestClock()
	c := newCacheWithErrTTL[string, int](5*time.Minute, 5*time.Second, time.Second, clock.Now, nil)

	var calls int32
	load := func(fail bool) func(context.Context) (int, error) {
		return func(context.Context) (int, error) {
			atomic.AddInt32(&calls, 1)
			if fail {
				return 0, errors.New("http 504 Gateway Timeout")
			}
			return 42, nil
		}
	}

	if _, err := c.get(context.Background(), "k", load(true)); err == nil {
		t.Fatal("want the failure")
	}
	clock.advance(6 * time.Second)
	if got, err := c.get(context.Background(), "k", load(false)); err != nil || got != 42 {
		t.Fatalf("get = %v, %v: the failure outlived its own ttl", got, err)
	}

	// The value, by contrast, is kept for the long one.
	clock.advance(time.Minute)
	if got, _ := c.get(context.Background(), "k", load(false)); got != 42 {
		t.Fatalf("get = %v, want the cached value", got)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("loader ran %d times, want 2", got)
	}
}

// TestCacheKeepsASettledRefusal: a 403 is the documented state of a read-only
// token. It changes when somebody edits an ACL, not on its own, and re-asking
// it every few seconds is sixty times the requests for a settled answer.
func TestCacheKeepsASettledRefusal(t *testing.T) {
	clock := newTestClock()
	refused := errors.New("forbidden")
	c := newCacheWithErrTTL[string, int](5*time.Minute, 5*time.Second, time.Second, clock.Now,
		func(err error) bool { return errors.Is(err, refused) })

	var calls int32
	load := func(context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		return 0, refused
	}

	if _, err := c.get(context.Background(), "k", load); !errors.Is(err, refused) {
		t.Fatalf("err = %v", err)
	}
	clock.advance(time.Minute)
	if _, err := c.get(context.Background(), "k", load); !errors.Is(err, refused) {
		t.Fatalf("err = %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("loader ran %d times, want 1: a settled refusal was re-asked", got)
	}
}
