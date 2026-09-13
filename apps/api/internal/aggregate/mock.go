package aggregate

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Byte units of the mock data set. Proxmox reports raw byte counts, so does the
// overview payload: GiB is 2^30 bytes and TiB is 2^40 bytes.
const (
	mockGiB uint64 = 1 << 30
	mockTiB uint64 = 1 << 40
)

// mockMemoryThreshold mirrors the default configuration threshold, so the
// frontend sees the same limit the mock alerts were computed against.
const mockMemoryThreshold = 0.80

// Mock is an OverviewSource serving a frozen data set that reproduces the
// cluster overview screen of the handoff document, so the frontend can be built
// without a single reachable Proxmox cluster. It opens no connection and reads
// no configuration.
//
// Every call rebuilds the payload from scratch: a caller may freely mutate the
// slices it gets back without affecting the next call. Only GeneratedAt moves.
// The timestamps standing for collection times are frozen at construction, so
// two successive calls differ by GeneratedAt alone while still looking recent
// when the daemon has just started.
type Mock struct {
	// base is the instant the mock was created. Every frozen timestamp is
	// expressed relative to it.
	base time.Time
	// now is the clock GeneratedAt is read from, time.Now unless a caller
	// pinned it. It exists so that the payload can be made byte-for-byte
	// reproducible, which is what lets a test generate the frontend fixtures
	// instead of someone capturing them by hand.
	now func() time.Time
}

// NewMock returns a Mock ready to serve.
func NewMock() *Mock {
	return &Mock{base: time.Now().UTC(), now: func() time.Time { return time.Now().UTC() }}
}

// NewMockAt returns a Mock whose every timestamp is derived from base, and
// whose GeneratedAt is base itself. Two calls then return identical bytes.
//
// It is what makes the sample payload a golden file: the frontend fixtures are
// generated from it, so a field added to the model and forgotten in
// apps/web/src/api/types.ts fails a test instead of drifting quietly.
func NewMockAt(base time.Time) *Mock {
	base = base.UTC()
	return &Mock{base: base, now: func() time.Time { return base }}
}

// Overview implements the source contract consumed by the HTTP layer. It never
// fails, unless the caller's context is already done.
func (m *Mock) Overview(ctx context.Context) (*Overview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Overview{
		GeneratedAt: m.generatedAt(),
		Thresholds:  Thresholds{Memory: mockMemoryThreshold},
		// Totals are stated rather than derived: they are part of the frozen
		// data set, and the tests check the clusters below add up to them.
		// 148 running guests is 13 + 46 + 89, templates excluded.
		Totals: Totals{
			Clusters: 4,
			// 11 + the lab's 3.
			Nodes: 14,
			// The lab has one node offline and one unmeasured; the unmeasured
			// one is still up, so 11 + 2.
			NodesOnline: 13,
			// 148 + the lab's 6.
			VMs: 154,
			// 0 + 3 + 1: preproduction carries memory_high, updates_uneven and
			// updates_available; production the update banner alone. The lab
			// adds node_offline, node_stats_unavailable and unreachable.
			Alerts: 7,
		},
		Clusters: []ClusterOverview{
			m.qualification(),
			m.preproduction(),
			m.production(),
			m.lab(),
		},
	}, nil
}

