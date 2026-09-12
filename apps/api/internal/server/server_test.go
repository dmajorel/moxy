package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
}

func TestHealthzRejectsOtherMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("Allow = %q, want %q", got, http.MethodGet)
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
