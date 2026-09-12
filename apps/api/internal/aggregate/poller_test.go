package aggregate

import (
	"testing"
	"time"
)

// A returned card must share no mutable state with the poller: a caller that
// sorts or trims a slice it received would otherwise corrupt the next response.
func TestCloneSharesNothingMutable(t *testing.T) {
	version := "9.2.12"
	ratio := 0.83
	pending := 4
	fetchedAt := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	color := "#378ADD"

	original := &ClusterOverview{
		ID:        "preproduction",
		Name:      "Préproduction",
		Color:     &color,
		Status:    StatusDegraded,
		FetchedAt: &fetchedAt,
		Error:     &Error{Kind: "timeout", Message: "deadline exceeded"},
		Quorum:    &Quorum{Quorate: true, Nodes: 3, Online: 3},
		Nodes: []Node{{
			Name:           "prox-pprd-2301-cit",
			Status:         NodeOnline,
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
