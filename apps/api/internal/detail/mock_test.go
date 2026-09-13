package detail

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"testing"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
)

func newMock(t *testing.T) (*Mock, *aggregate.Overview) {
	t.Helper()
	source := aggregate.NewMock()
	overview, err := source.Overview(context.Background())
	if err != nil {
		t.Fatalf("sample overview: %v", err)
	}
	return NewMock(source), overview
}

// The reason this mock exists: what a node view shows must be what its card
// showed. Figures invented independently would drift the moment either side
// changed.
func TestMockNodeMatchesTheOverview(t *testing.T) {
	mock, overview := newMock(t)
	cluster := overview.Clusters[0]
	want := cluster.Nodes[0]

	got, err := mock.Node(context.Background(), cluster.ID, want.Name)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}

	if got.Name != want.Name || got.Status != want.Status || !sameUptime(got.Uptime, want.Uptime) {
		t.Errorf("identity = %+v, want %q/%q/%v", got, want.Name, want.Status, want.Uptime)
	}
	if want.CPU == nil || got.CPU != *want.CPU {
		t.Errorf("cpu = %+v, want %+v", got.CPU, want.CPU)
	}
	if want.Memory == nil || got.Memory != *want.Memory {
		t.Errorf("memory = %+v, want %+v", got.Memory, want.Memory)
	}
	if len(got.Guests) != len(want.Guests) {
		t.Errorf("guests = %d, want %d", len(got.Guests), len(want.Guests))
	}
	// Compared by value: the mock rebuilds its snapshot on every call, so the
	// pointers differ even when the figures agree.
	if got.Quorum == nil || cluster.Quorum == nil || *got.Quorum != *cluster.Quorum {
		t.Errorf("quorum = %+v, want %+v", got.Quorum, cluster.Quorum)
	}
}

// TestMockNodeUpdatesMatchTheCount walks every sample node: a list that
// disagreed with the count its card shows would let a broken UI look right.
func TestMockNodeUpdatesMatchTheCount(t *testing.T) {
	mock, overview := newMock(t)
	listed := 0

	for _, cluster := range overview.Clusters {
		for _, want := range cluster.Nodes {
			got, err := mock.Node(context.Background(), cluster.ID, want.Name)
			if err != nil {
				t.Fatalf("Node(%s/%s): %v", cluster.ID, want.Name, err)
			}
			if want.PendingUpdates == nil {
				if got.Updates != nil {
					t.Errorf("%s: updates = %v, want nil like the count", want.Name, got.Updates)
				}
				continue
			}
			if len(got.Updates) != *want.PendingUpdates {
				t.Errorf("%s: %d packages for a count of %d", want.Name, len(got.Updates), *want.PendingUpdates)
			}
			for _, u := range got.Updates {
				if u.Package == "" || u.Version == "" {
					t.Errorf("%s: nameless or versionless package %+v", want.Name, u)
				}
			}
			listed += len(got.Updates)
		}
	}

	if listed == 0 {
		t.Fatal("no sample node lists a pending package: the view would never be exercised")
	}
}

func TestMockNodeUnknown(t *testing.T) {
	mock, overview := newMock(t)

	if _, err := mock.Node(context.Background(), overview.Clusters[0].ID, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown node: err = %v, want ErrNotFound", err)
	}
	if _, err := mock.Node(context.Background(), "nope", "any"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown cluster: err = %v, want ErrNotFound", err)
	}
}

// A node drained by maintenance is the interesting case: it must serialise an
// empty array, never null, and say why it is empty.
func TestMockDrainedNode(t *testing.T) {
	mock, overview := newMock(t)

	var found bool
	for _, cluster := range overview.Clusters {
		for _, node := range cluster.Nodes {
			if node.Status != aggregate.NodeMaintenance {
				continue
			}
			found = true
			got, err := mock.Node(context.Background(), cluster.ID, node.Name)
			if err != nil {
				t.Fatalf("Node: %v", err)
			}
			if got.Guests == nil {
				t.Error("guests is nil; the model promises an array")
			}
			if len(got.Guests) != 0 {
				t.Errorf("guests = %d, want none on a drained node", len(got.Guests))
			}
			if got.HAState == nil || *got.HAState != "maintenance" {
				t.Errorf("haState = %v, want \"maintenance\"", got.HAState)
			}
		}
	}
	if !found {
		t.Fatal("the sample overview no longer has a node in maintenance")
	}
}

