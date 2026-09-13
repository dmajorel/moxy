package detail

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

// ErrNotFound is returned when the cluster, node or guest does not exist.
var ErrNotFound = errors.New("not found")

// Defaults of the service.
const (
	// DefaultTTL is how long a fetched detail is served from the cache. It
	// matches the refresh interval of the UI: a shorter one would let every
	// refresh through, a longer one would show an operator a reading older
	// than the page they are looking at.
	DefaultTTL = 5 * time.Second

	// fetchBudget bounds one upstream call. It is deliberately larger than
	// the per-request timeout of the proxmox client, which fails over between
	// the nodes of a cluster within it.
	fetchBudget = 15 * time.Second

	// requestBudget bounds one whole detail request, fan-out included.
	requestBudget = 20 * time.Second

	// updatesTTL is the lifetime of a pending-updates answer. It is far
	// longer than DefaultTTL on purpose: apt/update is the most expensive
	// call of the set, its answer changes about once a day, and the overview
	// poller itself only asks every ten minutes.
	updatesTTL = 5 * time.Minute

	// Task list bounds. Zero means "the caller did not say", not "none".
	defaultTaskLimit = 50
	maxTaskLimit     = 500
)

// clusterClient is the part of *proxmox.Client this package uses.
//
// It is declared here, narrow, on the consumer side: the service depends on
// what it calls rather than on a concrete type, which is what lets the tests
// below run against a fake with no network, no TLS and no httptest server.
// *proxmox.Client satisfies it as it stands.
type clusterClient interface {
	ClusterResources(ctx context.Context) ([]proxmox.Resource, error)
	ClusterStatus(ctx context.Context) ([]proxmox.ClusterStatusEntry, error)
	HAManagerStatus(ctx context.Context) (*proxmox.HAManagerStatus, error)
	AptUpdates(ctx context.Context, node string) ([]proxmox.AptUpdate, error)
	NodeStatus(ctx context.Context, node string) (*proxmox.NodeStatus, error)
	GuestStatus(ctx context.Context, node, kind string, vmid int) (*proxmox.GuestStatus, error)
	NodeRRD(ctx context.Context, node, timeframe string) ([]proxmox.RRDPoint, error)
	GuestRRD(ctx context.Context, node, kind string, vmid int, timeframe string) ([]proxmox.RRDPoint, error)
	ClusterTasks(ctx context.Context) ([]proxmox.Task, error)
	NodeTasks(ctx context.Context, node string, vmid, limit int) ([]proxmox.Task, error)
	GuestIPv4(ctx context.Context, node string, vmid int) (string, error)
}

// clusterView is the cluster-wide material both detail views need: the
// resource listing that says which node hosts which guest, the status that
// says which nodes are up, and the HA manager that says which are draining.
//
// The three are cached together, under one key, because they are always
// wanted together and are collected by a single fan-out.
type clusterView struct {
	Resources []proxmox.Resource
	Status    []proxmox.ClusterStatusEntry
	// HA is nil when the cluster runs no HA manager or the token may not ask.
	HA *proxmox.HAManagerStatus
}

// Service answers the per-object views, on demand and through a short cache.
//
// One cache per kind of call rather than one for everything: the values have
// different types and different lifetimes, and a generic cache is cheap.
//
// One rule holds across every route: no PVE call carrying a name supplied by
// the client leaves before the cluster view has confirmed that name. The view
// is polled for the overview anyway and shared by all the routes, so the proof
// is nearly free; without it a made-up node name reaches the hypervisor, where
// a 5xx makes the client try every configured URL in turn, and leaves a cache
// entry per name asked for. What reaches PVE is provably known, the same way
// parseTimeframe only lets a known window through.
type Service struct {
	clients map[string]clusterClient
	budget  time.Duration
	now     func() time.Time

	views   *cache[string, stamped[clusterView]]
	nodes   *cache[string, stamped[*proxmox.NodeStatus]]
	guests  *cache[string, stamped[*proxmox.GuestStatus]]
	updates *cache[string, stamped[[]proxmox.AptUpdate]]
	ipv4    *cache[string, stamped[string]]
	series  *cache[string, stamped[[]proxmox.RRDPoint]]
	tasks   *cache[string, stamped[[]proxmox.Task]]
}

