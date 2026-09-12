package detail

import (
	"math"
	"sort"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

// This file is pure: no network, no clock, no globals. Everything it needs to
// know about WHEN the data was collected is handed to it as a parameter, which
// is what makes the derivation testable against a table of fixtures.
//
// It deliberately mirrors the conventions of internal/aggregate rather than
// inventing its own — the node status vocabulary, the quorum rule, the guest
// sort order. The two views show the same objects, and an operator who sees a
// node reported "en maintenance" on the overview and "hors ligne" on its own
// page has been told a lie by one of the two. The functions there are
// unexported, so the rules are restated here; they must not be changed on one
// side alone.

// haStateManaged is the only word the guest status offers about HA: the
// endpoint reports whether the stack manages the guest, not which state the
// CRM has it in. A node, whose state comes from the HA manager itself, gets a
// real state instead.
const haStateManaged = "managed"

// taskStatusRunning is what a task that has not finished yet reports, in place
// of the empty status PVE sends. See Task.Status.
const taskStatusRunning = "running"

// taskStatusUnknown is the status of a finished task PVE reported nothing for.
// A finished task with no status is not a success: saying so would turn an
// unknown outcome into a green line.
const taskStatusUnknown = "unknown"

// nodeInput is everything one node view is derived from. Only Status is
// essential; the rest degrades to a nil field.
type nodeInput struct {
	Cluster string
	Node    string
	// FetchedAt is when Status was collected, which a cache makes different
	// from "now".
	FetchedAt time.Time
	// Status is /nodes/{node}/status: what the node says about itself.
	Status *proxmox.NodeStatus
	// Resources and ClusterStatus come from the two cluster-wide calls, and
	// carry respectively the guest list and the node reachability.
	Resources     []proxmox.Resource
	ClusterStatus []proxmox.ClusterStatusEntry
	// HA is nil when the cluster runs no HA manager or the token may not ask.
	HA *proxmox.HAManagerStatus
	// Updates holds the pending packages, and UpdatesKnown says whether the
	// question could be asked at all: an empty list means "none pending", a
	// false UpdatesKnown means "nobody knows".
	Updates      []proxmox.AptUpdate
	UpdatesKnown bool
}

// deriveNode builds the payload of one node view.
func deriveNode(in nodeInput) Node {
	st := in.Status
	if st == nil {
		st = &proxmox.NodeStatus{}
	}

	n := Node{
		Cluster:     in.Cluster,
		Name:        in.Node,
		Status:      deriveNodeStatus(in.Node, in.ClusterStatus, in.HA),
		Uptime:      st.Uptime.Int(),
		FetchedAt:   in.FetchedAt,
		PVEVersion:  optionalString(st.PVEVersion),
		KernelVer:   optionalString(st.KVersion),
		CPU:         aggregate.CPU{Ratio: st.CPU.Float(), Cores: int(st.CPUInfo.CPUs.Int())},
		Memory:      usageOf(st.Memory),
		Swap:        usageOf(st.Swap),
		RootFS:      usageOf(st.RootFS),
		LoadAverage: deriveLoadAverage(st.LoadAvg),
		Quorum:      deriveQuorum(in.ClusterStatus),
		HAState:     deriveNodeHAState(in.Node, in.HA),
		Guests:      deriveGuests(in.Node, in.Resources),
	}
	if in.UpdatesKnown {
		pending := len(in.Updates)
		n.PendingUpdates = &pending
	}
	return n
}

// deriveNodeStatus decides the state of one node, with the rule of the
// overview: MAINTENANCE WINS. A node being drained still answers
// /cluster/status as online, and reporting it as online would hide the very
// state the operator opened the page to see. A node in maintenance is not
// offline — it runs, it holds its guests, it merely refuses new ones.
//
// Only the HA manager and /cluster/status are trusted. A node known from
// /cluster/resources alone is reported as unknown rather than guessed to be
// online: PVE keeps a stale entry there for a node that has just left.
func deriveNodeStatus(node string, entries []proxmox.ClusterStatusEntry, ha *proxmox.HAManagerStatus) aggregate.NodeStatus {
	if ha != nil && ha.NodeState(node) == proxmox.HANodeMaintenance {
		return aggregate.NodeMaintenance
	}
	for _, e := range entries {
		if e.Type != proxmox.ClusterStatusTypeNode || e.Name != node {
			continue
		}
		if e.Online.Bool() {
			return aggregate.NodeOnline
		}
		return aggregate.NodeOffline
	}
	return aggregate.NodeUnknown
}

// deriveNodeHAState returns the CRM's own word for this node, or nil when no
// HA manager runs — which is not the same as a node the manager does not know.
func deriveNodeHAState(node string, ha *proxmox.HAManagerStatus) *string {
	if ha == nil || len(ha.NodeStatus) == 0 {
		return nil
	}
	state, ok := ha.NodeStatus[node]
	if !ok || state == "" {
		return nil
	}
	return &state
}

// deriveQuorum reads corosync quorum from /cluster/status.
//
// A standalone node returns node entries only, with no "cluster" entry: its
// quorum is not lost, it simply does not exist, and nil is how the payload
// says so.
func deriveQuorum(entries []proxmox.ClusterStatusEntry) *aggregate.Quorum {
	var q *aggregate.Quorum
	online := 0
	for _, e := range entries {
		switch e.Type {
		case proxmox.ClusterStatusTypeCluster:
			if q == nil {
				q = &aggregate.Quorum{Quorate: e.Quorate.Bool(), Nodes: int(e.Nodes.Int())}
			}
		case proxmox.ClusterStatusTypeNode:
			if e.Online.Bool() {
				online++
			}
		}
	}
	if q == nil {
		return nil
	}
	q.Online = online
	return q
}

// deriveLoadAverage turns the node's load average into the payload's triple,
// or nil when the figures cannot be believed.
//
// The endpoint sends three STRINGS ("0.53"), which the proxmox package decodes
// through FlexFloat: a value that is not a number at all fails the decode one
// layer down and never reaches here. What is left to catch is the implausible
// reading — negative, NaN or infinite — which must become nil rather than a
// row of zeroes: "—" says nothing, "0,00" claims the node is idle.
//
// A genuine 0.00 0.00 0.00 IS kept: a quiet node really does report it, and
// turning that into "unknown" would be the symmetrical lie.
func deriveLoadAverage(l proxmox.LoadAvg) *[3]float64 {
	var out [3]float64
	for i, v := range l {
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
			return nil
		}
		out[i] = f
	}
	return &out
}

