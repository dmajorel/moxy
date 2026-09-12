package detail

import (
	"context"
	"sort"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

// DefaultMemoryThreshold is the share of memory a target node must stay under
// once it has absorbed the guests of a drained one. It matches the threshold
// the overview uses for its memory alert.
const DefaultMemoryThreshold = 0.80

// MaintenancePlan answers, before anything is done: what would move, where, and
// does the rest of the cluster have room for it.
//
// The handoff is explicit that a confirmation dialog saying only "are you
// sure?" is worthless. This is the material that replaces it, and it is
// strictly read-only: computing the plan changes nothing.
type MaintenancePlan struct {
	Cluster   string    `json:"cluster"`
	Node      string    `json:"node"`
	FetchedAt time.Time `json:"fetchedAt"`
	// Threshold is the memory share each target must stay under, as a fraction.
	Threshold float64 `json:"threshold"`
	// Feasible is false as soon as one guest cannot be placed within the
	// threshold, or there is nowhere to place it at all.
	Feasible bool `json:"feasible"`
	// Moves is sorted by descending memory, which is also the order the
	// placement considered them in.
	Moves   []PlannedMove  `json:"moves"`
	Staying []StayingGuest `json:"staying"`
	Targets []TargetNode   `json:"targets"`
	// Blockers name what prevents the plan, using stable keys the frontend
	// translates: no_target, source_unknown, source_offline.
	Blockers []string `json:"blockers"`
}

// PlannedMove is one guest and the node it would land on.
type PlannedMove struct {
	VMID   int                   `json:"vmid"`
	Name   string                `json:"name"`
	Kind   aggregate.GuestKind   `json:"kind"`
	Status aggregate.GuestStatus `json:"status"`
	// Memory is the figure the capacity check used: the guest's configured
	// maximum while it runs, and zero once stopped, since a stopped guest
	// reserves nothing on its target until it is started again.
	Memory uint64 `json:"memory"`
	// Target is empty when nowhere could take this guest.
	Target string `json:"target"`
	Placed bool   `json:"placed"`
}

// StayingGuest is a guest the drain would leave where it is.
type StayingGuest struct {
	VMID int    `json:"vmid"`
	Name string `json:"name"`
	// Reason is a stable key, not a sentence: template.
	Reason string `json:"reason"`
}

// TargetNode reports a candidate before and after absorbing its share.
type TargetNode struct {
	Name   string          `json:"name"`
	Before aggregate.Usage `json:"before"`
	After  aggregate.Usage `json:"after"`
	// Incoming counts the guests the plan sends here.
	Incoming int `json:"incoming"`
	// Exceeds is true when After crosses the threshold. That is what the
	// dialog must show before the click, not after.
	Exceeds bool `json:"exceeds"`
}

// MaintenancePlan computes the plan for draining one node.
func (s *Service) MaintenancePlan(ctx context.Context, cluster, node string) (*MaintenancePlan, error) {
	client, err := s.client(cluster)
	if err != nil {
		return nil, err
	}
	view, err := s.clusterView(ctx, cluster, client)
	if err != nil {
		return nil, err
	}

	plan := buildPlan(cluster, node, view.Value, DefaultMemoryThreshold)
	if plan == nil {
		return nil, notFoundf("node %q in cluster %q", node, cluster)
	}
	plan.FetchedAt = view.At
	return plan, nil
}

// buildPlan is pure: no clock, no network, no globals. It returns nil when the
// node is not part of the cluster at all.
func buildPlan(cluster, node string, view clusterView, threshold float64) *MaintenancePlan {
	online := onlineNodes(view)
	if _, known := online[node]; !known && !nodeExists(view, node) {
		return nil
	}

	plan := &MaintenancePlan{
		Cluster:   cluster,
		Node:      node,
		Threshold: threshold,
		Moves:     []PlannedMove{},
		Staying:   []StayingGuest{},
		Targets:   []TargetNode{},
		Blockers:  []string{},
	}

	if _, up := online[node]; !up {
		// Draining a node that is already down is not a maintenance plan; the
		// guests are not running there to be moved.
		plan.Blockers = append(plan.Blockers, "source_offline")
	}

	// Candidate targets: every other node the cluster reports as online. A node
	// already in maintenance is excluded — it refuses new guests by definition.
	targets := make([]*TargetNode, 0, len(online))
	for _, resource := range view.Resources {
		if resource.Type != proxmox.ResourceTypeNode || resource.Node == node {
			continue
		}
		if _, up := online[resource.Node]; !up {
			continue
		}
		if maintenanceState(view, resource.Node) {
			continue
		}
		used := asBytes(resource.Mem.Int())
		total := asBytes(resource.MaxMem.Int())
		target := &TargetNode{
			Name:   resource.Node,
			Before: usage(used, total),
			After:  usage(used, total),
		}
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })

	// Guests hosted by the node under consideration.
	type candidate struct {
		resource proxmox.Resource
		memory   uint64
	}
	candidates := make([]candidate, 0)
	for _, resource := range view.Resources {
		if !resource.IsGuest() || resource.Node != node {
			continue
		}
		if resource.Template.Bool() {
			// A template is not running anywhere; the handoff says it stays.
			plan.Staying = append(plan.Staying, StayingGuest{
				VMID:   int(resource.VMID.Int()),
				Name:   resource.Name,
				Reason: "template",
			})
			continue
		}
		var memory uint64
		if resource.Status == proxmox.StatusRunning {
			memory = asBytes(resource.MaxMem.Int())
		}
		candidates = append(candidates, candidate{resource: resource, memory: memory})
	}

	sort.Slice(plan.Staying, func(i, j int) bool { return plan.Staying[i].VMID < plan.Staying[j].VMID })

	// Largest first: placing the big guests while the cluster is still empty is
	// what keeps a plan feasible when it barely fits.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].memory != candidates[j].memory {
			return candidates[i].memory > candidates[j].memory
		}
		return candidates[i].resource.VMID.Int() < candidates[j].resource.VMID.Int()
	})

	if len(targets) == 0 && len(candidates) > 0 {
		plan.Blockers = append(plan.Blockers, "no_target")
	}

	feasible := true
	for _, c := range candidates {
		move := PlannedMove{
			VMID:   int(c.resource.VMID.Int()),
			Name:   c.resource.Name,
			Kind:   guestKindOf(c.resource),
			Status: guestStatusOf(false, c.resource.Status),
			Memory: c.memory,
		}

		if best := place(targets, c.memory, threshold); best != nil {
			best.After = usage(best.After.Used+c.memory, best.After.Total)
			best.Incoming++
			move.Target = best.Name
			move.Placed = true
		} else {
			feasible = false
		}
		plan.Moves = append(plan.Moves, move)
	}

	for _, target := range targets {
		// Exceeds is informational, and deliberately does not decide
		// feasibility: place() already refuses to send a guest to a node that
		// would end up over the threshold, so a target flagged here is one that
		// was already full before this plan and receives nothing. Letting that
		// mark the drain impossible would refuse a node with nothing to move.
		target.Exceeds = target.After.Total > 0 && target.After.Ratio > threshold
		plan.Targets = append(plan.Targets, *target)
	}

	plan.Feasible = feasible && len(plan.Blockers) == 0
	return plan
}

