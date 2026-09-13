package aggregate

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/config"
	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

// A returned card must share no mutable state with the poller: a caller that
// sorts or trims a slice it received would otherwise corrupt the next response.
func TestCloneSharesNothingMutable(t *testing.T) {
	version := "9.2.12"
	ratio := 0.83
	pending := 4
	fetchedAt := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	color := "#378ADD"

	cpu := CPU{Ratio: 0.31, Cores: 96}
	memory := Usage{Used: 100, Total: 200, Ratio: 0.5}
	original := &ClusterOverview{
		ID:        "preproduction",
		CPU:       &cpu,
		Memory:    &memory,
		Name:      "Préproduction",
		Color:     &color,
		Status:    StatusDegraded,
		FetchedAt: &fetchedAt,
		Error:     &Error{Kind: "timeout", Message: "deadline exceeded"},
		Quorum:    &Quorum{Quorate: true, Nodes: 3, Online: 3},
		Nodes: []Node{{
			Name:           "prox-pprd-2301-cit",
			Status:         NodeOnline,
			CPU:            &CPU{Ratio: 0.44, Cores: 32},
			Memory:         &Usage{Used: 50, Total: 100, Ratio: 0.5},
			PendingUpdates: &pending,
			Guests: []Guest{{
				VMID:   101,
				Name:   "sli-airflow-sep-exp-2601-ppr",
				Kind:   GuestQemu,
				Status: GuestRunning,
				Tags:   []string{"env.preproduction", "backup.none"},
			}},
		}},
		Updates: &Updates{Nodes: []string{"prox-pprd-2301-cit"}, PVEManagerVersion: &version},
		Alerts:  []Alert{{Kind: AlertMemoryHigh, Nodes: []string{"prox-pprd-2301-cit"}, Ratio: &ratio}},
	}

	clone := original.clone()

	// Mutate every slice and pointer reachable from the clone.
	clone.Nodes[0].Name = "mutated"
	clone.Nodes[0].Guests[0].Name = "mutated"
	clone.Nodes[0].Guests[0].Tags[0] = "mutated"
	*clone.Nodes[0].PendingUpdates = 99
	clone.Alerts[0].Nodes[0] = "mutated"
	*clone.Alerts[0].Ratio = 9.9
	clone.Updates.Nodes[0] = "mutated"
	*clone.Updates.PVEManagerVersion = "mutated"
	clone.Quorum.Nodes = 99
	*clone.Color = "mutated"
	clone.Error.Kind = "mutated"
	// Added with the "unknown is not zero" change, and missed by clone until
	// this test grew to reach them.
	clone.CPU.Ratio = 9.9
	clone.Memory.Used = 99
	clone.Nodes[0].CPU.Ratio = 9.9
	clone.Nodes[0].Memory.Used = 99

	if got := original.Nodes[0].Name; got != "prox-pprd-2301-cit" {
		t.Errorf("node name = %q, want unchanged", got)
	}
	if got := original.Nodes[0].Guests[0].Name; got != "sli-airflow-sep-exp-2601-ppr" {
		t.Errorf("guest name = %q, want unchanged", got)
	}
	if got := original.Nodes[0].Guests[0].Tags[0]; got != "env.preproduction" {
		t.Errorf("guest tag = %q, want unchanged", got)
	}
	if got := *original.Nodes[0].PendingUpdates; got != 4 {
		t.Errorf("pending updates = %d, want 4", got)
	}
	if got := original.CPU.Ratio; got != 0.31 {
		t.Errorf("cluster cpu ratio = %v, want 0.31", got)
	}
	if got := original.Memory.Used; got != 100 {
		t.Errorf("cluster memory used = %d, want 100", got)
	}
	if got := original.Nodes[0].CPU.Ratio; got != 0.44 {
		t.Errorf("node cpu ratio = %v, want 0.44", got)
	}
	if got := original.Nodes[0].Memory.Used; got != 50 {
		t.Errorf("node memory used = %d, want 50", got)
	}
	if got := original.Alerts[0].Nodes[0]; got != "prox-pprd-2301-cit" {
		t.Errorf("alert node = %q, want unchanged", got)
	}
	if got := *original.Alerts[0].Ratio; got != 0.83 {
		t.Errorf("alert ratio = %v, want 0.83", got)
	}
	if got := original.Updates.Nodes[0]; got != "prox-pprd-2301-cit" {
		t.Errorf("updates node = %q, want unchanged", got)
	}
	if got := *original.Updates.PVEManagerVersion; got != "9.2.12" {
		t.Errorf("pve-manager version = %q, want unchanged", got)
	}
	if got := original.Quorum.Nodes; got != 3 {
		t.Errorf("quorum nodes = %d, want 3", got)
	}
	if got := *original.Color; got != "#378ADD" {
		t.Errorf("color = %q, want unchanged", got)
	}
	if got := original.Error.Kind; got != "timeout" {
		t.Errorf("error kind = %q, want unchanged", got)
	}
}

