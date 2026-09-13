package aggregate

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// overviewSource mirrors the interface the server package consumes. Asserting
// it here keeps the mock's signature from drifting away from the contract.
type overviewSource interface {
	Overview(ctx context.Context) (*Overview, error)
}

var _ overviewSource = (*Mock)(nil)

// mockOverview builds a fresh mock payload or fails the test.
func mockOverview(t *testing.T) *Overview {
	t.Helper()
	ov, err := NewMock().Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview() returned an error: %v", err)
	}
	if ov == nil {
		t.Fatal("Overview() returned a nil payload")
	}
	return ov
}

func TestMockTotals(t *testing.T) {
	ov := mockOverview(t)
	want := Totals{Clusters: 4, Nodes: 14, NodesOnline: 13, VMs: 154, Alerts: 7}
	if ov.Totals != want {
		t.Errorf("Totals = %+v, want %+v", ov.Totals, want)
	}
	if got := len(ov.Clusters); got != want.Clusters {
		t.Errorf("len(Clusters) = %d, want %d", got, want.Clusters)
	}
	// All three, and not memory alone: the frontend colours each reading by
	// the limit of its own resource, and a zero would read as "warn always".
	if want := (Thresholds{Memory: 0.80, CPU: 0.80, Storage: 0.80}); ov.Thresholds != want {
		t.Errorf("Thresholds = %+v, want %+v", ov.Thresholds, want)
	}
	if ov.GeneratedAt.IsZero() {
		t.Error("GeneratedAt is zero")
	}
}

// TestMockTotalsMatchClusters checks the stated totals against the clusters the
// mock actually reports, nodes and guests alike.
func TestMockTotalsMatchClusters(t *testing.T) {
	ov := mockOverview(t)
	var nodes, online, vms, alerts int
	for _, c := range ov.Clusters {
		nodes += len(c.Nodes)
		for _, n := range c.Nodes {
			// A node drained for maintenance is still up and still votes.
			if n.Status == NodeOnline || n.Status == NodeMaintenance {
				online++
			}
		}
		vms += c.VMs.Total
		alerts += len(c.Alerts)
	}
	if nodes != ov.Totals.Nodes {
		t.Errorf("counted %d nodes, Totals.Nodes = %d", nodes, ov.Totals.Nodes)
	}
	if online != ov.Totals.NodesOnline {
		t.Errorf("counted %d online nodes, Totals.NodesOnline = %d", online, ov.Totals.NodesOnline)
	}
	if vms != ov.Totals.VMs {
		t.Errorf("counted %d guests, Totals.VMs = %d", vms, ov.Totals.VMs)
	}
	if alerts != ov.Totals.Alerts {
		t.Errorf("counted %d alerts, Totals.Alerts = %d", alerts, ov.Totals.Alerts)
	}
}