// place picks the target that would be least loaded afterwards, among those
// that stay under the threshold. Returning nil means nowhere has room.
func place(targets []*TargetNode, memory uint64, threshold float64) *TargetNode {
	var best *TargetNode
	var bestRatio float64
	for _, target := range targets {
		if target.After.Total == 0 {
			continue
		}
		after := usage(target.After.Used+memory, target.After.Total)
		if after.Ratio > threshold {
			continue
		}
		if best == nil || after.Ratio < bestRatio {
			best, bestRatio = target, after.Ratio
		}
	}
	return best
}

func onlineNodes(view clusterView) map[string]struct{} {
	online := make(map[string]struct{})
	for _, entry := range view.Status {
		if entry.Type == proxmox.ClusterStatusTypeNode && entry.Online.Bool() {
			online[entry.Name] = struct{}{}
		}
	}
	return online
}

func nodeExists(view clusterView, node string) bool {
	for _, entry := range view.Status {
		if entry.Type == proxmox.ClusterStatusTypeNode && entry.Name == node {
			return true
		}
	}
	for _, resource := range view.Resources {
		if resource.Type == proxmox.ResourceTypeNode && resource.Node == node {
			return true
		}
	}
	return false
}

func maintenanceState(view clusterView, node string) bool {
	return view.HA != nil && view.HA.NodeState(node) == proxmox.HANodeMaintenance
}

func guestKindOf(resource proxmox.Resource) aggregate.GuestKind {
	if resource.Type == proxmox.ResourceTypeLXC {
		return aggregate.GuestLXC
	}
	return aggregate.GuestQemu
}
