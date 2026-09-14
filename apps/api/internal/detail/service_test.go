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
	"github.com/dmajorel/moxy/apps/api/internal/config"
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
	config       proxmox.GuestConfig
	configErr    error
	updates      []proxmox.AptUpdate
	updatesErr   error
	ipv4         string
	ipv4Err      error
	vnets        []proxmox.SDNVNet
	vnetsErr     error
	networks     []proxmox.NodeNetwork
	networksErr  error
	points       []proxmox.RRDPoint
	pointsErr    error
	// pointsByNode and pointsErrByNode override points and pointsErr for one
	// node, which is what lets a test give two nodes different histories, or
	// refuse one of them the way a missing Sys.Audit does.
	pointsByNode    map[string][]proxmox.RRDPoint
	pointsErrByNode map[string]error
	tasks           []proxmox.Task
	tasksErr        error
	// nodeTasks records what the last per-guest call asked for, which is the
	// only way to prove the filtering happens upstream and not here.
	nodeTasks    []proxmox.Task
	nodeTasksErr error
	askedNode    string
	askedVMID    int
	askedLimit   int

	// gate, when set, runs at the start of every call. It is how a test
	// holds a call in flight.
	gate func()
}

func (f *fakeClient) record(ctx context.Context, name string) {
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

func (f *fakeClient) ClusterResources(ctx context.Context) ([]proxmox.Resource, error) {
	f.record(ctx, "resources")
	return f.resources, f.resourcesErr
}

func (f *fakeClient) ClusterStatus(ctx context.Context) ([]proxmox.ClusterStatusEntry, error) {
	f.record(ctx, "status")
	return f.status, f.statusErr
}

func (f *fakeClient) HAManagerStatus(ctx context.Context) (*proxmox.HAManagerStatus, error) {
	f.record(ctx, "ha")
	return f.ha, f.haErr
}

func (f *fakeClient) AptUpdates(ctx context.Context, _ string) ([]proxmox.AptUpdate, error) {
	f.record(ctx, "updates")
	return f.updates, f.updatesErr
}

func (f *fakeClient) NodeStatus(ctx context.Context, _ string) (*proxmox.NodeStatus, error) {
	f.record(ctx, "nodeStatus")
	return f.node, f.nodeErr
}

func (f *fakeClient) GuestStatus(ctx context.Context, _, _ string, _ int) (*proxmox.GuestStatus, error) {
	f.record(ctx, "guestStatus")
	return f.guest, f.guestErr
}

func (f *fakeClient) GuestConfig(ctx context.Context, _, _ string, _ int) (proxmox.GuestConfig, error) {
	f.record(ctx, "guestConfig")
	return f.config, f.configErr
}

func (f *fakeClient) NodeRRD(ctx context.Context, node, _ string) ([]proxmox.RRDPoint, error) {
	f.record(ctx, "nodeRRD")
	if err, ok := f.pointsErrByNode[node]; ok {
		return nil, err
	}
	if points, ok := f.pointsByNode[node]; ok {
		return points, nil
	}
	return f.points, f.pointsErr
}

func (f *fakeClient) GuestRRD(ctx context.Context, _, _ string, _ int, _ string) ([]proxmox.RRDPoint, error) {
	f.record(ctx, "guestRRD")
	return f.points, f.pointsErr
}

func (f *fakeClient) ClusterTasks(ctx context.Context) ([]proxmox.Task, error) {
	f.record(ctx, "tasks")
	return f.tasks, f.tasksErr
}

func (f *fakeClient) NodeTasks(ctx context.Context, node string, vmid, limit int) ([]proxmox.Task, error) {
	f.mu.Lock()
	f.askedNode, f.askedVMID, f.askedLimit = node, vmid, limit
	f.mu.Unlock()
	f.record(ctx, "nodeTasks")
	return f.nodeTasks, f.nodeTasksErr
}

func (f *fakeClient) GuestIPv4(ctx context.Context, _ string, _ int) (string, error) {
	f.record(ctx, "ipv4")
	return f.ipv4, f.ipv4Err
}

func (f *fakeClient) SDNVNets(ctx context.Context) ([]proxmox.SDNVNet, error) {
	f.record(ctx, "sdnVNets")
	return f.vnets, f.vnetsErr
}

func (f *fakeClient) NodeNetworks(ctx context.Context, _ string) ([]proxmox.NodeNetwork, error) {
	f.record(ctx, "nodeNetworks")
	return f.networks, f.networksErr
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
		config: proxmox.GuestConfig{
			"scsi0":   "ceph-vm:vm-102-disk-0,size=32G",
			"ide2":    "local:iso/debian-13.iso,media=cdrom",
			"unused0": "local-lvm:vm-102-disk-1",
		},
		points: []proxmox.RRDPoint{{Time: 100, CPU: floatPtr(0.5)}},
		tasks:  []proxmox.Task{{UPID: "a", Node: "pve-1", StartTime: 100, EndTime: flexPtr(160), Status: proxmox.TaskStatusOK}},
	}
}