// deriveGuests lists the guests hosted by one node, sorted by VMID so the
// table does not shuffle between two identical refreshes. It is NEVER nil: the
// payload always carries an array, and the frontend should not have to tell
// "no guest" from "field missing".
func deriveGuests(node string, resources []proxmox.Resource) []aggregate.Guest {
	guests := make([]aggregate.Guest, 0, 8)
	for _, r := range resources {
		if !r.IsGuest() || r.Node != node {
			continue
		}
		guests = append(guests, guestOf(r))
	}
	sort.SliceStable(guests, func(a, b int) bool {
		if guests[a].VMID != guests[b].VMID {
			return guests[a].VMID < guests[b].VMID
		}
		return guests[a].Name < guests[b].Name
	})
	return guests
}

// guestOf turns one /cluster/resources entry into a guest of the node table.
func guestOf(r proxmox.Resource) aggregate.Guest {
	return aggregate.Guest{
		VMID:   int(r.VMID.Int()),
		Name:   r.Name,
		Kind:   guestKind(r.Type),
		Status: resourceGuestStatus(r),
		CPU:    aggregate.CPU{Ratio: r.CPU.Float(), Cores: int(r.MaxCPU.Int())},
		Memory: usage(asBytes(r.Mem.Int()), asBytes(r.MaxMem.Int())),
		Tags:   tagList(r.TagList()),
	}
}

