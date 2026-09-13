package proxmox

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/config"
)

// The token of the test cluster. The secret is a made-up UUID, and every
// non-leak assertion in this file looks for these two exact strings.
const (
	testTokenID = "moxy@pve!ro"
	testSecret  = "0f3c1c1e-4a2b-4c6d-9e8f-1a2b3c4d5e6f"
	testAuth    = "PVEAPIToken=" + testTokenID + "=" + testSecret
)

// testCluster is the configuration of a cluster served by httptest. TLS
// verification is relaxed because the httptest certificate is signed by an
// in-memory authority; the pinned and system paths get their own tests.
func testCluster(urls ...string) config.Cluster {
	return config.Cluster{
		ID:             "preproduction",
		Name:           "Preproduction",
		URLs:           urls,
		TokenID:        testTokenID,
		Secret:         config.NewSecret(testSecret),
		TLS:            config.TLS{Mode: config.TLSModeInsecure},
		RequestTimeout: 2 * time.Second,
	}
}

func newTestClient(t *testing.T, urls ...string) *Client {
	t.Helper()
	c, err := New(testCluster(urls...))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// fixtureServer serves the testdata files under their API paths.
func fixtureServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, file := range routes {
		body := fixtureBytes(t, file)
		mux.HandleFunc(apiPrefix+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		})
	}
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// statusServer answers every request with a fixed status and counts the
// requests it received.
func statusServer(t *testing.T, status int) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"errors":{"detail":"server side detail"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func kindOf(t *testing.T, err error) Kind {
	t.Helper()
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	kind, ok := KindOf(err)
	if !ok {
		t.Fatalf("want a *proxmox.Error, got %T: %v", err, err)
	}
	return kind
}

func TestClientDecodesClusterResources(t *testing.T) {
	srv := fixtureServer(t, map[string]string{"/cluster/resources": "cluster_resources.json"})
	c := newTestClient(t, srv.URL)

	resources, err := c.ClusterResources(context.Background())
	if err != nil {
		t.Fatalf("ClusterResources: %v", err)
	}
	if len(resources) != 17 {
		t.Fatalf("len(resources) = %d, want 17", len(resources))
	}

	byID := make(map[string]Resource, len(resources))
	for _, r := range resources {
		byID[r.ID] = r
	}

	// A node whose numbers come as JSON numbers.
	node := byID["node/prox-pprd-2301-cit"]
	if node.Type != ResourceTypeNode || node.Status != StatusOnline {
		t.Errorf("node 2301: type = %q, status = %q", node.Type, node.Status)
	}
	if got := node.CPU.Float(); got != 0.42 {
		t.Errorf("node 2301 cpu = %v, want 0.42 (a fraction, not a percentage)", got)
	}
	if got := node.MaxMem.Int(); got != 103079215104 {
		t.Errorf("node 2301 maxmem = %d bytes, want 103079215104", got)
	}

	// A guest whose numbers come as JSON strings: the Flex* types must make
	// the two forms indistinguishable to the caller.
	vm := byID["qemu/102"]
	if got := vm.VMID.Int(); got != 102 {
		t.Errorf(`qemu/102 vmid = %d, want 102 (serialised as "102")`, got)
	}
	if got := vm.MaxCPU.Int(); got != 8 {
		t.Errorf(`qemu/102 maxcpu = %d, want 8 (serialised as "8")`, got)
	}
	if !vm.IsGuest() {
		t.Error("qemu/102 should be a guest")
	}
	if got := vm.TagList(); len(got) != 2 || got[0] != "pprd" || got[1] != "db" {
		t.Errorf("qemu/102 tags = %v, want [pprd db]", got)
	}

	if tpl := byID["qemu/9000"]; !tpl.Template.Bool() {
		t.Error("qemu/9000 should be a template (serialised as 1)")
	}

	// A shared storage, reported once per node: same de-duplication key.
	for _, id := range []string{
		"storage/prox-pprd-2301-cit/nfs-shared",
		"storage/prox-pprd-2302-cit/nfs-shared",
	} {
		st := byID[id]
		if !st.Shared.Bool() {
			t.Errorf("%s should be shared", id)
		}
		if got := st.StorageKey(); got != "nfs-shared" {
			t.Errorf("%s key = %q, want %q", id, got, "nfs-shared")
		}
		if got := st.MaxDisk.Int(); got != 8796093022208 {
			t.Errorf("%s maxdisk = %d, want 8796093022208", id, got)
		}
	}
	local := byID["storage/prox-pprd-2302-cit/local-lvm"]
	if local.Shared.Bool() {
		t.Error(`local-lvm should not be shared (serialised as "0")`)
	}
	if got, want := local.StorageKey(), "prox-pprd-2302-cit/local-lvm"; got != want {
		t.Errorf("local-lvm key = %q, want %q", got, want)
	}
}

func TestClientDecodesClusterStatus(t *testing.T) {
	srv := fixtureServer(t, map[string]string{"/cluster/status": "cluster_status.json"})
	c := newTestClient(t, srv.URL)

	entries, err := c.ClusterStatus(context.Background())
	if err != nil {
		t.Fatalf("ClusterStatus: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("len(entries) = %d, want 4", len(entries))
	}

	var cluster, last ClusterStatusEntry
	for _, e := range entries {
		if e.Type == ClusterStatusTypeCluster {
			cluster = e
		}
		if e.Name == "prox-pprd-2303-cit" {
			last = e
		}
	}
	if cluster.Name != "preproduction" {
		t.Errorf("cluster name = %q, want %q", cluster.Name, "preproduction")
	}
	if !cluster.Quorate.Bool() {
		t.Error("cluster should be quorate (serialised as 1)")
	}
	if got := cluster.Nodes.Int(); got != 3 {
		t.Errorf("cluster nodes = %d, want 3", got)
	}
	if !last.Online.Bool() {
		t.Error(`node 2303 should be online (serialised as "1")`)
	}
	if got := last.NodeID.Int(); got != 3 {
		t.Errorf(`node 2303 nodeid = %d, want 3 (serialised as "3")`, got)
	}
}

func TestClientDecodesHAManagerStatus(t *testing.T) {
	srv := fixtureServer(t, map[string]string{"/cluster/ha/status/manager_status": "ha_manager_status.json"})
	c := newTestClient(t, srv.URL)

	status, err := c.HAManagerStatus(context.Background())
	if err != nil {
		t.Fatalf("HAManagerStatus: %v", err)
	}
	if status == nil {
		t.Fatal("HAManagerStatus returned nil without an error")
	}
	// The one structured source of "this node is in maintenance".
	if got := status.NodeState("prox-pprd-2302-cit"); got != HANodeMaintenance {
		t.Errorf("node 2302 state = %q, want %q", got, HANodeMaintenance)
	}
	if got := status.NodeState("prox-pprd-2301-cit"); got != HANodeOnline {
		t.Errorf("node 2301 state = %q, want %q", got, HANodeOnline)
	}
	if got := status.NodeState("prox-pprd-9999-cit"); got != HANodeUnknown {
		t.Errorf("unknown node state = %q, want %q", got, HANodeUnknown)
	}
	if status.MasterNode != "prox-pprd-2301-cit" {
		t.Errorf("master node = %q, want %q", status.MasterNode, "prox-pprd-2301-cit")
	}
	if got := status.Timestamp.Int(); got != 1757671200 {
		t.Errorf("timestamp = %d, want 1757671200", got)
	}
}

func TestClientDecodesAptUpdates(t *testing.T) {
	srv := fixtureServer(t, map[string]string{"/nodes/prox-pprd-2301-cit/apt/update": "apt_update.json"})
	c := newTestClient(t, srv.URL)

	updates, err := c.AptUpdates(context.Background(), "prox-pprd-2301-cit")
	if err != nil {
		t.Fatalf("AptUpdates: %v", err)
	}
	if len(updates) != 4 {
		t.Fatalf("len(updates) = %d, want 4", len(updates))
	}
	version, ok := PVEManagerVersion(updates)
	if !ok {
		t.Fatal("pve-manager should be among the pending updates")
	}
	if version != "9.2.12" {
		t.Errorf("pve-manager version = %q, want %q", version, "9.2.12")
	}
	if updates[0].OldVersion != "9.2.9" {
		t.Errorf("pve-manager old version = %q, want %q", updates[0].OldVersion, "9.2.9")
	}
}

func TestAptUpdatesEscapesNodeName(t *testing.T) {
	var path string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)

	if _, err := c.AptUpdates(context.Background(), "weird/node name"); err != nil {
		t.Fatalf("AptUpdates: %v", err)
	}
	want := apiPrefix + "/nodes/weird%2Fnode%20name/apt/update"
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}

	if _, err := c.AptUpdates(context.Background(), ""); err == nil {
		t.Error("an empty node name should be rejected")
	}
}

