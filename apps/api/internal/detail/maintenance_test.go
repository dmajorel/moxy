package detail

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
	"github.com/dmajorel/moxy/apps/api/internal/maintenance"
	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

// fakeExecutor stands in for *maintenance.Service: it records the request it
// was handed and answers whatever the test put in it.
//
// The point of the narrow MaintenanceExecutor interface is exactly this: the
// conversion and the state this package builds are testable with no key
// source, no runner and no session to open on anything.
type fakeExecutor struct {
	enabled map[string]bool
	calls   int
	got     maintenance.Request
	outcome *maintenance.Outcome
	err     error
}

func (f *fakeExecutor) Enabled(cluster string) bool { return f.enabled[cluster] }

func (f *fakeExecutor) Execute(_ context.Context, req maintenance.Request) (*maintenance.Outcome, error) {
	f.calls++
	f.got = req
	return f.outcome, f.err
}

// newExecutableService wires a fake cluster whose maintenance is turned on.
func newExecutableService(t *testing.T, exec MaintenanceExecutor) (*Service, *fakeClient) {
	t.Helper()
	client := newFake()
	svc := newFakeService(t, client, newTestClock())
	svc.SetMaintenance(exec)
	return svc, client
}

func TestExecuteMaintenanceConvertsTheOutcome(t *testing.T) {
	requested := time.Date(2026, 9, 16, 9, 12, 4, 0, time.UTC)
	output := "queued\n"
	exec := &fakeExecutor{
		enabled: map[string]bool{"preproduction": true},
		outcome: &maintenance.Outcome{
			Cluster:        "preproduction",
			Node:           "pve-1",
			Action:         maintenance.ActionEnable,
			RequestedAt:    requested,
			Via:            "pve-2",
			Accepted:       true,
			AlreadyInState: false,
			Output:         &output,
		},
	}
	svc, _ := newExecutableService(t, exec)

	got, err := svc.ExecuteMaintenance(context.Background(), "preproduction", "pve-1", maintenance.ActionEnable)
	if err != nil {
		t.Fatalf("ExecuteMaintenance: %v", err)
	}
	want := MaintenanceResult{
		Cluster:     "preproduction",
		Node:        "pve-1",
		Action:      maintenance.ActionEnable,
		RequestedAt: requested,
		Via:         "pve-2",
		Accepted:    true,
	}
	want.Output = got.Output
	if *got != want {
		t.Errorf("result = %+v, want %+v", *got, want)
	}
	if got.Output == nil || *got.Output != output {
		t.Fatalf("output = %v, want %q", got.Output, output)
	}
	// Copied, not aliased: the payload outlives the call, and a caller able to
	// reach back into the outcome could edit an answer already serialised.
	if got.Output == exec.outcome.Output {
		t.Error("output points at the outcome's own string, want a copy")
	}
}

// A guest of the contract: nil output means UNKNOWN — nothing ran — and never
// an empty answer.
func TestExecuteMaintenanceKeepsAnUnknownOutputNil(t *testing.T) {
	exec := &fakeExecutor{
		enabled: map[string]bool{"preproduction": true},
		outcome: &maintenance.Outcome{
			Cluster:        "preproduction",
			Node:           "pve-1",
			Action:         maintenance.ActionEnable,
			Accepted:       true,
			AlreadyInState: true,
		},
	}
	svc, _ := newExecutableService(t, exec)

	got, err := svc.ExecuteMaintenance(context.Background(), "preproduction", "pve-1", maintenance.ActionEnable)
	if err != nil {
		t.Fatalf("ExecuteMaintenance: %v", err)
	}
	if !got.AlreadyInState || got.Output != nil || got.Via != "" {
		t.Errorf("result = %+v, want already in state with no output and no via", *got)
	}
}

