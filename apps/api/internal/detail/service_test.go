package detail

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

// fakeClient is a clusterClient that answers from memory.
//
// The service depends on the narrow interface rather than on *proxmox.Client
// precisely so this can exist: no network, no TLS, no httptest server, and a
// call counter to prove that ten simultaneous readers cost the cluster one
// request.
type fakeClient struct {
	mu    sync.Mutex
	calls map[string]int

	resources    []proxmox.Resource
	resourcesErr error
	status       []proxmox.ClusterStatusEntry
	statusErr    error
	ha           *proxmox.HAManagerStatus
	haErr        error
	node         *proxmox.NodeStatus
	nodeErr      error
	guest        *proxmox.GuestStatus
	guestErr     error
	updates      []proxmox.AptUpdate
	updatesErr   error
	ipv4         string
	ipv4Err      error
	points       []proxmox.RRDPoint
	pointsErr    error
	tasks        []proxmox.Task
	tasksErr     error

	// gate, when set, runs at the start of every call. It is how a test
	// holds a call in flight.
	gate func()
}

func (f *fakeClient) record(name string) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = make(map[string]int)
	}
	f.calls[name]++
	f.mu.Unlock()
	if f.gate != nil {
		f.gate()
	}
}

func (f *fakeClient) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func (f *fakeClient) ClusterResources(context.Context) ([]proxmox.Resource, error) {
	f.record("resources")
	return f.resources, f.resourcesErr
}

func (f *fakeClient) ClusterStatus(context.Context) ([]proxmox.ClusterStatusEntry, error) {
	f.record("status")
	return f.status, f.statusErr
}

func (f *fakeClient) HAManagerStatus(context.Context) (*proxmox.HAManagerStatus, error) {
	f.record("ha")
	return f.ha, f.haErr
}

func (f *fakeClient) AptUpdates(context.Context, string) ([]proxmox.AptUpdate, error) {
	f.record("updates")
	return f.updates, f.updatesErr
}

func (f *fakeClient) NodeStatus(context.Context, string) (*proxmox.NodeStatus, error) {
	f.record("nodeStatus")
	return f.node, f.nodeErr
}

func (f *fakeClient) GuestStatus(context.Context, string, string, int) (*proxmox.GuestStatus, error) {
	f.record("guestStatus")
	return f.guest, f.guestErr
}

func (f *fakeClient) NodeRRD(context.Context, string, string) ([]proxmox.RRDPoint, error) {
	f.record("nodeRRD")
	return f.points, f.pointsErr
}

func (f *fakeClient) GuestRRD(context.Context, string, string, int, string) ([]proxmox.RRDPoint, error) {
	f.record("guestRRD")
	return f.points, f.pointsErr
}

func (f *fakeClient) ClusterTasks(context.Context) ([]proxmox.Task, error) {
	f.record("tasks")
	return f.tasks, f.tasksErr
}

func (f *fakeClient) GuestIPv4(context.Context, string, int) (string, error) {
	f.record("ipv4")
	return f.ipv4, f.ipv4Err
}