// newFakeService wires a fake under the cluster id "preproduction".
func newFakeService(t *testing.T, f *fakeClient, clock *testClock) *Service {
	t.Helper()
	return newService(map[string]clusterClient{"preproduction": f}, 5*time.Second, 0, clock.Now)
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
	if _, err := svc.ClusterSeries(ctx, "nowhere", proxmox.TimeframeHour); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ClusterSeries returned %v, want ErrNotFound", err)
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

// cacheSize reports how many entries a cache holds. It is the only way a test
// can show that a refused name left nothing behind.
func cacheSize[K comparable, V any](c *cache[K, V]) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// TestServiceAsksNothingUpstreamForAnUnknownNode is the whole point of
// resolving the cluster view first. A node name nobody has must cost no PVE
// call and leave no cache entry behind: upstream, an unknown name draws a 5xx,
// which the client reads as a dead node and retries against every configured
// URL of the cluster, and each name asked for would otherwise keep an entry in
// three caches for the length of its TTL.
func TestServiceAsksNothingUpstreamForAnUnknownNode(t *testing.T) {
	fake := newFake()
	svc := newFakeService(t, fake, newTestClock())

	if _, err := svc.Node(context.Background(), "preproduction", "pve-9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Node returned %v, want ErrNotFound", err)
	}
	if _, err := svc.NodeSeries(context.Background(), "preproduction", "pve-9", proxmox.TimeframeDay); !errors.Is(err, ErrNotFound) {
		t.Fatalf("NodeSeries returned %v, want ErrNotFound", err)
	}

	for _, call := range []string{"nodeStatus", "updates", "nodeRRD"} {
		if n := fake.count(call); n != 0 {
			t.Errorf("%s calls = %d, want none", call, n)
		}
	}
	for _, c := range []struct {
		name string
		size int
	}{
		{"nodes", cacheSize(svc.nodes)},
		{"updates", cacheSize(svc.updates)},
		{"series", cacheSize(svc.series)},
	} {
		if c.size != 0 {
			t.Errorf("%s cache holds %d entries, want none", c.name, c.size)
		}
	}
}

