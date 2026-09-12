package aggregate

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
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
	want := Totals{Clusters: 3, Nodes: 11, NodesOnline: 11, VMs: 148, Alerts: 2}
	if ov.Totals != want {
		t.Errorf("Totals = %+v, want %+v", ov.Totals, want)
	}
	if got := len(ov.Clusters); got != want.Clusters {
		t.Errorf("len(Clusters) = %d, want %d", got, want.Clusters)
	}
	if ov.Thresholds.Memory != 0.80 {
		t.Errorf("Thresholds.Memory = %v, want 0.80", ov.Thresholds.Memory)
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
	// Every production node reports a count; the other clusters report none.
	for _, n := range c.Nodes {
		if n.PendingUpdates == nil {
			t.Errorf("node %s has no pending-update count", n.Name)
		}
	}
}

func TestMockPendingUpdatesUnknownOutsideProduction(t *testing.T) {
	ov := mockOverview(t)
	for _, c := range ov.Clusters {
		if c.ID == "production" {
			continue
		}
		if c.Updates != nil {
			t.Errorf("cluster %s: Updates = %+v, want nil", c.ID, c.Updates)
		}
		for _, n := range c.Nodes {
			if n.PendingUpdates != nil {
				t.Errorf("node %s: PendingUpdates = %d, want nil", n.Name, *n.PendingUpdates)
			}
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
				used += n.Memory.Used
				total += n.Memory.Total
				weighted += n.CPU.Ratio * float64(n.CPU.Cores)
				cores += n.CPU.Cores
				if got := float64(n.Memory.Used) / float64(n.Memory.Total); math.Abs(n.Memory.Ratio-got) > 1e-9 {
					t.Errorf("node %s: memory ratio = %v, want %v", n.Name, n.Memory.Ratio, got)
				}
				if n.Uptime <= 0 {
					t.Errorf("node %s: uptime = %d, want a positive duration", n.Name, n.Uptime)
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
			if c.Error != nil {
				t.Errorf("error = %+v, want nil", c.Error)
			}
			if c.Color != nil {
				t.Errorf("color = %q, want nil", *c.Color)
			}
		})
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
			if math.Abs(*a.Ratio-c.Memory.Ratio) > 1e-9 {
				t.Errorf("cluster %s: alert ratio = %v, cluster ratio = %v", c.ID, *a.Ratio, c.Memory.Ratio)
			}
			for _, name := range a.Nodes {
				listed[name] = true
			}
		}
		for _, n := range c.Nodes {
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