// newFake builds a client answering for a two-node cluster hosting two guests.
func newFake() *fakeClient {
	return &fakeClient{
		resources: []proxmox.Resource{
			{Type: proxmox.ResourceTypeNode, Node: "pve-1", Status: proxmox.StatusOnline},
			{Type: proxmox.ResourceTypeNode, Node: "pve-2", Status: proxmox.StatusOnline},
			{Type: proxmox.ResourceTypeQemu, Node: "pve-1", VMID: 102, Name: "web", Status: proxmox.StatusRunning, Tags: "prod"},
			{Type: proxmox.ResourceTypeLXC, Node: "pve-2", VMID: 101, Name: "dns", Status: proxmox.StatusRunning},
		},
		status: []proxmox.ClusterStatusEntry{
			{Type: proxmox.ClusterStatusTypeCluster, Name: "pprd", Nodes: 2, Quorate: true},
			{Type: proxmox.ClusterStatusTypeNode, Name: "pve-1", Online: true},
			{Type: proxmox.ClusterStatusTypeNode, Name: "pve-2", Online: true},
		},
		ha:      &proxmox.HAManagerStatus{NodeStatus: map[string]string{"pve-1": proxmox.HANodeOnline}},
		node:    &proxmox.NodeStatus{Uptime: 3600, CPU: 0.25, CPUInfo: proxmox.NodeCPUInfo{CPUs: 16}, Memory: proxmox.Usage{Used: 4, Total: 8}},
		guest:   &proxmox.GuestStatus{Status: proxmox.StatusRunning, Name: "web", Uptime: 60, Mem: 1, MaxMem: 2},
		updates: []proxmox.AptUpdate{{Package: "pve-manager", Version: "9.2.10"}},
		ipv4:    "10.18.160.4",
		points:  []proxmox.RRDPoint{{Time: 100, CPU: floatPtr(0.5)}},
		tasks:   []proxmox.Task{{UPID: "a", Node: "pve-1", StartTime: 100, EndTime: flexPtr(160), Status: proxmox.TaskStatusOK}},
	}
}

// newFakeService wires a fake under the cluster id "preproduction".
func newFakeService(t *testing.T, f *fakeClient, clock *testClock) *Service {
	t.Helper()
	return newService(map[string]clusterClient{"preproduction": f}, 5*time.Second, clock.Now)
}

func TestServiceRejectsAnUnknownCluster(t *testing.T) {
	svc := newFakeService(t, newFake(), newTestClock())
	ctx := context.Background()

	if _, err := svc.Node(ctx, "nowhere", "pve-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Node returned %v, want ErrNotFound", err)
	}
	if _, err := svc.Guest(ctx, "nowhere", 102); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Guest returned %v, want ErrNotFound", err)
	}
	if _, err := svc.NodeSeries(ctx, "nowhere", "pve-1", proxmox.TimeframeHour); !errors.Is(err, ErrNotFound) {
		t.Fatalf("NodeSeries returned %v, want ErrNotFound", err)
	}
	if _, err := svc.GuestSeries(ctx, "nowhere", 102, proxmox.TimeframeHour); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GuestSeries returned %v, want ErrNotFound", err)
	}
	if _, err := svc.Tasks(ctx, "nowhere", 10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Tasks returned %v, want ErrNotFound", err)
	}
}

func TestServiceRejectsAnUnknownNode(t *testing.T) {
	svc := newFakeService(t, newFake(), newTestClock())

	if _, err := svc.Node(context.Background(), "preproduction", "pve-9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Node returned %v, want ErrNotFound", err)
	}
	if _, err := svc.NodeSeries(context.Background(), "preproduction", "pve-9", proxmox.TimeframeDay); !errors.Is(err, ErrNotFound) {
		t.Fatalf("NodeSeries returned %v, want ErrNotFound", err)
	}
}

func TestServiceRejectsAnUnknownGuest(t *testing.T) {
	svc := newFakeService(t, newFake(), newTestClock())

	if _, err := svc.Guest(context.Background(), "preproduction", 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Guest returned %v, want ErrNotFound", err)
	}
	if _, err := svc.GuestSeries(context.Background(), "preproduction", 999, proxmox.TimeframeDay); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GuestSeries returned %v, want ErrNotFound", err)
	}
}

func TestServiceRejectsAnUnknownTimeframe(t *testing.T) {
	svc := newFakeService(t, newFake(), newTestClock())

	if _, err := svc.NodeSeries(context.Background(), "preproduction", "pve-1", "decade"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("NodeSeries returned %v, want ErrNotFound", err)
	}
}

