package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmajorel/moxy/apps/api/internal/config"
)

func proxyAuth(t *testing.T) config.Auth {
	t.Helper()
	// Built through the constructor the loader uses, so the test cannot drift
	// from what a real configuration produces.
	auth, err := config.NewProxyHeaderAuth("", []string{"127.0.0.1/32", "10.0.0.0/8"})
	if err != nil {
		t.Fatalf("NewProxyHeaderAuth: %v", err)
	}
	return auth
}

func request(method, path, remote, header string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = remote
	if header != "" {
		r.Header.Set(config.DefaultAuthHeader, header)
	}
	return r
}

// TestAuthNeedsBothTheProxyAndTheHeader: either alone proves nothing. The
// header can be set by anyone who reaches the port, and the proxy address is
// shared by everyone it forwards for.
func TestAuthNeedsBothTheProxyAndTheHeader(t *testing.T) {
	handler := newHandler(Options{Auth: proxyAuth(t)})

	tests := []struct {
		name   string
		remote string
		header string
		want   int
	}{
		{"trusted proxy and a user", "127.0.0.1:5000", "roman", http.StatusOK},
		{"trusted range and a user", "10.4.1.9:5000", "roman", http.StatusOK},
		{"no header at all", "127.0.0.1:5000", "", http.StatusUnauthorized},
		{"header from an untrusted peer", "192.0.2.7:5000", "roman", http.StatusUnauthorized},
		{"neither", "192.0.2.7:5000", "", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, request(http.MethodGet, "/api/overview", tc.remote, tc.header))
			// 503 is what an unconfigured overview source answers; anything
			// other than 401 means the request got past the guard.
			if tc.want == http.StatusOK && rec.Code == http.StatusUnauthorized {
				t.Fatalf("status = %d, want the request to be let through", rec.Code)
			}
			if tc.want == http.StatusUnauthorized && rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

// TestAuthLeavesTheProbesAlone: an orchestrator has no identity to present, and
// a liveness check that fails on authentication restarts a healthy daemon.
func TestAuthLeavesTheProbesAlone(t *testing.T) {
	handler := newHandler(Options{Auth: proxyAuth(t)})

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, request(http.MethodGet, path, "192.0.2.7:5000", ""))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

// TestAuthGuardsTheBundleToo: the frontend is the estate's topology rendered as
// a page. A 401 there is as necessary as on the API.
func TestAuthGuardsTheBundleToo(t *testing.T) {
	web, err := NewWebHandler(writeBundle(t))
	if err != nil {
		t.Fatalf("NewWebHandler: %v", err)
	}
	handler := newHandler(Options{Auth: proxyAuth(t), Web: web})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request(http.MethodGet, "/", "192.0.2.7:5000", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestAuthNeverEchoesTheIdentity: the header value is a user name asserted by
// someone who may have no business asserting it.
func TestAuthNeverEchoesTheIdentity(t *testing.T) {
	handler := newHandler(Options{Auth: proxyAuth(t)})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request(http.MethodGet, "/api/overview", "192.0.2.7:5000", "impersonated"))
	if body := rec.Body.String(); strings.Contains(body, "impersonated") {
		t.Fatalf("the refusal echoes the asserted identity: %s", body)
	}
}

func TestListensBeyondLoopback(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1:8080": false,
		"[::1]:8080":     false,
		"0.0.0.0:8080":   true,
		"[::]:8080":      true,
		":8080":          true,
		"10.4.1.9:8080":  true,
		"moxy.local:80":  true,
	}
	for addr, want := range tests {
		if got := ListensBeyondLoopback(addr); got != want {
			t.Errorf("ListensBeyondLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}
