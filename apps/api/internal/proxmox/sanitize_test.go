package proxmox

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmajorel/moxy/apps/api/internal/config"
)

// hostAndPort splits "https://127.0.0.1:36305" into its host and its port, the
// two strings a message served to the browser must not contain.
func hostAndPort(t *testing.T, rawURL string) (host, port string) {
	t.Helper()
	trimmed := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	host, port, err := net.SplitHostPort(trimmed)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", trimmed, err)
	}
	return host, port
}

// TestTransportErrorNamesNoHost is what unwrapURL only half achieved. Stripping
// the *url.Error removed one copy of the address; the cause underneath still
// spelled it out, and that message is served in /api/overview to a browser, on
// a service that has no authentication.
//
// Each case proves the leak exists before proving it is closed: Unsanitized
// must still carry the host, or the test would pass against an error that
// never named anything.
func TestTransportErrorNamesNoHost(t *testing.T) {
	t.Run("connection refused", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		addr := srv.URL
		srv.Close() // nothing listens there any more
		host, port := hostAndPort(t, addr)

		c := newTestClient(t, addr)
		_, err := c.ClusterStatus(context.Background())
		if err == nil {
			t.Fatal("want an error against a closed port")
		}

		if !strings.Contains(Unsanitized(err), host) {
			t.Fatalf("the cause never named the host: this test would prove nothing (%s)", Unsanitized(err))
		}
		assertNoHost(t, err, host, port)
		if got := kindOf(t, err); got != KindNetwork {
			t.Errorf("kind = %q, want %q", got, KindNetwork)
		}
	})

	t.Run("dns failure", func(t *testing.T) {
		const host = "no-such-host.moxy.invalid"

		c := newTestClient(t, "https://"+host+":8006")
		_, err := c.ClusterStatus(context.Background())
		if err == nil {
			t.Fatal("want an error against a name that cannot resolve")
		}

		var dnsErr *net.DNSError
		if !errors.As(err, &dnsErr) {
			t.Skipf("the resolver did not answer with a DNS error: %v", err)
		}
		if !strings.Contains(Unsanitized(err), host) {
			t.Fatalf("the cause never named the host: this test would prove nothing (%s)", Unsanitized(err))
		}
		assertNoHost(t, err, host, "8006")
		// The resolver's own address is the second thing a DNS error spells
		// out, and it describes the operator's network, not the cluster.
		if server := dnsErr.Server; server != "" && strings.Contains(err.Error(), server) {
			t.Errorf("the message names the resolver %q: %s", server, err)
		}
		if got := kindOf(t, err); got != KindNetwork && got != KindTimeout {
			t.Errorf("kind = %q, want %q or %q", got, KindNetwork, KindTimeout)
		}
	})

	t.Run("untrusted certificate", func(t *testing.T) {
		srv := fixtureServer(t, map[string]string{"/cluster/status": "cluster_status.json"})
		host, port := hostAndPort(t, srv.URL)

		// An empty pool trusts nothing, so the server's certificate is signed
		// by an unknown authority.
		cl := testCluster(srv.URL)
		cl.TLS = config.TLS{Mode: config.TLSModePinned, CAFile: "pve-root-ca.pem", Pool: x509.NewCertPool()}
		c, err := New(cl)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		_, err = c.ClusterStatus(context.Background())
		if err == nil {
			t.Fatal("want an error against an untrusted certificate")
		}
		assertNoHost(t, err, host, port)
		if got := kindOf(t, err); got != KindTLS {
			t.Errorf("kind = %q, want %q", got, KindTLS)
		}
	})
}

// assertNoHost checks the message an API response would carry.
func assertNoHost(t *testing.T, err error, host, port string) {
	t.Helper()
	msg := err.Error()
	if strings.Contains(msg, host) {
		t.Errorf("the message names the host %q: %s", host, msg)
	}
	// A bare port number is short enough to appear by coincidence, so it is
	// only reported together with its colon, the way an address is written.
	if strings.Contains(msg, ":"+port) {
		t.Errorf("the message carries the port %q: %s", port, msg)
	}
}

// TestSanitizeLeavesOtherErrorsAlone guards against over-reach: an error this
// package builds itself, or one that comes from decoding a body, names no host
// and must keep its own message.
func TestSanitizeLeavesOtherErrorsAlone(t *testing.T) {
	for _, err := range []error{errNilRequest, errFlexDecode} {
		if got := sanitize(err); got != err {
			t.Errorf("sanitize(%v) = %v, want the error unchanged", err, got)
		}
	}
	if sanitize(nil) != nil {
		t.Error("sanitize(nil) is not nil")
	}
}

// TestUnsanitizedIsTheFullCause is the other half of the deal: the detail that
// left the API response has to be somewhere, and that somewhere is the log.
func TestUnsanitizedIsTheFullCause(t *testing.T) {
	cause := &net.OpError{
		Op:   "dial",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 36305},
		Err:  errNilRequest,
	}
	wrapped := sanitize(cause)

	if strings.Contains(wrapped.Error(), "127.0.0.1") {
		t.Errorf("the sanitised message carries the address: %s", wrapped)
	}
	if !strings.Contains(Unsanitized(wrapped), "127.0.0.1") {
		t.Errorf("Unsanitized dropped the address the log needs: %s", Unsanitized(wrapped))
	}
	if Unsanitized(nil) != "" {
		t.Error("Unsanitized(nil) is not empty")
	}
}