// qualification is the healthy, mostly idle cluster: three identical nodes, no
// alert, quorum 3/3.
func (m *Mock) qualification() ClusterOverview {
	nodes := []Node{
		mockNode("prox-qual-2201-cit", NodeOnline, 4723200, 0.05, 22*mockGiB, 128*mockGiB, nil),
		mockNode("prox-qual-2202-cit", NodeOnline, 4720800, 0.03, 19*mockGiB, 128*mockGiB, nil),
		mockNode("prox-qual-2203-cit", NodeOnline, 4719600, 0.04, 20*mockGiB, 128*mockGiB, nil),
	}
	nodes = withGuests(nodes, mockGuestPlan{
		env:       "qualification",
		suffix:    "qul",
		baseVMID:  100,
		running:   13,
		templates: 1,
		hosts:     mockOnlineHosts(nodes),
	})
	return ClusterOverview{
		ID:        "qualification",
		Name:      "Qualification",
		Color:     nil,
		Status:    StatusHealthy,
		FetchedAt: m.fetchedAt(3 * time.Second),
		Error:     nil,
		Quorum:    &Quorum{Quorate: true, Nodes: 3, Online: 3},
		// Weighted mean of the node ratios over 3 x 32 cores.
		CPU: &CPU{Ratio: 0.04, Cores: 96},
		// 61 / 384 GiB, the sum of the three nodes above.
		Memory:  mockPtr(mockUsage(61*mockGiB, 384*mockGiB)),
		Storage: mockUsage(12*mockTiB/10, 54*mockTiB/10),
		VMs:     VMCounts{Running: 13, Stopped: 0, Templates: 1, Total: 13},
		Nodes:   nodes,
		Updates: nil,
		// Empty rather than nil: the payload always carries an array, so the
		// frontend never has to tell "no alert" from a missing field.
		Alerts: []Alert{},
	}
}

// lab is the cluster nothing goes right on, and it exists for the states the
// other three never reach.
//
// Every screen has a path for a cluster that cannot be read, a node PVE lists
// without its figures, a node that is down and a quorum that is lost. None of
// those paths were rendered in development: they waited for a real cluster with
// a token too narrow, which is exactly how the first one was found. A sample
// where everything works lets through an interface that cannot show anything
// else.
//
// It is deliberately NOT one of the three screens of the handoff: those keep
// their labels — Qualification sain, Préproduction dégradé, Production sain
// sous son bandeau de mise à jour — and this one is the fourth card.
func (m *Mock) lab() ClusterOverview {
	nodes := []Node{
		mockNode("prox-lab-2501-cit", NodeOnline, 864000, 0.12, 30*mockGiB, 64*mockGiB, mockPtr(0)),
		// Up, and unreadable: PVE lists the row without cpu/maxcpu/mem/maxmem
		// when the token has no Sys.Audit on the node. Unknown is not zero, so
		// the card must render an em dash rather than a node at rest.
		mockBlindNode("prox-lab-2502-cit"),
		// Down: no uptime, no figures, and its guests are not running anywhere.
		mockOfflineNode("prox-lab-2503-cit"),
	}
	nodes = withGuests(nodes, mockGuestPlan{
		env:      "lab",
		suffix:   "lab",
		baseVMID: 3000,
		running:  5,
		stopped:  1,
		hosts:    mockOnlineHosts(nodes),
	})
	return ClusterOverview{
		ID:   "lab",
		Name: "Laboratoire",
		// The last known snapshot, three minutes old, served under an
		// unreachable status: the figures below are stale, not absent, which
		// is the whole reason the poller keeps them.
		Status:    StatusUnreachable,
		FetchedAt: m.fetchedAt(3 * time.Minute),
		Error: &Error{
			Kind:    "tls",
			Message: "cluster lab: GET /cluster/status: x509: certificate signed by unknown authority",
		},
		// One of three nodes voting: the cluster has lost quorum.
		Quorum: &Quorum{Quorate: false, Nodes: 3, Online: 1},
		// One node measured out of three, so the cluster figures are that
		// node's alone.
		CPU:     &CPU{Ratio: 0.12, Cores: 32},
		Memory:  mockPtr(mockUsage(30*mockGiB, 64*mockGiB)),
		Storage: mockUsage(8*mockTiB/10, 2*mockTiB),
		VMs:     VMCounts{Running: 5, Stopped: 1, Templates: 0, Total: 6},
		Nodes:   nodes,
		// A node that is up and up to date: zero pending, which must read as
		// "à jour" and never as the em dash of an unknown count.
		Updates: &Updates{
			Nodes:             []string{},
			PVEManagerVersion: nil,
			CheckedAt:         m.base.Add(-7 * time.Minute),
		},
		Alerts: []Alert{
			{Kind: AlertQuorumLost},
			{Kind: AlertNodeOffline, Nodes: []string{"prox-lab-2503-cit"}},
			{Kind: AlertNodeStatsUnavailable, Nodes: []string{"prox-lab-2502-cit"}},
		},
	}
}

