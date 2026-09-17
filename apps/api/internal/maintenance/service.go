package maintenance

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
)

// NodeState is one member of the cluster, as the overview already reads it.
//
// The status is aggregate.NodeStatus and not a second vocabulary of this
// package: an operator who reads "en maintenance" on a card must not be told
// "hors ligne" by the route that drains the same node. The rules live in
// aggregate/rules.go and are not copied here.
type NodeState struct {
	Name   string
	Status aggregate.NodeStatus
}

// ClusterState is everything that has to be true before a session is opened,
// read from the cluster view the overview polls anyway.
//
// It is a plain struct handed in with the request rather than something this
// package fetches: the caller holds the cached view already (ADR 0005), the
// preconditions below are then pure, and the whole decision -- which node runs
// the command, and whether one runs at all -- is testable without a network.
type ClusterState struct {
	// Quorum is nil on a standalone node, which has no vote to lose. Nil is
	// therefore NOT "no quorum": it is "the question does not apply", and
	// refusing on it would refuse every single-node install forever.
	Quorum *aggregate.Quorum
	// HAManager says whether a CRM answered. False means nobody would honour
	// the command.
	HAManager bool
	// Nodes is every node the cluster view knows, the target included.
	Nodes []NodeState
}

// Request is one maintenance execution asked for.
type Request struct {
	Cluster string
	Node    string
	Action  string
	State   ClusterState
}

// Outcome is what the route reports. It mirrors detail.MaintenanceResult, which
// carries the JSON tags: the contract with the frontend lives there, this type
// is what crosses the package boundary.
//
// IT SAYS "THE REQUEST WENT THROUGH", NEVER "THE NODE IS DRAINED". The CRM
// command writes an intention into the cluster filesystem; the drain follows,
// asynchronously, and the real state keeps being read by the existing polling.
type Outcome struct {
	Cluster     string
	Node        string
	Action      string
	RequestedAt time.Time
	// Via is the node the command ran on, and is empty when none did.
	Via string
	// Accepted says the cluster now holds the requested intention, whether
	// this call put it there or found it already set.
	Accepted bool
	// AlreadyInState is the node being in the requested state before the
	// call, read BEFORE any session: it is not an error, and nothing ran.
	AlreadyInState bool
	// Output is the capped output of the command, and is nil when no command
	// ran -- nil means unknown, never an empty answer.
	Output *string
}

// Service executes maintenance requests. One per process.
type Service struct {
	ssh      SSHOptions
	clusters map[string]ClusterOptions
	// keys is the single key provider of the process, chosen once at load
	// time. There is no map from cluster to provider and no mode parameter
	// travelling down the call: the function that opens a session does not
	// know which mode is active, which is what makes mixing two impossible.
	keys   KeyProvider
	runner Runner
	now    func() time.Time

	// mu guards running, the set of (cluster, node) pairs an execution is open
	// on.
	//
	// THIS IS THE OPPOSITE OF THE ANTI-STAMPEDE LOCK OF ADR 0005. That one
	// coalesces concurrent READS: ten tabs on one node share a single upstream
	// call and all get the answer. Here two concurrent WRITES refuse each
	// other -- the second is told already_running at once, and never waits.
	// Waiting would queue a second privileged command behind the first, which
	// is precisely what a double click must not do.
	mu      sync.Mutex
	running map[string]struct{}
}

// NewService builds the service. Options left zero take the defaults of the
// SSH block; a nil clock is time.Now.
func NewService(opts Options, keys KeyProvider, runner Runner) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	clusters := make(map[string]ClusterOptions, len(opts.Clusters))
	for id, c := range opts.Clusters {
		clusters[id] = c
	}
	return &Service{
		ssh:      opts.SSH.withDefaults(),
		clusters: clusters,
		keys:     keys,
		runner:   runner,
		now:      now,
		running:  make(map[string]struct{}),
	}
}

// Enabled reports whether this cluster has maintenance turned on. A cluster
// that has not is one the route answers 404 for, and the UI shows no button
// for -- not a disabled one with a tooltip.
func (s *Service) Enabled(cluster string) bool {
	c, ok := s.clusters[cluster]
	return ok && c.Enabled
}

