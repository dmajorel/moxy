package proxmox

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeNetError is a net.Error that reports a timeout without involving a real
// socket. It is what a dialer returns when the deadline is reached.
type fakeNetError struct{ timeout bool }

func (e fakeNetError) Error() string   { return "fake network error" }
func (e fakeNetError) Timeout() bool   { return e.timeout }
func (e fakeNetError) Temporary() bool { return false }

func syntaxError(t *testing.T) error {
	t.Helper()
	var v map[string]interface{}
	err := json.Unmarshal([]byte(`{"data":`), &v)
	if err == nil {
		t.Fatal("expected a syntax error from the truncated payload")
	}
	return err
}

func TestClassifyKind(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
		want   Kind
	}{
		{"unauthorized", http.StatusUnauthorized, nil, KindAuth},
		{"forbidden", http.StatusForbidden, nil, KindAuth},
		{"forbidden on apt update", http.StatusForbidden, errors.New("unexpected status"), KindAuth},
		{"context deadline", 0, context.DeadlineExceeded, KindTimeout},
		{"wrapped context deadline", 0,
			&url.Error{Op: "Get", URL: "https://node:8006", Err: context.DeadlineExceeded}, KindTimeout},
		{"os deadline", 0, os.ErrDeadlineExceeded, KindTimeout},
		{"net error timeout", 0, fakeNetError{timeout: true}, KindTimeout},
		{"wrapped net error timeout", 0,
			&url.Error{Op: "Get", URL: "https://node:8006", Err: fakeNetError{timeout: true}}, KindTimeout},
		{"unknown authority", 0, x509.UnknownAuthorityError{}, KindTLS},
		{"wrapped unknown authority", 0,
			&url.Error{Op: "Get", URL: "https://node:8006", Err: x509.UnknownAuthorityError{}}, KindTLS},
		{"hostname mismatch", 0,
			x509.HostnameError{Certificate: &x509.Certificate{}, Host: "prox-pprd-2301-cit"}, KindTLS},
		{"certificate expired", 0,
			x509.CertificateInvalidError{Reason: x509.Expired, Detail: "expired"}, KindTLS},
		{"system roots", 0, x509.SystemRootsError{}, KindTLS},
		{"internal server error", http.StatusInternalServerError, nil, KindProtocol},
		{"bad gateway", http.StatusBadGateway, errors.New("unexpected status"), KindProtocol},
		{"not found", http.StatusNotFound, nil, KindProtocol},
		{"bad request", http.StatusBadRequest, nil, KindProtocol},
		{"undecodable body", http.StatusOK, syntaxError(t), KindProtocol},
		{"truncated body", http.StatusOK, io.ErrUnexpectedEOF, KindProtocol},
		{"empty body", http.StatusOK, io.EOF, KindProtocol},
		{"tolerant type gave up", http.StatusOK, fmt.Errorf("decoding: %w", errFlexDecode), KindProtocol},
		{"connection refused", 0,
			&url.Error{Op: "Get", URL: "https://node:8006",
				Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}, KindNetwork},
		{"non timeout net error", 0, fakeNetError{}, KindNetwork},
		{"unknown cause", 0, errors.New("boom"), KindNetwork},
		{"nothing at all", 0, nil, KindNetwork},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify("preproduction", "/cluster/status", tc.status, tc.err)
			if got.Kind != tc.want {
				t.Errorf("Classify(%d, %v).Kind = %q, want %q", tc.status, tc.err, got.Kind, tc.want)
			}
			if got.Cluster != "preproduction" || got.Path != "/cluster/status" {
				t.Errorf("Classify lost its context: %+v", got)
			}
			if got.Status != tc.status {
				t.Errorf("Classify(%d).Status = %d, want %d", tc.status, got.Status, tc.status)
			}
		})
	}
}

// TestClassifyAuthWinsOverTransport pins the order of the tests: a 403 is
// authoritative even when the transport also produced a timeout-shaped error,
// because it must never trigger a failover to another node.
func TestClassifyAuthWinsOverTransport(t *testing.T) {
	err := Classify("production", "/nodes/n1/apt/update", http.StatusForbidden, context.DeadlineExceeded)
	if err.Kind != KindAuth {
		t.Errorf("Kind = %q, want %q", err.Kind, KindAuth)
	}
}

// TestClassifyTimeoutWinsOverTLS pins the other ordering decision: a handshake
// cut short by the budget is a timeout, not a certificate problem.
func TestClassifyTimeoutWinsOverTLS(t *testing.T) {
	cause := fmt.Errorf("tls: %w", context.DeadlineExceeded)
	if got := Classify("production", "/cluster/status", 0, cause); got.Kind != KindTimeout {
		t.Errorf("Kind = %q, want %q", got.Kind, KindTimeout)
	}
}

