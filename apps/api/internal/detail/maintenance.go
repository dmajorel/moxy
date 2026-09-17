package detail

import (
	"context"
	"errors"
	"sort"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
	"github.com/dmajorel/moxy/apps/api/internal/maintenance"
	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

// MaintenanceExecutor is the part of *maintenance.Service this package uses.
//
// It is declared here, narrow, on the consumer side, for the same reason
// clusterClient is: what this package needs is "does this cluster take part"
// and "run one request", and depending on that pair rather than on a concrete
// type is what lets the tests below run without a key source, without a
// runner, and without a session that would have to be opened on something.
type MaintenanceExecutor interface {
	// Enabled reports whether this cluster takes part in maintenance. A
	// cluster that does not is one the route answers 404 for and the UI shows
	// no button for.
	Enabled(cluster string) bool
	Execute(ctx context.Context, req maintenance.Request) (*maintenance.Outcome, error)
}

// SetMaintenance wires the executor that POST .../maintenance runs through.
//
// It is called ONCE, at wiring time, before the service serves anything: the
// executor is a property of the process, chosen from the configuration at
// start-up, and there is deliberately no way to swap it while requests are in
// flight. A nil executor -- no maintenance block in the configuration at all
// -- leaves every cluster answering 404, which is exactly the state the whole
// estate was in before ADR 0010.
func (s *Service) SetMaintenance(exec MaintenanceExecutor) { s.maintenance = exec }

// maintenanceEnabled answers the one question both the node payload and the
// route ask: can this deployment really drain a node of this cluster?
func (s *Service) maintenanceEnabled(cluster string) bool {
	return s.maintenance != nil && s.maintenance.Enabled(cluster)
}

// errNoOutcome is a defensive guard, not a case that upstream produces: the
// executor is an interface, and one returning neither an outcome nor an error
// must fail a request rather than panic inside an HTTP handler.
var errNoOutcome = errors.New("detail: maintenance returned no outcome")

// ExecuteMaintenance serves POST /api/clusters/{cluster}/nodes/{node}/maintenance.
//
// It is the one route of this package that WRITES, and it is not a detail view
// with a verb: what it does is gather the state the decision needs -- quorum,
// whether a CRM is there to honour the command, and every node with its status
// -- and hand it to internal/maintenance, which owns the preconditions, the
// choice of the node the command runs on, and the retry rule.
//
// ERRORS TRAVEL THROUGH UNCHANGED. Every failure of the maintenance package
// carries a Kind, which is what the HTTP layer turns into a status code and
// the frontend into a French sentence; wrapping one in a way that hid it would
// leave the browser with a 500 and no vocabulary. The two sentinels that carry
// no kind -- ErrNotFound and ErrInvalidAction -- pass through just as they are.
func (s *Service) ExecuteMaintenance(ctx context.Context, cluster, node, action string) (*MaintenanceResult, error) {
	// Refused before anything is fetched: a cluster that does not take part
	// has no route, and an action outside the two verbs is the caller's
	// mistake. Neither is worth a fan-out against a hypervisor.
	if !s.maintenanceEnabled(cluster) {
		return nil, maintenance.ErrNotFound
	}
	if action != maintenance.ActionEnable && action != maintenance.ActionDisable {
		return nil, maintenance.ErrInvalidAction
	}

	client, err := s.client(cluster)
	if err != nil {
		return nil, err
	}

	// THE BUDGET BOUNDS THE READ, NOT THE EXECUTION. s.budget is what one
	// detail request may spend on PVE; an execution has its own budget per
	// session, applied upstream, and tries the next node when the transport
	// fails -- a total that legitimately exceeds this one. Bounding the whole
	// call here would cut a perfectly healthy second attempt short.
	viewCtx, cancel := context.WithTimeout(ctx, s.budget)
	view, err := s.clusterView(viewCtx, cluster, client)
	cancel()
	if err != nil {
		return nil, err
	}

	outcome, err := s.maintenance.Execute(ctx, maintenance.Request{
		Cluster: cluster,
		Node:    node,
		Action:  action,
		State:   clusterStateOf(view.Value),
	})
	if err != nil {
		return nil, err
	}
	result := maintenanceResultOf(outcome)
	if result == nil {
		return nil, errNoOutcome
	}
	return result, nil
}

// clusterStateOf reads the preconditions of an execution off the cluster view
// the detail routes already hold.
//
// THE RULES ARE THE SHARED ONES. Quorum and the node status come from
// aggregate/rules.go, not from a second reading made here: an operator told
// "en maintenance" on a card must not be told "hors ligne" by the route that
// drains the same node, and upstream picks the node to run the command on from
// this very status.
func clusterStateOf(view clusterView) maintenance.ClusterState {
	names := nodeNames(view)
	nodes := make([]maintenance.NodeState, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, maintenance.NodeState{
			Name:   name,
			Status: aggregate.NodeStatusOf(name, view.Status, view.HA),
		})
	}
	return maintenance.ClusterState{
		// Nil is "the question does not apply", not "no quorum": a standalone
		// node has no vote to lose, and refusing on nil would refuse every
		// single-node install forever.
		Quorum:    aggregate.QuorumOf(view.Status),
		HAManager: hasHAManager(view),
		Nodes:     nodes,
	}
}

// hasHAManager reports whether a CRM answered for this cluster.
//
// It is the same reading deriveNodeHAState makes, deliberately: the issue
// states the refusal as "haState nil everywhere", and a manager status with no
// node in it is exactly what leaves every node page showing an em dash. The
// two must not disagree, or the UI would offer a button for a cluster whose
// own pages say no CRM is watching.
func hasHAManager(view clusterView) bool {
	return view.HA != nil && len(view.HA.NodeStatus) > 0
}

// nodeNames lists every node the cluster knows, sorted.
//
// Membership is the one hasNode already defines -- /cluster/status for the
// members, /cluster/resources for a node that has just joined -- because
// upstream refuses a target absent from this list with ErrNotFound, and a
// narrower list would answer 404 for a node whose own page exists.
func nodeNames(view clusterView) []string {
	seen := make(map[string]struct{})
	add := func(name string) {
		if name == "" {
			return
		}
		seen[name] = struct{}{}
	}
	for _, e := range view.Status {
		if e.Type == proxmox.ClusterStatusTypeNode {
			add(e.Name)
		}
	}
	for _, r := range view.Resources {
		if r.Type == proxmox.ResourceTypeNode {
			add(r.Node)
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// maintenanceResultOf turns the outcome into the JSON contract. It returns nil
// for a nil outcome, which the caller turns into an error.
func maintenanceResultOf(outcome *maintenance.Outcome) *MaintenanceResult {
	if outcome == nil {
		return nil
	}
	result := &MaintenanceResult{
		Cluster:        outcome.Cluster,
		Node:           outcome.Node,
		Action:         outcome.Action,
		RequestedAt:    outcome.RequestedAt,
		Via:            outcome.Via,
		Accepted:       outcome.Accepted,
		AlreadyInState: outcome.AlreadyInState,
	}
	if outcome.Output != nil {
		// Copied rather than aliased: the payload outlives the call, and a
		// caller that could reach back into the outcome would be a way to
		// edit an answer already serialised somewhere else.
		output := *outcome.Output
		result.Output = &output
	}
	return result
}
