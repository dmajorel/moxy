package detail

import (
	"testing"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
	"github.com/dmajorel/moxy/apps/api/internal/config"
	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

const gib = 1024 * 1024 * 1024

func nodeResource(name string, usedGiB, totalGiB int64) proxmox.Resource {
	return proxmox.Resource{
		Type:   proxmox.ResourceTypeNode,
		Node:   name,
		Name:   name,
		Status: proxmox.StatusOnline,
		Mem:    proxmox.FlexInt(usedGiB * gib),
		MaxMem: proxmox.FlexInt(totalGiB * gib),
	}
}

func guestResource(vmid int64, node string, maxMemGiB int64, status string) proxmox.Resource {
	return proxmox.Resource{
		Type:   proxmox.ResourceTypeQemu,
		Node:   node,
		Name:   "guest-" + status,
		VMID:   proxmox.FlexInt(vmid),
		Status: status,
		MaxMem: proxmox.FlexInt(maxMemGiB * gib),
	}
}

func statusEntries(names ...string) []proxmox.ClusterStatusEntry {
	entries := []proxmox.ClusterStatusEntry{
		{Type: proxmox.ClusterStatusTypeCluster, Name: "c", Nodes: proxmox.FlexInt(int64(len(names))), Quorate: proxmox.FlexBool(true)},
	}
	for _, name := range names {
		entries = append(entries, proxmox.ClusterStatusEntry{
			Type:   proxmox.ClusterStatusTypeNode,
			Name:   name,
			Online: proxmox.FlexBool(true),
		})
	}
	return entries
}

func TestPlanSpreadsGuestsAndChecksCapacity(t *testing.T) {
	view := clusterView{
		Status: statusEntries("n1", "n2", "n3"),
		Resources: []proxmox.Resource{
			nodeResource("n1", 20, 128),
			nodeResource("n2", 30, 128),
			nodeResource("n3", 40, 128),
			guestResource(100, "n1", 8, proxmox.StatusRunning),
			guestResource(101, "n1", 16, proxmox.StatusRunning),
			guestResource(102, "n1", 4, proxmox.StatusRunning),
		},
	}

	plan := buildPlan("c", "n1", view, config.DefaultMemoryThreshold)
	if plan == nil {
		t.Fatal("no plan for a known node")
	}

	if !plan.Feasible {
		t.Errorf("plan is not feasible: %+v", plan)
	}
	if len(plan.Moves) != 3 {
		t.Fatalf("moves = %d, want 3", len(plan.Moves))
	}
	// Largest first: 16 GiB is considered before 8 and 4.
	if plan.Moves[0].VMID != 101 {
		t.Errorf("first move = %d, want the largest guest", plan.Moves[0].VMID)
	}
	for _, move := range plan.Moves {
		if !move.Placed || move.Target == "" {
			t.Errorf("guest %d was not placed", move.VMID)
		}
		if move.Target == "n1" {
			t.Errorf("guest %d was placed back on the node being drained", move.VMID)
		}
	}

	// The plan must account for what it moved, not just report the status quo.
	var incoming int
	for _, target := range plan.Targets {
		incoming += target.Incoming
		if target.After.Used < target.Before.Used {
			t.Errorf("%s: memory shrank after absorbing guests", target.Name)
		}
	}
	if incoming != 3 {
		t.Errorf("incoming total = %d, want 3", incoming)
	}
}

func TestPlanLeavesTemplatesInPlace(t *testing.T) {
	template := guestResource(900, "n1", 8, proxmox.StatusStopped)
	template.Template = proxmox.FlexBool(true)

	view := clusterView{
		Status:    statusEntries("n1", "n2"),
		Resources: []proxmox.Resource{nodeResource("n1", 10, 128), nodeResource("n2", 10, 128), template},
	}

	plan := buildPlan("c", "n1", view, config.DefaultMemoryThreshold)

	if len(plan.Moves) != 0 {
		t.Errorf("moves = %d, want none: a template does not migrate", len(plan.Moves))
	}
	if len(plan.Staying) != 1 || plan.Staying[0].Reason != "template" {
		t.Errorf("staying = %+v, want one template", plan.Staying)
	}
}

// A stopped guest still moves, but reserves nothing on its target until it is
// started again: counting its configured maximum would refuse plans that fit.
func TestPlanCountsAStoppedGuestAsWeightless(t *testing.T) {
	view := clusterView{
		Status: statusEntries("n1", "n2"),
		Resources: []proxmox.Resource{
			nodeResource("n1", 10, 128),
			nodeResource("n2", 100, 128),
			guestResource(100, "n1", 64, proxmox.StatusStopped),
		},
	}

	plan := buildPlan("c", "n1", view, config.DefaultMemoryThreshold)

	if len(plan.Moves) != 1 {
		t.Fatalf("moves = %d, want 1", len(plan.Moves))
	}
	if plan.Moves[0].Memory != 0 {
		t.Errorf("memory = %d, want 0 for a stopped guest", plan.Moves[0].Memory)
	}
	if !plan.Feasible {
		t.Error("a stopped guest should not make the plan infeasible")
	}
}

// The point of the whole endpoint: say before the click that it will not fit.
func TestPlanReportsInsufficientCapacity(t *testing.T) {
	view := clusterView{
		Status: statusEntries("n1", "n2"),
		Resources: []proxmox.Resource{
			nodeResource("n1", 60, 128),
			nodeResource("n2", 100, 128),
			guestResource(100, "n1", 60, proxmox.StatusRunning),
		},
	}

	plan := buildPlan("c", "n1", view, config.DefaultMemoryThreshold)

	if plan.Feasible {
		t.Error("plan reported feasible while the only target would exceed the threshold")
	}
	if plan.Moves[0].Placed {
		t.Error("the guest was placed on a target that cannot hold it")
	}
}

func TestPlanExcludesNodesThatCannotReceive(t *testing.T) {
	ha := &proxmox.HAManagerStatus{NodeStatus: map[string]string{
		"n2": proxmox.HANodeMaintenance,
		"n3": proxmox.HANodeOnline,
	}}
	view := clusterView{
		Status: append(statusEntries("n1", "n3"), proxmox.ClusterStatusEntry{
			Type:   proxmox.ClusterStatusTypeNode,
			Name:   "n2",
			Online: proxmox.FlexBool(true),
		}),
		Resources: []proxmox.Resource{
			nodeResource("n1", 10, 128),
			nodeResource("n2", 10, 128),
			nodeResource("n3", 10, 128),
			guestResource(100, "n1", 8, proxmox.StatusRunning),
		},
		HA: ha,
	}

	plan := buildPlan("c", "n1", view, config.DefaultMemoryThreshold)

	for _, target := range plan.Targets {
		if target.Name == "n2" {
			t.Error("a node already in maintenance was offered as a target")
		}
		if target.Name == "n1" {
			t.Error("the node being drained was offered as a target")
		}
	}
	if plan.Moves[0].Target != "n3" {
		t.Errorf("target = %q, want n3", plan.Moves[0].Target)
	}
}

func TestPlanWithNowhereToGo(t *testing.T) {
	view := clusterView{
		Status: statusEntries("n1"),
		Resources: []proxmox.Resource{
			nodeResource("n1", 10, 128),
			guestResource(100, "n1", 8, proxmox.StatusRunning),
		},
	}

	plan := buildPlan("c", "n1", view, config.DefaultMemoryThreshold)

	if plan.Feasible {
		t.Error("a single-node cluster cannot be drained")
	}
	if len(plan.Blockers) == 0 || plan.Blockers[0] != "no_target" {
		t.Errorf("blockers = %v, want no_target", plan.Blockers)
	}
}

func TestPlanOnAnOfflineNode(t *testing.T) {
	view := clusterView{
		Status: append(statusEntries("n2"), proxmox.ClusterStatusEntry{
			Type:   proxmox.ClusterStatusTypeNode,
			Name:   "n1",
			Online: proxmox.FlexBool(false),
		}),
		Resources: []proxmox.Resource{nodeResource("n1", 0, 128), nodeResource("n2", 10, 128)},
	}

	plan := buildPlan("c", "n1", view, config.DefaultMemoryThreshold)

	if plan == nil {
		t.Fatal("an offline node is still part of the cluster")
	}
	if plan.Feasible {
		t.Error("draining an offline node is not a plan")
	}
	if len(plan.Blockers) == 0 || plan.Blockers[0] != "source_offline" {
		t.Errorf("blockers = %v, want source_offline", plan.Blockers)
	}
}

func TestPlanUnknownNode(t *testing.T) {
	view := clusterView{
		Status:    statusEntries("n1"),
		Resources: []proxmox.Resource{nodeResource("n1", 10, 128)},
	}

	if plan := buildPlan("c", "ghost", view, config.DefaultMemoryThreshold); plan != nil {
		t.Errorf("plan = %+v, want nil for an unknown node", plan)
	}
}

func TestPlanSlicesAreNeverNil(t *testing.T) {
	view := clusterView{
		Status:    statusEntries("n1", "n2"),
		Resources: []proxmox.Resource{nodeResource("n1", 10, 128), nodeResource("n2", 10, 128)},
	}

	plan := buildPlan("c", "n1", view, config.DefaultMemoryThreshold)

	if plan.Moves == nil || plan.Staying == nil || plan.Targets == nil || plan.Blockers == nil {
		t.Errorf("a nil slice would serialise as null: %+v", plan)
	}
}

// A node with nothing on it can always be drained, even when the rest of the
// cluster is already tight: nothing moves, so nothing can fail.
func TestPlanOnAnEmptyNodeIsFeasibleEvenWhenTheClusterIsFull(t *testing.T) {
	view := clusterView{
		Status: statusEntries("n1", "n2", "n3"),
		Resources: []proxmox.Resource{
			nodeResource("n1", 5, 128),
			nodeResource("n2", 120, 128),
			nodeResource("n3", 120, 128),
		},
	}

	plan := buildPlan("c", "n1", view, config.DefaultMemoryThreshold)

	if len(plan.Moves) != 0 {
		t.Fatalf("moves = %d, want none", len(plan.Moves))
	}
	if !plan.Feasible {
		t.Error("draining an empty node was reported impossible")
	}
	// The saturated targets are still flagged, for the dialog to show.
	var flagged int
	for _, target := range plan.Targets {
		if target.Exceeds {
			flagged++
		}
	}
	if flagged != 2 {
		t.Errorf("flagged targets = %d, want 2 already over the threshold", flagged)
	}
}

// nodeWithoutFigures is the node PVE returns when the token has no Sys.Audit on
// /nodes: the row is there, the measurements are not, and no error is raised.
func nodeWithoutFigures(name string) proxmox.Resource {
	return proxmox.Resource{Type: proxmox.ResourceTypeNode, Node: name, Name: name, Status: "online"}
}

// TestPlanNamesUnmeasuredTargets: without figures the plan looked exactly like
// a cluster that was full -- every guest unplaced, no blocker -- so the dialog
// blamed the memory threshold for what is a missing privilege on moxy's token.
func TestPlanNamesUnmeasuredTargets(t *testing.T) {
	view := clusterView{
		Resources: []proxmox.Resource{
			nodeWithoutFigures("n1"),
			nodeWithoutFigures("n2"),
			nodeWithoutFigures("n3"),
			guestResource(100, "n1", 8, proxmox.StatusRunning),
		},
		Status: statusEntries("n1", "n2", "n3"),
	}

	plan := buildPlan("c", "n1", view, config.DefaultMemoryThreshold)
	if plan == nil {
		t.Fatal("buildPlan returned nil")
	}
	if plan.Feasible {
		t.Error("feasible with no measured destination")
	}
	if !hasBlocker(plan, "target_stats_unavailable") {
		t.Errorf("blockers = %v, want target_stats_unavailable", plan.Blockers)
	}
	for _, target := range plan.Targets {
		if target.Measured {
			t.Errorf("%s is marked measured", target.Name)
		}
		if target.Exceeds {
			t.Errorf("%s is marked as exceeding a threshold it has no figures for", target.Name)
		}
	}
}

// TestPlanPlacesOnTheMeasuredTargetsOnly: one node with figures among several
// without is enough to plan; the others are reported unknown, not full.
func TestPlanPlacesOnTheMeasuredTargetsOnly(t *testing.T) {
	view := clusterView{
		Resources: []proxmox.Resource{
			nodeWithoutFigures("n1"),
			nodeWithoutFigures("n2"),
			nodeResource("n3", 10, 128),
			guestResource(100, "n1", 8, proxmox.StatusRunning),
		},
		Status: statusEntries("n1", "n2", "n3"),
	}

	plan := buildPlan("c", "n1", view, config.DefaultMemoryThreshold)
	if plan == nil {
		t.Fatal("buildPlan returned nil")
	}
	if hasBlocker(plan, "target_stats_unavailable") {
		t.Errorf("blockers = %v, want none: one destination is measured", plan.Blockers)
	}
	if !plan.Feasible {
		t.Error("not feasible though a measured destination has room")
	}
	if len(plan.Moves) != 1 || plan.Moves[0].Target != "n3" {
		t.Fatalf("moves = %+v, want the guest placed on n3", plan.Moves)
	}
}

func hasBlocker(plan *MaintenancePlan, want string) bool {
	for _, blocker := range plan.Blockers {
		if blocker == want {
			return true
		}
	}
	return false
}

// TestPlanReportsTheMigrationMethod: Proxmox has no live migration for
// containers, so a running one is stopped, moved and started again. A plan that
// shows that beside a live VM migration hides an interruption.
func TestPlanReportsTheMigrationMethod(t *testing.T) {
	container := guestResource(105, "n1", 2, proxmox.StatusRunning)
	container.Type = proxmox.ResourceTypeLXC

	view := clusterView{
		Status: statusEntries("n1", "n2"),
		Resources: []proxmox.Resource{
			nodeResource("n1", 20, 128),
			nodeResource("n2", 20, 128),
			guestResource(100, "n1", 8, proxmox.StatusRunning),
			container,
			guestResource(106, "n1", 4, proxmox.StatusStopped),
		},
	}

	plan := buildPlan("c", "n1", view, config.DefaultMemoryThreshold)
	if plan == nil {
		t.Fatal("buildPlan returned nil")
	}
	want := map[int]string{100: MethodOnline, 105: MethodRestart, 106: MethodOffline}
	if len(plan.Moves) != len(want) {
		t.Fatalf("moves = %d, want %d", len(plan.Moves), len(want))
	}
	for _, move := range plan.Moves {
		if got := move.Method; got != want[move.VMID] {
			t.Errorf("vmid %d: method = %q, want %q", move.VMID, got, want[move.VMID])
		}
	}
	// The container is an LXC, which the kind must say too: guestKindOf had no
	// test placing one until now.
	for _, move := range plan.Moves {
		if move.VMID == 105 && move.Kind != aggregate.GuestLXC {
			t.Errorf("vmid 105: kind = %q, want %q", move.Kind, aggregate.GuestLXC)
		}
	}
}
