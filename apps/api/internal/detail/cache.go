package detail

import (
	"context"
	"sync"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/metrics"
)

// cache is a short-lived, single-flight cache.
//
// It exists for one reason: the detail views are fetched ON DEMAND, and the UI
// refreshes every five seconds. Ten operators watching the same node would
// otherwise mean ten /nodes/{node}/status calls every five seconds, for data
// that is identical in all ten answers. An entry therefore serves two
// purposes at once — it remembers a result for a TTL, and it makes the
// concurrent requests that miss it WAIT for a single upstream call instead of
// each starting one of their own.
//
// It is generic over the key and the value, and reads its clock through a
// function, so the TTL can be tested without sleeping.
type cache[K comparable, V any] struct {
	ttl time.Duration
	// errTTL is how long a FAILED load is remembered. It is separate because
	// the two answers age differently: a value is as good as its age, while a
	// failure may have been a single slow second. On the long-lived caches --
	// pending updates, guest configuration -- remembering an error as long as
	// a value meant one timeout froze the field for minutes.
	errTTL time.Duration
	// sticky reports the failures that deserve the FULL ttl anyway. A 403 on
	// apt/update is the documented state of a read-only token: it will not
	// change without somebody editing an ACL, and re-asking it every few
	// seconds is sixty times the requests for an answer that is settled.
	sticky func(error) bool
	budget time.Duration
	now    func() time.Time
	// name labels this cache in /metrics. It is a constant of the service --
	// "view", "node", "guest" -- never a key, so the exposition carries one
	// series per cache rather than one per object.
	name string

	// mu guards entries only. It is NEVER held while the loader runs, which
	// is what keeps one slow key from blocking every other key.
	mu      sync.Mutex
	entries map[K]*entry[V]
}

// entry is one cached result, possibly still in flight.
//
// value, err and storedAt are written exactly once, before done is closed;
// every reader waits on done first, so the close is the happens-before edge
// that publishes them.
type entry[V any] struct {
	done     chan struct{}
	value    V
	err      error
	storedAt time.Time
}

// stamped pairs a value with the moment it was collected.
//
// Cached values carry their own timestamp because the payloads say when the
// data was fetched, and with a cache in the way that is NOT the moment the
// request came in: serving a four-second-old reading stamped "now" would be a
// small, silent lie in every detail payload.
type stamped[V any] struct {
	Value V
	At    time.Time
}

// newCache builds a cache whose entries live for ttl and whose loader is
// bounded by budget. now defaults to time.Now. Failures are remembered for
// ttl as well; see newCacheWithErrTTL for the caches where that is too long.
func newCache[K comparable, V any](name string, ttl, budget time.Duration, now func() time.Time) *cache[K, V] {
	return newCacheWithErrTTL[K, V](name, ttl, ttl, budget, now, nil)
}

// newCacheWithErrTTL is newCache with a shorter memory for failures.
func newCacheWithErrTTL[K comparable, V any](name string, ttl, errTTL, budget time.Duration, now func() time.Time, sticky func(error) bool) *cache[K, V] {
	if now == nil {
		now = time.Now
	}
	if errTTL <= 0 || errTTL > ttl {
		errTTL = ttl
	}
	return &cache[K, V]{
		name:    name,
		ttl:     ttl,
		errTTL:  errTTL,
		sticky:  sticky,
		budget:  budget,
		now:     now,
		entries: make(map[K]*entry[V]),
	}
}

// get returns the cached value for key, calling load at most once for all the
// callers that arrive while it runs.
//
// ERRORS ARE CACHED, for errTTL rather than ttl. A cluster that is down or a
// token that lacks a privilege would otherwise be asked again by every single
// request, which is precisely the stampede this type exists to prevent; a few
// seconds of remembering "this failed" costs nothing and spares the cluster.
//
// The two ages differ where the value lives a long time. Pending updates are
// kept five minutes because they change about once a day -- but a single slow
// answer is not five minutes of news, and remembering it that long left a node
// page saying "unknown" long after the cluster had recovered.
//
// CANCELLATION. The loader does NOT run under the caller's context. It runs
// under a context that keeps the caller's values but has no deadline and no
// cancellation of its own (see detach), bounded instead by the cache's own
// budget. This is the classic trap of the pattern: with the caller's context,
// the first arrival closing its browser tab would cancel the call the nine
// others are waiting on, and they would all fail for a reason that has nothing
// to do with them. A caller that goes away here stops waiting — get returns
// ctx.Err() to that caller alone — while the shared work continues and lands
// in the cache for whoever else wanted it.
func (c *cache[K, V]) get(ctx context.Context, key K, load func(context.Context) (V, error)) (V, error) {
	e, mine := c.lookup(key)
	if mine {
		go c.fill(ctx, e, load)
	}
	select {
	case <-e.done:
		return e.value, e.err
	case <-ctx.Done():
		var zero V
		return zero, ctx.Err()
	}
}