func TestServiceNode(t *testing.T) {
	f := newFake()
	svc := newFakeService(t, f, newTestClock())

	node, err := svc.Node(context.Background(), "preproduction", "pve-1")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if node.Cluster != "preproduction" || node.Name != "pve-1" {
		t.Fatalf("node is %s/%s", node.Cluster, node.Name)
	}
	if node.Status != aggregate.NodeOnline {
		t.Fatalf("status is %q, want online", node.Status)
	}
	if node.Quorum == nil || !node.Quorum.Quorate {
		t.Fatalf("quorum is %+v", node.Quorum)
	}
	if node.PendingUpdates == nil || *node.PendingUpdates != 1 {
		t.Fatalf("pending updates is %v, want 1", node.PendingUpdates)
	}
	if len(node.Updates) != 1 || node.Updates[0].Package != "pve-manager" || node.Updates[0].Version != "9.2.10" {
		t.Fatalf("updates are %+v, want the pending pve-manager", node.Updates)
	}
	if len(node.Guests) != 1 || node.Guests[0].VMID != 102 {
		t.Fatalf("guests are %+v, want the one hosted by pve-1", node.Guests)
	}
}

func TestServiceNodeInMaintenanceIsNotOffline(t *testing.T) {
	f := newFake()
	f.ha = &proxmox.HAManagerStatus{NodeStatus: map[string]string{"pve-1": proxmox.HANodeMaintenance}}
	svc := newFakeService(t, f, newTestClock())

	node, err := svc.Node(context.Background(), "preproduction", "pve-1")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if node.Status != aggregate.NodeMaintenance {
		t.Fatalf("status is %q, want maintenance", node.Status)
	}
}

func TestServiceNodeSurvivesEveryOptionalFailure(t *testing.T) {
	f := newFake()
	// A token without Sys.Modify, a cluster without an HA manager: both are
	// ordinary, and neither may cost the operator the page.
	f.updatesErr = errors.New("http 403 Forbidden")
	f.haErr = errors.New("http 501 Not Implemented")
	svc := newFakeService(t, f, newTestClock())

	node, err := svc.Node(context.Background(), "preproduction", "pve-1")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if node.PendingUpdates != nil {
		t.Fatalf("pending updates is %v, want nil rather than a 0 that claims the node is up to date", node.PendingUpdates)
	}
	if node.Updates != nil {
		t.Fatalf("updates is %v, want nil rather than an [] that claims the node is up to date", node.Updates)
	}
	if node.HAState != nil {
		t.Fatalf("ha state is %v, want nil", node.HAState)
	}
	if node.Status != aggregate.NodeOnline {
		t.Fatalf("status is %q, want online: the rest of the page is served", node.Status)
	}
	if node.CPU.Cores != 16 || node.Uptime != 3600 {
		t.Fatalf("the node figures were lost: %+v", node)
	}
}

func TestServiceNodePropagatesTheEssentialFailure(t *testing.T) {
	f := newFake()
	f.nodeErr = errors.New("http 500 Internal Server Error")
	svc := newFakeService(t, f, newTestClock())

	if _, err := svc.Node(context.Background(), "preproduction", "pve-1"); err == nil {
		t.Fatal("Node returned no error although the node status failed")
	}
}

func TestServiceGuest(t *testing.T) {
	f := newFake()
	svc := newFakeService(t, f, newTestClock())

	guest, err := svc.Guest(context.Background(), "preproduction", 102)
	if err != nil {
		t.Fatalf("Guest: %v", err)
	}
	if guest.Node != "pve-1" {
		t.Fatalf("node is %q, want the one hosting it", guest.Node)
	}
	if guest.Kind != aggregate.GuestQemu || guest.Status != aggregate.GuestRunning {
		t.Fatalf("kind/status are %q/%q", guest.Kind, guest.Status)
	}
	if guest.IPv4 == nil || *guest.IPv4 != "10.18.160.4" {
		t.Fatalf("ipv4 is %v", guest.IPv4)
	}
}