func TestErrorUnwrap(t *testing.T) {
	err := Classify("qualification", "/cluster/resources", 0, context.DeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("errors.Is(err, context.DeadlineExceeded) = false, want true")
	}

	var target *Error
	if !errors.As(error(err), &target) {
		t.Fatal("errors.As(err, **Error) = false, want true")
	}
	if target.Kind != KindTimeout {
		t.Errorf("Kind = %q, want %q", target.Kind, KindTimeout)
	}

	wrapped := fmt.Errorf("polling cluster: %w", err)
	kind, ok := KindOf(wrapped)
	if !ok || kind != KindTimeout {
		t.Errorf("KindOf(wrapped) = %q, %v; want %q, true", kind, ok, KindTimeout)
	}
	if _, ok := KindOf(errors.New("unrelated")); ok {
		t.Error("KindOf(unrelated) = true, want false")
	}
	if (&Error{}).Unwrap() != nil {
		t.Error("Unwrap of an error without a cause = non-nil, want nil")
	}
}

func TestErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "status and cause",
			err: &Error{Cluster: "preproduction", Path: "/cluster/status",
				Kind: KindProtocol, Status: 500, Err: errors.New("unexpected status")},
			want: "cluster preproduction: /cluster/status: http 500 Internal Server Error: unexpected status",
		},
		{
			name: "cause only",
			err: &Error{Cluster: "preproduction", Path: "/cluster/status",
				Kind: KindTimeout, Err: context.DeadlineExceeded},
			want: "cluster preproduction: /cluster/status: context deadline exceeded",
		},
		{
			name: "status only",
			err: &Error{Cluster: "production", Path: "/nodes/n1/apt/update",
				Kind: KindAuth, Status: 403},
			want: "cluster production: /nodes/n1/apt/update: http 403 Forbidden",
		},
		{
			name: "unknown status code",
			err:  &Error{Cluster: "qualification", Path: "/cluster/status", Status: 599},
			want: "cluster qualification: /cluster/status: http 599",
		},
		{
			name: "nothing to report",
			err:  &Error{},
			want: "proxmox: unknown error",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestErrorDoesNotLeakResponseContent is the reason this type exists.
//
// A cluster that answers 500 while echoing the Authorization header it
// received, both in a response header and in the body, must not get that
// header anywhere near the error message. The proof is structural: Classify is
// handed a status code and a transport error, never the response, so the only
// text that can reach the message is the cluster id, the path, the status and
// the cause.
func TestErrorDoesNotLeakResponseContent(t *testing.T) {
	const secret = "0f3c1c1e-secret-uuid-do-not-log"
	const tokenID = "moxy@pve!ro"
	header := "PVEAPIToken=" + tokenID + "=" + secret

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A hostile (or merely careless) cluster: it reflects the credential.
		got := r.Header.Get("Authorization")
		w.Header().Set("X-Echo-Authorization", got)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"errors":{"auth":%q},"message":"internal error"}`, got)
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api2/json/cluster/status", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Authorization", header)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("calling the test server: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}

	// Guard against a test that proves nothing: the secret really is in the
	// response, in both the header and the body.
	if !strings.Contains(string(body), secret) {
		t.Fatalf("the test server did not echo the credential, the test is void")
	}
	if !strings.Contains(resp.Header.Get("X-Echo-Authorization"), secret) {
		t.Fatalf("the test server did not echo the credential in a header, the test is void")
	}

	pveErr := Classify("preproduction", "/cluster/status", resp.StatusCode, errors.New("unexpected status"))
	msg := pveErr.Error()

	for _, forbidden := range []string{secret, header, tokenID, string(body),
		resp.Header.Get("X-Echo-Authorization"), srv.URL} {
		if strings.Contains(msg, forbidden) {
			t.Errorf("Error() leaked %q; message was %q", forbidden, msg)
		}
	}
	// And what it does say: cluster, path, status, cause, nothing else.
	want := "cluster preproduction: /cluster/status: http 500 Internal Server Error: unexpected status"
	if msg != want {
		t.Errorf("Error() = %q, want exactly %q", msg, want)
	}
	if pveErr.Kind != KindProtocol {
		t.Errorf("Kind = %q, want %q", pveErr.Kind, KindProtocol)
	}
}

// TestErrorFormattingDoesNotLeak covers the other way a secret escapes: a
// struct printed with %v or %+v. Error has no field holding a credential, so
// every verb is safe, and this test keeps it that way.
func TestErrorFormattingDoesNotLeak(t *testing.T) {
	const secret = "0f3c1c1e-secret-uuid-do-not-log"
	err := Classify("preproduction", "/cluster/status", http.StatusInternalServerError,
		errors.New("unexpected status"))
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if got := fmt.Sprintf(format, err); strings.Contains(got, secret) {
			t.Errorf("Sprintf(%q, err) leaked the secret: %s", format, got)
		}
	}
}
