package proxmox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// everyVerb is every formatting verb fmt knows, minus %T, which prints a type
// and not a value. %p is in the list on purpose: fmt answers it before it ever
// consults a Formatter, and its bad-verb path prints an argument by reflection
// with method calls disabled, which is how a value type can still leak.
var everyVerb = []string{
	"%v", "%+v", "%#v", "%s", "%q", "%x", "%X",
	"%d", "%t", "%f", "%e", "%g", "%c", "%U", "%b", "%o", "%p",
	"%8v", "%-8s", "%.3s",
}

// TestAuthTransportRedactedThroughEveryVerb covers the one struct in moxy that
// holds a secret next to the header it is destined for. config guarantees the
// redaction; this proves the guarantee survives being embedded here, which is
// where it actually matters.
func TestAuthTransportRedactedThroughEveryVerb(t *testing.T) {
	cl := testCluster("https://pve.invalid")
	tr := &authTransport{TokenID: cl.TokenID, Secret: cl.Secret}

	for _, format := range everyVerb {
		if out := fmt.Sprintf(format, tr); strings.Contains(out, testSecret) {
			t.Errorf("Sprintf(%q, *authTransport) leaked the secret: %s", format, out)
		}
		if out := fmt.Sprintf(format, *tr); strings.Contains(out, testSecret) {
			t.Errorf("Sprintf(%q, authTransport) leaked the secret: %s", format, out)
		}
	}
}

// recordingTransport stands in for http.Transport at the bottom of the chain:
// it hands back a response shaped the way a real one is, Request filled in
// with the request it was given, and keeps both that response and the header
// it saw so the test can look at them once the client is done.
type recordingTransport struct {
	last     *http.Response
	sentAuth string
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.sentAuth = req.Header.Get(authHeader)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
		Request:    req,
	}
	rt.last = resp
	return resp, nil
}

// TestResponseKeepsNoRequest closes the last path by which a request carrying
// the Authorization header can reach a log. attempt never reads resp.Request,
// but a response is exactly the kind of value someone prints whole while
// chasing a bug, and %+v on one would print the token.
//
// The recorder sits UNDER the authentication transport, so the request it is
// handed is the real one, header and all — the test first proves that, or it
// would be asserting nothing.
func TestResponseKeepsNoRequest(t *testing.T) {
	rec := &recordingTransport{}
	c := newTestClient(t, "https://pve.invalid")
	cl := testCluster("https://pve.invalid")
	c.http.Transport = &authTransport{Base: rec, TokenID: cl.TokenID, Secret: cl.Secret}

	if _, err := c.ClusterStatus(context.Background()); err != nil {
		t.Fatalf("ClusterStatus: %v", err)
	}
	if rec.last == nil {
		t.Fatal("the transport was never called")
	}
	if rec.sentAuth != testAuth {
		t.Fatalf("the request did not carry the token (%q): this test would prove nothing", rec.sentAuth)
	}

	if rec.last.Request != nil {
		t.Fatalf("the response still carries the request that held the token: %v", rec.last.Request.Header.Get(authHeader))
	}

	// And so printing the response whole reveals nothing.
	for _, format := range everyVerb {
		if out := fmt.Sprintf(format, rec.last); strings.Contains(out, testSecret) {
			t.Errorf("Sprintf(%q, response) leaked the secret: %s", format, out)
		}
	}
}