// preproduction is the degraded cluster: one node drained for maintenance, the
// two remaining ones carrying its guests and crossing the memory threshold.
func (m *Mock) preproduction() ClusterOverview {
	// The counts diverge on purpose: the drained node was left behind while the
	// two others were updated, which is exactly what updates_uneven surfaces. A
	// node in maintenance is up and its packages are real, so it is compared
	// like any other.
	nodes := []Node{
		mockNode("prox-pprd-2301-cit", NodeOnline, 2419200, 0.44, 100*mockGiB, 112*mockGiB, mockPtr(8)),
		// Emptied by the maintenance drain, and rebooted two hours ago.
		mockNode("prox-pprd-2302-cit", NodeMaintenance, 7200, 0.05, 12*mockGiB, 32*mockGiB, mockPtr(14)),
		mockNode("prox-pprd-2303-cit", NodeOnline, 2415600, 0.44, 100*mockGiB, 112*mockGiB, mockPtr(8)),
	}
	// The drained node is left out of the hosts: its guests were migrated away
	// to the two others, which is why they are the ones running out of memory.
	nodes = withGuests(nodes, mockGuestPlan{
		env:      "preproduction",
		suffix:   "ppr",
		baseVMID: 1000,
		running:  44,
		stopped:  2,
		hosts:    mockOnlineHosts(nodes),
	})
	// 212 / 256 GiB, the sum of the three nodes above: ratio 0.828125.
	memory := mockUsage(212*mockGiB, 256*mockGiB)
	return ClusterOverview{
		ID:        "preproduction",
		Name:      "Préproduction",
		Color:     nil,
		Status:    StatusDegraded,
		FetchedAt: m.fetchedAt(2 * time.Second),
		Error:     nil,
		// A node in maintenance still votes: quorum is unaffected.
		Quorum:  &Quorum{Quorate: true, Nodes: 3, Online: 3},
		CPU:     &CPU{Ratio: 0.31, Cores: 96},
		Memory:  &memory,
		Storage: mockUsage(39*mockTiB/10, 8*mockTiB),
		VMs:     VMCounts{Running: 44, Stopped: 2, Templates: 0, Total: 46},
		Nodes:   nodes,
		Updates: &Updates{
			Nodes: []string{
				"prox-pprd-2301-cit",
				"prox-pprd-2302-cit",
				"prox-pprd-2303-cit",
			},
			PVEManagerVersion: mockPtr("9.2.12"),
			CheckedAt:         m.base.Add(-6 * time.Minute),
		},
		// Three banners, in the order deriveAlerts produces them. A card shows
		// alerts[0] only, so the last two also demonstrate the rule that the
		// fault comes before the news.
		Alerts: []Alert{
			{
				Kind:  AlertMemoryHigh,
				Nodes: []string{"prox-pprd-2301-cit", "prox-pprd-2303-cit"},
				// The ratio of the nodes named, not of the cluster: both sit
				// at 100 of 112 GiB, well above the 0.828 of the cluster that
				// the drained node pulls down. A banner reading "sur 2 nœuds"
				// must quote a figure true of those two.
				Ratio: mockPtr(mockUsage(100*mockGiB, 112*mockGiB).Ratio),
			},
			{
				Kind:       AlertUpdatesUneven,
				PendingMin: mockPtr(8),
				PendingMax: mockPtr(14),
			},
			{
				Kind: AlertUpdatesAvailable,
				Nodes: []string{
					"prox-pprd-2301-cit",
					"prox-pprd-2302-cit",
					"prox-pprd-2303-cit",
				},
				Version: mockPtr("9.2.12"),
			},
		},
	}
}