// cluster returns the cluster with the given id, failing the test if absent.
func cluster(t *testing.T, ov *Overview, id string) ClusterOverview {
	t.Helper()
	for _, c := range ov.Clusters {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("cluster %q not found", id)
	return ClusterOverview{}
}

func TestMockPreproductionIsDegraded(t *testing.T) {
	c := cluster(t, mockOverview(t), "preproduction")
	if c.Status != StatusDegraded {
		t.Errorf("status = %q, want %q", c.Status, StatusDegraded)
	}
	if c.Name != "Préproduction" {
		t.Errorf("name = %q, want %q", c.Name, "Préproduction")
	}
	var found bool
	for _, n := range c.Nodes {
		if n.Name == "prox-pprd-2302-cit" {
			found = true
			if n.Status != NodeMaintenance {
				t.Errorf("prox-pprd-2302-cit status = %q, want %q", n.Status, NodeMaintenance)
			}
		}
	}
	if !found {
		t.Error("prox-pprd-2302-cit is missing from the node list")
	}
	if math.Abs(c.Memory.Ratio-0.828) > 0.001 {
		t.Errorf("memory ratio = %v, want 0.828 +/- 0.001", c.Memory.Ratio)
	}
}

func TestMockQualificationHasNoAlert(t *testing.T) {
	c := cluster(t, mockOverview(t), "qualification")
	if c.Status != StatusHealthy {
		t.Errorf("status = %q, want %q", c.Status, StatusHealthy)
	}
	if len(c.Alerts) != 0 {
		t.Errorf("alerts = %+v, want none", c.Alerts)
	}
	if c.Quorum == nil || !c.Quorum.Quorate || c.Quorum.Nodes != 3 || c.Quorum.Online != 3 {
		t.Errorf("quorum = %+v, want a quorate 3/3", c.Quorum)
	}
}

// TestMockProductionUpdates checks the update banner of the mockup: available
// release 9.2.12 on all five nodes, without degrading the cluster.
func TestMockProductionUpdates(t *testing.T) {
	c := cluster(t, mockOverview(t), "production")
	if c.Status != StatusHealthy {
		t.Errorf("status = %q, want %q", c.Status, StatusHealthy)
	}
	if c.Updates == nil {
		t.Fatal("Updates is nil")
	}
	if c.Updates.PVEManagerVersion == nil || *c.Updates.PVEManagerVersion != "9.2.12" {
		t.Errorf("pveManagerVersion = %v, want 9.2.12", c.Updates.PVEManagerVersion)
	}
	if len(c.Updates.Nodes) != len(c.Nodes) {
		t.Errorf("updates cover %d nodes, cluster has %d", len(c.Updates.Nodes), len(c.Nodes))
	}
	if c.Updates.CheckedAt.IsZero() {
		t.Error("checkedAt is zero")
	}
	if len(c.Alerts) != 1 || c.Alerts[0].Kind != AlertUpdatesAvailable {
		t.Fatalf("alerts = %+v, want a single updates_available", c.Alerts)
	}
	if c.Alerts[0].Version == nil || *c.Alerts[0].Version != "9.2.12" {
		t.Errorf("alert version = %v, want 9.2.12", c.Alerts[0].Version)
	}
	// Every production node reports a count, and they are all equal: the
	// cluster stays healthy under its update banner.
	for _, n := range c.Nodes {
		if n.PendingUpdates == nil {
			t.Errorf("node %s has no pending-update count", n.Name)
		}
	}
}

// TestMockQualificationHasUnknownPendingUpdates keeps one cluster demonstrating
// the unknown count: a token without Sys.Modify answers nothing, and nil must
// stay readable as "unknown" rather than as "up to date".
func TestMockQualificationHasUnknownPendingUpdates(t *testing.T) {
	c := cluster(t, mockOverview(t), "qualification")
	if c.Updates != nil {
		t.Errorf("Updates = %+v, want nil", c.Updates)
	}
	for _, n := range c.Nodes {
		if n.PendingUpdates != nil {
			t.Errorf("node %s: PendingUpdates = %d, want nil", n.Name, *n.PendingUpdates)
		}
	}
}

// TestMockPreproductionUpdatesAreUneven is the mockup of the uneven case: the
// drained node lags behind the two others, and the fault banner comes before
// the news since a card only ever shows alerts[0].
func TestMockPreproductionUpdatesAreUneven(t *testing.T) {
	c := cluster(t, mockOverview(t), "preproduction")

	counts := map[string]int{}
	for _, n := range c.Nodes {
		if n.PendingUpdates == nil {
			t.Fatalf("node %s has no pending-update count", n.Name)
		}
		counts[n.Name] = *n.PendingUpdates
	}
	if counts["prox-pprd-2302-cit"] == counts["prox-pprd-2301-cit"] {
		t.Errorf("counts = %v, want the drained node out of step", counts)
	}

	var uneven, available int
	for i, a := range c.Alerts {
		switch a.Kind {
		case AlertUpdatesUneven:
			uneven = i + 1
			if a.PendingMin == nil || *a.PendingMin != 8 {
				t.Errorf("pendingMin = %v, want 8", a.PendingMin)
			}
			if a.PendingMax == nil || *a.PendingMax != 14 {
				t.Errorf("pendingMax = %v, want 14", a.PendingMax)
			}
			if len(a.Nodes) != 0 {
				t.Errorf("nodes = %v, want none: the alert is about the spread", a.Nodes)
			}
		case AlertUpdatesAvailable:
			available = i + 1
		}
	}
	if uneven == 0 || available == 0 {
		t.Fatalf("alerts = %+v, want both update banners", c.Alerts)
	}
	if uneven > available {
		t.Errorf("updates_uneven is at %d, after updates_available at %d", uneven, available)
	}
}

// TestMockAlertsMatchDeriveAlerts pins the hand-written banners to the ones the
// derivation would produce: a mock that contradicted the real path would let a
// frontend be built against a lie.
func TestMockAlertsMatchDeriveAlerts(t *testing.T) {
	ov := mockOverview(t)
	for _, c := range ov.Clusters {
		got := deriveAlerts(c, ov.Thresholds.Memory)
		if !reflect.DeepEqual(got, c.Alerts) {
			t.Errorf("cluster %s: derived %+v, mock states %+v", c.ID, got, c.Alerts)
		}
		// StatusUnreachable is the poller's decision, not the derivation's:
		// Derive never reads a clock, so it cannot know a snapshot is stale.
		// Under the staleness the card would be degraded, and that is what the
		// derivation must agree with.
		want := deriveStatus(c)
		if c.Status == StatusUnreachable {
			if want == StatusHealthy {
				t.Errorf("cluster %s is served unreachable but derives healthy", c.ID)
			}
			continue
		}
		if want != c.Status {
			t.Errorf("cluster %s: derived status %q, mock states %q", c.ID, want, c.Status)
		}
	}
}

// TestMockArithmeticConsistency is the test that matters: the cluster figures
// must be exactly what their nodes add up to, or the frontend built against the
// mock would be built against a lie.
func TestMockArithmeticConsistency(t *testing.T) {
	ov := mockOverview(t)
	for _, c := range ov.Clusters {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			var used, total uint64
			var weighted float64
			var cores int
			for _, n := range c.Nodes {
				if n.Status != NodeOnline && n.Status != NodeMaintenance {
					continue
				}
				// A node PVE listed without its figures is up and unknown, not
				// up and idle: it contributes nothing to the cluster totals,
				// which is exactly what deriveCPUAndMemory does with it.
				if n.CPU == nil || n.Memory == nil {
					if n.Uptime != nil {
						t.Errorf("node %s: an unmeasured node reports an uptime of %d", n.Name, *n.Uptime)
					}
					continue
				}
				used += n.Memory.Used
				total += n.Memory.Total
				weighted += n.CPU.Ratio * float64(n.CPU.Cores)
				cores += n.CPU.Cores
				if got := float64(n.Memory.Used) / float64(n.Memory.Total); math.Abs(n.Memory.Ratio-got) > 1e-9 {
					t.Errorf("node %s: memory ratio = %v, want %v", n.Name, n.Memory.Ratio, got)
				}
				if n.Uptime == nil || *n.Uptime <= 0 {
					t.Errorf("node %s: uptime = %v, want a positive duration", n.Name, n.Uptime)
				}
			}
			if used != c.Memory.Used {
				t.Errorf("nodes use %d bytes, cluster reports %d", used, c.Memory.Used)
			}
			if total != c.Memory.Total {
				t.Errorf("nodes total %d bytes, cluster reports %d", total, c.Memory.Total)
			}
			if want := float64(c.Memory.Used) / float64(c.Memory.Total); math.Abs(c.Memory.Ratio-want) > 1e-9 {
				t.Errorf("memory ratio = %v, want %v", c.Memory.Ratio, want)
			}
			if want := float64(c.Storage.Used) / float64(c.Storage.Total); math.Abs(c.Storage.Ratio-want) > 1e-9 {
				t.Errorf("storage ratio = %v, want %v", c.Storage.Ratio, want)
			}
			if cores != c.CPU.Cores {
				t.Errorf("nodes total %d cores, cluster reports %d", cores, c.CPU.Cores)
			}
			// The cluster load is the core-weighted mean, not the mean of the
			// node fractions.
			if want := weighted / float64(cores); math.Abs(c.CPU.Ratio-want) > 1e-9 {
				t.Errorf("cpu ratio = %v, want the weighted mean %v", c.CPU.Ratio, want)
			}
			if got := c.VMs.Running + c.VMs.Stopped; got != c.VMs.Total {
				t.Errorf("vms total = %d, want running+stopped = %d", c.VMs.Total, got)
			}
			if c.Quorum == nil {
				t.Fatal("quorum is nil")
			}
			if c.Quorum.Nodes != len(c.Nodes) {
				t.Errorf("quorum counts %d nodes, cluster lists %d", c.Quorum.Nodes, len(c.Nodes))
			}
			if c.Quorum.Online > c.Quorum.Nodes {
				t.Errorf("quorum online = %d, more than its %d nodes", c.Quorum.Online, c.Quorum.Nodes)
			}
			if c.FetchedAt == nil || c.FetchedAt.IsZero() {
				t.Error("fetchedAt is not set")
			}
			// An unreachable cluster carries the failure that made it so; a
			// reachable one carries nothing.
			if c.Status == StatusUnreachable {
				if c.Error == nil {
					t.Error("an unreachable cluster carries no error")
				}
			} else if c.Error != nil {
				t.Errorf("error = %+v, want nil", c.Error)
			}
			// An accent is optional, and when it is there it has the shape the
			// configuration enforces: the frontend puts it in a style
			// attribute, so a demonstration payload may not be the one place
			// the contract is bent.
			if c.Color != nil && !mockColorPattern.MatchString(*c.Color) {
				t.Errorf("color = %q, want a #rrggbb value", *c.Color)
			}
		})
	}
}