// The kind is the whole vocabulary the HTTP layer picks a status code from and
// the frontend translates: an error wrapped so that KindOf no longer finds one
// leaves the browser with a 500 and nothing to say.
func TestExecuteMaintenanceKeepsTheKind(t *testing.T) {
	for _, kind := range []maintenance.Kind{
		maintenance.KindNoQuorum,
		maintenance.KindNoHAManager,
		maintenance.KindAlreadyRunning,
		maintenance.KindCommandFailed,
		maintenance.KindTimeout,
	} {
		exec := &fakeExecutor{
			enabled: map[string]bool{"preproduction": true},
			err:     &maintenance.Error{Cluster: "preproduction", Node: "pve-1", Kind: kind},
		}
		svc, _ := newExecutableService(t, exec)

		_, err := svc.ExecuteMaintenance(context.Background(), "preproduction", "pve-1", maintenance.ActionEnable)
		got, ok := maintenance.KindOf(err)
		if !ok || got != kind {
			t.Errorf("KindOf(%v) = %q, %v, want %q", err, got, ok, kind)
		}
	}
}

// A cluster that does not take part has no route at all, and a verb outside
// the two is the caller's mistake. Neither may cost a call against PVE: the
// refusals of §6 happen before anything is fetched, let alone connected to.
func TestExecuteMaintenanceRefusesBeforeReadingAnything(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		cluster string
		action  string
		want    error
	}{
		{"cluster not enrolled", false, "preproduction", maintenance.ActionEnable, maintenance.ErrNotFound},
		{"unknown cluster", true, "nowhere", maintenance.ActionEnable, maintenance.ErrNotFound},
		{"unknown action", true, "preproduction", "reboot", maintenance.ErrInvalidAction},
		{"empty action", true, "preproduction", "", maintenance.ErrInvalidAction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := &fakeExecutor{enabled: map[string]bool{"preproduction": tc.enabled}}
			svc, client := newExecutableService(t, exec)

			_, err := svc.ExecuteMaintenance(context.Background(), tc.cluster, "pve-1", tc.action)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ExecuteMaintenance returned %v, want %v", err, tc.want)
			}
			if exec.calls != 0 {
				t.Errorf("executor called %d times, want none", exec.calls)
			}
			if got := client.count("status"); got != 0 {
				t.Errorf("cluster status read %d times, want none", got)
			}
		})
	}
}

// Without a maintenance service wired at all — no maintenance block in the
// configuration — every cluster answers as if it were not enrolled.
func TestExecuteMaintenanceWithoutAnExecutor(t *testing.T) {
	svc := newFakeService(t, newFake(), newTestClock())

	_, err := svc.ExecuteMaintenance(context.Background(), "preproduction", "pve-1", maintenance.ActionEnable)
	if !errors.Is(err, maintenance.ErrNotFound) {
		t.Fatalf("ExecuteMaintenance returned %v, want ErrNotFound", err)
	}
}

// What the executor decides on must be what the overview shows: the shared
// rules of aggregate/rules.go, read once, not a second reading made here.
func TestExecuteMaintenanceHandsOverTheSharedRules(t *testing.T) {
	exec := &fakeExecutor{
		enabled: map[string]bool{"preproduction": true},
		outcome: &maintenance.Outcome{Cluster: "preproduction", Node: "pve-1", Accepted: true},
	}
	client := newFake()
	// pve-2 is being drained, and a third node is known to /cluster/resources
	// alone — the shape PVE leaves behind for a node that has just joined or
	// just left, which the shared rule calls unknown rather than online.
	client.resources = append(client.resources, proxmox.Resource{Type: proxmox.ResourceTypeNode, Node: "pve-3"})
	client.ha = &proxmox.HAManagerStatus{NodeStatus: map[string]string{
		"pve-1": proxmox.HANodeOnline,
		"pve-2": proxmox.HANodeMaintenance,
	}}
	svc := newFakeService(t, client, newTestClock())
	svc.SetMaintenance(exec)

	if _, err := svc.ExecuteMaintenance(context.Background(), "preproduction", "pve-1", maintenance.ActionDisable); err != nil {
		t.Fatalf("ExecuteMaintenance: %v", err)
	}

	got := exec.got
	if got.Cluster != "preproduction" || got.Node != "pve-1" || got.Action != maintenance.ActionDisable {
		t.Errorf("request = %+v, want the cluster, node and action as asked", got)
	}
	if got.State.Quorum == nil || !got.State.Quorum.Quorate {
		t.Errorf("quorum = %+v, want the quorate cluster", got.State.Quorum)
	}
	if !got.State.HAManager {
		t.Error("HAManager = false, want true: the fake cluster runs a CRM")
	}
	want := []maintenance.NodeState{
		{Name: "pve-1", Status: aggregate.NodeOnline},
		{Name: "pve-2", Status: aggregate.NodeMaintenance},
		{Name: "pve-3", Status: aggregate.NodeUnknown},
	}
	if len(got.State.Nodes) != len(want) {
		t.Fatalf("nodes = %+v, want %+v", got.State.Nodes, want)
	}
	for i, node := range want {
		if got.State.Nodes[i] != node {
			t.Errorf("node %d = %+v, want %+v", i, got.State.Nodes[i], node)
		}
	}
}