// lookup returns the entry for key and whether the caller is the one that must
// load it. A fresh entry is reused, an in-flight one is joined, and an expired
// one is replaced.
func (c *cache[K, V]) lookup(key K) (*entry[V], bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.entries[key]; ok && !c.expired(e) {
		// A hit is a finished entry; an unfinished one is a caller joining a
		// call already in flight, which is the anti-stampede lock earning its
		// keep. Telling the two apart is the whole reason this metric exists:
		// nothing else in the process can see a join happen.
		c.observe(inFlight(e))
		return e, false
	}
	c.sweep()

	c.observe(metrics.EventMiss)
	e := &entry[V]{done: make(chan struct{})}
	c.entries[key] = e
	return e, true
}

// observe counts one lookup. It is called with mu held, which is where the
// decision it reports is made.
func (c *cache[K, V]) observe(event string) {
	if c.name == "" {
		return
	}
	metrics.DetailCacheEvents.Inc(c.name, event)
}

// inFlight says whether an entry has finished loading.
func inFlight[V any](e *entry[V]) string {
	select {
	case <-e.done:
		return metrics.EventHit
	default:
		return metrics.EventJoin
	}
}

// expired reports whether an entry may no longer be served. An entry still in
// flight is never expired: joining it is the whole point, and starting a
// second call for the same key would defeat it.
//
// It must be called with mu held.
func (c *cache[K, V]) expired(e *entry[V]) bool {
	select {
	case <-e.done:
	default:
		return false
	}
	ttl := c.ttl
	if e.err != nil && !(c.sticky != nil && c.sticky(e.err)) {
		ttl = c.errTTL
	}
	return !c.now().Before(e.storedAt.Add(ttl))
}

// sweep drops the expired entries. The map is keyed by node name, vmid and
// timeframe, so it is small and bounded in practice; sweeping on insertion
// keeps it from growing with every guest that was ever looked at.
//
// It must be called with mu held.
func (c *cache[K, V]) sweep() {
	for k, e := range c.entries {
		if c.expired(e) {
			delete(c.entries, k)
		}
	}
}

// fill runs the loader and publishes its result to everyone waiting on the
// entry. ctx is the context of the caller that happened to arrive first: only
// its VALUES are kept, never its cancellation. See get.
func (c *cache[K, V]) fill(ctx context.Context, e *entry[V], load func(context.Context) (V, error)) {
	loadCtx, cancel := context.WithTimeout(detach(ctx), c.budget)
	defer cancel()

	value, err := load(loadCtx)

	c.mu.Lock()
	e.value, e.err, e.storedAt = value, err, c.now()
	c.mu.Unlock()

	close(e.done)
}

// detached carries the values of a context without its cancellation or its
// deadline. context.WithoutCancel does the same thing, and arrived in Go 1.21;
// this project builds with 1.19.
type detached struct {
	ctx context.Context
}

// Deadline implements context.Context: there is none.
func (d detached) Deadline() (time.Time, bool) { return time.Time{}, false }

// Done implements context.Context. A nil channel blocks forever, which is how
// the standard library spells "this context is never cancelled".
func (d detached) Done() <-chan struct{} { return nil }

// Err implements context.Context: never cancelled, so never an error.
func (d detached) Err() error { return nil }

// Value implements context.Context. Values ARE forwarded: they carry the
// request-scoped data a caller may have attached, and none of them expires.
func (d detached) Value(key any) any { return d.ctx.Value(key) }

// detach strips the cancellation of ctx while keeping its values.
func detach(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return detached{ctx: ctx}
}
