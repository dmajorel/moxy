package aggregate

import (
	"context"
	"errors"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/config"
	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

const (
	// pollInterval matches the refresh rate the UI is designed around. Polling
	// in the background rather than on request keeps the cost constant in the
	// number of viewers, and avoids a stampede whenever a cache entry expires.
	pollInterval = 5 * time.Second
	// pollBudgetMargin is what a poll round gets on top of the attempts it is
	// meant to allow: decoding, deriving, and the connect budget of the url
	// that is about to be tried.
	pollBudgetMargin = 2 * time.Second
	// maxFailoverAttempts is how many urls a single round is sized to try. A
	// cluster with a dozen node urls does not need to walk all of them inside
	// one round: the sticky index means the next round starts where this one
	// stopped, so the search continues rather than restarting.
	maxFailoverAttempts = 2

	// updatesInterval is deliberately long: pending packages change rarely, the
	// call costs one request per node, and it needs a privilege the token may
	// not even have.
	updatesInterval = 10 * time.Minute
	updatesBudget   = 30 * time.Second

	// staleAfter is how long a cluster keeps serving its last known snapshot
	// before it is reported as unreachable. The data stays in the payload: a
	// stale reading is more useful to an operator than an empty card.
	staleAfter = 60 * time.Second
	// StaleAfter is staleAfter, exported so that the daemon can warn when a
	// cluster is configured with budgets that cannot complete a round before
	// its own data is declared stale.
	StaleAfter = staleAfter
)

// auditClient is the part of *proxmox.Client a poll round uses.
//
// It is declared here, narrow, on the consumer side -- the same shape the
// detail service uses -- so that the poller can be driven by a fake with no
// network, no TLS and no httptest server. Without it the only way to exercise a
// round was to stand up a TLS server per case, and the cycle of the poller went
// untested: the first round, a partial failure, a cancelled context.
type auditClient interface {
	ClusterResources(ctx context.Context) ([]proxmox.Resource, error)
	ClusterStatus(ctx context.Context) ([]proxmox.ClusterStatusEntry, error)
	HAManagerStatus(ctx context.Context) (*proxmox.HAManagerStatus, error)
	AptUpdates(ctx context.Context, node string) ([]proxmox.AptUpdate, error)
}

// Poller keeps one background goroutine per cluster and serves whatever each of
// them last managed to collect.
//
// It implements the same contract as the mock, so the HTTP layer cannot tell
// them apart.
type Poller struct {
	threshold float64
	clusters  []*clusterState
	now       func() time.Time

	ready     chan struct{}
	readyOnce sync.Once
}

// clusterState is the mutable state of one cluster: its last successful card,
// when that was collected, and the error of the most recent failure.
type clusterState struct {
	identity        Identity
	client          auditClient
	memoryThreshold float64
	// budget bounds one poll round. It is derived from the cluster rather
	// than fixed, so that the url failover has room to reach a second node.
	budget time.Duration
	now    func() time.Time

	mu        sync.Mutex
	card      *ClusterOverview
	fetchedAt time.Time
	lastErr   *Error

	// updates is collected on its own slower schedule. A nil map means unknown,
	// which is not the same as "no pending updates".
	updates          map[string][]proxmox.AptUpdate
	updatesCheckedAt time.Time

	// ha is the last HA manager status that was actually read, with the
	// moment it was read. It is kept across a failed call: the HA endpoint is
	// the most fragile of the three, and losing it for one tick makes a node
	// being drained flicker back to "online".
	ha   *proxmox.HAManagerStatus
	haAt time.Time

	// carded is closed by the first poll that stores a card. The update check
	// waits on it: it asks each node in turn and has no node list to work from
	// until the cluster has been read once.
	carded     chan struct{}
	cardedOnce sync.Once
}

// PollBudgetFor is how long one poll round of this cluster may take.
//
// It used to be a flat six seconds, which quietly disabled the very failover
// the "urls" list exists for. With a four second per-call timeout and a first
// node that is wedged rather than down, that node consumed four seconds, the
// second got the remaining two, and the third was never tried; with a six
// second timeout the first attempt consumed the whole round on its own. Every
// tick then failed, and the cluster went unreachable after a minute while two
// of its three nodes were answering.
//
// The budget is therefore sized on the cluster: enough for maxFailoverAttempts
// full attempts, plus a margin. It may exceed pollInterval, and that is
// deliberate and harmless -- a round that overruns simply makes the ticker drop
// the tick it fired during, so rounds space out instead of piling up.
func PollBudgetFor(cl config.Cluster) time.Duration {
	timeout := cl.RequestTimeout
	if timeout <= 0 {
		timeout = config.DefaultTimeout
	}
	attempts := len(cl.URLs)
	if attempts > maxFailoverAttempts {
		attempts = maxFailoverAttempts
	}
	if attempts < 1 {
		attempts = 1
	}
	return time.Duration(attempts)*timeout + pollBudgetMargin
}

// NewPoller builds a poller from a validated configuration. It opens no
// connection: polling starts with Start.
func NewPoller(cfg *config.Config) (*Poller, error) {
	if cfg == nil || len(cfg.Clusters) == 0 {
		return nil, errors.New("no cluster configured")
	}

	states := make([]*clusterState, 0, len(cfg.Clusters))
	for _, cl := range cfg.Clusters {
		client, err := proxmox.New(cl)
		if err != nil {
			return nil, err
		}
		states = append(states, newClusterState(
			Identity{ID: cl.ID, Name: cl.Name, Color: cl.Color},
			client, PollBudgetFor(cl), cfg.Thresholds.Memory, nil,
		))
	}
	return newPoller(cfg.Thresholds.Memory, states, nil), nil
}

// newPoller is the constructor the tests use: it takes states already built,
// so a fake client and a controlled clock can stand in for a cluster.
func newPoller(threshold float64, clusters []*clusterState, now func() time.Time) *Poller {
	if now == nil {
		now = time.Now
	}
	return &Poller{
		threshold: threshold,
		clusters:  clusters,
		now:       now,
		ready:     make(chan struct{}),
	}
}

// newClusterState builds the mutable state of one cluster. now defaults to
// time.Now; the tests pass a clock they move by hand.
func newClusterState(id Identity, client auditClient, budget time.Duration, threshold float64, now func() time.Time) *clusterState {
	if now == nil {
		now = time.Now
	}
	return &clusterState{
		carded:          make(chan struct{}),
		budget:          budget,
		identity:        id,
		client:          client,
		memoryThreshold: threshold,
		now:             now,
	}
}

// Start launches the background goroutines and RETURNS IMMEDIATELY.
//
// It used to block until every cluster had completed a first attempt, so that
// the first HTTP response would carry real data. Overview already guarantees
// that on its own by waiting on Ready, and blocking here cost something else:
// the listener was not open yet, so /healthz answered "connection refused" for
// as long as the slowest cluster took. A liveness probe would then restart a
// perfectly healthy daemon because a cluster was slow, and the restart began
// the wait again.
func (p *Poller) Start(ctx context.Context) {
	var first sync.WaitGroup
	first.Add(len(p.clusters))

	for _, state := range p.clusters {
		state := state

		go func() {
			state.pollOnce(ctx)
			first.Done()

			ticker := time.NewTicker(pollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					state.pollOnce(ctx)
				}
			}
		}()

		go func() {
			// WAIT FOR THE FIRST CARD. pollUpdates asks each node in turn, and
			// the node list comes from the last derived card. Starting beside
			// the first poll meant finding no card, returning at once, and not
			// trying again for ten minutes: the update banner was missing for
			// that long after every restart, on a cluster whose token had the
			// privilege all along.
			select {
			case <-state.carded:
			case <-ctx.Done():
				return
			}

			// The update check then runs on its own schedule so a slow or
			// forbidden apt/update never delays the overview.
			state.pollUpdates(ctx)
			// Derive once more straight away. The card is built from the
			// counts held at the time of the poll, so without this the banner
			// would still wait for the next tick to appear -- and the banner
			// is exactly what an operator looks for right after a restart.
			// One extra round, once per process.
			state.pollOnce(ctx)

			ticker := time.NewTicker(updatesInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					state.pollUpdates(ctx)
				}
			}
		}()
	}

	go func() {
		done := make(chan struct{})
		go func() {
			first.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
		}
		p.readyOnce.Do(func() { close(p.ready) })
	}()
}

