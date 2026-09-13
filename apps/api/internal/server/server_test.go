package server

import (
	"encoding/json"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/config"
	"github.com/dmajorel/moxy/apps/api/internal/metrics"
)

func TestHealthzReturnsOK(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}

	var body health
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("unreadable body: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want \"ok\"", body.Status)
	}
	if body.Version == "" {
		t.Error("empty version")
	}
	// A probe answered from a cache would report a daemon that is no longer
	// there, which is the one thing a liveness check must not do.
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want \"no-store\"", got)
	}
}

// Load balancers probe with HEAD — HAProxy's httpchk defaults to it — and a 405
// there reads as an unhealthy backend.
func TestReadRoutesAnswerHead(t *testing.T) {
	handler := newHandler(Options{Overview: fakeSource{overview: sampleOverview()}})

	for _, target := range []string{"/healthz", "/api/overview"} {
		t.Run(target, func(t *testing.T) {
			// A real server, not a recorder: dropping the body of a HEAD is
			// net/http's doing, and a recorder would keep it.
			srv := httptest.NewServer(handler)
			defer srv.Close()

			req, err := http.NewRequest(http.MethodHead, srv.URL+target, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if len(body) != 0 {
				t.Errorf("HEAD body = %q, want empty", body)
			}
		})
	}
}

// The detail routes answer HEAD too, and reach the same 501 as a GET in mock
// mode: the method check must not sit between the route and its answer.
func TestDetailRoutesAnswerHead(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/api/clusters/qualification/nodes/pve-01", nil))

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

// nosniff is a property of the whole server, not of the bundle handler: a JSON
// error answered without it is exactly what a sniffing browser reinterprets.
func TestEveryJSONAnswerCarriesNosniff(t *testing.T) {
	handler := newHandler(Options{Overview: fakeSource{overview: sampleOverview()}})

	for _, tc := range []struct {
		name   string
		method string
		target string
		status int
	}{
		{"healthz", http.MethodGet, "/healthz", http.StatusOK},
		{"overview", http.MethodGet, "/api/overview", http.StatusOK},
		{"not found", http.MethodGet, "/api/unknown", http.StatusNotFound},
		{"healthz subtree", http.MethodGet, "/healthz/", http.StatusNotFound},
		{"method not allowed", http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.target, nil))

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want \"nosniff\"", got)
			}
		})
	}
}

// Without -web there is no page, so nothing describes one: a content policy on
// a JSON answer would only be noise.
func TestAPIOnlyModeSetsNoPageHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if got := rec.Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("Content-Security-Policy = %q, want none", got)
	}
}

func TestHealthzRejectsOtherMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q, want \"GET, HEAD\"", got)
	}

	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("unreadable body: %v", err)
	}
	if body.Error != "method not allowed" {
		t.Errorf("error = %q", body.Error)
	}
}

// An API path that ServeMux would rewrite must be refused outright rather than
// redirected: an authorization rule added later would otherwise be evaluated
// against the cleaned path while the caller sent another one.
func TestUncleanAPIPathsAreRefusedNotRedirected(t *testing.T) {
	handler := newHandler(Options{})

	for _, target := range []string{
		"/api/clusters/preproduction/nodes/../../overview",
		"/api/clusters/preproduction/nodes/%2e%2e%2f%2e%2e%2fetc",
		"/api/clusters//preproduction/nodes/n1",
		"/api/./overview",
		"/api/overview/../overview",
	} {
		t.Run(target, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusNotFound, rec.Body.String())
			}
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Errorf("the API redirected to %q; it must never redirect", loc)
			}

			var body errorBody
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("unreadable body: %v", err)
			}
			if body.Error != "not found" {
				t.Errorf("error = %q", body.Error)
			}
		})
	}
}

// The guard is confined to the API namespace: the SPA below it still wants the
// usual cleaning, so a browser reaching /clusters/../ gets the page.
func TestCleanAPIPathsAndTheSPAAreUntouched(t *testing.T) {
	spa := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("index"))
	})
	handler := newHandler(Options{Web: spa})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/healthz = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/clusters/../clusters", nil))
	if rec.Code == http.StatusNotFound {
		t.Errorf("the SPA fallback lost its path cleaning: %d", rec.Code)
	}
}

// TestReadyzReportsWarmUp: readiness is a different question from liveness.
// The daemon is up — /healthz says so — but it has nothing to serve until the
// first poll round lands.
func TestReadyzReportsWarmUp(t *testing.T) {
	warming := make(chan struct{})
	handler := newHandler(Options{Ready: warming})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d while warming up", rec.Code, http.StatusServiceUnavailable)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want \"no-store\"", got)
	}

	// Liveness is unaffected: the process answers, so nothing should restart it.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d during warm-up, want %d", rec.Code, http.StatusOK)
	}

	close(warming)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d once warm, want %d", rec.Code, http.StatusOK)
	}
	var body health
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("unreadable body: %v", err)
	}
	if body.Status != "ready" {
		t.Errorf("status = %q, want \"ready\"", body.Status)
	}
}