// NewService builds the detail service over one client per cluster. ttl is the
// lifetime of a cache entry; a value of zero or less means DefaultTTL.
func NewService(clients map[string]*proxmox.Client, ttl time.Duration) *Service {
	narrowed := make(map[string]clusterClient, len(clients))
	for id, client := range clients {
		narrowed[id] = client
	}
	return newService(narrowed, ttl, nil)
}

// newService is the constructor the tests use: it takes the narrow interface
// and an injectable clock.
func newService(clients map[string]clusterClient, ttl time.Duration, now func() time.Time) *Service {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if now == nil {
		now = time.Now
	}
	updatesLifetime := updatesTTL
	if ttl > updatesLifetime {
		updatesLifetime = ttl
	}
	return &Service{
		clients: clients,
		budget:  requestBudget,
		now:     now,
		views:   newCache[string, stamped[clusterView]](ttl, fetchBudget, now),
		nodes:   newCache[string, stamped[*proxmox.NodeStatus]](ttl, fetchBudget, now),
		guests:  newCache[string, stamped[*proxmox.GuestStatus]](ttl, fetchBudget, now),
		updates: newCache[string, stamped[[]proxmox.AptUpdate]](updatesLifetime, fetchBudget, now),
		ipv4:    newCache[string, stamped[string]](ttl, fetchBudget, now),
		series:  newCache[string, stamped[[]proxmox.RRDPoint]](ttl, fetchBudget, now),
		tasks:   newCache[string, stamped[[]proxmox.Task]](ttl, fetchBudget, now),
	}
}

// Node serves GET /api/clusters/{cluster}/nodes/{node}.
//
// The cluster view is resolved first, and alone: it is what proves the node
// exists, and nothing naming the node may reach PVE before that proof. The
// two per-node calls are independent of each other, so they still go out at
// once. Only the node's own status is essential: the pending updates need a
// privilege the token may not have, and their absence costs one nil field,
// not the page.
func (s *Service) Node(ctx context.Context, cluster, node string) (*Node, error) {
	client, err := s.client(cluster)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()

	view, err := s.clusterView(ctx, cluster, client)
	if err != nil {
		return nil, err
	}
	if !hasNode(view.Value, node) {
		return nil, notFoundf("cluster %s: node %s", cluster, node)
	}

	var (
		wg           sync.WaitGroup
		status       stamped[*proxmox.NodeStatus]
		statusErr    error
		updates      []proxmox.AptUpdate
		updatesKnown bool
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		status, statusErr = s.nodeStatus(ctx, cluster, node, client)
	}()
	go func() {
		defer wg.Done()
		// OPTIONAL. /nodes/{node}/apt/update needs Sys.Modify, which a
		// read-only audit token does not have: a failure here means the
		// count is unknown, never that nothing is pending.
		if pending, err := s.aptUpdates(ctx, cluster, node, client); err == nil {
			updates, updatesKnown = pending.Value, true
		}
	}()
	wg.Wait()

	if statusErr != nil {
		return nil, statusErr
	}

	out := deriveNode(nodeInput{
		Cluster:       cluster,
		Node:          node,
		FetchedAt:     status.At,
		Status:        status.Value,
		Resources:     view.Value.Resources,
		ClusterStatus: view.Value.Status,
		HA:            view.Value.HA,
		Updates:       updates,
		UpdatesKnown:  updatesKnown,
	})
	return &out, nil
}