// mockColorPattern is the shape config.validateCluster enforces, repeated here
// rather than imported: the config package depends on this one.
var mockColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// TestMockAccentsOneClusterOnly checks the demonstration data exercises both
// halves of clusters[].color: the mark the frontend draws for a cluster that
// declares an accent, and the nothing it draws for one that does not. A mock
// where every cluster looked alike would let either half rot unnoticed.
func TestMockAccentsOneClusterOnly(t *testing.T) {
	ov := mockOverview(t)
	var accented []string
	for _, c := range ov.Clusters {
		if c.Color != nil {
			accented = append(accented, c.ID)
		}
	}
	if len(accented) != 1 {
		t.Errorf("clusters with an accent = %v, want exactly one", accented)
	}
	if len(ov.Clusters) < 2 {
		t.Fatalf("clusters = %d, want several so that both cases are shown", len(ov.Clusters))
	}
}

// TestMockMemoryHighAlertNodes checks the alert names exactly the nodes above
// the threshold, and no other.
func TestMockMemoryHighAlertNodes(t *testing.T) {
	ov := mockOverview(t)
	for _, c := range ov.Clusters {
		listed := map[string]bool{}
		for _, a := range c.Alerts {
			if a.Kind != AlertMemoryHigh {
				continue
			}
			if a.Ratio == nil {
				t.Fatalf("cluster %s: memory_high without a ratio", c.ID)
			}
			if c.Memory == nil {
				t.Fatalf("cluster %s: memory_high on a cluster with no memory figure", c.ID)
			}
			// The ratio describes what the banner names: the worst of the
			// listed nodes, or the cluster when none is listed. Anything else
			// states a figure of a node it is not true of.
			want := c.Memory.Ratio
			if len(a.Nodes) > 0 {
				want = 0
				for _, name := range a.Nodes {
					n := mockNodeByName(t, c, name)
					if n.Memory == nil {
						t.Fatalf("cluster %s: memory_high lists %s, which has no memory figure", c.ID, name)
					}
					if n.Memory.Ratio > want {
						want = n.Memory.Ratio
					}
				}
			}
			if math.Abs(*a.Ratio-want) > 1e-9 {
				t.Errorf("cluster %s: alert ratio = %v, want %v", c.ID, *a.Ratio, want)
			}
			for _, name := range a.Nodes {
				listed[name] = true
			}
		}
		for _, n := range c.Nodes {
			if n.Memory == nil {
				// Unknown is not "below the threshold": an unmeasured node is
				// neither listed nor expected to be.
				if listed[n.Name] {
					t.Errorf("node %s has no memory figure but is listed as over the threshold", n.Name)
				}
				continue
			}
			over := n.Memory.Ratio > ov.Thresholds.Memory
			if over && !listed[n.Name] {
				t.Errorf("node %s is at %v, above the threshold, but is not listed", n.Name, n.Memory.Ratio)
			}
			if !over && listed[n.Name] {
				t.Errorf("node %s is at %v, below the threshold, but is listed", n.Name, n.Memory.Ratio)
			}
		}
	}
}