func TestMockGuestMatchesTheOverview(t *testing.T) {
	mock, overview := newMock(t)
	cluster := overview.Clusters[0]

	var want aggregate.Guest
	var host string
	for _, node := range cluster.Nodes {
		if len(node.Guests) > 0 {
			want = node.Guests[0]
			host = node.Name
			break
		}
	}

	got, err := mock.Guest(context.Background(), cluster.ID, want.VMID)
	if err != nil {
		t.Fatalf("Guest: %v", err)
	}

	if got.VMID != want.VMID || got.Name != want.Name || got.Kind != want.Kind {
		t.Errorf("identity = %+v, want %+v", got, want)
	}
	if got.Node != host {
		t.Errorf("node = %q, want %q", got.Node, host)
	}
	if got.CPU != want.CPU || got.Memory != want.Memory {
		t.Errorf("figures = %+v/%+v, want %+v/%+v", got.CPU, got.Memory, want.CPU, want.Memory)
	}
	if got.Tags == nil {
		t.Error("tags is nil; the model promises an array")
	}
}

// TestMockGuestDisksShowEveryShape walks the sample guests and demands that
// the demonstration carry each case the volume list has to render. A mock too
// tidy to hold a detached volume or a size nobody knows would let through a UI
// that cannot draw either — the same reason the sample RRD series carry holes.
func TestMockGuestDisksShowEveryShape(t *testing.T) {
	mock, overview := newMock(t)

	var (
		withDisks   int
		unreadable  int
		detached    int
		unknownSize int
		partial     int
		multiVolume int
	)
	for _, cluster := range overview.Clusters {
		for _, node := range cluster.Nodes {
			for _, guest := range node.Guests {
				got, err := mock.Guest(context.Background(), cluster.ID, guest.VMID)
				if err != nil {
					t.Fatalf("Guest %d: %v", guest.VMID, err)
				}
				if got.Disks == nil {
					if got.Allocated != nil {
						t.Errorf("guest %d: allocated without disks", guest.VMID)
					}
					unreadable++
					continue
				}
				withDisks++
				if got.Allocated == nil {
					t.Fatalf("guest %d: disks without a total", guest.VMID)
				}
				if len(got.Disks) > 1 {
					multiVolume++
				}
				if got.Allocated.Partial {
					partial++
				}
				if got.Allocated.Detached > 0 {
					detached++
				}
				for _, disk := range got.Disks {
					if disk.Size == nil {
						unknownSize++
					}
					// An optical drive allocates nothing and must never reach
					// the list, whatever key it was plugged into.
					if disk.Volume == "" {
						t.Errorf("guest %d: volume %q has no declaration", guest.VMID, disk.Key)
					}
				}
			}
		}
	}

	for _, c := range []struct {
		name  string
		count int
	}{
		{"guests with a readable configuration", withDisks},
		{"guests without VM.Audit", unreadable},
		{"guests with several volumes", multiVolume},
		{"guests with a detached volume", detached},
		{"volumes of unknown size", unknownSize},
		{"guests whose total is a floor", partial},
	} {
		if c.count == 0 {
			t.Errorf("the sample holds no %s; the view would never be exercised on it", c.name)
		}
	}
}