// Guest serves GET /api/clusters/{cluster}/guests/{vmid}.
//
// The caller names a guest, not a node: the hosting node is resolved through
// the cluster listing, which is cached like everything else. That lookup is
// NOT remembered any longer than the TTL — a migrated guest changes node, and
// a stale mapping would ask the wrong node about it.
func (s *Service) Guest(ctx context.Context, cluster string, vmid int) (*Guest, error) {
	client, err := s.client(cluster)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()

	view, err := s.clusterView(ctx, cluster, client)
	if err != nil {
		return nil, err
	}
	resource, ok := findGuest(view.Value, vmid)
	if !ok {
		return nil, notFoundf("cluster %s: guest %d", cluster, vmid)
	}

	var (
		wg        sync.WaitGroup
		status    stamped[*proxmox.GuestStatus]
		statusErr error
		address   *string
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		status, statusErr = s.guestStatus(ctx, cluster, resource.Node, resource.Type, vmid, client)
	}()

	if wantsIPv4(resource) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// OPTIONAL, AND EXPECTED TO FAIL. The address comes from the
			// QEMU guest agent, which most VMs do not run; PVE then answers
			// 500 or 501. That is the normal state of a VM without an agent,
			// so the field stays nil and the rest of the page is served.
			if got, err := s.guestIPv4(ctx, cluster, resource.Node, vmid, client); err == nil && got.Value != "" {
				value := got.Value
				address = &value
			}
		}()
	}
	wg.Wait()

	if statusErr != nil {
		return nil, statusErr
	}

	out := deriveGuest(guestInput{
		Cluster:   cluster,
		Resource:  resource,
		Status:    status.Value,
		IPv4:      address,
		FetchedAt: status.At,
	})
	return &out, nil
}

// NodeSeries serves the RRD history of one node.
func (s *Service) NodeSeries(ctx context.Context, cluster, node, timeframe string) (*Series, error) {
	client, err := s.client(cluster)
	if err != nil {
		return nil, err
	}
	timeframe, err = checkTimeframe(timeframe)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()

	// The listing is what turns an unknown node into a 404 rather than into
	// whatever PVE answers for a path that names nobody, so it is resolved
	// before the RRD call that would carry that name upstream.
	view, err := s.clusterView(ctx, cluster, client)
	if err != nil {
		return nil, err
	}
	if !hasNode(view.Value, node) {
		return nil, notFoundf("cluster %s: node %s", cluster, node)
	}

	points, err := s.nodeRRD(ctx, cluster, node, timeframe, client)
	if err != nil {
		return nil, err
	}

	out := deriveSeries(cluster, timeframe, points.Value, points.At)
	return &out, nil
}

// GuestSeries serves the RRD history of one guest, whose node is resolved the
// same way Guest resolves it.
func (s *Service) GuestSeries(ctx context.Context, cluster string, vmid int, timeframe string) (*Series, error) {
	client, err := s.client(cluster)
	if err != nil {
		return nil, err
	}
	timeframe, err = checkTimeframe(timeframe)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()

	view, err := s.clusterView(ctx, cluster, client)
	if err != nil {
		return nil, err
	}
	resource, ok := findGuest(view.Value, vmid)
	if !ok {
		return nil, notFoundf("cluster %s: guest %d", cluster, vmid)
	}

	cacheKey := key(cluster, resource.Type, strconv.Itoa(vmid), timeframe)
	points, err := s.series.get(ctx, cacheKey, func(ctx context.Context) (stamped[[]proxmox.RRDPoint], error) {
		raw, err := client.GuestRRD(ctx, resource.Node, resource.Type, vmid, timeframe)
		return stamped[[]proxmox.RRDPoint]{Value: raw, At: s.now()}, s.wrap(err, "cluster %s: guest %d rrd", cluster, vmid)
	})
	if err != nil {
		return nil, err
	}

	out := deriveSeries(cluster, timeframe, points.Value, points.At)
	return &out, nil
}

// ClusterSeries serves the history of a whole cluster, aggregated over its
// nodes: it is what the overview card draws in place of a pair of gauges.
//
// PVE has no cluster-wide RRD — the figure does not exist upstream — so this
// reads every online node and folds the answers together. Two properties make
// that affordable. The per-node reads go through the SAME cache entries as
// NodeSeries, so a card and an open node view share one upstream call; and RRD
// only moves once a minute, which is the cadence the frontend polls at.
//
// A node that cannot be read is dropped rather than fatal: 403 on one node
// costs its share of the curve, not the chart. The error is only propagated
// when not one node answered, which is an outage or a token without Sys.Audit
// anywhere — a case where serving an empty hour would claim the cluster was
// idle.
func (s *Service) ClusterSeries(ctx context.Context, cluster, timeframe string) (*Series, error) {
	client, err := s.client(cluster)
	if err != nil {
		return nil, err
	}
	timeframe, err = checkTimeframe(timeframe)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()

	view, err := s.clusterView(ctx, cluster, client)
	if err != nil {
		return nil, err
	}

	nodes := seriesNodes(view.Value)
	if len(nodes) == 0 {
		// Every node is down, or the listing names none. An empty series says
		// "nothing to draw" without a failed request to explain.
		out := deriveClusterSeries(cluster, timeframe, nil, s.now())
		return &out, nil
	}

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		collected [][]proxmox.RRDPoint
		fetchedAt time.Time
		lastErr   error
	)
	for _, node := range nodes {
		node := node
		wg.Add(1)
		go func() {
			defer wg.Done()
			points, err := s.nodeRRD(ctx, cluster, node, timeframe, client)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				lastErr = err
				return
			}
			collected = append(collected, points.Value)
			if points.At.After(fetchedAt) {
				fetchedAt = points.At
			}
		}()
	}
	wg.Wait()

	if len(collected) == 0 {
		return nil, lastErr
	}

	out := deriveClusterSeries(cluster, timeframe, collected, fetchedAt)
	return &out, nil
}

