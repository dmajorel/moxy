package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestTextIsAValidExposition walks the shape a Prometheus scraper parses: a
// HELP and a TYPE line per family, then one sample line per series, with the
// labels braced and quoted. It is written by hand here, so the grammar is
// worth an assertion rather than a hope.
func TestTextIsAValidExposition(t *testing.T) {
	r := New()
	c := r.CounterVec("moxy_test_total", "A counter.", "cluster", "outcome")
	c.Inc("prod", "ok")
	c.Inc("prod", "ok")
	c.Inc("prod", "auth")
	g := r.GaugeVec("moxy_test_gauge", "A gauge.", "cluster")
	g.Set(0.5, "prod")

	got := r.Text()
	for _, want := range []string{
		"# HELP moxy_test_total A counter.\n",
		"# TYPE moxy_test_total counter\n",
		`moxy_test_total{cluster="prod",outcome="auth"} 1` + "\n",
		`moxy_test_total{cluster="prod",outcome="ok"} 2` + "\n",
		"# TYPE moxy_test_gauge gauge\n",
		`moxy_test_gauge{cluster="prod"} 0.5` + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("exposition is missing %q:\n%s", want, got)
		}
	}

	// Every line is either a comment or a sample; nothing else is legal.
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if strings.HasPrefix(line, "# ") {
			continue
		}
		if !strings.Contains(line, " ") {
			t.Errorf("sample line has no value: %q", line)
		}
	}
}

// TestSameNameIsTheSameFamily: a family is declared where it is used, so two
// declarations of one name must return the same object. Two families would
// mean two HELP lines for one name, which a scraper rejects outright.
func TestSameNameIsTheSameFamily(t *testing.T) {
	r := New()
	r.CounterVec("moxy_test_total", "A counter.", "cluster").Inc("a")
	r.CounterVec("moxy_test_total", "A counter.", "cluster").Inc("a")

	got := r.Text()
	if n := strings.Count(got, "# HELP moxy_test_total"); n != 1 {
		t.Errorf("the name was declared %d times, want 1:\n%s", n, got)
	}
	if !strings.Contains(got, `moxy_test_total{cluster="a"} 2`) {
		t.Errorf("the two increments did not land on one series:\n%s", got)
	}
}

// TestEmptyFamilyIsNotExposed: a metric nobody has written to has nothing to
// say, and a scraper reads its absence the same way it reads a HELP line with
// no samples under it.
func TestEmptyFamilyIsNotExposed(t *testing.T) {
	r := New()
	r.CounterVec("moxy_test_total", "A counter.", "cluster")

	if got := r.Text(); got != "" {
		t.Errorf("exposition = %q, want it empty", got)
	}
}

func TestHistogramExposition(t *testing.T) {
	r := New()
	h := r.HistogramVec("moxy_test_seconds", "A histogram.", []float64{0.1, 1}, "cluster")
	h.Observe(0.05, "prod")
	h.Observe(0.5, "prod")
	h.Observe(30, "prod")

	got := r.Text()
	for _, want := range []string{
		"# TYPE moxy_test_seconds histogram\n",
		// CUMULATIVE buckets: le="1" counts everything at or below 1, the
		// 0.05 included. A scraper computing a quantile on non-cumulative
		// buckets gets nonsense.
		`moxy_test_seconds_bucket{cluster="prod",le="0.1"} 1` + "\n",
		`moxy_test_seconds_bucket{cluster="prod",le="1"} 2` + "\n",
		// The +Inf bucket is mandatory, and equals the count.
		`moxy_test_seconds_bucket{cluster="prod",le="+Inf"} 3` + "\n",
		`moxy_test_seconds_sum{cluster="prod"} 30.55` + "\n",
		`moxy_test_seconds_count{cluster="prod"} 3` + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("exposition is missing %q:\n%s", want, got)
		}
	}
}

// A counter that went backwards makes every rate() over it nonsense, so a
// negative delta is dropped rather than applied.
func TestCounterRefusesToGoBackwards(t *testing.T) {
	r := New()
	c := r.CounterVec("moxy_test_total", "A counter.", "cluster")
	c.Add(5, "prod")
	c.Add(-3, "prod")

	if got := r.Text(); !strings.Contains(got, `moxy_test_total{cluster="prod"} 5`) {
		t.Errorf("exposition = %q, want the counter unchanged at 5", got)
	}
}