// TestServiceStillFansOutPerNodeCalls guards the other half of the rule: only
// the cluster view moved ahead of the rest. The two calls that name a node
// still go out together, so a node page costs one round trip, not two.
func TestServiceStillFansOutPerNodeCalls(t *testing.T) {
	fake := newFake()
	svc := newFakeService(t, fake, newTestClock())

	// Warm the view through the one route that needs nothing else, so the
	// gate set below can only ever see the per-node calls.
	if _, err := svc.MaintenancePlan(context.Background(), "preproduction", "pve-1"); err != nil {
		t.Fatalf("MaintenancePlan returned %v", err)
	}

	release := make(chan struct{})
	inFlight := make(chan struct{}, 2)
	fake.gate = func() {
		inFlight <- struct{}{}
		<-release
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.Node(context.Background(), "preproduction", "pve-1")
		done <- err
	}()

	// The first call announces itself, then blocks. The second can only
	// announce itself too if it was started without waiting for the first.
	<-inFlight
	select {
	case <-inFlight:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the second per-node call waited for the first: the fan-out is gone")
	}
	close(release)

	if err := <-done; err != nil {
		t.Fatalf("Node returned %v", err)
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

// An unknown window is the CALLER's mistake, not a missing object: the cluster
// and the node are both there. It used to be reported as ErrNotFound, which
// the HTTP layer would have turned into a 404 about a node that exists --
// sending an operator to look at the cluster for a typo in their query string.
// The server answers 400 for the same string, and the two must agree.
func TestServiceRejectsAnUnknownTimeframe(t *testing.T) {
	svc := newFakeService(t, newFake(), newTestClock())
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"node series", func() error {
			_, err := svc.NodeSeries(ctx, "preproduction", "pve-1", "decade")
			return err
		}},
		{"cluster series", func() error {
			_, err := svc.ClusterSeries(ctx, "preproduction", "decade")
			return err
		}},
		{"guest series", func() error {
			_, err := svc.GuestSeries(ctx, "preproduction", 102, "decade")
			return err
		}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("returned %v, want ErrInvalidArgument", err)
			}
			if errors.Is(err, ErrNotFound) {
				t.Error("a bad window was reported as a missing object")
			}
		})
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
	if node.CPU.Cores != 16 || node.Uptime == nil || *node.Uptime != 3600 {
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

// TestServiceGuestSkipsTheAgentWhenItIsNotConfigured: a VM whose configuration
// carries no guest agent will never answer the agent endpoint, so asking is a
// request spent on a certainty. PVE says so in status/current, which the view
// already reads.
func TestServiceGuestSkipsTheAgentWhenItIsNotConfigured(t *testing.T) {
	off := proxmox.FlexBool(false)
	f := newFake()
	f.guest.Agent = &off
	svc := newFakeService(t, f, newTestClock())

	guest, err := svc.Guest(context.Background(), "preproduction", 102)
	if err != nil {
		t.Fatalf("Guest: %v", err)
	}
	if n := f.count("ipv4"); n != 0 {
		t.Fatalf("the agent was asked %d times though it is not configured, want 0", n)
	}
	if guest.IPv4 != nil {
		t.Fatalf("ipv4 is %v, want nil", guest.IPv4)
	}
	// The rest of the page is untouched: this is a saving, not a degradation.
	if guest.Name != "web" {
		t.Fatalf("the guest figures were lost: %+v", guest)
	}
}

// TestServiceGuestAsksTheAgentWhenConfigured covers the two cases that must
// keep asking: the agent is declared, and the field is absent altogether —
// which older PVE releases do, and which must read as unknown rather than no.
func TestServiceGuestAsksTheAgentWhenConfigured(t *testing.T) {
	on := proxmox.FlexBool(true)
	for name, agent := range map[string]*proxmox.FlexBool{
		"declared": &on,
		"absent":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake()
			f.guest.Agent = agent
			svc := newFakeService(t, f, newTestClock())

			guest, err := svc.Guest(context.Background(), "preproduction", 102)
			if err != nil {
				t.Fatalf("Guest: %v", err)
			}
			if n := f.count("ipv4"); n != 1 {
				t.Fatalf("the agent was asked %d times, want 1", n)
			}
			if guest.IPv4 == nil || *guest.IPv4 != "10.18.160.4" {
				t.Fatalf("ipv4 = %v, want the address the agent reported", guest.IPv4)
			}
		})
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

// The per-guest log is asked of the node hosting the guest, with the vmid and
// the limit carried upstream: the filtering is PVE's, which is the whole point
// — sieving the cluster log here is what lost a guest's own lines behind two
// hundred fresher ones.
func TestServiceGuestTasksAsksTheHostingNode(t *testing.T) {
	f := newFake()
	f.nodeTasks = []proxmox.Task{
		{UPID: "old", Node: "pve-1", ID: "102", StartTime: 100, EndTime: flexPtr(160), Status: proxmox.TaskStatusOK},
		{UPID: "new", Node: "pve-1", ID: "102", StartTime: 900},
	}
	svc := newFakeService(t, f, newTestClock())

	tasks, err := svc.GuestTasks(context.Background(), "preproduction", 102, 25)
	if err != nil {
		t.Fatalf("GuestTasks: %v", err)
	}
	if len(tasks.Entries) != 2 || tasks.Entries[0].UPID != "new" {
		t.Fatalf("entries are %+v, want the most recent first", tasks.Entries)
	}
	// The cluster log must not have been read at all: it is the call this
	// route exists to stop making.
	if got := f.count("tasks"); got != 0 {
		t.Errorf("cluster tasks fetched %d times, want none", got)
	}
	// 102 runs on pve-1 in the fake cluster; 101 is the container on pve-2.
	if f.askedNode != "pve-1" || f.askedVMID != 102 || f.askedLimit != 25 {
		t.Errorf("asked %q for vmid %d limit %d, want pve-1/102/25", f.askedNode, f.askedVMID, f.askedLimit)
	}
}

// A limit of zero means "the caller did not say", and an oversized one is
// clamped rather than refused — the same rule the cluster log follows.
func TestServiceGuestTasksClampsTheLimitItSendsUpstream(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{in: 0, want: DefaultTaskLimit},
		{in: MaxTaskLimit + 1, want: MaxTaskLimit},
	} {
		f := newFake()
		svc := newFakeService(t, f, newTestClock())

		if _, err := svc.GuestTasks(context.Background(), "preproduction", 102, tc.in); err != nil {
			t.Fatalf("GuestTasks: %v", err)
		}
		if f.askedLimit != tc.want {
			t.Errorf("limit %d reached upstream as %d, want %d", tc.in, f.askedLimit, tc.want)
		}
	}
}