// Tasks serves the recent job log of a cluster. A limit of zero or less means
// the default; anything beyond maxTaskLimit is clamped, so a hand-written
// query parameter cannot ask a cluster for its entire history.
func (s *Service) Tasks(ctx context.Context, cluster string, limit int) (*Tasks, error) {
	client, err := s.client(cluster)
	if err != nil {
		return nil, err
	}
	limit = clampLimit(limit)

	ctx, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()

	// The limit is not part of the cache key: /cluster/tasks takes no
	// parameter, so the upstream call is the same whatever the caller asked
	// for, and one entry per cluster serves every limit. The cut is made
	// after deriveTasks has ordered the log, so that it keeps the newest
	// entries rather than the first ones PVE happened to list.
	entries, err := s.tasks.get(ctx, cluster, func(ctx context.Context) (stamped[[]proxmox.Task], error) {
		raw, err := client.ClusterTasks(ctx)
		return stamped[[]proxmox.Task]{Value: raw, At: s.now()}, s.wrap(err, "cluster %s: tasks", cluster)
	})
	if err != nil {
		return nil, err
	}

	out := deriveTasks(cluster, entries.Value, entries.At)
	if len(out.Entries) > limit {
		out.Entries = out.Entries[:limit]
	}
	return &out, nil
}

// GuestTasks serves the recent jobs of ONE guest, read from the node hosting
// it. The hosting node is resolved through the cluster listing, exactly as
// Guest resolves it, and nothing naming the guest reaches PVE before that.
//
// Sieving the cluster log would not do. /cluster/tasks takes no parameter, so
// narrowing it means asking for its tail and dropping what does not match: on
// a cluster backing up ninety guests a night, the lines of any one of them are
// long gone from that tail, and the view that should say "backed up two hours
// ago" says "no recent task" instead. The per-node route takes a vmid and is
// therefore asked for exactly what is displayed.
//
// TRADE-OFF, worth knowing. A guest that has migrated leaves its older tasks
// on the node it came from, which this route does not read: its history starts
// where it arrived. An incomplete history beats the empty list.
func (s *Service) GuestTasks(ctx context.Context, cluster string, vmid, limit int) (*Tasks, error) {
	client, err := s.client(cluster)
	if err != nil {
		return nil, err
	}
	limit = clampLimit(limit)

	ctx, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()

	view, err := s.clusterView(ctx, cluster, client)
	if err != nil {
		return nil, err
	}
	resource, ok := findGuest(view.Value, vmid)
	if !ok {
		return nil, notFoundf("cluster %s: guest %d", cluster, vmid)
	}

	// The limit IS part of the key here, unlike Tasks: upstream is asked for
	// it, so two limits are two different answers rather than two cuts of one.
	// The node is in the key too, so a guest that migrates is read from where
	// it now runs instead of from a neighbour's entry.
	cacheKey := key(cluster, "guest", resource.Node, strconv.Itoa(vmid), strconv.Itoa(limit))
	entries, err := s.tasks.get(ctx, cacheKey, func(ctx context.Context) (stamped[[]proxmox.Task], error) {
		raw, err := client.NodeTasks(ctx, resource.Node, vmid, limit)
		return stamped[[]proxmox.Task]{Value: raw, At: s.now()}, s.wrap(err, "cluster %s: guest %d tasks", cluster, vmid)
	})
	if err != nil {
		return nil, err
	}

	out := deriveTasks(cluster, entries.Value, entries.At)
	if len(out.Entries) > limit {
		// PVE honours the limit, but the cut is cheap and the promise is ours.
		out.Entries = out.Entries[:limit]
	}
	return &out, nil
}