func TestAuthorizationHeader(t *testing.T) {
	var got string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)

	if _, err := c.ClusterStatus(context.Background()); err != nil {
		t.Fatalf("ClusterStatus: %v", err)
	}
	if got != testAuth {
		t.Errorf("Authorization = %q, want %q", got, testAuth)
	}
}

// TestTransportDoesNotMutateRequest checks the RoundTripper contract: the
// request handed to it must come back untouched, or an Authorization header
// ends up wherever that request is used next.
func TestTransportDoesNotMutateRequest(t *testing.T) {
	var seen string
	inner := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})
	tr := &authTransport{Base: inner, TokenID: testTokenID, Secret: config.NewSecret(testSecret)}

	req, err := http.NewRequest(http.MethodGet, "https://node.invalid/api2/json/cluster/status", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()

	if seen != testAuth {
		t.Errorf("the clone should carry the token, got %q", seen)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("the original request was mutated: Authorization = %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestFailoverOnUnreachableURL: the first node is not listening at all, the
// second serves the call.
func TestFailoverOnUnreachableURL(t *testing.T) {
	dead := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	alive := fixtureServer(t, map[string]string{"/cluster/status": "cluster_status.json"})
	c := newTestClient(t, deadURL, alive.URL)

	entries, err := c.ClusterStatus(context.Background())
	if err != nil {
		t.Fatalf("ClusterStatus: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("len(entries) = %d, want 4", len(entries))
	}
}

// TestFailoverOn5xxIsSticky: a 5xx moves the call to the next node, and the
// index STAYS there — the sick node is not solicited again on the next round.
func TestFailoverOnGatewayStatusIsSticky(t *testing.T) {
	broken, brokenHits := statusServer(t, http.StatusServiceUnavailable)
	good := fixtureServer(t, map[string]string{"/cluster/status": "cluster_status.json"})
	c := newTestClient(t, broken.URL, good.URL)

	for round := 1; round <= 3; round++ {
		if _, err := c.ClusterStatus(context.Background()); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}
	if got := atomic.LoadInt32(brokenHits); got != 1 {
		t.Errorf("the broken node was asked %d times, want exactly 1: the index is not sticky", got)
	}
}

// TestApplicationErrorIsNotReplayed: PVE answers 500 for its own errors — no
// guest agent, cluster not ready — and 501 for an endpoint that does not
// exist. Those come from the cluster, so every node relays the same answer and
// replaying costs one authenticated request per configured url for nothing.
// GuestIPv4 is the call that made this expensive: it is expected to fail on
// every VM without an agent, on every cache miss.
func TestApplicationErrorIsNotReplayed(t *testing.T) {
	for _, status := range []int{
		http.StatusInternalServerError,
		http.StatusNotImplemented,
		595, // the entry node cannot reach the target node
		596,
	} {
		first, firstHits := statusServer(t, status)
		second, secondHits := statusServer(t, status)
		c := newTestClient(t, first.URL, second.URL)

		if _, err := c.GuestIPv4(context.Background(), "prox-qual-2201-cit", 103); err == nil {
			t.Fatalf("status %d: want an error", status)
		}
		if got := atomic.LoadInt32(firstHits); got != 1 {
			t.Errorf("status %d: first node hits = %d, want 1", status, got)
		}
		if got := atomic.LoadInt32(secondHits); got != 0 {
			t.Errorf("status %d: second node hits = %d, want 0: the answer would be identical", status, got)
		}
	}
}

// TestNoFailoverOnAuthError: a 401 is identical on every node of a cluster, so
// the second node must receive nothing at all.
func TestNoFailoverOnAuthError(t *testing.T) {
	denying, denyingHits := statusServer(t, http.StatusUnauthorized)
	second, secondHits := statusServer(t, http.StatusOK)
	c := newTestClient(t, denying.URL, second.URL)

	_, err := c.ClusterStatus(context.Background())
	if got := kindOf(t, err); got != KindAuth {
		t.Errorf("kind = %q, want %q", got, KindAuth)
	}
	if got := atomic.LoadInt32(denyingHits); got != 1 {
		t.Errorf("first node hits = %d, want 1", got)
	}
	if got := atomic.LoadInt32(secondHits); got != 0 {
		t.Errorf("second node hits = %d, want 0: a 401 must not fail over", got)
	}
}

func TestErrorKindsAndFields(t *testing.T) {
	t.Run("auth", func(t *testing.T) {
		srv, _ := statusServer(t, http.StatusForbidden)
		c := newTestClient(t, srv.URL)
		_, err := c.AptUpdates(context.Background(), "prox-pprd-2301-cit")
		if got := kindOf(t, err); got != KindAuth {
			t.Fatalf("kind = %q, want %q", got, KindAuth)
		}
		var e *Error
		if !errors.As(err, &e) {
			t.Fatal("want a *proxmox.Error")
		}
		if e.Cluster != "preproduction" {
			t.Errorf("cluster = %q, want %q", e.Cluster, "preproduction")
		}
		if e.Path != "/nodes/prox-pprd-2301-cit/apt/update" {
			t.Errorf("path = %q, want the relative api path", e.Path)
		}
		if e.Status != http.StatusForbidden {
			t.Errorf("status = %d, want %d", e.Status, http.StatusForbidden)
		}
		// The path, not the URL: an error ends up in /api/overview.
		if strings.Contains(err.Error(), srv.URL) {
			t.Errorf("the error carries the node url: %v", err)
		}
	})

	t.Run("protocol on every url", func(t *testing.T) {
		// The first node answers as a broken gateway, which is a property of
		// that node and does earn a second try; the second answers 500, which
		// is the cluster speaking and ends the sequence.
		first, firstHits := statusServer(t, http.StatusBadGateway)
		second, secondHits := statusServer(t, http.StatusInternalServerError)
		c := newTestClient(t, first.URL, second.URL)

		_, err := c.ClusterResources(context.Background())
		if got := kindOf(t, err); got != KindProtocol {
			t.Fatalf("kind = %q, want %q", got, KindProtocol)
		}
		if got := atomic.LoadInt32(firstHits); got != 1 {
			t.Errorf("first node hits = %d, want 1", got)
		}
		if got := atomic.LoadInt32(secondHits); got != 1 {
			t.Errorf("second node hits = %d, want 1", got)
		}
		// The error is the one of the LAST attempt, told how many urls it
		// went through.
		if !strings.Contains(err.Error(), "tried 2 of 2 urls") {
			t.Errorf("the error does not say how many urls were tried: %v", err)
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("the error is not the last attempt's: %v", err)
		}
		if strings.Contains(err.Error(), "server side detail") {
			t.Errorf("the error carries the response body: %v", err)
		}
	})

	t.Run("tls untrusted in system mode", func(t *testing.T) {
		srv := fixtureServer(t, map[string]string{"/cluster/status": "cluster_status.json"})
		cl := testCluster(srv.URL)
		cl.TLS = config.TLS{Mode: config.TLSModeSystem}
		c, err := New(cl)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_, err = c.ClusterStatus(context.Background())
		if got := kindOf(t, err); got != KindTLS {
			t.Fatalf("kind = %q, want %q (err: %v)", got, KindTLS, err)
		}
	})

	t.Run("timeout on the per-attempt budget", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		t.Cleanup(srv.Close)
		cl := testCluster(srv.URL)
		cl.RequestTimeout = 50 * time.Millisecond
		c, err := New(cl)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_, err = c.ClusterStatus(context.Background())
		if got := kindOf(t, err); got != KindTimeout {
			t.Fatalf("kind = %q, want %q (err: %v)", got, KindTimeout, err)
		}
	})

	t.Run("cancelled caller context", func(t *testing.T) {
		started := make(chan struct{})
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
		}))
		t.Cleanup(srv.Close)
		c := newTestClient(t, srv.URL)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			<-started
			cancel()
		}()
		defer cancel()

		_, err := c.ClusterStatus(ctx)
		if got := kindOf(t, err); got != KindTimeout {
			t.Fatalf("kind = %q, want %q (err: %v)", got, KindTimeout, err)
		}
	})

	t.Run("undecodable body", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"data": "not a list"}`)
		}))
		t.Cleanup(srv.Close)
		c := newTestClient(t, srv.URL)

		_, err := c.ClusterResources(context.Background())
		if got := kindOf(t, err); got != KindProtocol {
			t.Fatalf("kind = %q, want %q (err: %v)", got, KindProtocol, err)
		}
	})
}

// TestCancelledContextMakesNoRequest: the budget is checked before the socket
// is opened, so an aggregator whose round is already over costs the cluster
// nothing.
func TestCancelledContextMakesNoRequest(t *testing.T) {
	srv, hits := statusServer(t, http.StatusOK)
	c := newTestClient(t, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.ClusterStatus(ctx)
	if got := kindOf(t, err); got != KindTimeout {
		t.Fatalf("kind = %q, want %q (err: %v)", got, KindTimeout, err)
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Errorf("hits = %d, want 0: a dead context must not reach the network", got)
	}
}

// TestBodyIsBounded: a node that answers with an endless body must not be able
// to exhaust the daemon's memory. The read stops at the bound of the endpoint
// and says so, rather than handing truncated JSON to the decoder.
func TestBodyIsBounded(t *testing.T) {
	const oversize = 2 * maxListBytes
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":"`)
		chunk := strings.Repeat("a", 64*1024)
		for written := 0; written < oversize; written += len(chunk) {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, `"}`)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)

	resources, err := c.ClusterResources(context.Background())
	if err == nil {
		t.Fatal("want an error on a body larger than the limit")
	}
	if resources != nil {
		t.Errorf("resources = %v, want nil", resources)
	}
	if got := kindOf(t, err); got != KindProtocol {
		t.Fatalf("kind = %q, want %q (err: %v)", got, KindProtocol, err)
	}
	if len(err.Error()) > 1024 {
		t.Errorf("the error message is %d bytes long, it carries the body", len(err.Error()))
	}
	// The message must name the bound, not a broken document: the cluster is
	// bigger than expected, it is not speaking malformed JSON.
	if !errors.Is(err, errBodyTooLarge) {
		t.Errorf("the error is not errBodyTooLarge: %v", err)
	}
	if strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Errorf("the oversized body was truncated and decoded: %v", err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(maxListBytes)) {
		t.Errorf("the error does not name the bound: %v", err)
	}
}