// A node with no guest must still clone into an empty slice, never nil, so the
// payload keeps carrying an array.
func TestCloneKeepsEmptyGuestsNonNil(t *testing.T) {
	original := &ClusterOverview{Nodes: []Node{{Name: "n1", Guests: []Guest{}}}}

	clone := original.clone()

	if clone.Nodes[0].Guests == nil {
		t.Fatal("guests cloned to nil, want empty slice")
	}
	if len(clone.Nodes[0].Guests) != 0 {
		t.Errorf("guests = %d, want 0", len(clone.Nodes[0].Guests))
	}
}

// Until the first successful poll the cluster is still reported, with its
// identity and an explicit unreachable alert rather than an empty card.
func TestSnapshotBeforeFirstSuccess(t *testing.T) {
	state := &clusterState{
		identity: Identity{ID: "qualification", Name: "Qualification"},
		lastErr:  &Error{Kind: "network", Message: "no such host"},
	}

	card := state.snapshot(time.Now())

	if card.Status != StatusUnreachable {
		t.Errorf("status = %q, want %q", card.Status, StatusUnreachable)
	}
	if card.ID != "qualification" {
		t.Errorf("id = %q", card.ID)
	}
	if card.FetchedAt != nil {
		t.Error("fetchedAt should stay nil before any success")
	}
	if card.Nodes == nil || card.Alerts == nil {
		t.Fatal("nodes and alerts must be empty slices, not nil")
	}
	if len(card.Alerts) != 1 || card.Alerts[0].Kind != AlertUnreachable {
		t.Errorf("alerts = %+v, want a single unreachable alert", card.Alerts)
	}
}

// Past staleAfter the card is flagged unreachable, but the last known reading
// stays in the payload: stale data beats an empty card for an operator.
func TestSnapshotGoesStaleButKeepsData(t *testing.T) {
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	state := &clusterState{
		identity: Identity{ID: "qualification", Name: "Qualification"},
		card: &ClusterOverview{
			ID:     "qualification",
			Status: StatusHealthy,
			Nodes:  []Node{{Name: "prox-qual-2201-cit", Status: NodeOnline}},
			Alerts: []Alert{},
		},
		fetchedAt: now.Add(-staleAfter - time.Second),
		lastErr:   &Error{Kind: "timeout", Message: "deadline exceeded"},
	}

	card := state.snapshot(now)

	if card.Status != StatusUnreachable {
		t.Errorf("status = %q, want %q", card.Status, StatusUnreachable)
	}
	if len(card.Nodes) != 1 {
		t.Fatalf("stale snapshot dropped its nodes: %+v", card.Nodes)
	}
	if card.Error == nil || card.Error.Kind != "timeout" {
		t.Errorf("error = %+v, want the last failure", card.Error)
	}
	if card.FetchedAt == nil {
		t.Fatal("fetchedAt must date the data being served")
	}

	// Just inside the window the cluster keeps its own verdict.
	state.fetchedAt = now.Add(-staleAfter + time.Second)
	if got := state.snapshot(now).Status; got != StatusHealthy {
		t.Errorf("status = %q, want %q while fresh", got, StatusHealthy)
	}
}