// Unlike the cluster log, this call carries the limit upstream: two limits are
// two different answers and must not share one cache entry.
func TestServiceGuestTasksKeysItsCacheOnTheLimit(t *testing.T) {
	f := newFake()
	svc := newFakeService(t, f, newTestClock())
	ctx := context.Background()

	if _, err := svc.GuestTasks(ctx, "preproduction", 102, 25); err != nil {
		t.Fatalf("GuestTasks: %v", err)
	}
	if _, err := svc.GuestTasks(ctx, "preproduction", 102, 25); err != nil {
		t.Fatalf("GuestTasks: %v", err)
	}
	if got := f.count("nodeTasks"); got != 1 {
		t.Fatalf("fetched %d times for one limit, want 1", got)
	}
	if _, err := svc.GuestTasks(ctx, "preproduction", 102, 10); err != nil {
		t.Fatalf("GuestTasks: %v", err)
	}
	if got := f.count("nodeTasks"); got != 2 {
		t.Fatalf("fetched %d times for two limits, want 2", got)
	}
}

// An unknown guest is a 404 decided from the cluster listing, and no request
// naming it ever leaves.
func TestServiceGuestTasksRejectsAnUnknownGuest(t *testing.T) {
	f := newFake()
	svc := newFakeService(t, f, newTestClock())

	_, err := svc.GuestTasks(context.Background(), "preproduction", 999, 25)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if got := f.count("nodeTasks"); got != 0 {
		t.Errorf("tasks fetched %d times for an unknown guest, want none", got)
	}
}