func TestServiceGuestWithoutAnAgentKeepsTheRest(t *testing.T) {
	f := newFake()
	// The normal state of a VM with no guest agent: PVE answers 500.
	f.ipv4Err = errors.New("http 500 Internal Server Error")
	svc := newFakeService(t, f, newTestClock())

	guest, err := svc.Guest(context.Background(), "preproduction", 102)
	if err != nil {
		t.Fatalf("Guest: %v", err)
	}
	if guest.IPv4 != nil {
		t.Fatalf("ipv4 is %v, want nil", guest.IPv4)
	}
	if guest.Name != "web" || guest.Memory.Total != 2 {
		t.Fatalf("the guest figures were lost: %+v", guest)
	}
}

func TestServiceGuestDoesNotAskTheAgentOfAContainer(t *testing.T) {
	f := newFake()
	svc := newFakeService(t, f, newTestClock())

	// 101 is an LXC container: it has no agent tree at all, and the request
	// would be rejected by the client without ever leaving.
	if _, err := svc.Guest(context.Background(), "preproduction", 101); err != nil {
		t.Fatalf("Guest: %v", err)
	}
	if n := f.count("ipv4"); n != 0 {
		t.Fatalf("the agent was asked %d times about a container, want 0", n)
	}
}

func TestServiceGuestPropagatesTheEssentialFailure(t *testing.T) {
	f := newFake()
	f.guestErr = errors.New("http 500 Internal Server Error")
	svc := newFakeService(t, f, newTestClock())

	if _, err := svc.Guest(context.Background(), "preproduction", 102); err == nil {
		t.Fatal("Guest returned no error although the guest status failed")
	}
}

func TestServiceGuestFollowsAMigrationWithinOneTTL(t *testing.T) {
	clock := newTestClock()
	f := newFake()
	svc := newFakeService(t, f, clock)

	guest, err := svc.Guest(context.Background(), "preproduction", 102)
	if err != nil {
		t.Fatalf("Guest: %v", err)
	}
	if guest.Node != "pve-1" {
		t.Fatalf("node is %q, want pve-1", guest.Node)
	}

	// The guest migrates. The vmid-to-node mapping must not outlive the TTL,
	// or the service would keep asking the wrong node about it.
	f.mu.Lock()
	f.resources[2].Node = "pve-2"
	f.mu.Unlock()
	clock.advance(6 * time.Second)

	guest, err = svc.Guest(context.Background(), "preproduction", 102)
	if err != nil {
		t.Fatalf("Guest after migration: %v", err)
	}
	if guest.Node != "pve-2" {
		t.Fatalf("node is %q, want pve-2 after the migration", guest.Node)
	}
}

func TestServiceSeries(t *testing.T) {
	f := newFake()
	svc := newFakeService(t, f, newTestClock())

	series, err := svc.NodeSeries(context.Background(), "preproduction", "pve-1", proxmox.TimeframeWeek)
	if err != nil {
		t.Fatalf("NodeSeries: %v", err)
	}
	if series.Timeframe != proxmox.TimeframeWeek || len(series.Points) != 1 {
		t.Fatalf("series is %+v", series)
	}

	guestSeries, err := svc.GuestSeries(context.Background(), "preproduction", 102, "")
	if err != nil {
		t.Fatalf("GuestSeries: %v", err)
	}
	if guestSeries.Timeframe != proxmox.TimeframeHour {
		t.Fatalf("timeframe is %q, want the hour by default", guestSeries.Timeframe)
	}
}

func TestServiceTasks(t *testing.T) {
	f := newFake()
	f.tasks = []proxmox.Task{
		{UPID: "old", StartTime: 100, EndTime: flexPtr(160), Status: proxmox.TaskStatusOK},
		{UPID: "new", StartTime: 900},
	}
	svc := newFakeService(t, f, newTestClock())

	tasks, err := svc.Tasks(context.Background(), "preproduction", 0)
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks.Entries) != 2 || tasks.Entries[0].UPID != "new" {
		t.Fatalf("entries are %+v, want the most recent first", tasks.Entries)
	}
	if tasks.Entries[0].Status != taskStatusRunning || tasks.Entries[0].Duration != nil {
		t.Fatalf("the running task is %+v", tasks.Entries[0])
	}
}