// Ready is closed once every cluster has completed a first poll attempt, or
// once ctx is done, whichever comes first. It is what /readyz reports and what
// Overview waits on: a reader arriving during the warm-up waits for real data
// rather than receiving a document with no cluster in it.
func (p *Poller) Ready() <-chan struct{} { return p.ready }

// Overview serves the current snapshot. It never blocks on the clusters: it
// reads what the background goroutines have already stored.
func (p *Poller) Overview(ctx context.Context) (*Overview, error) {
	select {
	case <-p.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	now := p.now()
	clusters := make([]ClusterOverview, 0, len(p.clusters))
	for _, state := range p.clusters {
		clusters = append(clusters, state.snapshot(now))
	}

	return &Overview{
		GeneratedAt: now.UTC(),
		Thresholds:  Thresholds{Memory: p.threshold},
		Totals:      ComputeTotals(clusters),
		Clusters:    clusters,
	}, nil
}

// pollOnce runs the three audit calls in parallel and derives a fresh card.
func (s *clusterState) pollOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()

	var (
		wg        sync.WaitGroup
		resources []proxmox.Resource
		status    []proxmox.ClusterStatusEntry
		ha        *proxmox.HAManagerStatus
		errRes    error
		errStatus error
		errHA     error
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		resources, errRes = s.client.ClusterResources(ctx)
	}()
	go func() {
		defer wg.Done()
		status, errStatus = s.client.ClusterStatus(ctx)
	}()
	go func() {
		defer wg.Done()
		// HA is optional: a standalone node has no HA manager, and a token may
		// lack the privilege. Its absence only costs the maintenance state.
		//
		// It is also the most fragile of the three calls, and the one whose
		// absence is most visible: without it a node being drained reads as
		// plain "online", so one failed tick made maintenance flicker off and
		// the cluster flip back to healthy. A remembered answer covers that,
		// up to the point where the overview would call itself stale anyway.
		got, err := s.client.HAManagerStatus(ctx)
		if err == nil {
			ha = got
			s.rememberHA(got)
			return
		}
		errHA = err
		ha = s.rememberedHA()
	}()
	wg.Wait()

	if errHA != nil {
		// Not a failure of the round -- the card is still derived -- but not
		// silent either: a token missing Sys.Audit on the HA tree looks
		// exactly like a cluster with no HA manager from the outside.
		kept := ""
		if ha != nil {
			kept = " (keeping the last known one)"
		}
		log.Printf("cluster %s: ha status unavailable%s: %v", s.identity.ID, kept, errHA)
	}

	if err := errRes; err != nil {
		s.recordFailure(err)
		return
	}
	if err := errStatus; err != nil {
		s.recordFailure(err)
		return
	}

	s.mu.Lock()
	updates, checkedAt := s.updates, s.updatesCheckedAt
	s.mu.Unlock()

	card := Derive(s.identity, ClusterData{
		Resources:        resources,
		Status:           status,
		HA:               ha,
		Updates:          updates,
		UpdatesCheckedAt: checkedAt,
	}, s.memoryThreshold)

	s.mu.Lock()
	s.card = &card
	s.fetchedAt = s.now()
	s.lastErr = nil
	s.mu.Unlock()

	// There is a node list now: release the update check. The channel is nil
	// in the unit tests that drive a clusterState directly, which never poll.
	if s.carded != nil {
		s.cardedOnce.Do(func() { close(s.carded) })
	}
}