// TestReadyzWithoutASourceIsReady: mock mode has nothing to warm up, and a
// server built without a poller must not report a warm-up that never ends.
func TestReadyzWithoutASourceIsReady(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadyzRejectsOtherMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/readyz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// TestReadyzWithATrailingSlashIsNotTheSPA: same trap as /healthz/, same 404.
func TestReadyzWithATrailingSlashIsNotTheSPA(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz/", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestWriteJSONReportsAnEncodingFailure: encoding into the ResponseWriter meant
// the status had already gone out when the encoder failed. The client got 200
// with an empty body and an application/json type, read it as a parse error far
// from the cause, and nothing was logged.
func TestWriteJSONReportsAnEncodingFailure(t *testing.T) {
	var logged strings.Builder
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]any{"ratio": math.NaN()})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the body is not the error shape: %q (%v)", rec.Body.String(), err)
	}
	if body.Error == "" {
		t.Error("the error body is empty")
	}
	if logged.Len() == 0 {
		t.Error("nothing was logged about the encoding failure")
	}
}

func TestWriteJSONSetsContentLength(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, health{Status: "ok", Version: "test"})

	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(rec.Body.Len()) {
		t.Errorf("Content-Length = %q, body is %d bytes", got, rec.Body.Len())
	}
}

// TestServerTimeouts: the daemon talks to hypervisor nodes that can be slow or
// wedged, and an http.Server built without deadlines holds a file descriptor
// for as long as a client cares to keep it. The four bounds are the whole
// point of having a constructor at all, so they are pinned rather than left to
// whoever next edits the struct.
//
// The order matters as much as the values: reading the headers must be the
// tightest bound -- that is the one a slowloris client attacks -- and the idle
// bound the loosest, since a keep-alive connection of a reverse proxy is
// expected to sit unused between requests.
func TestServerTimeouts(t *testing.T) {
	srv := New(Options{Addr: "127.0.0.1:0"})

	if srv.Addr != "127.0.0.1:0" {
		t.Errorf("Addr = %q, want the one given", srv.Addr)
	}
	if srv.Handler == nil {
		t.Fatal("New built a server with no handler")
	}

	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"ReadHeaderTimeout", srv.ReadHeaderTimeout, 5 * time.Second},
		{"ReadTimeout", srv.ReadTimeout, 30 * time.Second},
		{"WriteTimeout", srv.WriteTimeout, 60 * time.Second},
		{"IdleTimeout", srv.IdleTimeout, 120 * time.Second},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %s, want %s", tt.name, tt.got, tt.want)
		}
	}
	if !(srv.ReadHeaderTimeout < srv.ReadTimeout &&
		srv.ReadTimeout < srv.WriteTimeout &&
		srv.WriteTimeout < srv.IdleTimeout) {
		t.Error("the four bounds must widen from the header read to the idle connection")
	}

	// The handler New wires is the real one, not an empty mux: a server with
	// correct timeouts and no routes would pass every assertion above.
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/healthz through the built server = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestMetricsIsServed: /metrics sits with the API, not with the probes. The
// exposition names every configured cluster and says when each was last
// reachable, which is operational detail about an estate; a scraper is
// configured with credentials like any other client.
func TestMetricsIsServed(t *testing.T) {
	metrics.Default.CounterVec("moxy_server_test_total", "A counter.", "cluster").Inc("prod")
	handler := newHandler(Options{})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q, want the Prometheus text format", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "moxy_server_test_total") {
		t.Errorf("body does not carry the registry:\n%s", body)
	}
	// The build identity is written when the handler is built, so a fresh
	// process exposes it before a single request has been served.
	if !strings.Contains(body, "moxy_build_info{version=") {
		t.Errorf("body does not carry the build info:\n%s", body)
	}

	// One slash too many is a 404, like every other route of this daemon:
	// falling through to the SPA would answer a scraper with index.html.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("/metrics/ = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestMetricsRequiresTheSameIdentityAsTheAPI: the two probes are exempt from
// authentication because a load balancer cannot carry a header; /metrics is
// not, and must not be. It names clusters.
func TestMetricsRequiresTheSameIdentityAsTheAPI(t *testing.T) {
	auth, err := config.NewProxyHeaderAuth("X-Forwarded-User", []string{"192.0.2.0/24"})
	if err != nil {
		t.Fatalf("NewProxyHeaderAuth: %v", err)
	}
	handler := newHandler(Options{Auth: auth})

	// From an untrusted peer, with no identity: refused, exactly as
	// /api/overview is.
	for _, target := range []string{"/metrics", "/api/overview"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.RemoteAddr = "198.51.100.7:5555"
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Errorf("%s answered 200 to an unauthenticated caller", target)
		}
	}

	// The probes stay open, or a load balancer would restart a healthy daemon.
	for _, target := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.RemoteAddr = "198.51.100.7:5555"
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want %d: a probe carries no identity", target, rec.Code, http.StatusOK)
		}
	}
}