// "haState nil everywhere" is the wording of the refusal, and this is what
// makes it true: a manager status that names no node leaves every node page
// showing an em dash, so the route must not claim a CRM is there to honour
// the command.
func TestExecuteMaintenanceSeesNoHAManager(t *testing.T) {
	cases := map[string]*proxmox.HAManagerStatus{
		"absent": nil,
		"empty":  {NodeStatus: map[string]string{}},
	}
	for name, ha := range cases {
		t.Run(name, func(t *testing.T) {
			exec := &fakeExecutor{
				enabled: map[string]bool{"preproduction": true},
				outcome: &maintenance.Outcome{Cluster: "preproduction", Node: "pve-1"},
			}
			client := newFake()
			client.ha, client.haErr = ha, nil
			svc := newFakeService(t, client, newTestClock())
			svc.SetMaintenance(exec)

			if _, err := svc.ExecuteMaintenance(context.Background(), "preproduction", "pve-1", maintenance.ActionEnable); err != nil {
				t.Fatalf("ExecuteMaintenance: %v", err)
			}
			if exec.got.State.HAManager {
				t.Error("HAManager = true, want false")
			}
		})
	}
}

// The flag decides whether the UI draws a button at all, so it must follow the
// configuration and nothing else.
func TestNodeReportsWhetherMaintenanceCanRun(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		svc, _ := newExecutableService(t, &fakeExecutor{enabled: map[string]bool{"preproduction": enabled}})

		node, err := svc.Node(context.Background(), "preproduction", "pve-1")
		if err != nil {
			t.Fatalf("Node: %v", err)
		}
		if node.MaintenanceExecutable != enabled {
			t.Errorf("maintenanceExecutable = %v, want %v", node.MaintenanceExecutable, enabled)
		}
	}
}

func TestNodeWithoutAnExecutorIsNotExecutable(t *testing.T) {
	svc := newFakeService(t, newFake(), newTestClock())

	node, err := svc.Node(context.Background(), "preproduction", "pve-1")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if node.MaintenanceExecutable {
		t.Error("maintenanceExecutable = true with no maintenance configured")
	}
}

/* ----------------------------------------------------------------- mock */

func TestMockExecuteMaintenanceAccepts(t *testing.T) {
	mock, _ := newMock(t)

	got, err := mock.ExecuteMaintenance(context.Background(), "qualification", "prox-qual-2201-cit", maintenance.ActionEnable)
	if err != nil {
		t.Fatalf("ExecuteMaintenance: %v", err)
	}
	if !got.Accepted || got.AlreadyInState {
		t.Errorf("result = %+v, want accepted and not already in state", *got)
	}
	// Never the node being drained: the command runs elsewhere, and the demo
	// has to show which node answered.
	if got.Via == "" || got.Via == got.Node {
		t.Errorf("via = %q, want another node of the cluster", got.Via)
	}
	if got.Output == nil {
		t.Error("output = nil, want the empty output ha-manager prints")
	}
	if got.RequestedAt != mock.base {
		t.Errorf("requestedAt = %v, want the frozen base %v", got.RequestedAt, mock.base)
	}
}