// The limit cuts the log after it has been ordered, so it keeps the newest
// entries and not the first ones PVE happened to list. And because
// /cluster/tasks takes no parameter, every limit is served by the same
// upstream call: asking for one entry after asking for two must not fetch
// again.
func TestServiceTasksLimitCutsTheNewestAndSharesOneFetch(t *testing.T) {
	f := newFake()
	f.tasks = []proxmox.Task{
		{UPID: "old", StartTime: 100, EndTime: flexPtr(160), Status: proxmox.TaskStatusOK},
		{UPID: "new", StartTime: 900},
	}
	svc := newFakeService(t, f, newTestClock())

	if _, err := svc.Tasks(context.Background(), "preproduction", 2); err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	tasks, err := svc.Tasks(context.Background(), "preproduction", 1)
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks.Entries) != 1 || tasks.Entries[0].UPID != "new" {
		t.Fatalf("entries are %+v, want only the most recent one", tasks.Entries)
	}
	if got := f.count("tasks"); got != 1 {
		t.Fatalf("tasks fetched %d times, want 1: the limit is not part of the cache key", got)
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct {
		in, want int
	}{
		{in: 0, want: defaultTaskLimit},
		{in: -1, want: defaultTaskLimit},
		{in: 10, want: 10},
		{in: maxTaskLimit + 1, want: maxTaskLimit},
	}
	for _, tt := range tests {
		if got := clampLimit(tt.in); got != tt.want {
			t.Fatalf("clampLimit(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestServiceCollapsesSimultaneousReadersOfTheSameNode(t *testing.T) {
	const readers = 10

	f := newFake()
	var arrived int32
	f.gate = func() {
		// Hold every upstream call in flight until all ten readers are on
		// their way: this is the five-second refresh of ten open tabs.
		for atomic.LoadInt32(&arrived) < readers {
			runtime.Gosched()
		}
	}
	svc := newFakeService(t, f, newTestClock())

	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt32(&arrived, 1)
			if _, err := svc.Node(context.Background(), "preproduction", "pve-1"); err != nil {
				t.Errorf("Node: %v", err)
			}
		}()
	}
	wg.Wait()

	for _, call := range []string{"resources", "status", "ha", "nodeStatus", "updates"} {
		if n := f.count(call); n != 1 {
			t.Fatalf("%d readers cost the cluster %d %s calls, want 1", readers, n, call)
		}
	}
}

func TestServiceKeepsServingWhenOneReaderGivesUp(t *testing.T) {
	f := newFake()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	f.gate = func() {
		once.Do(func() { close(entered) })
		<-release
	}
	svc := newFakeService(t, f, newTestClock())

	leaving, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := svc.Node(leaving, "preproduction", "pve-1")
		first <- err
	}()

	<-entered

	second := make(chan error, 1)
	go func() {
		_, err := svc.Node(context.Background(), "preproduction", "pve-1")
		second <- err
	}()

	// The reader that closed its tab must not take the answer away from the
	// readers still waiting for it.
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("the reader that went away got %v, want context.Canceled", err)
	}

	close(release)
	if err := <-second; err != nil {
		t.Fatalf("the reader that stayed got %v", err)
	}
}

func TestNewServiceDefaultsItsTTL(t *testing.T) {
	svc := NewService(nil, 0)
	if svc.views.ttl != DefaultTTL {
		t.Fatalf("ttl is %v, want %v", svc.views.ttl, DefaultTTL)
	}
	if _, err := svc.Node(context.Background(), "preproduction", "pve-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Node on a service with no cluster returned %v, want ErrNotFound", err)
	}
}

// TestClientSatisfiesTheNarrowInterface is the compile-time check that the
// interface this package declares is the one *proxmox.Client implements.
func TestClientSatisfiesTheNarrowInterface(t *testing.T) {
	var _ clusterClient = (*proxmox.Client)(nil)
}