// TestLargeClusterResourcesDecode: the bound on the listings is what the whole
// split is for. Five thousand guests is a cluster PVE supports and moxy claims
// to aggregate; under the old single megabyte it came back as broken JSON.
func TestLargeClusterResourcesDecode(t *testing.T) {
	const guests = 5000
	var b strings.Builder
	b.WriteString(`{"data":[`)
	for i := 0; i < guests; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"type":"qemu","id":"qemu/%d","node":"prox-prod-2401-cit",`+
			`"name":"sli-service-role-2601-prd-%d.intranet.opt","status":"running",`+
			`"vmid":%d,"cpu":0.0125,"maxcpu":4,"mem":2147483648,"maxmem":4294967296,`+
			`"disk":0,"maxdisk":34359738368,"uptime":3542400,`+
			`"tags":"env.production;backup.none;date.20260907;from.template-rocky10"}`,
			100+i, 100+i, 100+i)
	}
	b.WriteString(`]}`)
	if b.Len() <= maxObjectBytes {
		t.Fatalf("the fixture is %d bytes, it no longer exercises the old bound", b.Len())
	}
	payload := b.String()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, payload)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)

	resources, err := c.ClusterResources(context.Background())
	if err != nil {
		t.Fatalf("ClusterResources: %v", err)
	}
	if len(resources) != guests {
		t.Fatalf("decoded %d resources, want %d", len(resources), guests)
	}
}

// TestBodyOnTheBoundIsAccepted: the refusal starts one byte past the bound, so
// a body sitting exactly on it still decodes.
func TestBodyOnTheBoundIsAccepted(t *testing.T) {
	// {"data":["aaa…"]} padded so the whole document is exactly maxListBytes.
	const envelope = len(`{"data":[""]}`)
	payload := `{"data":["` + strings.Repeat("a", maxListBytes-envelope) + `"]}`
	if len(payload) != maxListBytes {
		t.Fatalf("the fixture is %d bytes, want exactly %d", len(payload), maxListBytes)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, payload)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)

	if _, err := c.ClusterTasks(context.Background()); err == nil {
		t.Fatal("want a decode error: the payload is a string list, not tasks")
	} else if errors.Is(err, errBodyTooLarge) {
		t.Errorf("a body exactly on the bound was refused: %v", err)
	}
}

// TestErrorNeverLeaksTheToken is the most important test of this package.
//
// It puts a hostile-but-plausible node in front of the client: a 500 that
// echoes the Authorization header it received, both in a response header and
// in the body — exactly what a misconfigured reverse proxy or a debug error
// page does. The test first proves the server really did leak, otherwise it
// would be asserting nothing at all, then proves none of it reaches the error
// the aggregator serialises into /api/overview.
func TestErrorNeverLeaksTheToken(t *testing.T) {
	var (
		mu       sync.Mutex
		received string
	)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		mu.Lock()
		received = auth
		mu.Unlock()
		w.Header().Set("X-Echoed-Authorization", auth)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "internal error while authenticating "+auth)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)

	_, err := c.ClusterStatus(context.Background())
	if err == nil {
		t.Fatal("want an error on a 500")
	}

	mu.Lock()
	leaked := received
	mu.Unlock()
	if leaked != testAuth {
		t.Fatalf("the server did not receive the token (%q): this test would prove nothing", leaked)
	}

	msg := err.Error()
	if strings.Contains(msg, testSecret) {
		t.Errorf("the error carries the token SECRET: %s", msg)
	}
	if strings.Contains(msg, testTokenID) {
		t.Errorf("the error carries the token id: %s", msg)
	}
	if strings.Contains(msg, "PVEAPIToken") {
		t.Errorf("the error carries the authorization header: %s", msg)
	}
	if strings.Contains(msg, "internal error while authenticating") {
		t.Errorf("the error carries the response body: %s", msg)
	}
	if got := kindOf(t, err); got != KindProtocol {
		t.Errorf("kind = %q, want %q", got, KindProtocol)
	}

	// Printing the client itself must not reveal the secret either: it lives
	// in the transport, behind the http.Client pointer, which fmt does not
	// follow.
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		printed := fmt.Sprintf(format, c)
		if strings.Contains(printed, testSecret) {
			t.Errorf("fmt.Sprintf(%q, client) reveals the secret: %s", format, printed)
		}
	}
}

// TestPinnedTLSMode exercises the real pinned path: the certificate of the
// test server is the whole trust anchor, so a client pinned to it succeeds and
// a client pinned to an empty pool fails as a TLS problem.
func TestPinnedTLSMode(t *testing.T) {
	srv := fixtureServer(t, map[string]string{"/cluster/status": "cluster_status.json"})

	t.Run("pinned to the server certificate", func(t *testing.T) {
		pool := x509.NewCertPool()
		pool.AddCert(srv.Certificate())
		cl := testCluster(srv.URL)
		cl.TLS = config.TLS{Mode: config.TLSModePinned, CAFile: "pve-root-ca.pem", Pool: pool}
		c, err := New(cl)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		entries, err := c.ClusterStatus(context.Background())
		if err != nil {
			t.Fatalf("ClusterStatus: %v", err)
		}
		if len(entries) != 4 {
			t.Errorf("len(entries) = %d, want 4", len(entries))
		}
	})

	t.Run("pinned to another authority", func(t *testing.T) {
		cl := testCluster(srv.URL)
		cl.TLS = config.TLS{Mode: config.TLSModePinned, CAFile: "other-ca.pem", Pool: x509.NewCertPool()}
		c, err := New(cl)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_, err = c.ClusterStatus(context.Background())
		if got := kindOf(t, err); got != KindTLS {
			t.Fatalf("kind = %q, want %q (err: %v)", got, KindTLS, err)
		}
	})

	t.Run("pinned without a pool is a configuration error", func(t *testing.T) {
		cl := testCluster(srv.URL)
		cl.TLS = config.TLS{Mode: config.TLSModePinned, CAFile: "pve-root-ca.pem"}
		if _, err := New(cl); err == nil {
			t.Error("want an error when the pinned pool is missing")
		}
	})
}

func TestNewValidatesCluster(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Cluster)
	}{
		{"no id", func(c *config.Cluster) { c.ID = "" }},
		{"no url", func(c *config.Cluster) { c.URLs = nil }},
		{"empty url", func(c *config.Cluster) { c.URLs = []string{"  "} }},
		{"no token id", func(c *config.Cluster) { c.TokenID = "" }},
		{"no secret", func(c *config.Cluster) { c.Secret = config.Secret{} }},
		{"unknown tls mode", func(c *config.Cluster) { c.TLS.Mode = "whatever" }},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cl := testCluster("https://node.invalid:8006")
			tt.mutate(&cl)
			if _, err := New(cl); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestNewDefaultsAndClusterID(t *testing.T) {
	cl := testCluster("https://node.invalid:8006/")
	cl.RequestTimeout = 0
	c, err := New(cl)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.ClusterID(); got != "preproduction" {
		t.Errorf("ClusterID = %q, want %q", got, "preproduction")
	}
	if c.timeout != config.DefaultTimeout {
		t.Errorf("timeout = %v, want the default %v", c.timeout, config.DefaultTimeout)
	}
	// The trailing slash must be gone, or the url would carry "//api2/json".
	if c.urls[0] != "https://node.invalid:8006" {
		t.Errorf("url = %q, want it normalised without a trailing slash", c.urls[0])
	}
	// No Timeout on the http.Client: the budget comes from the context.
	if c.http.Timeout != 0 {
		t.Errorf("http.Client.Timeout = %v, want 0", c.http.Timeout)
	}
}

// baseTransport reaches the *http.Transport underneath the auth transport,
// which is where the proxy policy of a cluster ends up.
func baseTransport(t *testing.T, c *Client) *http.Transport {
	t.Helper()
	auth, ok := c.http.Transport.(*authTransport)
	if !ok {
		t.Fatalf("Transport = %T, want *authTransport", c.http.Transport)
	}
	base, ok := auth.Base.(*http.Transport)
	if !ok {
		t.Fatalf("authTransport.Base = %T, want *http.Transport", auth.Base)
	}
	return base
}

// TestNewIgnoresTheEnvironmentProxy is the regression test of the rule: a
// cluster without a proxy connects directly, whatever HTTPS_PROXY says.
//
// The assertion is on the field rather than on a request through a server with
// HTTPS_PROXY set: http.ProxyFromEnvironment reads the environment once, under
// a sync.Once, so an environment-based test would quietly stop proving anything
// as soon as something else in the process had triggered that read first.
func TestNewIgnoresTheEnvironmentProxy(t *testing.T) {
	c := newTestClient(t, "https://node.invalid:8006")
	if got := baseTransport(t, c).Proxy; got != nil {
		t.Error("Transport.Proxy is set, want nil so that nodes are reached directly")
	}
}

func TestNewUsesTheConfiguredProxy(t *testing.T) {
	want := "http://proxy.invalid:3128"
	cl := testCluster("https://node.invalid:8006")
	cl.Proxy = want
	u, err := url.Parse(want)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cl.ProxyURL = u

	c, err := New(cl)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	proxy := baseTransport(t, c).Proxy
	if proxy == nil {
		t.Fatal("Transport.Proxy is nil, want the configured proxy")
	}
	req, err := http.NewRequest(http.MethodGet, "https://node.invalid:8006/api2/json/cluster/status", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	got, err := proxy(req)
	if err != nil {
		t.Fatalf("Proxy: %v", err)
	}
	if got == nil || got.String() != want {
		t.Errorf("Proxy(req) = %v, want %q", got, want)
	}
}

// TestNewRejectsAnUnresolvedProxy guards the hand-built Cluster: a proxy
// written in the file but never parsed must fail loudly rather than be dropped.
func TestNewRejectsAnUnresolvedProxy(t *testing.T) {
	cl := testCluster("https://node.invalid:8006")
	cl.Proxy = "http://proxy.invalid:3128"
	if _, err := New(cl); err == nil {
		t.Error("want an error when proxy is set but ProxyURL is nil")
	}
}