// The other half of the sample: a node whose drain fails, because a mock that
// always succeeds lets through a UI that cannot show a failure.
func TestMockExecuteMaintenanceFails(t *testing.T) {
	mock, _ := newMock(t)

	_, err := mock.ExecuteMaintenance(context.Background(), "preproduction", mockMaintenanceFailure, maintenance.ActionEnable)
	kind, ok := maintenance.KindOf(err)
	if !ok || kind != maintenance.KindCommandFailed {
		t.Fatalf("ExecuteMaintenance returned %v (kind %q), want command_failed", err, kind)
	}
}

// The sample node the tree shows as drained is the one that reports itself
// already in state — read before anything runs, and not an error.
func TestMockExecuteMaintenanceIsAlreadyInState(t *testing.T) {
	mock, overview := newMock(t)
	drained := ""
	for _, cluster := range overview.Clusters {
		if cluster.ID != "preproduction" {
			continue
		}
		for _, node := range cluster.Nodes {
			if node.Status == aggregate.NodeMaintenance {
				drained = node.Name
			}
		}
	}
	if drained == "" {
		t.Fatal("the sample overview has no drained node in preproduction")
	}

	got, err := mock.ExecuteMaintenance(context.Background(), "preproduction", drained, maintenance.ActionEnable)
	if err != nil {
		t.Fatalf("ExecuteMaintenance: %v", err)
	}
	if !got.Accepted || !got.AlreadyInState || got.Via != "" || got.Output != nil {
		t.Errorf("result = %+v, want accepted, already in state, nothing run", *got)
	}
}

// Two of the four sample clusters are enrolled, and the two that are not are
// one healthy and one broken: a frontend must not be able to conclude that the
// button follows cluster health.
func TestMockMaintenanceExecutableFollowsTheConfiguration(t *testing.T) {
	mock, overview := newMock(t)

	for _, cluster := range overview.Clusters {
		want := mockMaintenanceClusters[cluster.ID].Enabled
		for _, node := range cluster.Nodes {
			got, err := mock.Node(context.Background(), cluster.ID, node.Name)
			if err != nil {
				t.Fatalf("Node %s/%s: %v", cluster.ID, node.Name, err)
			}
			if got.MaintenanceExecutable != want {
				t.Errorf("%s/%s maintenanceExecutable = %v, want %v", cluster.ID, node.Name, got.MaintenanceExecutable, want)
			}
		}
		if want {
			continue
		}
		_, err := mock.ExecuteMaintenance(context.Background(), cluster.ID, cluster.Nodes[0].Name, maintenance.ActionEnable)
		if !errors.Is(err, maintenance.ErrNotFound) {
			t.Errorf("%s: ExecuteMaintenance returned %v, want ErrNotFound", cluster.ID, err)
		}
	}
	if len(mockMaintenanceClusters) < 2 {
		t.Fatalf("enrolled clusters = %d, want the demo to show both states", len(mockMaintenanceClusters))
	}
}

func TestMockExecuteMaintenanceRefusesAnUnknownAction(t *testing.T) {
	mock, _ := newMock(t)

	_, err := mock.ExecuteMaintenance(context.Background(), "qualification", "prox-qual-2201-cit", "reboot")
	if !errors.Is(err, maintenance.ErrInvalidAction) {
		t.Fatalf("ExecuteMaintenance returned %v, want ErrInvalidAction", err)
	}
}

func TestMockExecuteMaintenanceRefusesAnUnknownNode(t *testing.T) {
	mock, _ := newMock(t)

	_, err := mock.ExecuteMaintenance(context.Background(), "qualification", "prox-qual-9999-cit", maintenance.ActionEnable)
	if !errors.Is(err, maintenance.ErrNotFound) {
		t.Fatalf("ExecuteMaintenance returned %v, want ErrNotFound", err)
	}
}