// guestInput is everything one guest view is derived from. Only Status is
// essential: IPv4 comes from an agent that is usually not there.
type guestInput struct {
	Cluster string
	// Resource is the guest's entry in /cluster/resources, which names the
	// node hosting it right now and carries its tags.
	Resource proxmox.Resource
	// Status is /nodes/{node}/{kind}/{vmid}/status/current.
	Status *proxmox.GuestStatus
	// IPv4 is nil without a guest agent, which is the common case.
	IPv4 *string
	// FetchedAt is when Status was collected.
	FetchedAt time.Time
}

// deriveGuest builds the payload of one guest view.
//
// The runtime figures come from the per-guest status, which is fresher and
// richer than the cluster-wide listing; the identity — node, kind, tags —
// comes from the listing, which is the only source that says WHERE the guest
// runs, and that changes under a migration.
func deriveGuest(in guestInput) Guest {
	st := in.Status
	if st == nil {
		st = &proxmox.GuestStatus{}
	}

	name := st.Name
	if name == "" {
		name = in.Resource.Name
	}
	tags := st.TagList()
	if len(tags) == 0 {
		tags = in.Resource.TagList()
	}

	g := Guest{
		Cluster:   in.Cluster,
		Node:      in.Resource.Node,
		VMID:      int(in.Resource.VMID.Int()),
		Name:      name,
		Kind:      guestKind(in.Resource.Type),
		Status:    statusGuestStatus(st),
		Uptime:    st.Uptime.Int(),
		FetchedAt: in.FetchedAt,
		CPU:       aggregate.CPU{Ratio: st.CPU.Float(), Cores: int(st.CPUs.Int())},
		Memory:    usage(asBytes(st.Mem.Int()), asBytes(st.MaxMem.Int())),
		Disk:      usage(asBytes(st.Disk.Int()), asBytes(st.MaxDisk.Int())),
		Tags:      tagList(tags),
		IPv4:      in.IPv4,
	}
	if st.HA.Managed.Bool() {
		state := haStateManaged
		g.HAState = &state
	}
	// What the hypervisor actually spends on a VM is not a field of the
	// status endpoint before PVE 8.3; the balloon target is the closest
	// honest approximation — it is the memory the host has handed to the
	// guest, which is why it exceeds what the guest reports using. Zero means
	// ballooning is off or unsupported, which is "unknown", not "nothing".
	if balloon := asBytes(st.Balloon.Int()); balloon > 0 {
		g.HostMemory = &balloon
	}
	return g
}

// guestKind maps a resource type to the kind of guest it denotes.
func guestKind(resourceType string) aggregate.GuestKind {
	if resourceType == proxmox.ResourceTypeLXC {
		return aggregate.GuestLXC
	}
	return aggregate.GuestQemu
}

// resourceGuestStatus decides the state of a guest from the cluster listing.
// Being a template wins over the reported state: a template PVE happens to
// report as running is still a template.
func resourceGuestStatus(r proxmox.Resource) aggregate.GuestStatus {
	return guestStatusOf(r.Template.Bool(), r.Status)
}

// statusGuestStatus decides the state of a guest from its own status endpoint,
// by the same rule.
func statusGuestStatus(st *proxmox.GuestStatus) aggregate.GuestStatus {
	return guestStatusOf(st.Template.Bool(), st.Status)
}

// guestStatusOf is the shared rule of the two functions above.
func guestStatusOf(template bool, status string) aggregate.GuestStatus {
	if template {
		return aggregate.GuestTemplate
	}
	if status == proxmox.StatusRunning {
		return aggregate.GuestRunning
	}
	return aggregate.GuestStopped
}

// deriveSeries turns RRD samples into the payload of a sparkline.
//
// A HOLE STAYS A HOLE. RRD omits the field of a step it has no data for, the
// proxmox package decodes that absence as nil, and it travels all the way to
// the JSON as null: drawing it as zero would invent a drop that never
// happened. The CPU average is computed over the POINTS THAT HAVE A VALUE for
// the same reason — counting the holes as zero would drag the figure down
// towards an idleness nobody measured. With no value at all the average is 0,
// and the points are still served: the chart shows its gaps.
func deriveSeries(cluster, timeframe string, raw []proxmox.RRDPoint, fetchedAt time.Time) Series {
	points := make([]Point, 0, len(raw))
	for _, r := range raw {
		points = append(points, Point{
			Time:     time.Unix(r.Time, 0).UTC(),
			CPU:      copyFloat(r.CPU),
			MemUsed:  copyUint(firstUint(r.MemUsed, r.Mem)),
			MemTotal: copyUint(firstUint(r.MemTotal, r.MaxMem)),
			NetIn:    copyUint(r.NetIn),
			NetOut:   copyUint(r.NetOut),
		})
	}
	return seriesOf(cluster, timeframe, points, fetchedAt)
}