func TestMockGuestUnknown(t *testing.T) {
	mock, overview := newMock(t)

	if _, err := mock.Guest(context.Background(), overview.Clusters[0].ID, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestMockSeries(t *testing.T) {
	mock, overview := newMock(t)
	cluster := overview.Clusters[0]
	node := cluster.Nodes[0].Name

	for _, tc := range []struct {
		timeframe string
		want      int
	}{
		{"hour", 60}, {"day", 96}, {"week", 84}, {"month", 90}, {"year", 73},
	} {
		series, err := mock.NodeSeries(context.Background(), cluster.ID, node, tc.timeframe)
		if err != nil {
			t.Fatalf("%s: %v", tc.timeframe, err)
		}
		if len(series.Points) != tc.want {
			t.Errorf("%s: %d points, want %d", tc.timeframe, len(series.Points), tc.want)
		}
		if series.Timeframe != tc.timeframe {
			t.Errorf("%s: timeframe echoed as %q", tc.timeframe, series.Timeframe)
		}
	}
}

// RRD returns gaps; a mock without any would let a chart that cannot draw one
// pass every test and then break against a real cluster.
func TestMockSeriesHasGapsAndStaysInRange(t *testing.T) {
	mock, overview := newMock(t)
	cluster := overview.Clusters[0]

	series, err := mock.NodeSeries(context.Background(), cluster.ID, cluster.Nodes[0].Name, "hour")
	if err != nil {
		t.Fatalf("NodeSeries: %v", err)
	}

	var gaps int
	for _, point := range series.Points {
		if point.CPU == nil {
			gaps++
			if point.MemUsed != nil {
				t.Error("a gap must leave every metric unknown, not just the cpu")
			}
			continue
		}
		if *point.CPU < 0 || *point.CPU > 1 {
			t.Errorf("cpu = %v, want a ratio in [0,1]", *point.CPU)
		}
	}
	if gaps == 0 {
		t.Error("no gap in the window")
	}
	if series.CPUAverage == nil {
		t.Error("average = nil: this window has measured points")
	} else if *series.CPUAverage < 0 || *series.CPUAverage > 1 {
		t.Errorf("average = %v, want a ratio", *series.CPUAverage)
	}
}

func TestMockSeriesIsDeterministic(t *testing.T) {
	mock, overview := newMock(t)
	cluster := overview.Clusters[0]
	node := cluster.Nodes[0].Name

	first, err := mock.NodeSeries(context.Background(), cluster.ID, node, "hour")
	if err != nil {
		t.Fatalf("NodeSeries: %v", err)
	}
	second, err := mock.NodeSeries(context.Background(), cluster.ID, node, "hour")
	if err != nil {
		t.Fatalf("NodeSeries: %v", err)
	}

	for i := range first.Points {
		if (first.Points[i].CPU == nil) != (second.Points[i].CPU == nil) {
			t.Fatalf("point %d changed between two reads", i)
		}
		if first.Points[i].CPU != nil && *first.Points[i].CPU != *second.Points[i].CPU {
			t.Fatalf("point %d changed between two reads", i)
		}
	}
}

func TestMockSeriesRejectsUnknownTimeframe(t *testing.T) {
	mock, overview := newMock(t)
	cluster := overview.Clusters[0]

	_, err := mock.NodeSeries(context.Background(), cluster.ID, cluster.Nodes[0].Name, "decade")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestMockTasks(t *testing.T) {
	mock, overview := newMock(t)
	cluster := overview.Clusters[0]

	tasks, err := mock.Tasks(context.Background(), cluster.ID, 5)
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks.Entries) != 5 {
		t.Fatalf("entries = %d, want 5", len(tasks.Entries))
	}

	// The newest task is still running: a null duration is not a zero one, and
	// the table must be able to show that.
	first := tasks.Entries[0]
	if first.End != nil || first.Duration != nil || first.Outcome != TaskOutcomeRunning {
		t.Errorf("newest task = %+v, want it still running", first)
	}
	for _, task := range tasks.Entries[1:] {
		if task.Duration == nil || *task.Duration <= 0 {
			t.Errorf("finished task has no duration: %+v", task)
		}
		if task.End == nil || !task.End.After(task.Start) {
			t.Errorf("finished task ends before it starts: %+v", task)
		}
	}
}

// firstGuest names a guest of the sample cluster and the node hosting it.
func firstGuest(t *testing.T, cluster aggregate.ClusterOverview) (aggregate.Node, aggregate.Guest) {
	t.Helper()
	for _, node := range cluster.Nodes {
		if len(node.Guests) > 0 {
			return node, node.Guests[0]
		}
	}
	t.Fatalf("cluster %q hosts no guest", cluster.ID)
	return aggregate.Node{}, aggregate.Guest{}
}

// A guest's own log carries its vmid on every line and its node on every
// entry. An empty one would be the very bug this route was added to fix, so
// the mock must never serve one for a guest the overview knows.
func TestMockGuestTasks(t *testing.T) {
	mock, overview := newMock(t)
	cluster := overview.Clusters[0]
	node, guest := firstGuest(t, cluster)

	tasks, err := mock.GuestTasks(context.Background(), cluster.ID, guest.VMID, 25)
	if err != nil {
		t.Fatalf("GuestTasks: %v", err)
	}
	if len(tasks.Entries) == 0 {
		t.Fatal("no entry: a guest known to the overview has a history")
	}
	for _, entry := range tasks.Entries {
		if entry.ID != strconv.Itoa(guest.VMID) {
			t.Errorf("entry %+v belongs to another guest", entry)
		}
		if entry.Node != node.Name {
			t.Errorf("entry %+v is filed on another node than %q", entry, node.Name)
		}
	}

	// The same rule as the cluster journal: the newest task is still running,
	// so the table is exercised on a null duration rather than a zero one.
	first := tasks.Entries[0]
	if first.End != nil || first.Duration != nil || first.Outcome != TaskOutcomeRunning {
		t.Errorf("newest task = %+v, want it still running", first)
	}
}

// The limit is a ceiling, not a quota: a guest with three tasks to its name
// answers three, not twenty-five invented ones.
func TestMockGuestTasksRespectsTheLimit(t *testing.T) {
	mock, overview := newMock(t)
	cluster := overview.Clusters[0]
	_, guest := firstGuest(t, cluster)

	tasks, err := mock.GuestTasks(context.Background(), cluster.ID, guest.VMID, 1)
	if err != nil {
		t.Fatalf("GuestTasks: %v", err)
	}
	if len(tasks.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(tasks.Entries))
	}
}

func TestMockGuestTasksUnknown(t *testing.T) {
	mock, overview := newMock(t)

	_, err := mock.GuestTasks(context.Background(), overview.Clusters[0].ID, 999999, 25)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// The overview card draws this series in place of its gauges: it must carry
// the figures of the very cluster it sits on, gaps included.
func TestMockClusterSeriesMatchesTheOverview(t *testing.T) {
	mock, overview := newMock(t)
	cluster := overview.Clusters[0]

	series, err := mock.ClusterSeries(context.Background(), cluster.ID, "hour")
	if err != nil {
		t.Fatalf("ClusterSeries: %v", err)
	}
	if series.Cluster != cluster.ID || series.Timeframe != "hour" {
		t.Errorf("series = %+v, want the hour of %s", series, cluster.ID)
	}
	if len(series.Points) == 0 {
		t.Fatal("no point: the card would have nothing to draw")
	}

	holes := 0
	for _, p := range series.Points {
		if p.CPU == nil {
			holes++
			continue
		}
		if cluster.Memory != nil && (p.MemTotal == nil || *p.MemTotal != cluster.Memory.Total) {
			t.Fatalf("memory total = %v, want the %d of the card", p.MemTotal, cluster.Memory.Total)
		}
	}
	if holes == 0 {
		t.Error("no hole in the series: a mock too clean lets through a chart unable to draw one")
	}
}

func TestMockClusterSeriesUnknown(t *testing.T) {
	mock, overview := newMock(t)

	if _, err := mock.ClusterSeries(context.Background(), "nope", "hour"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown cluster: err = %v, want ErrNotFound", err)
	}
	if _, err := mock.ClusterSeries(context.Background(), overview.Clusters[0].ID, "decade"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown timeframe: err = %v, want ErrNotFound", err)
	}
}

// TestMockServesTheNodesThatHaveNoFigures: a node PVE listed without its
// measurements still has a page. Every size derived from a zero total is zero
// too, and the quotient is a NaN that encoding/json refuses -- which turned the
// whole node view into a 500.
func TestMockServesTheNodesThatHaveNoFigures(t *testing.T) {
	m := NewMock(aggregate.NewMock())
	ctx := context.Background()

	for _, name := range []string{"prox-lab-2502-cit", "prox-lab-2503-cit"} {
		node, err := m.Node(ctx, "lab", name)
		if err != nil {
			t.Fatalf("%s: Node: %v", name, err)
		}
		if _, err := json.Marshal(node); err != nil {
			t.Fatalf("%s: the node view does not encode: %v", name, err)
		}
		for label, value := range map[string]float64{
			"cpu":    node.CPU.Ratio,
			"memory": node.Memory.Ratio,
			"rootfs": node.RootFS.Ratio,
			"swap":   node.Swap.Ratio,
		} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Errorf("%s: %s ratio is %v", name, label, value)
			}
		}
	}
}