// clusterView fetches the three cluster-wide calls, through one cache entry
// and one fan-out. HA is optional: a standalone node has no HA manager, and a
// token may lack the privilege — its absence only costs the maintenance state.
func (s *Service) clusterView(ctx context.Context, cluster string, client clusterClient) (stamped[clusterView], error) {
	return s.views.get(ctx, cluster, func(ctx context.Context) (stamped[clusterView], error) {
		var (
			wg        sync.WaitGroup
			view      clusterView
			resErr    error
			statusErr error
		)
		wg.Add(3)
		go func() {
			defer wg.Done()
			view.Resources, resErr = client.ClusterResources(ctx)
		}()
		go func() {
			defer wg.Done()
			view.Status, statusErr = client.ClusterStatus(ctx)
		}()
		go func() {
			defer wg.Done()
			if ha, err := client.HAManagerStatus(ctx); err == nil {
				view.HA = ha
			}
		}()
		wg.Wait()

		out := stamped[clusterView]{Value: view, At: s.now()}
		if resErr != nil {
			return out, s.wrap(resErr, "cluster %s: resources", cluster)
		}
		return out, s.wrap(statusErr, "cluster %s: status", cluster)
	})
}

// nodeStatus fetches /nodes/{node}/status through the cache.
func (s *Service) nodeStatus(ctx context.Context, cluster, node string, client clusterClient) (stamped[*proxmox.NodeStatus], error) {
	return s.nodes.get(ctx, key(cluster, node), func(ctx context.Context) (stamped[*proxmox.NodeStatus], error) {
		status, err := client.NodeStatus(ctx, node)
		return stamped[*proxmox.NodeStatus]{Value: status, At: s.now()}, s.wrap(err, "cluster %s: node %s status", cluster, node)
	})
}

// guestStatus fetches the current status of one guest through the cache.
func (s *Service) guestStatus(ctx context.Context, cluster, node, kind string, vmid int, client clusterClient) (stamped[*proxmox.GuestStatus], error) {
	cacheKey := key(cluster, node, kind, strconv.Itoa(vmid))
	return s.guests.get(ctx, cacheKey, func(ctx context.Context) (stamped[*proxmox.GuestStatus], error) {
		status, err := client.GuestStatus(ctx, node, kind, vmid)
		return stamped[*proxmox.GuestStatus]{Value: status, At: s.now()}, s.wrap(err, "cluster %s: guest %d status", cluster, vmid)
	})
}

// aptUpdates fetches the pending packages of one node through the cache.
// nodeRRD reads the history of one node through the shared cache entry. The
// node view and the cluster card ask for the very same key, so ten tabs on the
// same cluster still cost one upstream read per node.
func (s *Service) nodeRRD(ctx context.Context, cluster, node, timeframe string, client clusterClient) (stamped[[]proxmox.RRDPoint], error) {
	return s.series.get(ctx, key(cluster, "node", node, timeframe), func(ctx context.Context) (stamped[[]proxmox.RRDPoint], error) {
		raw, err := client.NodeRRD(ctx, node, timeframe)
		return stamped[[]proxmox.RRDPoint]{Value: raw, At: s.now()}, s.wrap(err, "cluster %s: node %s rrd", cluster, node)
	})
}

func (s *Service) aptUpdates(ctx context.Context, cluster, node string, client clusterClient) (stamped[[]proxmox.AptUpdate], error) {
	return s.updates.get(ctx, key(cluster, node), func(ctx context.Context) (stamped[[]proxmox.AptUpdate], error) {
		pending, err := client.AptUpdates(ctx, node)
		return stamped[[]proxmox.AptUpdate]{Value: pending, At: s.now()}, s.wrap(err, "cluster %s: node %s updates", cluster, node)
	})
}