// TestMockIsStableAndIsolated checks that successive calls agree on everything
// but GeneratedAt, and that a caller mutating what it got back cannot corrupt
// the next call.
func TestMockIsStableAndIsolated(t *testing.T) {
	m := NewMock()
	ctx := context.Background()
	first, err := m.Overview(ctx)
	if err != nil {
		t.Fatalf("first Overview() returned an error: %v", err)
	}
	second, err := m.Overview(ctx)
	if err != nil {
		t.Fatalf("second Overview() returned an error: %v", err)
	}
	if first == second {
		t.Fatal("Overview() handed out the same pointer twice")
	}
	if !first.GeneratedAt.Equal(second.GeneratedAt) && !second.GeneratedAt.After(first.GeneratedAt) {
		t.Errorf("generatedAt went backwards: %v then %v", first.GeneratedAt, second.GeneratedAt)
	}
	if !sameApartFromGeneratedAt(*first, *second) {
		t.Fatal("two successive calls differ by more than GeneratedAt")
	}

	// Mutate everything reachable from the first payload, then check the mock
	// still serves the untouched data set.
	first.Clusters[0].Name = "tampered"
	first.Clusters[0].Nodes[0].Status = NodeOffline
	first.Clusters = first.Clusters[:1]
	*first.Clusters[0].FetchedAt = time.Unix(0, 0).UTC()

	third, err := m.Overview(ctx)
	if err != nil {
		t.Fatalf("third Overview() returned an error: %v", err)
	}
	if !sameApartFromGeneratedAt(*second, *third) {
		t.Error("mutating a returned payload leaked into the next call")
	}
}