// pollUpdates asks every known node for its pending packages. A node that
// answers 403 stays absent from the map, which the model reports as unknown
// rather than as zero.
func (s *clusterState) pollUpdates(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, updatesBudget)
	defer cancel()

	nodes := s.knownNodes()
	if len(nodes) == 0 {
		return
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = make(map[string][]proxmox.AptUpdate, len(nodes))
	)
	for _, node := range nodes {
		node := node
		wg.Add(1)
		go func() {
			defer wg.Done()
			pending, err := s.client.AptUpdates(ctx, node)
			if err != nil {
				return
			}
			mu.Lock()
			results[node] = pending
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(results) == 0 {
		// Nothing came back: most likely the token lacks Sys.Modify. Keep the
		// previous answer rather than flapping between known and unknown.
		return
	}

	s.mu.Lock()
	s.updates = results
	s.updatesCheckedAt = s.now()
	s.mu.Unlock()
}

// knownNodes lists the nodes of the last successful derivation, so the update
// check follows the cluster as it changes shape.
func (s *clusterState) knownNodes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.card == nil {
		return nil
	}
	nodes := make([]string, 0, len(s.card.Nodes))
	for _, n := range s.card.Nodes {
		if n.Status == NodeOffline || n.Status == NodeUnknown {
			continue
		}
		nodes = append(nodes, n.Name)
	}
	sort.Strings(nodes)
	return nodes
}

// recordFailure keeps the last failure of a cluster, and logs it.
//
// The two carry different texts on purpose. Message is served in the JSON of
// /api/overview and reaches the browser, so it holds the sanitised cause, which
// names no host, address or resolver. The log holds the full one: an operator
// reading the server's own output is entitled to know which address was
// dialled, and it is the only place that says so.
//
// Only a change is logged. The poll runs every five seconds, and a cluster
// that stays unreachable would otherwise write the same line twelve times a
// minute for as long as it is down.
// rememberHA stores a successful HA read.
func (s *clusterState) rememberHA(status *proxmox.HAManagerStatus) {
	s.mu.Lock()
	s.ha, s.haAt = status, s.now()
	s.mu.Unlock()
}

// rememberedHA returns the last HA status read, while it is recent enough to
// still describe the cluster. Past staleAfter it returns nil: the overview
// gives up on data of that age everywhere else, and claiming a node is still
// draining on the strength of a minute-old reading would be worse than saying
// nothing.
func (s *clusterState) rememberedHA() *proxmox.HAManagerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ha == nil || s.now().Sub(s.haAt) > staleAfter {
		return nil
	}
	return s.ha
}