// guestIPv4 fetches the address the guest agent reports, through the cache.
func (s *Service) guestIPv4(ctx context.Context, cluster, node string, vmid int, client clusterClient) (stamped[string], error) {
	cacheKey := key(cluster, node, strconv.Itoa(vmid))
	return s.ipv4.get(ctx, cacheKey, func(ctx context.Context) (stamped[string], error) {
		address, err := client.GuestIPv4(ctx, node, vmid)
		return stamped[string]{Value: address, At: s.now()}, s.wrap(err, "cluster %s: guest %d ipv4", cluster, vmid)
	})
}

// client returns the client of a cluster, or ErrNotFound. An unconfigured
// cluster is not an internal failure: the caller named something that does not
// exist.
func (s *Service) client(cluster string) (clusterClient, error) {
	client, ok := s.clients[cluster]
	if !ok || client == nil {
		return nil, notFoundf("cluster %s", cluster)
	}
	return client, nil
}

// wrap annotates an error of the proxmox package with what was being fetched,
// and returns nil for a nil error so callers can pass one through.
//
// SECURITY. The annotation is built from the cluster id, the node name and the
// vmid — never from a URL, a header or a response body. The errors of the
// proxmox package are already safe by construction; nothing here may make them
// less so.
func (s *Service) wrap(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("detail: "+format+": %w", append(args, err)...)
}

// notFoundf builds an ErrNotFound naming what was looked for.
func notFoundf(format string, args ...any) error {
	return fmt.Errorf("detail: "+format+": %w", append(args, ErrNotFound)...)
}

// seriesNodes names the nodes worth asking for a history, sorted so the
// fan-out is deterministic.
//
// Membership is the one onlineNodes already defines for the maintenance plan,
// so the two features cannot disagree about which nodes are up. An offline
// node is left out: its RRD holds nothing but holes for the window being
// drawn, and asking costs a request against a host that is down.
func seriesNodes(view clusterView) []string {
	online := onlineNodes(view)
	names := make([]string, 0, len(online))
	for name := range online {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// hasNode reports whether a node exists in the cluster.
//
// Both sources are consulted: /cluster/status knows the members, and
// /cluster/resources knows a node that has just joined and not yet been seen
// by corosync. Either one naming it is enough for the page to exist; deciding
// what state it is in is a separate question, answered by deriveNodeStatus.
func hasNode(view clusterView, node string) bool {
	if node == "" {
		return false
	}
	for _, e := range view.Status {
		if e.Type == proxmox.ClusterStatusTypeNode && e.Name == node {
			return true
		}
	}
	for _, r := range view.Resources {
		if r.Type == proxmox.ResourceTypeNode && r.Node == node {
			return true
		}
	}
	return false
}

// findGuest resolves a vmid to its resource entry, which names the node
// hosting it now and the kind of guest it is.
func findGuest(view clusterView, vmid int) (proxmox.Resource, bool) {
	for _, r := range view.Resources {
		if r.IsGuest() && int(r.VMID.Int()) == vmid {
			return r, true
		}
	}
	return proxmox.Resource{}, false
}

// wantsIPv4 reports whether asking the guest agent is worth a request. Only a
// running QEMU VM has an agent tree at all: an LXC container is rejected by
// the client, and a stopped guest answers nothing.
func wantsIPv4(r proxmox.Resource) bool {
	return r.Type == proxmox.ResourceTypeQemu && r.Status == proxmox.StatusRunning && !r.Template.Bool()
}

// checkTimeframe defaults an empty window to the hour and rejects anything the
// RRD endpoints do not define.
//
// An unknown window is reported as ErrNotFound: it comes from a path or query
// parameter naming a series that does not exist, and the alternative would be
// to spend a request learning the same thing from PVE.
func checkTimeframe(timeframe string) (string, error) {
	if timeframe == "" {
		return proxmox.TimeframeHour, nil
	}
	if !proxmox.ValidTimeframe(timeframe) {
		return "", notFoundf("timeframe %s", timeframe)
	}
	return timeframe, nil
}

// clampLimit bounds the number of task entries asked for.
func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultTaskLimit
	}
	if limit > maxTaskLimit {
		return maxTaskLimit
	}
	return limit
}

// key joins the parts of a cache key. The separator is a NUL, which cannot
// appear in a cluster id, a node name or a timeframe: a node named "a" on a
// cluster named "b-c" must not collide with a node named "c" on a cluster
// named "b".
func key(parts ...string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\x00"
		}
		out += p
	}
	return out
}