// sameApartFromGeneratedAt compares two payloads, ignoring the only field the
// mock is allowed to move.
func sameApartFromGeneratedAt(a, b Overview) bool {
	a.GeneratedAt = time.Time{}
	b.GeneratedAt = time.Time{}
	return reflect.DeepEqual(a, b)
}

func TestMockMarshalsToJSON(t *testing.T) {
	if _, err := json.Marshal(mockOverview(t)); err != nil {
		t.Fatalf("json.Marshal() returned an error: %v", err)
	}
}

// guestCounts counts the guests listed under the nodes of a cluster, in the
// same terms as VMCounts.
func guestCounts(c ClusterOverview) VMCounts {
	var counts VMCounts
	for _, n := range c.Nodes {
		for _, g := range n.Guests {
			switch g.Status {
			case GuestRunning:
				counts.Running++
			case GuestStopped:
				counts.Stopped++
			case GuestTemplate:
				counts.Templates++
			}
		}
	}
	counts.Total = counts.Running + counts.Stopped
	return counts
}

// TestMockGuestsMatchVMCounts is the invariant of the whole generated
// population: the guests listed under the nodes are exactly the ones the
// counters of the cluster card claim, templates included.
func TestMockGuestsMatchVMCounts(t *testing.T) {
	ov := mockOverview(t)
	for _, c := range ov.Clusters {
		got := guestCounts(c)
		if got != c.VMs {
			t.Errorf("cluster %s: guests add up to %+v, VMCounts = %+v", c.ID, got, c.VMs)
		}
		var listed int
		for _, n := range c.Nodes {
			listed += len(n.Guests)
		}
		if want := c.VMs.Running + c.VMs.Stopped + c.VMs.Templates; listed != want {
			t.Errorf("cluster %s: %d guests listed, want %d", c.ID, listed, want)
		}
	}
}