func (s *clusterState) recordFailure(err error) {
	kind := "network"
	if k, ok := proxmox.KindOf(err); ok {
		kind = string(k)
	}

	message := err.Error()
	s.mu.Lock()
	changed := s.lastErr == nil || s.lastErr.Message != message || s.lastErr.Kind != kind
	s.lastErr = &Error{Kind: kind, Message: message}
	s.mu.Unlock()

	if changed {
		log.Printf("poll failed (%s): %s", kind, proxmox.Unsanitized(err))
	}
}

// snapshot returns a deep copy of the cluster card, with freshness applied.
func (s *clusterState) snapshot(now time.Time) ClusterOverview {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.card == nil {
		// No poll has ever succeeded: report the cluster, not an empty page.
		card := ClusterOverview{
			ID:     s.identity.ID,
			Name:   s.identity.Name,
			Color:  s.identity.Color,
			Status: StatusUnreachable,
			Error:  s.lastErr,
			Nodes:  []Node{},
			Alerts: []Alert{},
		}
		card.Alerts = append(card.Alerts, Alert{Kind: AlertUnreachable})
		return card
	}

	card := s.card.clone()
	fetchedAt := s.fetchedAt
	card.FetchedAt = &fetchedAt
	card.Error = s.lastErr

	if now.Sub(fetchedAt) > staleAfter {
		// The data below is the last known good reading; saying so is more
		// useful than dropping it.
		card.Status = StatusUnreachable
		card.Alerts = append(card.Alerts, Alert{Kind: AlertUnreachable})
	}
	return card
}

// clone deep-copies a card so a caller can never mutate poller state through a
// returned slice.
func (c *ClusterOverview) clone() ClusterOverview {
	out := *c

	if c.CPU != nil {
		cpu := *c.CPU
		out.CPU = &cpu
	}
	if c.Memory != nil {
		memory := *c.Memory
		out.Memory = &memory
	}

	out.Nodes = make([]Node, len(c.Nodes))
	copy(out.Nodes, c.Nodes)
	for i := range out.Nodes {
		// Added with the "unknown is not zero" change and missed here: a
		// caller writing through one of these would have reached across into
		// the state the poller serves to everybody else.
		if c.Nodes[i].CPU != nil {
			cpu := *c.Nodes[i].CPU
			out.Nodes[i].CPU = &cpu
		}
		if c.Nodes[i].Memory != nil {
			memory := *c.Nodes[i].Memory
			out.Nodes[i].Memory = &memory
		}
		if c.Nodes[i].PendingUpdates != nil {
			v := *c.Nodes[i].PendingUpdates
			out.Nodes[i].PendingUpdates = &v
		}

		// Guests and their tags are slices: copying the Node struct alone would
		// leave them shared with poller state.
		out.Nodes[i].Guests = make([]Guest, len(c.Nodes[i].Guests))
		copy(out.Nodes[i].Guests, c.Nodes[i].Guests)
		for j := range out.Nodes[i].Guests {
			tags := make([]string, len(c.Nodes[i].Guests[j].Tags))
			copy(tags, c.Nodes[i].Guests[j].Tags)
			out.Nodes[i].Guests[j].Tags = tags
		}
	}

	out.Alerts = make([]Alert, len(c.Alerts))
	copy(out.Alerts, c.Alerts)
	for i := range out.Alerts {
		if c.Alerts[i].Nodes != nil {
			nodes := make([]string, len(c.Alerts[i].Nodes))
			copy(nodes, c.Alerts[i].Nodes)
			out.Alerts[i].Nodes = nodes
		}
		if c.Alerts[i].Ratio != nil {
			v := *c.Alerts[i].Ratio
			out.Alerts[i].Ratio = &v
		}
		if c.Alerts[i].Version != nil {
			v := *c.Alerts[i].Version
			out.Alerts[i].Version = &v
		}
	}

	if c.Quorum != nil {
		q := *c.Quorum
		out.Quorum = &q
	}
	if c.FetchedAt != nil {
		t := *c.FetchedAt
		out.FetchedAt = &t
	}
	if c.Error != nil {
		e := *c.Error
		out.Error = &e
	}
	if c.Updates != nil {
		u := *c.Updates
		u.Nodes = make([]string, len(c.Updates.Nodes))
		copy(u.Nodes, c.Updates.Nodes)
		if c.Updates.PVEManagerVersion != nil {
			v := *c.Updates.PVEManagerVersion
			u.PVEManagerVersion = &v
		}
		out.Updates = &u
	}
	if c.Color != nil {
		v := *c.Color
		out.Color = &v
	}
	return out
}