// production is healthy but has a pending release: updates_available alone is
// never a degradation.
func (m *Mock) production() ClusterOverview {
	// Every node sits at the same package level, so production keeps the verdict
	// the mockups show: healthy under an update banner. Uneven counts here would
	// degrade it and take away the very case the handoff illustrates; the
	// divergence is demonstrated by preproduction instead. Qualification keeps
	// demonstrating the unknown count.
	nodes := []Node{
		mockNode("prox-prod-2401-cit", NodeOnline, 6048000, 0.18, 98*mockGiB, 256*mockGiB, mockPtr(12)),
		mockNode("prox-prod-2402-cit", NodeOnline, 6044400, 0.25, 102*mockGiB, 256*mockGiB, mockPtr(12)),
		mockNode("prox-prod-2403-cit", NodeOnline, 6040800, 0.21, 104*mockGiB, 256*mockGiB, mockPtr(12)),
		mockNode("prox-prod-2404-cit", NodeOnline, 3628800, 0.24, 58*mockGiB, 128*mockGiB, mockPtr(12)),
		mockNode("prox-prod-2405-cit", NodeOnline, 3625200, 0.22, 56*mockGiB, 128*mockGiB, mockPtr(12)),
	}
	nodes = withGuests(nodes, mockGuestPlan{
		env:       "production",
		suffix:    "prd",
		baseVMID:  2000,
		running:   89,
		templates: 3,
		hosts:     mockOnlineHosts(nodes),
	})
	updated := []string{
		"prox-prod-2401-cit",
		"prox-prod-2402-cit",
		"prox-prod-2403-cit",
		"prox-prod-2404-cit",
		"prox-prod-2405-cit",
	}
	version := "9.2.12"
	return ClusterOverview{
		ID:        "production",
		Name:      "Production",
		Color:     nil,
		Status:    StatusHealthy,
		FetchedAt: m.fetchedAt(4 * time.Second),
		Error:     nil,
		Quorum:    &Quorum{Quorate: true, Nodes: 5, Online: 5},
		CPU:       &CPU{Ratio: 0.22, Cores: 160},
		// 418 / 1024 GiB, the sum of the five nodes above.
		Memory:  mockPtr(mockUsage(418*mockGiB, 1024*mockGiB)),
		Storage: mockUsage(14*mockTiB, 32*mockTiB),
		VMs:     VMCounts{Running: 89, Stopped: 0, Templates: 3, Total: 89},
		Nodes:   nodes,
		Updates: &Updates{
			Nodes:             append([]string(nil), updated...),
			PVEManagerVersion: mockPtr(version),
			// The update check runs on its own slower schedule.
			CheckedAt: m.base.Add(-4 * time.Minute),
		},
		Alerts: []Alert{
			{
				Kind:    AlertUpdatesAvailable,
				Nodes:   append([]string(nil), updated...),
				Version: mockPtr(version),
			},
		},
	}
}

// generatedAt is the instant of THIS answer. It moves between two calls of a
// live mock -- that is what tells the frontend the poller is alive -- and
// stands still on one built by NewMockAt.
func (m *Mock) generatedAt() time.Time {
	if m.now == nil {
		return time.Now().UTC()
	}
	return m.now().UTC()
}

// fetchedAt returns a pointer to an instant ago seconds before the mock was
// created, fresh on every call so the caller cannot reach the mock's own state.
func (m *Mock) fetchedAt(ago time.Duration) *time.Time {
	return mockPtr(m.base.Add(-ago))
}

