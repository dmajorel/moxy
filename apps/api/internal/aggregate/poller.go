package aggregate

import (
	"context"
	"errors"
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
	// pollBudget bounds one round of the three audit calls.
	pollBudget = 6 * time.Second

	// updatesInterval is deliberately long: pending packages change rarely, the
	// call costs one request per node, and it needs a privilege the token may
	// not even have.
	updatesInterval = 10 * time.Minute
	updatesBudget   = 30 * time.Second

	// staleAfter is how long a cluster keeps serving its last known snapshot
	// before it is reported as unreachable. The data stays in the payload: a
	// stale reading is more useful to an operator than an empty card.
	staleAfter = 60 * time.Second
)

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
	client          *proxmox.Client
	memoryThreshold float64
	now             func() time.Time

	mu        sync.Mutex
	card      *ClusterOverview
	fetchedAt time.Time
	lastErr   *Error

	// updates is collected on its own slower schedule. A nil map means unknown,
	// which is not the same as "no pending updates".
	updates          map[string][]proxmox.AptUpdate
	updatesCheckedAt time.Time
}

// NewPoller builds a poller from a validated configuration. It opens no
// connection: polling starts with Start.
func NewPoller(cfg *config.Config) (*Poller, error) {
	if cfg == nil || len(cfg.Clusters) == 0 {
		return nil, errors.New("no cluster configured")
	}

	p := &Poller{
		threshold: cfg.Thresholds.Memory,
		now:       time.Now,
		ready:     make(chan struct{}),
	}
	for _, cl := range cfg.Clusters {
		client, err := proxmox.New(cl)
		if err != nil {
			return nil, err
		}
		p.clusters = append(p.clusters, &clusterState{
			identity:        Identity{ID: cl.ID, Name: cl.Name, Color: cl.Color},
			client:          client,
			memoryThreshold: cfg.Thresholds.Memory,
			now:             time.Now,
		})
	}
	return p, nil
}

// Start launches the background goroutines and returns once every cluster has
// completed a first attempt, so that the first HTTP response carries real data
// rather than an empty payload. It gives up waiting when ctx is done.
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
			// The update check runs on its own schedule so a slow or forbidden
			// apt/update never delays the overview.
			state.pollUpdates(ctx)

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
}

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
	ctx, cancel := context.WithTimeout(ctx, pollBudget)
	defer cancel()

	var (
		wg        sync.WaitGroup
		resources []proxmox.Resource
		status    []proxmox.ClusterStatusEntry
		ha        *proxmox.HAManagerStatus
		errRes    error
		errStatus error
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
		if got, err := s.client.HAManagerStatus(ctx); err == nil {
			ha = got
		}
	}()
	wg.Wait()

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

func (s *clusterState) recordFailure(err error) {
	kind := "network"
	if k, ok := proxmox.KindOf(err); ok {
		kind = string(k)
	}

	s.mu.Lock()
	s.lastErr = &Error{Kind: kind, Message: err.Error()}
	s.mu.Unlock()
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

	out.Nodes = make([]Node, len(c.Nodes))
	copy(out.Nodes, c.Nodes)
	for i := range out.Nodes {
		if c.Nodes[i].PendingUpdates != nil {
			v := *c.Nodes[i].PendingUpdates
			out.Nodes[i].PendingUpdates = &v
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