// This log is essential to the view it fills: unlike an optional field, an
// unreadable one leaves nothing to show, so the failure is propagated rather
// than served as "no recent task".
func TestServiceGuestTasksPropagatesItsFailure(t *testing.T) {
	f := newFake()
	f.nodeTasksErr = errors.New("boom")
	svc := newFakeService(t, f, newTestClock())

	if _, err := svc.GuestTasks(context.Background(), "preproduction", 102, 25); err == nil {
		t.Fatal("GuestTasks returned no error")
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct {
		in, want int
	}{
		{in: 0, want: DefaultTaskLimit},
		{in: -1, want: DefaultTaskLimit},
		{in: 10, want: 10},
		{in: MaxTaskLimit + 1, want: MaxTaskLimit},
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
	svc := NewService(nil, 0, 0)
	if svc.views.ttl != DefaultTTL {
		t.Fatalf("ttl is %v, want %v", svc.views.ttl, DefaultTTL)
	}
	if svc.threshold != config.DefaultThreshold {
		t.Fatalf("threshold is %v, want %v", svc.threshold, config.DefaultThreshold)
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

func TestServiceClusterSeriesFoldsEveryNode(t *testing.T) {
	f := newFake()
	f.pointsByNode = map[string][]proxmox.RRDPoint{
		"pve-1": {{Time: 100, CPU: floatPtr(1), MaxCPU: floatPtr(4), MemUsed: uintPtr(3), MemTotal: uintPtr(8)}},
		"pve-2": {{Time: 100, CPU: floatPtr(0), MaxCPU: floatPtr(4), MemUsed: uintPtr(1), MemTotal: uintPtr(8)}},
	}
	svc := newFakeService(t, f, newTestClock())

	series, err := svc.ClusterSeries(context.Background(), "preproduction", proxmox.TimeframeHour)
	if err != nil {
		t.Fatalf("ClusterSeries: %v", err)
	}
	if len(series.Points) != 1 {
		t.Fatalf("got %d points, want the one step both nodes recorded", len(series.Points))
	}
	if series.Points[0].CPU == nil || *series.Points[0].CPU != 0.5 {
		t.Fatalf("cpu is %v, want the mean of both nodes", series.Points[0].CPU)
	}
	if series.Points[0].MemTotal == nil || *series.Points[0].MemTotal != 16 {
		t.Fatalf("memory total is %v, want the sum of both nodes", series.Points[0].MemTotal)
	}
	if f.count("nodeRRD") != 2 {
		t.Fatalf("nodeRRD called %d times, want once per node", f.count("nodeRRD"))
	}
}

func TestServiceClusterSeriesSurvivesANodeItMayNotRead(t *testing.T) {
	// Sys.Audit is granted on one node and not on the other: the curve loses
	// that node's share, not the chart.
	f := newFake()
	f.pointsByNode = map[string][]proxmox.RRDPoint{
		"pve-1": {{Time: 100, CPU: floatPtr(0.25), MaxCPU: floatPtr(4)}},
	}
	f.pointsErrByNode = map[string]error{"pve-2": errors.New("http 403 Forbidden")}
	svc := newFakeService(t, f, newTestClock())

	series, err := svc.ClusterSeries(context.Background(), "preproduction", proxmox.TimeframeHour)
	if err != nil {
		t.Fatalf("ClusterSeries: %v", err)
	}
	if len(series.Points) != 1 || series.Points[0].CPU == nil || *series.Points[0].CPU != 0.25 {
		t.Fatalf("points are %+v, want the readable node alone", series.Points)
	}
}

func TestServiceClusterSeriesFailsWhenNoNodeAnswers(t *testing.T) {
	// Serving an empty hour here would draw a cluster that was never idle.
	f := newFake()
	f.pointsErr = errors.New("http 403 Forbidden")
	svc := newFakeService(t, f, newTestClock())

	if _, err := svc.ClusterSeries(context.Background(), "preproduction", proxmox.TimeframeHour); err == nil {
		t.Fatal("ClusterSeries succeeded, want the failure of every node")
	}
}

func TestServiceClusterSeriesWithoutAnOnlineNode(t *testing.T) {
	f := newFake()
	f.status = []proxmox.ClusterStatusEntry{
		{Type: proxmox.ClusterStatusTypeCluster, Name: "pprd", Nodes: 2},
		{Type: proxmox.ClusterStatusTypeNode, Name: "pve-1", Online: false},
		{Type: proxmox.ClusterStatusTypeNode, Name: "pve-2", Online: false},
	}
	svc := newFakeService(t, f, newTestClock())

	series, err := svc.ClusterSeries(context.Background(), "preproduction", proxmox.TimeframeHour)
	if err != nil {
		t.Fatalf("ClusterSeries: %v", err)
	}
	if len(series.Points) != 0 {
		t.Fatalf("points are %+v, want none", series.Points)
	}
	if f.count("nodeRRD") != 0 {
		t.Fatalf("nodeRRD called %d times, want none: a node that is down has nothing to tell", f.count("nodeRRD"))
	}
}

func TestServiceClusterSeriesSharesTheCacheWithTheNodeView(t *testing.T) {
	// A card and an open node view ask for the same hour of the same node:
	// that must cost the cluster one read, not two.
	f := newFake()
	svc := newFakeService(t, f, newTestClock())
	ctx := context.Background()

	if _, err := svc.NodeSeries(ctx, "preproduction", "pve-1", proxmox.TimeframeHour); err != nil {
		t.Fatalf("NodeSeries: %v", err)
	}
	if _, err := svc.ClusterSeries(ctx, "preproduction", proxmox.TimeframeHour); err != nil {
		t.Fatalf("ClusterSeries: %v", err)
	}
	if f.count("nodeRRD") != 2 {
		t.Fatalf("nodeRRD called %d times, want 2: one per node, the first one reused", f.count("nodeRRD"))
	}
}

func TestServiceGuestListsTheVolumes(t *testing.T) {
	f := newFake()
	svc := newFakeService(t, f, newTestClock())

	guest, err := svc.Guest(context.Background(), "preproduction", 102)
	if err != nil {
		t.Fatalf("Guest: %v", err)
	}
	if len(guest.Disks) != 2 {
		t.Fatalf("disks are %+v, want the system disk and the detached volume, the CD-ROM left out", guest.Disks)
	}
	if guest.Allocated == nil || guest.Allocated.Bytes != 32<<30 || guest.Allocated.Detached != 1 {
		t.Fatalf("allocation is %+v, want 32 GiB attached and one volume detached", guest.Allocated)
	}
}

func TestServiceGuestWithoutVMAuditKeepsTheRest(t *testing.T) {
	f := newFake()
	// A token scoped without VM.Audit on the guest: PVE answers 403. That
	// costs the volume list, never the page.
	f.configErr = errors.New("http 403 Forbidden")
	svc := newFakeService(t, f, newTestClock())

	guest, err := svc.Guest(context.Background(), "preproduction", 102)
	if err != nil {
		t.Fatalf("Guest: %v", err)
	}
	if guest.Disks != nil || guest.Allocated != nil {
		t.Fatalf("disks/allocation are %+v/%+v, want nil", guest.Disks, guest.Allocated)
	}
	if guest.Name != "web" || guest.Memory.Total != 2 {
		t.Fatalf("the guest figures were lost: %+v", guest)
	}
}

func TestServiceGuestReadsOneConfigurationForManyReaders(t *testing.T) {
	f := newFake()
	svc := newFakeService(t, f, newTestClock())

	// Ten tabs open on the same guest: the configuration is read once, like
	// every other call of this service.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Guest(context.Background(), "preproduction", 102); err != nil {
				t.Errorf("Guest: %v", err)
			}
		}()
	}
	wg.Wait()

	if n := f.count("guestConfig"); n != 1 {
		t.Fatalf("the configuration was read %d times, want once", n)
	}
}

// TestServiceKeepsTheLastHAStatus: the HA call is the most fragile of the three
// a cluster view makes, and the one whose absence is most visible. Without it a
// node being drained reads as plain "online" on its own page, and the
// maintenance plan offers it as a destination. One failed call must not do that.
func TestServiceKeepsTheLastHAStatus(t *testing.T) {
	clock := newTestClock()
	f := newFake()
	f.ha = &proxmox.HAManagerStatus{NodeStatus: map[string]string{"pve-1": proxmox.HANodeMaintenance}}
	svc := newFakeService(t, f, clock)
	ctx := context.Background()

	node, err := svc.Node(ctx, "preproduction", "pve-1")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if node.Status != aggregate.NodeMaintenance {
		t.Fatalf("status = %q, want %q", node.Status, aggregate.NodeMaintenance)
	}

	// The HA endpoint now fails, and the cached view has expired.
	f.haErr = errors.New("http 500 Internal Server Error")
	f.ha = nil
	clock.advance(10 * time.Second)

	node, err = svc.Node(ctx, "preproduction", "pve-1")
	if err != nil {
		t.Fatalf("Node after the HA failure: %v", err)
	}
	if node.Status != aggregate.NodeMaintenance {
		t.Fatalf("status = %q after one failed HA call, want it kept at %q",
			node.Status, aggregate.NodeMaintenance)
	}

	// Past the staleness window the remembered answer is dropped: claiming a
	// node is still draining on a minute-old reading would be worse than
	// saying nothing.
	clock.advance(aggregate.StaleAfter + time.Second)
	node, err = svc.Node(ctx, "preproduction", "pve-1")
	if err != nil {
		t.Fatalf("Node long after the HA failure: %v", err)
	}
	if node.Status == aggregate.NodeMaintenance {
		t.Fatal("the maintenance state survived the staleness window")
	}
}

// TestPlanUsesTheConfiguredThreshold: the plan and the overview must agree on
// what counts as full. The plan used a constant of its own, so a cluster
// configured at 0.9 had its cards warn at ninety per cent while the plan
// refused placements at eighty -- two answers to the same question, and the
// payload said 0.80 where /api/overview said 0.90.
func TestPlanUsesTheConfiguredThreshold(t *testing.T) {
	generous := newService(map[string]clusterClient{"preproduction": newFake()}, 5*time.Second, 0.9, newTestClock().Now)
	plan, err := generous.MaintenancePlan(context.Background(), "preproduction", "pve-1")
	if err != nil {
		t.Fatalf("MaintenancePlan: %v", err)
	}
	if plan.Threshold != 0.9 {
		t.Fatalf("threshold = %v, want the configured 0.9", plan.Threshold)
	}

	strict := newService(map[string]clusterClient{"preproduction": newFake()}, 5*time.Second, 0, newTestClock().Now)
	plan, err = strict.MaintenancePlan(context.Background(), "preproduction", "pve-1")
	if err != nil {
		t.Fatalf("MaintenancePlan: %v", err)
	}
	if plan.Threshold != config.DefaultThreshold {
		t.Fatalf("threshold = %v, want the default %v", plan.Threshold, config.DefaultThreshold)
	}
}

// TestServiceKeepsTheDrainedNodeOutOfThePlan: same failure, seen from the
// maintenance plan, which must not offer a node that is itself being drained.
func TestServiceKeepsTheDrainedNodeOutOfThePlan(t *testing.T) {
	clock := newTestClock()
	f := newFake()
	f.ha = &proxmox.HAManagerStatus{NodeStatus: map[string]string{"pve-2": proxmox.HANodeMaintenance}}
	svc := newFakeService(t, f, clock)
	ctx := context.Background()

	if _, err := svc.MaintenancePlan(ctx, "preproduction", "pve-1"); err != nil {
		t.Fatalf("MaintenancePlan: %v", err)
	}

	f.haErr = errors.New("http 500 Internal Server Error")
	f.ha = nil
	clock.advance(10 * time.Second)

	plan, err := svc.MaintenancePlan(ctx, "preproduction", "pve-1")
	if err != nil {
		t.Fatalf("MaintenancePlan after the HA failure: %v", err)
	}
	for _, target := range plan.Targets {
		if target.Name == "pve-2" {
			t.Fatal("a node in maintenance was offered as a destination after one failed HA call")
		}
	}
}

// TestServiceRetriesUpdatesAfterATimeout: apt/update is the slowest call of the
// set and its answer is cached for five minutes, because pending packages
// change about once a day. A single slow answer is not five minutes of news:
// remembering it that long left the node page saying "unknown" long after the
// cluster had recovered, while the overview had moved on.
func TestServiceRetriesUpdatesAfterATimeout(t *testing.T) {
	clock := newTestClock()
	f := newFake()
	f.updatesErr = errors.New("http 504 Gateway Timeout")
	svc := newFakeService(t, f, clock)
	ctx := context.Background()

	node, err := svc.Node(ctx, "preproduction", "pve-1")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if node.PendingUpdates != nil {
		t.Fatalf("pendingUpdates = %v, want nil after a failed call", node.PendingUpdates)
	}

	// The cluster recovers, and the page is refreshed a couple of times.
	f.updatesErr = nil
	clock.advance(10 * time.Second)

	node, err = svc.Node(ctx, "preproduction", "pve-1")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if node.PendingUpdates == nil {
		t.Fatal("pendingUpdates is still unknown: the timeout was remembered for the value's ttl")
	}
}

// TestEveryMethodBoundsItsRequest: one slow cluster must not hold a connection
// for as long as PVE cares to take, so each entry point wraps the caller's
// context in the service budget. MaintenancePlan was the only one that did
// not -- and it is the one behind a button, which is to say the one a person
// is waiting on.
//
// The bound is observed from the CALLER's side, not from the client's: the
// caches deliberately detach the context of whoever triggered a fetch, so that
// one reader giving up does not cancel the call the others are waiting on.
// What the budget governs is therefore how long the METHOD waits, and the way
// to see it is to hold the upstream call and watch the method give up.
func TestEveryMethodBoundsItsRequest(t *testing.T) {
	ctx := context.Background()

	calls := []struct {
		name string
		call func(*Service) error
	}{
		{"Node", func(s *Service) error { _, err := s.Node(ctx, "preproduction", "pve-1"); return err }},
		{"Guest", func(s *Service) error { _, err := s.Guest(ctx, "preproduction", 102); return err }},
		{"NodeSeries", func(s *Service) error {
			_, err := s.NodeSeries(ctx, "preproduction", "pve-1", proxmox.TimeframeHour)
			return err
		}},
		{"ClusterSeries", func(s *Service) error {
			_, err := s.ClusterSeries(ctx, "preproduction", proxmox.TimeframeHour)
			return err
		}},
		{"GuestSeries", func(s *Service) error {
			_, err := s.GuestSeries(ctx, "preproduction", 102, proxmox.TimeframeHour)
			return err
		}},
		{"Tasks", func(s *Service) error { _, err := s.Tasks(ctx, "preproduction", 10); return err }},
		{"GuestTasks", func(s *Service) error { _, err := s.GuestTasks(ctx, "preproduction", 102, 10); return err }},
		{"MaintenancePlan", func(s *Service) error {
			_, err := s.MaintenancePlan(ctx, "preproduction", "pve-1")
			return err
		}},
	}
	for _, tc := range calls {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// The upstream call never answers. Nothing here sleeps: the only
			// way out of the wait is the deadline the method sets itself, and
			// a method that sets none would hang until the test binary is
			// killed -- which is the failure, loudly.
			held := make(chan struct{})
			defer close(held)

			f := newFake()
			f.gate = func() { <-held }
			svc := newFakeService(t, f, newTestClock())
			svc.budget = 20 * time.Millisecond

			err := tc.call(svc)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%s returned %v, want a deadline: it waits on its own budget", tc.name, err)
			}
		})
	}
}