// mockNode builds one node, deriving its memory ratio from used and total so
// that the payload can never contradict itself.
func mockNode(name string, status NodeStatus, uptime int64, cpu float64, used, total uint64, pending *int) Node {
	return Node{
		Name:           name,
		Status:         status,
		Uptime:         mockPtr(uptime),
		CPU:            &CPU{Ratio: cpu, Cores: 32},
		Memory:         mockPtr(mockUsage(used, total)),
		PendingUpdates: pending,
	}
}

// mockUsage pairs a used/total byte count with the ratio between them.
// mockBlindNode is a node PVE lists without its measurements, which is what it
// does when the token has no Sys.Audit on /nodes/{node}. Nil, not zero --
// uptime included: it is stripped with the rest.
func mockBlindNode(name string) Node {
	return Node{Name: name, Status: NodeOnline, Uptime: nil, PendingUpdates: nil}
}

// mockOfflineNode is a node that is down: no uptime, no figures, no count.
func mockOfflineNode(name string) Node {
	return Node{Name: name, Status: NodeOffline, Uptime: nil, PendingUpdates: nil}
}

func mockUsage(used, total uint64) Usage {
	return Usage{Used: used, Total: total, Ratio: float64(used) / float64(total)}
}

// mockPtr returns a pointer to a copy of v, so that every call to Overview
// hands out its own values.
func mockPtr[T any](v T) *T {
	return &v
}

// mockTemplateVMID is where the generated templates are numbered from: a PVE
// installation conventionally reserves a high range for them, so they sort
// after the guests that actually run.
const mockTemplateVMID = 9000

// The vocabularies the generated guest names and shapes are drawn from.
//
// Their lengths are pairwise coprime, and coprime with the node counts of the
// three clusters (3, 2 and 5), so that rotating over them gives every guest of
// a cluster a distinct name and spreads the shapes and tags evenly over the
// nodes instead of handing one node all the big guests.
var (
	mockGuestServices = []string{
		"airflow", "testproxmox", "gitlab", "keycloak", "grafana",
		"nexus", "sonarqube", "redis", "postgres", "rabbitmq", "vault",
	}
	mockGuestRoles = []string{
		"sep-exp", "web", "api", "worker", "batch", "front", "back",
	}
	mockGuestTemplateNames = []string{
		"template-rocky10", "template-debian13", "template-ubuntu2404",
	}
	// Memory sizes in GiB, deliberately modest: the guests of a node must fit
	// in the memory that node reports as used.
	mockGuestMemGiB = []uint64{1, 2, 4, 8, 2, 4, 1}
	mockGuestCores  = []int{1, 2, 2, 4, 4, 8, 2}
)

// mockGuestPlan is the guest population of one cluster, stated in the same
// terms as its VMCounts so that the list and the counters cannot drift apart.
type mockGuestPlan struct {
	// env is the value of the env.* tag; suffix is the trailing segment of the
	// long naming convention, as in "sli-airflow-sep-exp-2601-qul".
	env    string
	suffix string
	// baseVMID is where the running and stopped guests are numbered from.
	baseVMID  int
	running   int
	stopped   int
	templates int
	// hosts are the nodes the guests are spread over, round-robin.
	hosts []string
}

// withGuests attaches the planned guests to the nodes that host them, sorted by
// VMID. A node the plan places nothing on keeps an empty list rather than a nil
// one, so the payload always carries an array.
func withGuests(nodes []Node, plan mockGuestPlan) []Node {
	byNode := mockGuests(plan)
	for i := range nodes {
		guests := byNode[nodes[i].Name]
		if guests == nil {
			guests = []Guest{}
		}
		sort.Slice(guests, func(a, b int) bool { return guests[a].VMID < guests[b].VMID })
		nodes[i].Guests = guests
	}
	return nodes
}