// TestTextIsStable: two scrapes of an unchanged process must be identical, or
// a diff of them is unreadable. Map iteration order is what would break it.
func TestTextIsStable(t *testing.T) {
	r := New()
	c := r.CounterVec("moxy_test_total", "A counter.", "cluster")
	for _, cluster := range []string{"z", "a", "m", "b"} {
		c.Inc(cluster)
	}

	first := r.Text()
	for i := 0; i < 20; i++ {
		if got := r.Text(); got != first {
			t.Fatalf("scrape %d differs:\n%s\n---\n%s", i, first, got)
		}
	}
	// Sorted, so the diff of two scrapes reads by cluster.
	if a, z := strings.Index(first, `cluster="a"`), strings.Index(first, `cluster="z"`); a > z {
		t.Errorf("series are not sorted by label:\n%s", first)
	}
}

// A label value is caller data in principle, so the three characters the
// format reserves are escaped. Nothing in this repository produces one; the
// escaping exists so that the day something does, the exposition stays
// parsable rather than silently truncated.
func TestLabelValuesAreEscaped(t *testing.T) {
	r := New()
	r.CounterVec("moxy_test_total", "A counter.", "name").Inc(`a"b\c` + "\n" + "d")

	if got := r.Text(); !strings.Contains(got, `moxy_test_total{name="a\"b\\c\nd"} 1`) {
		t.Errorf("exposition did not escape the value:\n%s", got)
	}
}

// A miswired counter must not take the daemon down: a metric is never worth a
// panic. The wrong arity shows up as an obviously wrong label instead.
func TestWrongArityDoesNotPanic(t *testing.T) {
	r := New()
	c := r.CounterVec("moxy_test_total", "A counter.", "cluster", "outcome")
	c.Inc("prod")
	c.Inc("prod", "ok", "extra")

	if got := r.Text(); !strings.Contains(got, `outcome="unknown"`) {
		t.Errorf("exposition = %q, want the missing label filled in", got)
	}
}

func TestGaugeSetTime(t *testing.T) {
	r := New()
	at := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
	r.GaugeVec("moxy_test_timestamp_seconds", "A timestamp.", "cluster").SetTime(at, "prod")

	if got := r.Text(); !strings.Contains(got, `moxy_test_timestamp_seconds{cluster="prod"} 1.7892144e+09`) {
		t.Errorf("exposition = %q, want the unix seconds of %s", got, at)
	}
}

func TestHandlerServesTheExposition(t *testing.T) {
	r := New()
	r.CounterVec("moxy_test_total", "A counter.", "cluster").Inc("prod")

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// The version is part of the media type and is not optional: a scraper
	// reads it to know which format it is parsing.
	if got := rec.Header().Get("Content-Type"); got != contentType {
		t.Errorf("Content-Type = %q, want %q", got, contentType)
	}
	// A scraper reading a cached page would chart a process that has stopped.
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "moxy_test_total") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestHandlerRejectsOtherMethods(t *testing.T) {
	r := New()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		r.Handler().ServeHTTP(rec, httptest.NewRequest(method, "/metrics", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want %d", method, rec.Code, http.StatusMethodNotAllowed)
		}
		if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("%s Allow = %q", method, got)
		}
	}
}

// TestConcurrentWritersAgree: the poller writes from its own goroutines while
// a scraper reads. Without -race this proves nothing about data races, but it
// does prove the arithmetic survives contention, which a lock dropped from one
// path would not.
func TestConcurrentWritersAgree(t *testing.T) {
	r := New()
	c := r.CounterVec("moxy_test_total", "A counter.", "cluster")

	const writers, each = 8, 250
	done := make(chan struct{})
	for i := 0; i < writers; i++ {
		go func() {
			for j := 0; j < each; j++ {
				c.Inc("prod")
			}
			done <- struct{}{}
		}()
	}
	go func() {
		for i := 0; i < 100; i++ {
			_ = r.Text()
		}
	}()
	for i := 0; i < writers; i++ {
		<-done
	}

	want := "moxy_test_total{cluster=\"prod\"} " + formatFloat(float64(writers*each))
	if got := r.Text(); !strings.Contains(got, want) {
		t.Errorf("exposition = %q, want %q", got, want)
	}
}