// TestStartDoesNotBlockOnTheFirstRound: the listener must open before the
// first poll round finishes. Start used to block until every cluster had
// answered, so /healthz was refused for as long as the slowest cluster took
// and a liveness probe could restart a perfectly healthy daemon.
func TestStartDoesNotBlockOnTheFirstRound(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)

	p, err := NewPoller(&config.Config{
		Thresholds: config.Thresholds{Memory: 0.8},
		Clusters: []config.Cluster{{
			ID:             "qualification",
			Name:           "Qualification",
			URLs:           []string{srv.URL},
			TokenID:        "moxy@pve!ro",
			Secret:         config.NewSecret("sentinel"),
			TLS:            config.TLS{Mode: config.TLSModeInsecure},
			RequestTimeout: 2 * time.Second,
		}},
	})
	if err != nil {
		t.Fatalf("NewPoller: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	// Start has returned while the cluster is still held: that is the point.
	select {
	case <-p.Ready():
		t.Fatal("Ready is already closed: Start waited for the first round")
	default:
	}

	close(release)
	select {
	case <-p.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("Ready never closed after the first round completed")
	}
}

// TestReadyClosesWhenTheContextIsDone: a cluster that never answers must not
// leave readiness pending for ever; shutting down resolves it.
func TestReadyClosesWhenTheContextIsDone(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	p, err := NewPoller(&config.Config{
		Thresholds: config.Thresholds{Memory: 0.8},
		Clusters: []config.Cluster{{
			ID:             "qualification",
			Name:           "Qualification",
			URLs:           []string{srv.URL},
			TokenID:        "moxy@pve!ro",
			Secret:         config.NewSecret("sentinel"),
			TLS:            config.TLS{Mode: config.TLSModeInsecure},
			RequestTimeout: time.Minute,
		}},
	})
	if err != nil {
		t.Fatalf("NewPoller: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	cancel()

	select {
	case <-p.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("Ready never closed after the context was cancelled")
	}
}

// clusterServer answers the four calls a poll round makes, for one node.
// aptHits counts the apt/update calls, which is what this file is about.
func clusterServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var aptHits int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/cluster/resources"):
			_, _ = io.WriteString(w, `{"data":[{"type":"node","node":"prox-qual-2201-cit",`+
				`"status":"online","cpu":0.05,"maxcpu":32,"mem":1,"maxmem":2,"uptime":60}]}`)
		case strings.HasSuffix(r.URL.Path, "/cluster/status"):
			_, _ = io.WriteString(w, `{"data":[{"type":"cluster","name":"qual","nodes":1,"quorate":1},`+
				`{"type":"node","name":"prox-qual-2201-cit","online":1}]}`)
		case strings.HasSuffix(r.URL.Path, "/apt/update"):
			atomic.AddInt32(&aptHits, 1)
			_, _ = io.WriteString(w, `{"data":[{"Package":"pve-manager","Version":"9.2.12"}]}`)
		default:
			_, _ = io.WriteString(w, `{"data":null}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &aptHits
}

func testPoller(t *testing.T, url string) *Poller {
	t.Helper()
	p, err := NewPoller(&config.Config{
		Thresholds: config.Thresholds{Memory: 0.8},
		Clusters: []config.Cluster{{
			ID:             "qualification",
			Name:           "Qualification",
			URLs:           []string{url},
			TokenID:        "moxy@pve!ro",
			Secret:         config.NewSecret("sentinel"),
			TLS:            config.TLS{Mode: config.TLSModeInsecure},
			RequestTimeout: 2 * time.Second,
		}},
	})
	if err != nil {
		t.Fatalf("NewPoller: %v", err)
	}
	return p
}

// TestUpdatesAreCollectedAfterTheFirstRound: pollUpdates walks the node list of
// the last derived card. It used to start beside the very first poll, find no
// card, return at once, and not try again for ten minutes -- so the update
// banner was missing for that long after every restart, on a cluster whose
// token had the privilege all along.
func TestUpdatesAreCollectedAfterTheFirstRound(t *testing.T) {
	srv, aptHits := clusterServer(t)
	p := testPoller(t, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	select {
	case <-p.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("the first round never completed")
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		overview, err := p.Overview(ctx)
		if err != nil {
			t.Fatalf("Overview: %v", err)
		}
		if u := overview.Clusters[0].Updates; u != nil {
			if u.PVEManagerVersion == nil || *u.PVEManagerVersion != "9.2.12" {
				t.Fatalf("pveManagerVersion = %v, want the offered release", u.PVEManagerVersion)
			}
			if got := atomic.LoadInt32(aptHits); got < 1 {
				t.Fatalf("apt/update was called %d times", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("updates are still unknown well after the first round: the check did not run")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPollBudgetFor(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		urls    int
		want    time.Duration
	}{
		{"single url", 4 * time.Second, 1, 6 * time.Second},
		{"two urls", 4 * time.Second, 2, 10 * time.Second},
		// A dozen urls does not mean a round long enough to walk them all:
		// the sticky index makes the next round continue where this one left.
		{"many urls", 4 * time.Second, 12, 10 * time.Second},
		{"short timeout", time.Second, 3, 4 * time.Second},
		{"unset timeout", 0, 2, 10 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			urls := make([]string, tc.urls)
			got := PollBudgetFor(config.Cluster{URLs: urls, RequestTimeout: tc.timeout})
			if got != tc.want {
				t.Errorf("PollBudgetFor() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestPollRoundReachesASecondURL: the round used to be a flat six seconds, so a
// first node that accepts a connection and then never answers ate the whole
// budget and the second url was never tried. Every tick failed and the cluster
// went unreachable while another node was answering.
func TestPollRoundReachesASecondURL(t *testing.T) {
	wedged := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(wedged.Close)
	good, _ := clusterServer(t)

	p, err := NewPoller(&config.Config{
		Thresholds: config.Thresholds{Memory: 0.8},
		Clusters: []config.Cluster{{
			ID:             "qualification",
			Name:           "Qualification",
			URLs:           []string{wedged.URL, good.URL},
			TokenID:        "moxy@pve!ro",
			Secret:         config.NewSecret("sentinel"),
			TLS:            config.TLS{Mode: config.TLSModeInsecure},
			RequestTimeout: 300 * time.Millisecond,
			DialTimeout:    300 * time.Millisecond,
		}},
	})
	if err != nil {
		t.Fatalf("NewPoller: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	select {
	case <-p.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("the first round never completed")
	}

	overview, err := p.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	cluster := overview.Clusters[0]
	if cluster.Status == StatusUnreachable {
		t.Fatalf("the cluster is unreachable though its second node answers (error: %+v)", cluster.Error)
	}
	if len(cluster.Nodes) != 1 {
		t.Fatalf("nodes = %d, want the one the second url reported", len(cluster.Nodes))
	}
}

// TestRememberedHAExpires: a kept HA answer covers a failed call, not an
// absence. Past the window the overview uses before calling a reading stale,
// claiming a node is still draining would be worse than saying nothing.
func TestRememberedHAExpires(t *testing.T) {
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	s := &clusterState{now: func() time.Time { return now }}

	if got := s.rememberedHA(); got != nil {
		t.Fatalf("rememberedHA() = %v before anything was read, want nil", got)
	}

	s.rememberHA(&proxmox.HAManagerStatus{
		NodeStatus: map[string]string{"prox-pprd-2302-cit": proxmox.HANodeMaintenance},
	})
	kept := s.rememberedHA()
	if kept == nil {
		t.Fatal("rememberedHA() = nil right after a successful read")
	}
	if kept.NodeState("prox-pprd-2302-cit") != proxmox.HANodeMaintenance {
		t.Errorf("the kept status lost the node state")
	}

	now = now.Add(staleAfter - time.Second)
	if s.rememberedHA() == nil {
		t.Error("rememberedHA() = nil just inside the window")
	}
	now = now.Add(2 * time.Second)
	if got := s.rememberedHA(); got != nil {
		t.Errorf("rememberedHA() = %v past the window, want nil", got)
	}
}

/* ------------------------------------------------------ a poller with no network */

// fakeAudit stands in for a cluster: it answers the four calls of a poll round
// and counts them, so a test can watch a round happen rather than infer it.
type fakeAudit struct {
	mu        sync.Mutex
	calls     map[string]int
	resources []proxmox.Resource
	status    []proxmox.ClusterStatusEntry
	ha        *proxmox.HAManagerStatus
	updates   map[string][]proxmox.AptUpdate

	resourcesErr error
	statusErr    error
	haErr        error
	updatesErr   map[string]error
}

func newFakeAudit() *fakeAudit {
	return &fakeAudit{
		calls: map[string]int{},
		resources: []proxmox.Resource{
			{Type: proxmox.ResourceTypeNode, Node: "n1", Status: proxmox.StatusOnline,
				CPU: 0.25, MaxCPU: 32, Mem: proxmox.FlexInt(32 * mockGiB), MaxMem: proxmox.FlexInt(128 * mockGiB), Uptime: 3600},
			{Type: proxmox.ResourceTypeNode, Node: "n2", Status: proxmox.StatusOnline,
				CPU: 0.5, MaxCPU: 32, Mem: proxmox.FlexInt(64 * mockGiB), MaxMem: proxmox.FlexInt(128 * mockGiB), Uptime: 3600},
		},
		status: []proxmox.ClusterStatusEntry{
			{Type: proxmox.ClusterStatusTypeCluster, Name: "c", Nodes: 2, Quorate: true},
			{Type: proxmox.ClusterStatusTypeNode, Name: "n1", Online: true},
			{Type: proxmox.ClusterStatusTypeNode, Name: "n2", Online: true},
		},
		ha:         &proxmox.HAManagerStatus{NodeStatus: map[string]string{"n1": proxmox.HANodeOnline}},
		updates:    map[string][]proxmox.AptUpdate{"n1": {{Package: "pve-manager", Version: "9.2.12"}}},
		updatesErr: map[string]error{},
	}
}

func (f *fakeAudit) record(name string) {
	f.mu.Lock()
	f.calls[name]++
	f.mu.Unlock()
}

func (f *fakeAudit) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func (f *fakeAudit) ClusterResources(context.Context) ([]proxmox.Resource, error) {
	f.record("resources")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resources, f.resourcesErr
}

func (f *fakeAudit) ClusterStatus(context.Context) ([]proxmox.ClusterStatusEntry, error) {
	f.record("status")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, f.statusErr
}

func (f *fakeAudit) HAManagerStatus(context.Context) (*proxmox.HAManagerStatus, error) {
	f.record("ha")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ha, f.haErr
}

func (f *fakeAudit) AptUpdates(_ context.Context, node string) ([]proxmox.AptUpdate, error) {
	f.record("updates:" + node)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.updatesErr[node]; err != nil {
		return nil, err
	}
	return f.updates[node], nil
}

func fakePoller(f *fakeAudit) (*Poller, *clusterState) {
	state := newClusterState(Identity{ID: "c", Name: "Cluster"}, f, 2*time.Second, 0.8, nil)
	return newPoller(0.8, []*clusterState{state}, nil), state
}

func TestPollOnceDerivesACard(t *testing.T) {
	f := newFakeAudit()
	p, state := fakePoller(f)

	state.pollOnce(context.Background())
	// Overview waits on readiness, which only Start closes. Driving a single
	// round by hand means saying so.
	p.readyOnce.Do(func() { close(p.ready) })

	overview, err := p.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	card := overview.Clusters[0]
	if len(card.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(card.Nodes))
	}
	if card.Error != nil {
		t.Errorf("error = %+v, want nil", card.Error)
	}
	// Σ(cpu×cores)/Σ(cores) over two equal-core nodes.
	if card.CPU == nil || math.Abs(card.CPU.Ratio-0.375) > 1e-9 {
		t.Errorf("cpu = %+v, want the weighted mean 0.375", card.CPU)
	}
}

// TestPollOnceKeepsTheCardWhenOneCallFails: the card is the last good reading,
// and a failed round must not replace it with a worse one.
func TestPollOnceKeepsTheCardWhenOneCallFails(t *testing.T) {
	f := newFakeAudit()
	_, state := fakePoller(f)
	state.pollOnce(context.Background())

	before := state.snapshot(time.Now())
	f.statusErr = errors.New("http 500 Internal Server Error")
	state.pollOnce(context.Background())

	after := state.snapshot(time.Now())
	if len(after.Nodes) != len(before.Nodes) {
		t.Fatalf("nodes = %d after a failed round, want the previous %d", len(after.Nodes), len(before.Nodes))
	}
	if after.Error == nil {
		t.Fatal("the failure is not reported")
	}
	if after.FetchedAt == nil || before.FetchedAt == nil || !after.FetchedAt.Equal(*before.FetchedAt) {
		t.Error("fetchedAt moved on a round that failed")
	}
}

// TestPollUpdatesSurvivesAPartialRefusal: a token may audit some nodes and not
// others. The nodes that answered keep their count; the others stay unknown,
// which is not the same as zero.
func TestPollUpdatesSurvivesAPartialRefusal(t *testing.T) {
	f := newFakeAudit()
	f.updates["n2"] = []proxmox.AptUpdate{{Package: "curl", Version: "8.0"}}
	f.updatesErr["n1"] = errors.New("http 403 Forbidden")
	_, state := fakePoller(f)

	state.pollOnce(context.Background())
	state.pollUpdates(context.Background())
	state.pollOnce(context.Background())

	card := state.snapshot(time.Now())
	byName := map[string]*int{}
	for _, n := range card.Nodes {
		byName[n.Name] = n.PendingUpdates
	}
	if byName["n1"] != nil {
		t.Errorf("n1 refused the question but reports %d pending", *byName["n1"])
	}
	if byName["n2"] == nil || *byName["n2"] != 1 {
		t.Errorf("n2 answered but reports %v", byName["n2"])
	}
}

// TestStartStopsOnCancel: the goroutines must end with the context, or a
// shutdown would leave rounds running against clusters nobody is watching.
func TestStartStopsOnCancel(t *testing.T) {
	f := newFakeAudit()
	p, _ := fakePoller(f)

	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	select {
	case <-p.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("the first round never completed")
	}
	cancel()

	// Two intervals of quiet: whatever was in flight has landed, and nothing
	// new is started.
	settled := f.count("resources")
	time.Sleep(50 * time.Millisecond)
	before := f.count("resources")
	time.Sleep(150 * time.Millisecond)
	if after := f.count("resources"); after != before {
		t.Errorf("calls went from %d to %d after cancel (settled at %d)", before, after, settled)
	}
}