// mockGuests builds the whole population of a cluster, keyed by host. It is a
// pure function of the plan: the same plan always yields the same guests, down
// to their order.
func mockGuests(plan mockGuestPlan) map[string][]Guest {
	byNode := make(map[string][]Guest, len(plan.hosts))
	if len(plan.hosts) == 0 {
		return byNode
	}
	for i := 0; i < plan.running+plan.stopped+plan.templates; i++ {
		var g Guest
		switch {
		case i < plan.running:
			g = mockGuest(plan, i, plan.baseVMID+i, mockGuestName(plan.suffix, i), GuestRunning)
		case i < plan.running+plan.stopped:
			g = mockGuest(plan, i, plan.baseVMID+i, mockGuestName(plan.suffix, i), GuestStopped)
		default:
			n := i - plan.running - plan.stopped
			name := mockGuestTemplateNames[n%len(mockGuestTemplateNames)]
			g = mockGuest(plan, i, mockTemplateVMID+n, name, GuestTemplate)
		}
		host := plan.hosts[i%len(plan.hosts)]
		byNode[host] = append(byNode[host], g)
	}
	return byNode
}

// mockGuest builds one guest. Every varying field is a function of i, the index
// of the guest within its cluster, so the population is reproducible.
func mockGuest(plan mockGuestPlan, i, vmid int, name string, status GuestStatus) Guest {
	total := mockGuestMemGiB[i%len(mockGuestMemGiB)] * mockGiB
	g := Guest{
		VMID:   vmid,
		Name:   name,
		Kind:   GuestQemu,
		Status: status,
		CPU:    CPU{Cores: mockGuestCores[i%len(mockGuestCores)]},
		// A guest that does not run holds nothing and burns nothing; it keeps
		// the memory it was configured with as its total.
		Memory: Usage{Total: total},
		Tags:   mockGuestTags(plan.env, i),
	}
	// One guest in eleven is a container. A template is left a VM: the mockups
	// show QEMU templates, and a container template is a different object in
	// PVE anyway.
	if status != GuestTemplate && i%11 == 4 {
		g.Kind = GuestLXC
	}
	if status == GuestRunning {
		g.Memory = mockUsage(total/2+uint64(i%7)*total/16, total)
		g.CPU.Ratio = float64(1+i%17) / 200
	}
	return g
}

// mockGuestName builds a plausible name in the long convention of the handoff
// document, e.g. "sli-airflow-sep-exp-2601-qul".
func mockGuestName(suffix string, i int) string {
	return fmt.Sprintf("sli-%s-%s-26%02d-%s",
		mockGuestServices[i%len(mockGuestServices)],
		mockGuestRoles[i%len(mockGuestRoles)],
		1+i%12,
		suffix,
	)
}

// mockGuestTags gives every guest the env tag of its cluster, and some of them
// the backup and date tags of the handoff document. The result is never empty,
// and never nil.
//
// The set is deliberately UNEVEN, for the same reason the sample RRD series
// carry holes. A fleet writes its tags as "key.value", and the guest view cuts
// them at the LAST dot to line the keys up; a sample where every tag held
// exactly one dot would never exercise either end of that rule. So one guest
// in five carries a multi-level key — "ha.state.started", whose key is
// "ha.state" and not "ha" — and one in three a flag tag with no dot at all,
// which names without qualifying and renders an em dash for its value.
func mockGuestTags(env string, i int) []string {
	tags := []string{"env." + env}
	if i%5 == 0 {
		tags = append(tags, "ha.state.started")
	}
	if i%3 == 0 {
		tags = append(tags, "production")
	}
	if i%7 == 0 {
		tags = append(tags, "backup.none")
	}
	if i%13 == 0 {
		tags = append(tags, "date.20260907")
	}
	return tags
}

// mockOnlineHosts lists the nodes a guest can be placed on. A node drained for
// maintenance has had its guests migrated away and holds none, which is the
// whole point of draining it.
func mockOnlineHosts(nodes []Node) []string {
	hosts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.Status == NodeOnline {
			hosts = append(hosts, n.Name)
		}
	}
	return hosts
}