// Execute puts one node into maintenance, or takes it out.
//
// The order of what it refuses is the order of §6 of the issue: what is not
// there (404), what the caller got wrong (400), what is already running, what
// the cluster cannot honour, then the key source, then the nodes. Everything
// before the first session is a refusal that touched nothing.
func (s *Service) Execute(ctx context.Context, req Request) (*Outcome, error) {
	cluster, ok := s.clusters[req.Cluster]
	if !ok || !cluster.Enabled {
		return nil, ErrNotFound
	}
	if req.Action != ActionEnable && req.Action != ActionDisable {
		return nil, ErrInvalidAction
	}
	targetNode, known := req.State.node(req.Node)
	if !known || !validNodeName(req.Node) {
		return nil, ErrNotFound
	}

	// The claim is taken before the preconditions, not after: whether a second
	// concurrent request is refused must not depend on how far the first one
	// got through a list of checks.
	if !s.claim(req.Cluster, req.Node) {
		return nil, newError(req.Cluster, req.Node, KindAlreadyRunning, nil)
	}
	defer s.release(req.Cluster, req.Node)

	outcome := &Outcome{
		Cluster:     req.Cluster,
		Node:        req.Node,
		Action:      req.Action,
		RequestedAt: s.now(),
	}

	// Without quorum pmxcfs is read-only: the CRM command cannot be written,
	// so there is nothing a session could achieve.
	if q := req.State.Quorum; q != nil && !q.Quorate {
		return nil, newError(req.Cluster, req.Node, KindNoQuorum, nil)
	}
	// Without an HA manager there is no CRM to honour the command.
	if !req.State.HAManager {
		return nil, newError(req.Cluster, req.Node, KindNoHAManager, nil)
	}

	// Read before the execution, and reported as a success: asking for a state
	// the node is already in is not a failure, and running the command anyway
	// would open a session to no purpose.
	if alreadyInState(targetNode.Status, req.Action) {
		outcome.Accepted = true
		outcome.AlreadyInState = true
		return outcome, nil
	}

	candidates := s.candidates(cluster, req.State, req.Node)
	if len(candidates) == 0 {
		return nil, newError(req.Cluster, req.Node, KindNoOtherNode, nil)
	}

	// Once per execution, never cached across executions: see KeyProvider.
	// The retries below share this one credential because they are retries of
	// the same execution, not executions of their own.
	cred, err := s.keys.Credential(ctx)
	if err != nil {
		return nil, keySourceError(req.Cluster, err)
	}
	if cred == nil {
		return nil, newError(req.Cluster, "", KindKeySourceUnavailable, errNoCredential)
	}

	command := buildCommand(req.Action, req.Node)

	var last error
	for _, target := range candidates {
		result, err := s.attempt(ctx, target, cred, command)
		if err != nil {
			failure := runError(req.Cluster, target.Node, err)
			if !failure.Retryable() {
				// APPLICATIVE: the node side answered, so the command may
				// have taken effect. No other node is tried.
				return nil, failure
			}
			// TRANSPORT: nothing reached ha-manager, so the next node is
			// safe to try.
			last = failure
			if ctx.Err() != nil {
				// The whole request is over; trying the next node would only
				// produce the same failure against a dead context.
				break
			}
			continue
		}
		output := capOutput(result.Output)
		outcome.Via = target.Node
		outcome.Accepted = true
		outcome.Output = &output
		return outcome, nil
	}
	return nil, last
}

// attempt runs the command on one node, under the session budget.
//
// The budget is applied here rather than passed to the runner because Target is
// frozen and carries no timeouts: a runner that honours its context honours the
// budget, and one that does not is cut off by it anyway.
func (s *Service) attempt(ctx context.Context, target Target, cred *Credential, command string) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, s.ssh.DialTimeout+s.ssh.RunTimeout)
	defer cancel()

	result, err := s.runner.Run(ctx, target, cred, command)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errNoResult
	}
	if result.ExitCode != 0 {
		return nil, exitError(result.ExitCode)
	}
	return result, nil
}

// candidates are the nodes the command may run on, in the order they are tried.
//
// NEVER THE TARGET. The node being put into maintenance is often precisely the
// machine about to be switched off, it may already be unreachable, and a
// session opened on it can die with the command half sent.
//
// A node being drained is still a candidate: it runs, it holds quorum and its
// pmxcfs is writable -- it merely refuses new guests. Excluding it would leave
// a two-node cluster with nowhere to run the second drain from. It is tried
// after the plainly online ones, which is the whole of the ordering rule:
// online first, then maintenance, each alphabetically. Deterministic, so a
// test can assert which node was chosen, and so two operators reading the
// audit log see the same node picked for the same cluster.
func (s *Service) candidates(cluster ClusterOptions, state ClusterState, node string) []Target {
	type candidate struct {
		name string
		rank int
	}
	picked := make([]candidate, 0, len(state.Nodes))
	for _, n := range state.Nodes {
		if n.Name == node || !validNodeName(n.Name) {
			continue
		}
		switch n.Status {
		case aggregate.NodeOnline:
			picked = append(picked, candidate{name: n.Name, rank: 0})
		case aggregate.NodeMaintenance:
			picked = append(picked, candidate{name: n.Name, rank: 1})
		default:
			// Offline, or never seen by an authoritative source: not reachable
			// as far as anything here knows.
		}
	}
	sort.Slice(picked, func(i, j int) bool {
		if picked[i].rank != picked[j].rank {
			return picked[i].rank < picked[j].rank
		}
		return picked[i].name < picked[j].name
	})

	targets := make([]Target, 0, len(picked))
	for _, c := range picked {
		targets = append(targets, Target{
			Node: c.name,
			Host: cluster.host(c.name),
			Port: s.ssh.Port,
			User: s.ssh.User,
		})
	}
	return targets
}