// The point of the whole feature: "vmbr12" is not a network anybody
// recognises, "DMZ publique" is. A bridge nobody named keeps its own name —
// rendering a dash there would hide a usable one.
func TestServiceGuestNamesTheNetworksItIsWiredTo(t *testing.T) {
	f := newFake()
	f.config = proxmox.GuestConfig{
		"scsi0": "ceph-vm:vm-102-disk-0,size=32G",
		"net0":  "virtio=BC:24:11:AA:BB:CC,bridge=vmbr0,firewall=1",
		"net1":  "virtio=BC:24:11:AA:BB:DD,bridge=vmbr1,tag=120",
		"net2":  "virtio=BC:24:11:AA:BB:EE,bridge=vnet-adm",
	}
	f.networks = []proxmox.NodeNetwork{
		// PVE keeps the newline the interfaces file carries.
		{Iface: "vmbr1", Type: "bridge", Comments: "DMZ publique\n"},
		{Iface: "vmbr0", Type: "bridge", Comments: "   "},
	}
	f.vnets = []proxmox.SDNVNet{
		{VNet: "vnet-adm", Alias: "Administration", Zone: "zone-a"},
		{VNet: "vnet-other", Alias: "Pas la nôtre"},
	}
	svc := newFakeService(t, f, newTestClock())

	guest, err := svc.Guest(context.Background(), "preproduction", 102)
	if err != nil {
		t.Fatalf("Guest: %v", err)
	}
	if len(guest.Nets) != 3 {
		t.Fatalf("Nets = %+v, want three cards", guest.Nets)
	}

	byKey := make(map[string]GuestNet, len(guest.Nets))
	for _, net := range guest.Nets {
		byKey[net.Key] = net
	}

	// A bridge with nothing but blanks for a comment is a bridge with no
	// alias, not an alias made of spaces.
	if got := byKey["net0"]; got.Alias != nil || got.Bridge == nil || *got.Bridge != "vmbr0" {
		t.Errorf("net0 = %+v, want bridge vmbr0 and no alias", got)
	}
	if got := byKey["net1"]; got.Alias == nil || *got.Alias != "DMZ publique" {
		t.Errorf("net1 alias = %v, want the node comment, trimmed", got.Alias)
	}
	if got := byKey["net1"]; got.Tag == nil || *got.Tag != 120 {
		t.Errorf("net1 tag = %v, want 120", got.Tag)
	}
	if got := byKey["net2"]; got.Alias == nil || *got.Alias != "Administration" {
		t.Errorf("net2 alias = %v, want the SDN alias", got.Alias)
	}
}