// deriveClusterSeries folds the histories of the nodes of a cluster into one
// series: the card of the overview asks about the cluster, not about a node.
//
// THE STEPS ARE MATCHED ON THEIR TIMESTAMP, never on their index. Every node
// records on the same 60 second grid, but a node that joined an hour ago, or
// that was down, simply has fewer samples: pairing the i-th sample of two
// nodes would then add readings taken minutes apart.
//
// The CPU is the WEIGHTED mean Σ(cpu×maxcpu)/Σ(maxcpu) of the nodes that have
// both figures at that step, which is the rule deriveCPUAndMemory applies to
// the instantaneous reading — averaging the per-node fractions is wrong as
// soon as the nodes differ in core count. The memory is the sum of the nodes
// that reported both used and total. A step no node could measure stays nil:
// unknown is not an idle cluster.
//
// The network columns are left nil on purpose. The card draws no network line,
// and a sum over whichever nodes happened to answer would be a figure nobody
// asked for.
func deriveClusterSeries(cluster, timeframe string, nodes [][]proxmox.RRDPoint, fetchedAt time.Time) Series {
	steps := make(map[int64]*clusterStep)
	times := make([]int64, 0)

	for _, raw := range nodes {
		for _, r := range raw {
			step, ok := steps[r.Time]
			if !ok {
				step = &clusterStep{}
				steps[r.Time] = step
				times = append(times, r.Time)
			}
			step.add(r)
		}
	}
	sort.Slice(times, func(a, b int) bool { return times[a] < times[b] })

	points := make([]Point, 0, len(times))
	for _, at := range times {
		points = append(points, steps[at].point(at))
	}
	return seriesOf(cluster, timeframe, points, fetchedAt)
}

// clusterStep accumulates one instant across the nodes of a cluster.
type clusterStep struct {
	weighted float64
	cores    float64
	cpuSeen  bool

	memUsed  uint64
	memTotal uint64
	memSeen  bool
}

// add folds one node sample in. A column the node did not report is skipped
// rather than read as zero, and a sample without maxcpu carries no weight: the
// same reading that makes a node "unknown" on the overview when the token
// lacks Sys.Audit leaves it out of the cluster mean here.
func (s *clusterStep) add(r proxmox.RRDPoint) {
	if r.CPU != nil && r.MaxCPU != nil && *r.MaxCPU > 0 {
		s.weighted += *r.CPU * *r.MaxCPU
		s.cores += *r.MaxCPU
		s.cpuSeen = true
	}

	used, total := firstUint(r.MemUsed, r.Mem), firstUint(r.MemTotal, r.MaxMem)
	if used != nil && total != nil && *total > 0 {
		s.memUsed += *used
		s.memTotal += *total
		s.memSeen = true
	}
}

// point renders the accumulated step, leaving unmeasured metrics nil.
func (s *clusterStep) point(at int64) Point {
	p := Point{Time: time.Unix(at, 0).UTC()}
	if s.cpuSeen && s.cores > 0 {
		ratio := s.weighted / s.cores
		p.CPU = &ratio
	}
	if s.memSeen {
		used, total := s.memUsed, s.memTotal
		p.MemUsed = &used
		p.MemTotal = &total
	}
	return p
}

// seriesOf wraps points into the payload, computing the CPU average over the
// samples that have one. It is shared by the per-object and the cluster-wide
// derivations so the average cannot come to mean two different things.
func seriesOf(cluster, timeframe string, points []Point, fetchedAt time.Time) Series {
	var (
		sum   float64
		count int
	)
	for _, p := range points {
		if p.CPU != nil {
			sum += *p.CPU
			count++
		}
	}

	var average float64
	if count > 0 {
		average = sum / float64(count)
	}
	return Series{
		Cluster:    cluster,
		Timeframe:  timeframe,
		FetchedAt:  fetchedAt,
		Points:     points,
		CPUAverage: average,
	}
}