// TestMockGuestsAreWellFormed walks every generated guest: sorted by VMID,
// never a nil slice, never a NaN ratio, and never an empty name or tag list.
func TestMockGuestsAreWellFormed(t *testing.T) {
	ov := mockOverview(t)
	for _, c := range ov.Clusters {
		for _, n := range c.Nodes {
			if n.Guests == nil {
				t.Fatalf("cluster %s node %s: guests = nil, want a slice", c.ID, n.Name)
			}
			for i, g := range n.Guests {
				if i > 0 && n.Guests[i-1].VMID >= g.VMID {
					t.Errorf("node %s: vmid %d follows %d, want them ascending",
						n.Name, g.VMID, n.Guests[i-1].VMID)
				}
				if g.Name == "" {
					t.Errorf("node %s: guest %d has no name", n.Name, g.VMID)
				}
				if g.Kind != GuestQemu && g.Kind != GuestLXC {
					t.Errorf("guest %d: kind = %q", g.VMID, g.Kind)
				}
				if len(g.Tags) == 0 {
					t.Errorf("guest %d: tags = %v, want at least the env tag", g.VMID, g.Tags)
				}
				if g.CPU.Cores <= 0 {
					t.Errorf("guest %d: cores = %d, want a positive count", g.VMID, g.CPU.Cores)
				}
				if g.Memory.Total == 0 {
					t.Errorf("guest %d: memory total = 0", g.VMID)
				}
				want := float64(g.Memory.Used) / float64(g.Memory.Total)
				if math.IsNaN(g.Memory.Ratio) || math.Abs(g.Memory.Ratio-want) > 1e-9 {
					t.Errorf("guest %d: memory ratio = %v, want %v", g.VMID, g.Memory.Ratio, want)
				}
				// Only a running guest consumes anything: a stopped one and a
				// template hold no memory and burn no CPU.
				if g.Status != GuestRunning && (g.Memory.Used != 0 || g.CPU.Ratio != 0) {
					t.Errorf("guest %d is %q but reports %d bytes and %v cpu",
						g.VMID, g.Status, g.Memory.Used, g.CPU.Ratio)
				}
			}
		}
	}
}

// TestMockGuestNamesAreUniquePerCluster keeps the generator from handing two
// guests of the same cluster the same name, which would make the sidebar tree
// unreadable.
func TestMockGuestNamesAreUniquePerCluster(t *testing.T) {
	ov := mockOverview(t)
	for _, c := range ov.Clusters {
		seen := make(map[string]int)
		for _, n := range c.Nodes {
			for _, g := range n.Guests {
				if other, dup := seen[g.Name]; dup {
					t.Errorf("cluster %s: %q is both vmid %d and %d", c.ID, g.Name, other, g.VMID)
				}
				seen[g.Name] = g.VMID
			}
		}
	}
}