// The tables are read per CLUSTER and per NODE. A hundred guests on one node
// must not mean a hundred readings of the same two documents.
func TestServiceGuestReadsTheNetworkTablesOncePerClusterAndNode(t *testing.T) {
	f := newFake()
	f.config = proxmox.GuestConfig{"net0": "virtio=BC:24:11:AA:BB:CC,bridge=vmbr0"}
	svc := newFakeService(t, f, newTestClock())
	ctx := context.Background()

	// 102 sits on pve-1 and 101 on pve-2, and each is asked for twice.
	for _, vmid := range []int{102, 101, 102, 101} {
		if _, err := svc.Guest(ctx, "preproduction", vmid); err != nil {
			t.Fatalf("Guest %d: %v", vmid, err)
		}
	}

	if got := f.count("sdnVNets"); got != 1 {
		t.Errorf("sdn vnets fetched %d times, want 1: the table is keyed by cluster", got)
	}
	if got := f.count("nodeNetworks"); got != 2 {
		t.Errorf("node networks fetched %d times, want 2: one per node, not one per guest", got)
	}
}

// Both lookups are optional. Losing them costs the names and nothing else:
// the cards, their bridges and the rest of the page are still served.
func TestServiceGuestSurvivesUnreadableNetworkTables(t *testing.T) {
	f := newFake()
	f.config = proxmox.GuestConfig{"net0": "virtio=BC:24:11:AA:BB:CC,bridge=vmbr0,tag=7"}
	f.vnetsErr = errors.New("boom")
	f.networksErr = errors.New("boom")
	svc := newFakeService(t, f, newTestClock())

	guest, err := svc.Guest(context.Background(), "preproduction", 102)
	if err != nil {
		t.Fatalf("Guest returned %v, want the page served without its network names", err)
	}
	if len(guest.Nets) != 1 {
		t.Fatalf("Nets = %+v, want the card even with no table to name it", guest.Nets)
	}
	net := guest.Nets[0]
	if net.Alias != nil {
		t.Errorf("alias = %v, want nil when the table could not be read", *net.Alias)
	}
	if net.Bridge == nil || *net.Bridge != "vmbr0" {
		t.Errorf("bridge = %v, want vmbr0: the configuration alone carries it", net.Bridge)
	}
	if net.Tag == nil || *net.Tag != 7 {
		t.Errorf("tag = %v, want 7", net.Tag)
	}
}