// deriveTasks turns the cluster log into the payload of the task list, most
// recent first.
//
// The ordering is done here rather than trusted from PVE: the endpoint is
// documented as returning the newest first, and the list is the one place in
// the interface where an out-of-order line would be read as a fact about the
// cluster.
func deriveTasks(cluster string, raw []proxmox.Task, fetchedAt time.Time) Tasks {
	entries := make([]Task, 0, len(raw))
	for _, t := range raw {
		entries = append(entries, deriveTask(t))
	}
	sort.SliceStable(entries, func(a, b int) bool {
		if !entries[a].Start.Equal(entries[b].Start) {
			return entries[a].Start.After(entries[b].Start)
		}
		// A stable tie-break on the identifier, so two tasks started within
		// the same second do not swap places between two refreshes.
		return entries[a].UPID > entries[b].UPID
	})
	return Tasks{Cluster: cluster, FetchedAt: fetchedAt, Entries: entries}
}

// deriveTask turns one entry of /cluster/tasks into a line of the log.
//
// A RUNNING task has no end time, therefore no duration and no outcome: both
// are nil, and the status is "running" rather than the empty string PVE sends.
// Reporting a duration of zero, or an outcome of false, would state something
// about a task that has not finished yet.
func deriveTask(t proxmox.Task) Task {
	out := Task{
		UPID:   t.UPID,
		Node:   t.Node,
		Type:   t.Type,
		ID:     t.ID,
		User:   t.User,
		Start:  time.Unix(t.StartTime.Int(), 0).UTC(),
		Status: taskStatusRunning,
	}
	if t.EndTime == nil {
		return out
	}

	end := time.Unix(t.EndTime.Int(), 0).UTC()
	out.End = &end

	seconds := t.EndTime.Int() - t.StartTime.Int()
	if seconds < 0 {
		// The two timestamps come from different nodes' clocks. A negative
		// duration is a clock skew, not a task that finished before it
		// started, and zero is the least wrong thing to show.
		seconds = 0
	}
	out.Duration = &seconds

	out.Status = t.Status
	if out.Status == "" {
		out.Status = taskStatusUnknown
	}
	ok := t.Status == proxmox.TaskStatusOK
	out.OK = &ok
	return out
}

// usage pairs a used and a total with their ratio, which is ZERO when the
// total is: a division by zero would put a NaN in the payload, and NaN is not
// valid JSON — encoding/json refuses it and the whole response fails.
func usage(used, total uint64) aggregate.Usage {
	u := aggregate.Usage{Used: used, Total: total}
	if total > 0 {
		u.Ratio = float64(used) / float64(total)
	}
	return u
}

// usageOf converts one of the used/total pairs of a node status.
func usageOf(u proxmox.Usage) aggregate.Usage {
	return usage(asBytes(u.Used.Int()), asBytes(u.Total.Int()))
}

// asBytes clamps a byte count to zero. PVE has no negative sizes, but a
// tolerant decode of an unexpected payload could produce one, and an unsigned
// conversion would turn it into an absurdly large total.
func asBytes(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}

// tagList makes sure a tag list is never nil, so the payload always carries an
// array.
func tagList(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

// optionalString returns nil for an empty string, which is how the payload
// says "the node did not report this".
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// firstUint returns the first non-nil of its arguments. The RRD columns are
// named differently for a node (memused/memtotal) and for a guest (mem/maxmem)
// while meaning the same thing.
func firstUint(values ...*uint64) *uint64 {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

// copyFloat copies an optional float so the payload never aliases the decoded
// response.
func copyFloat(v *float64) *float64 {
	if v == nil {
		return nil
	}
	f := *v
	return &f
}

// copyUint copies an optional unsigned integer, for the same reason.
func copyUint(v *uint64) *uint64 {
	if v == nil {
		return nil
	}
	u := *v
	return &u
}