// TestMockGuestNamesFollowTheConvention checks the generated names carry the
// long convention of the handoff document, suffixed by the cluster, and the
// env tag of their cluster.
func TestMockGuestNamesFollowTheConvention(t *testing.T) {
	suffixes := map[string]string{
		"qualification": "-qul",
		"preproduction": "-ppr",
		"production":    "-prd",
	}
	ov := mockOverview(t)
	for _, c := range ov.Clusters {
		tag := "env." + c.ID
		for _, n := range c.Nodes {
			for _, g := range n.Guests {
				if g.Status == GuestTemplate {
					if !strings.HasPrefix(g.Name, "template-") {
						t.Errorf("cluster %s: template %d is named %q", c.ID, g.VMID, g.Name)
					}
				} else if !strings.HasPrefix(g.Name, "sli-") || !strings.HasSuffix(g.Name, suffixes[c.ID]) {
					t.Errorf("cluster %s: guest %d is named %q, want sli-...%s",
						c.ID, g.VMID, g.Name, suffixes[c.ID])
				}
				var tagged bool
				for _, got := range g.Tags {
					tagged = tagged || got == tag
				}
				if !tagged {
					t.Errorf("guest %d: tags = %v, want %q among them", g.VMID, g.Tags, tag)
				}
			}
		}
	}
}

// TestMockGuestsHaveBothKinds checks the population is not made of VMs alone:
// the frontend has a container icon to exercise.
func TestMockGuestsHaveBothKinds(t *testing.T) {
	kinds := make(map[GuestKind]int)
	for _, c := range mockOverview(t).Clusters {
		for _, n := range c.Nodes {
			for _, g := range n.Guests {
				kinds[g.Kind]++
			}
		}
	}
	if kinds[GuestQemu] == 0 || kinds[GuestLXC] == 0 {
		t.Errorf("kinds = %v, want both qemu and lxc guests", kinds)
	}
}

// TestMockGuestsFitInTheirNode keeps the generated data set from contradicting
// itself: a node cannot host guests using more memory than the node itself
// reports as used.
func TestMockGuestsFitInTheirNode(t *testing.T) {
	for _, c := range mockOverview(t).Clusters {
		for _, n := range c.Nodes {
			if n.Memory == nil {
				// Nothing to compare against: an unmeasured node is unknown,
				// not empty. Its guests are still listed, and that is fine.
				continue
			}
			var used uint64
			for _, g := range n.Guests {
				used += g.Memory.Used
			}
			if used > n.Memory.Used {
				t.Errorf("node %s: guests use %d bytes, the node reports %d",
					n.Name, used, n.Memory.Used)
			}
		}
	}
}

// TestMockDrainedNodeHoldsNoGuest: the preproduction node is in maintenance
// precisely because its guests were migrated away, and the mockup shows the two
// others carrying them. An empty list is still a list.
func TestMockDrainedNodeHoldsNoGuest(t *testing.T) {
	c := cluster(t, mockOverview(t), "preproduction")
	drained := false
	for _, n := range c.Nodes {
		if n.Status != NodeMaintenance {
			if len(n.Guests) == 0 {
				t.Errorf("node %s is online but hosts nothing", n.Name)
			}
			continue
		}
		drained = true
		if n.Guests == nil {
			t.Errorf("node %s: guests = nil, want an empty slice", n.Name)
		}
		if len(n.Guests) != 0 {
			t.Errorf("node %s is drained but still hosts %d guests", n.Name, len(n.Guests))
		}
	}
	if !drained {
		t.Fatal("no node in maintenance in the preproduction cluster")
	}
}

// mockNodeByName returns the node of a mock cluster, failing the test rather
// than returning a zero value: a banner naming a node that is not in the card
// is itself the bug.
func mockNodeByName(t *testing.T, c ClusterOverview, name string) Node {
	t.Helper()
	for _, n := range c.Nodes {
		if n.Name == name {
			return n
		}
	}
	t.Fatalf("cluster %s: no node named %s", c.ID, name)
	return Node{}
}