// host is the address a session to this node is opened on. A node with no
// entry is reached at its own name: hosts is an override table for the estates
// where the node name does not resolve, not a list of the nodes that exist.
func (c ClusterOptions) host(node string) string {
	if host, ok := c.Hosts[node]; ok && host != "" {
		return host
	}
	return node
}

// node finds one node of the state.
func (s ClusterState) node(name string) (NodeState, bool) {
	for _, n := range s.Nodes {
		if n.Name == name {
			return n, true
		}
	}
	return NodeState{}, false
}

// alreadyInState reports whether the node already holds what is being asked
// for. The HA manager is the authority on both answers, and it is the same
// source aggregate.NodeStatusOf reads, so this cannot disagree with what the
// card and the tree show.
func alreadyInState(status aggregate.NodeStatus, action string) bool {
	if action == ActionEnable {
		return status == aggregate.NodeMaintenance
	}
	return status != aggregate.NodeMaintenance
}

// claim takes the lock for one (cluster, node) pair, and reports whether it got
// it. It never waits.
func (s *Service) claim(cluster, node string) bool {
	key := cluster + "\x00" + node
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.running[key]; busy {
		return false
	}
	s.running[key] = struct{}{}
	return true
}

// release drops the lock taken by claim.
func (s *Service) release(cluster, node string) {
	key := cluster + "\x00" + node
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, key)
}

// Causes this package produces itself. They carry no detail from a node or a
// key source: what an operator needs is the kind, and what a node said belongs
// in the output field, not in an error string that is logged.
var (
	errNoCredential = errors.New("key source returned no credential")
	errNoResult     = errors.New("runner returned no result")
)

// exitCodeError is a non-zero exit of the node-side command, before it is
// turned into a kind.
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return "command exited non-zero" }

func exitError(code int) error { return exitCodeError{code: code} }

// keySourceError classifies a failure to obtain the credential.
//
// It happens before any session exists: no node has been contacted, and the
// message must say so. An implementation that knows better -- OpenBao telling
// a sealed vault from a refused role -- says so by returning an *Error of its
// own, which is kept as it stands.
func keySourceError(cluster string, err error) error {
	var known *Error
	if errors.As(err, &known) {
		if known.Cluster == "" {
			known.Cluster = cluster
		}
		return known
	}
	// Anything else, a timed-out request included, is "the source did not
	// answer": denial is a claim only the implementation can make, since it is
	// the one that read the refusal.
	return newError(cluster, "", KindKeySourceUnavailable, err)
}

// runError classifies a failed attempt on one node.
//
// AN ERROR IT CANNOT CLASSIFY IS APPLICATIVE. That is the safe reading and not
// a shortcut: a transport kind is a claim that the command never reached
// ha-manager, and a failure nobody classified is one nothing proves stopped
// short of it. Guessing "unreachable" would make an unknown failure replay a
// privileged command on every node of the cluster.
func runError(cluster, node string, err error) *Error {
	var known *Error
	if errors.As(err, &known) {
		if known.Cluster == "" {
			known.Cluster = cluster
		}
		if known.Node == "" {
			known.Node = node
		}
		return known
	}
	var exit exitCodeError
	if errors.As(err, &exit) {
		kind := KindCommandFailed
		if exit.code == exitBadRequest || exit.code == exitUnknownNode {
			// The node-side validator refused the request itself, which every
			// other node would refuse identically.
			kind = KindCommandRefused
		}
		return &Error{Cluster: cluster, Node: node, Kind: kind, ExitCode: exit.code, Err: err}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		// The budget ran out: connection, handshake or command. Nothing was
		// acknowledged, so the next node may be tried.
		return newError(cluster, node, KindTimeout, err)
	}
	return newError(cluster, node, KindCommandFailed, err)
}
