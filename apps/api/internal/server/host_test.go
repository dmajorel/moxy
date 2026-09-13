package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// serveWithHost runs one request through the whole handler, Host check
// included, against a handler built for the given listen address and list.
func serveWithHost(t *testing.T, addr string, allowedHosts []string, host, target string) *httptest.ResponseRecorder {
	t.Helper()
	h := newHandler(Options{Addr: addr, AllowedHosts: allowedHosts, Overview: fakeSource{overview: sampleOverview()}})
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestHostCheckRefusesAForeignName is the DNS rebinding case. A page on
// attacker.example that has repointed its own name at 127.0.0.1 reaches moxy
// with its own Host, from an origin it controls; without this check, moxy
// answers it with the cluster inventory.
func TestHostCheckRefusesAForeignName(t *testing.T) {
	for _, host := range []string{"attacker.example", "attacker.example:8080", "", "evil.localhost"} {
		t.Run("host="+host, func(t *testing.T) {
			rec := serveWithHost(t, "127.0.0.1:8080", nil, host, "/api/overview")

			if rec.Code != http.StatusMisdirectedRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusMisdirectedRequest)
			}
			if got := decodeError(t, rec).Error; got != "misdirected request" {
				t.Errorf("error = %q", got)
			}
		})
	}
}

// TestHostCheckAcceptsTheNamesMoxyAnswersTo covers what an operator types and
// what a reverse proxy passes through.
func TestHostCheckAcceptsTheNamesMoxyAnswersTo(t *testing.T) {
	cases := []struct {
		name  string
		addr  string
		hosts []string
		host  string
	}{
		{"loopback address", "127.0.0.1:8080", nil, "127.0.0.1:8080"},
		{"loopback name", "127.0.0.1:8080", nil, "localhost"},
		{"loopback name with port", "127.0.0.1:8080", nil, "localhost:8080"},
		{"ipv6 loopback", "127.0.0.1:8080", nil, "[::1]:8080"},
		{"host of addr", "192.168.1.10:8080", nil, "192.168.1.10:8080"},
		{"declared host", "0.0.0.0:8080", []string{"moxy.example"}, "moxy.example"},
		{"declared host, other case", "0.0.0.0:8080", []string{"moxy.example"}, "MOXY.Example"},
		{"declared host, trailing dot", "0.0.0.0:8080", []string{"moxy.example"}, "moxy.example."},
		{"declared list", "0.0.0.0:8080", []string{"a.example,b.example"}, "b.example"},
		// Binding to a fixed address must not lock the operator out of the
		// loopback names they reach it by through a tunnel.
		{"loopback despite a fixed addr", "192.168.1.10:8080", nil, "localhost"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveWithHost(t, tc.addr, tc.hosts, tc.host, "/api/overview")

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body.String())
			}
		})
	}
}

// TestHostCheckIsOffOnAGenericListen is the container deployment: moxy cannot
// guess the name it is reached by, so it blocks nothing and says so at startup
// instead. Refusing everything here would break the supported deployment.
func TestHostCheckIsOffOnAGenericListen(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8080", ":8080", "[::]:8080", ""} {
		t.Run("addr="+addr, func(t *testing.T) {
			if !HostCheckDisabled(addr, nil) {
				t.Errorf("HostCheckDisabled(%q, nil) = false, want true", addr)
			}
			rec := serveWithHost(t, addr, nil, "attacker.example", "/api/overview")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}

	// One declared name is enough to switch it back on.
	if HostCheckDisabled("0.0.0.0:8080", []string{"moxy.example"}) {
		t.Error("HostCheckDisabled = true with a declared host, want false")
	}
	if HostCheckDisabled("127.0.0.1:8080", nil) {
		t.Error("HostCheckDisabled = true on a loopback listen, want false")
	}
}

// TestHealthzIsExemptFromTheHostCheck documents the choice rather than merely
// asserting it: a liveness probe is the one caller whose Host the operator does
// not control, and /healthz says nothing about any cluster.
func TestHealthzIsExemptFromTheHostCheck(t *testing.T) {
	rec := serveWithHost(t, "127.0.0.1:8080", nil, "attacker.example", "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Everything under it is checked, /healthz/ included: that path is not the
	// probe, it is the typo handler.
	for _, target := range []string{"/healthz/", "/api/overview", "/api/clusters/prod/nodes/pve-01", "/"} {
		t.Run(target, func(t *testing.T) {
			rec := serveWithHost(t, "127.0.0.1:8080", nil, "attacker.example", target)
			if rec.Code != http.StatusMisdirectedRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusMisdirectedRequest)
			}
		})
	}
}

// TestHostCheckRunsBeforeRouting: a request that is not addressed to moxy must
// not reach a handler, not even to be told its path is wrong.
func TestHostCheckRunsBeforeRouting(t *testing.T) {
	rec := serveWithHost(t, "127.0.0.1:8080", nil, "attacker.example", "/api/nope")
	if rec.Code != http.StatusMisdirectedRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMisdirectedRequest)
	}
}