// sameUptime compares two optional durations, nil included: the node view and
// the card must agree on "unknown" as much as on a number.
func sameUptime(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// TestMockGuestSeries: the guest view draws the same chart as the node view,
// from the same window, and had no test at all -- the sample data served a
// route nothing in this package exercised.
func TestMockGuestSeries(t *testing.T) {
	mock, overview := newMock(t)
	cluster := overview.Clusters[0]
	guest := cluster.Nodes[0].Guests[0]

	series, err := mock.GuestSeries(context.Background(), cluster.ID, guest.VMID, "hour")
	if err != nil {
		t.Fatalf("GuestSeries: %v", err)
	}
	if series.Cluster != cluster.ID || series.Timeframe != "hour" {
		t.Errorf("series = %s/%s, want %s/hour", series.Cluster, series.Timeframe, cluster.ID)
	}
	if len(series.Points) == 0 {
		t.Fatal("the window carries no point")
	}
	// A guest's series is the guest's own, not a copy of its node's: an
	// interface built against the latter would show every VM of a node at the
	// same load.
	node, err := mock.NodeSeries(context.Background(), cluster.ID, cluster.Nodes[0].Name, "hour")
	if err != nil {
		t.Fatalf("NodeSeries: %v", err)
	}
	if reflect.DeepEqual(series.Points, node.Points) {
		t.Error("the guest series is a copy of its node's")
	}

	if _, err := mock.GuestSeries(context.Background(), cluster.ID, 999999, "hour"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GuestSeries of an unknown guest = %v, want ErrNotFound", err)
	}
	if _, err := mock.GuestSeries(context.Background(), cluster.ID, guest.VMID, "decade"); err == nil {
		t.Error("GuestSeries accepted a timeframe that does not exist")
	}
}

// TestMockMaintenancePlan: the plan is read-only and the mock had no test for
// it either. What matters is that it says something COHERENT with the card it
// is derived from -- a plan naming a guest the node does not host, or offering
// the node being drained as a target, would let through a dialog that cannot
// be trusted.
func TestMockMaintenancePlan(t *testing.T) {
	mock, overview := newMock(t)
	cluster := overview.Clusters[0]
	source := cluster.Nodes[0]

	plan, err := mock.MaintenancePlan(context.Background(), cluster.ID, source.Name)
	if err != nil {
		t.Fatalf("MaintenancePlan: %v", err)
	}
	if plan.Cluster != cluster.ID || plan.Node != source.Name {
		t.Fatalf("plan is %s/%s", plan.Cluster, plan.Node)
	}
	if plan.Threshold <= 0 || plan.Threshold > 1 {
		t.Errorf("threshold = %v, want a fraction", plan.Threshold)
	}

	hosted := map[int]bool{}
	for _, g := range source.Guests {
		hosted[g.VMID] = true
	}
	for _, move := range plan.Moves {
		if !hosted[move.VMID] {
			t.Errorf("the plan moves %d, which %s does not host", move.VMID, source.Name)
		}
		if move.Target == source.Name {
			t.Errorf("guest %d is moved to the node being drained", move.VMID)
		}
		if move.Placed && move.Target == "" {
			t.Errorf("guest %d is placed nowhere", move.VMID)
		}
	}
	for _, staying := range plan.Staying {
		if !hosted[staying.VMID] {
			t.Errorf("the plan keeps %d, which %s does not host", staying.VMID, source.Name)
		}
	}
	for _, target := range plan.Targets {
		if target.Name == source.Name {
			t.Errorf("%s is offered as a target for its own drain", target.Name)
		}
	}
	// Every guest of the node is accounted for, one way or the other: a plan
	// that quietly drops one understates what the drain entails.
	if got := len(plan.Moves) + len(plan.Staying); got != len(source.Guests) {
		t.Errorf("the plan accounts for %d guests, the node hosts %d", got, len(source.Guests))
	}

	if _, err := mock.MaintenancePlan(context.Background(), cluster.ID, "no-such-node"); !errors.Is(err, ErrNotFound) {
		t.Errorf("MaintenancePlan of an unknown node = %v, want ErrNotFound", err)
	}
}

// TestMockMaintenancePlanOfADegradedCluster walks the branches the healthy
// cluster never reaches: a node PVE lists without its figures cannot be
// offered as a target, and the plan must say so rather than place guests on a
// node whose free memory nobody knows.
func TestMockMaintenancePlanOfADegradedCluster(t *testing.T) {
	mock, overview := newMock(t)

	var lab *aggregate.ClusterOverview
	for i := range overview.Clusters {
		if overview.Clusters[i].ID == "lab" {
			lab = &overview.Clusters[i]
		}
	}
	if lab == nil {
		t.Skip("the sample data no longer carries the degraded cluster")
	}

	plan, err := mock.MaintenancePlan(context.Background(), lab.ID, lab.Nodes[0].Name)
	if err != nil {
		t.Fatalf("MaintenancePlan: %v", err)
	}
	if plan.Feasible {
		t.Error("a drain onto an unmeasured node was called feasible")
	}
	if !hasBlocker(plan, "target_stats_unavailable") {
		t.Errorf("blockers = %v, want target_stats_unavailable", plan.Blockers)
	}
	for _, target := range plan.Targets {
		if target.Measured {
			continue
		}
		// An unmeasured node has no usable before/after: the dialog renders
		// the em dash, and a plan that placed a guest there would be a guess.
		if target.Incoming != 0 {
			t.Errorf("%d guests are sent to %s, whose memory is unknown", target.Incoming, target.Name)
		}
	}
}
