package aggregate

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

// Identity is what the config knows about a cluster before any polling.
type Identity struct {
	ID    string
	Name  string
	Color *string
}

// ClusterData is one poll's raw material. HA and Updates may be absent.
type ClusterData struct {
	Resources        []proxmox.Resource
	Status           []proxmox.ClusterStatusEntry
	HA               *proxmox.HAManagerStatus       // nil when unavailable or not permitted
	Updates          map[string][]proxmox.AptUpdate // node -> pending packages; a missing key means unknown
	UpdatesCheckedAt time.Time                      // zero when Updates is nil
}

// Derive turns one poll into a cluster card. It is pure: no network, no clock,
// no globals.
//
// Two fields are deliberately left at their zero value: FetchedAt and Error
// belong to the poller, which alone knows when the data was collected and
// whether the last attempt failed. For the same reason Derive never returns
// StatusUnreachable — staleness is a decision about time, and this function
// does not read a clock.
func Derive(id Identity, data ClusterData, memoryThreshold float64) ClusterOverview {
	nodes := deriveNodes(data)
	attachGuests(nodes, data.Resources)

	cpu, memory := deriveCPUAndMemory(nodes)
	updates := deriveUpdates(data)
	applyPendingUpdates(nodes, data.Updates)

	out := ClusterOverview{
		ID:      id.ID,
		Name:    id.Name,
		Color:   id.Color,
		Quorum:  deriveQuorum(data.Status),
		CPU:     cpu,
		Memory:  memory,
		Storage: deriveStorage(data.Resources),
		VMs:     deriveVMs(data.Resources),
		Nodes:   nodes,
		Updates: updates,
	}
	out.Alerts = deriveAlerts(out, memoryThreshold)
	out.Status = deriveStatus(out)
	return out
}

// nodeAccum gathers what the two node sources say about a single node before a
// verdict is reached.
type nodeAccum struct {
	// inStatus and online come from /cluster/status, the authoritative source
	// for reachability.
	inStatus bool
	online   bool
	// The figures below come from /cluster/resources, which keeps reporting a
	// stale entry for a node that just left the cluster.
	cpu    float64
	cores  int64
	mem    uint64
	maxMem uint64
	uptime int64
}

// deriveNodes builds the node list from the union of /cluster/status and
// /cluster/resources, sorted by name so that the payload is stable from one
// poll to the next.
func deriveNodes(data ClusterData) []Node {
	accum := make(map[string]*nodeAccum)
	at := func(name string) *nodeAccum {
		a, ok := accum[name]
		if !ok {
			a = &nodeAccum{}
			accum[name] = a
		}
		return a
	}

	for _, e := range data.Status {
		if e.Type != proxmox.ClusterStatusTypeNode || e.Name == "" {
			continue
		}
		a := at(e.Name)
		a.inStatus = true
		a.online = e.Online.Bool()
	}
	for _, r := range data.Resources {
		if r.Type != proxmox.ResourceTypeNode || r.Node == "" {
			continue
		}
		a := at(r.Node)
		a.cpu = r.CPU.Float()
		a.cores = r.MaxCPU.Int()
		a.mem = asBytes(r.Mem.Int())
		a.maxMem = asBytes(r.MaxMem.Int())
		a.uptime = r.Uptime.Int()
	}

	names := make([]string, 0, len(accum))
	for name := range accum {
		names = append(names, name)
	}
	sort.Strings(names)

	nodes := make([]Node, 0, len(names))
	for _, name := range names {
		a := accum[name]
		n := Node{
			Name:   name,
			Status: nodeStatus(name, a, data.HA),
			Uptime: a.uptime,
		}
		// A node has cores and memory, always: a zero for both means PVE
		// listed it without figures, which it does when the token lacks
		// Sys.Audit on the node, or when the node is offline. Neither is a
		// measurement, so neither is reported as one.
		if a.cores > 0 || a.maxMem > 0 {
			n.CPU = &CPU{Ratio: a.cpu, Cores: int(a.cores)}
			n.Memory = usagePtr(a.mem, a.maxMem)
		}
		nodes = append(nodes, n)
	}
	return nodes
}

// hasFigures reports whether a node came with CPU and memory statistics.
func hasFigures(n Node) bool {
	return n.CPU != nil && n.Memory != nil
}

// nodeStatus decides the state of one node.
//
// Maintenance wins over everything else: a node being drained still answers
// /cluster/status as online, and reporting it as such would hide the very
// state the overview screen exists to show. Only two sources are trusted for
// the verdict, the HA manager status and /cluster/status; a node known from
// /cluster/resources alone is reported as unknown rather than guessed to be
// online, since that entry may simply not have been reaped yet.
func nodeStatus(name string, a *nodeAccum, ha *proxmox.HAManagerStatus) NodeStatus {
	if ha != nil && ha.NodeState(name) == proxmox.HANodeMaintenance {
		return NodeMaintenance
	}
	if a.inStatus {
		if a.online {
			return NodeOnline
		}
		return NodeOffline
	}
	return NodeUnknown
}

// countsTowardsCapacity reports whether a node contributes to the cluster CPU
// and memory figures. A node in maintenance is still up and still holds its
// memory, so it counts; an offline one reports maxmem and maxcpu as zero and
// would only dilute the averages.
func countsTowardsCapacity(n Node) bool {
	return n.Status == NodeOnline || n.Status == NodeMaintenance
}

// deriveCPUAndMemory sums the cluster figures over the nodes that are up and
// came with figures. Both results are nil when no node did: the card then
// shows the values as unknown rather than as an empty cluster.
//
// The CPU ratio is a WEIGHTED mean, Σ(cpu×cores)/Σ(cores): averaging the
// per-node fractions gives a wrong answer as soon as the nodes differ in core
// count, which they routinely do.
func deriveCPUAndMemory(nodes []Node) (*CPU, *Usage) {
	var (
		weighted float64
		cores    int
		memUsed  uint64
		memTotal uint64
		counted  bool
	)
	for _, n := range nodes {
		if !countsTowardsCapacity(n) || !hasFigures(n) {
			continue
		}
		counted = true
		weighted += n.CPU.Ratio * float64(n.CPU.Cores)
		cores += n.CPU.Cores
		memUsed += n.Memory.Used
		memTotal += n.Memory.Total
	}
	if !counted {
		return nil, nil
	}
	cpu := &CPU{Cores: cores}
	if cores > 0 {
		cpu.Ratio = weighted / float64(cores)
	}
	return cpu, usagePtr(memUsed, memTotal)
}

// cephBackend prefixes the de-duplication key of every Ceph-backed storage.
// The pools carved out of one Ceph cluster all report its free space, so that
// space completes the key: pools of the same Ceph share a key, an RBD pool on
// an external Ceph gets its own.
const cephBackend = "ceph"

// storageBackend groups the storages that draw on the same capacity, so that
// it is counted once. Ceph is the one case /cluster/resources lets us detect:
// every RBD pool and every CephFS of a Ceph cluster reports the same available
// space, which is what tells two Ceph clusters apart.
func storageBackend(r proxmox.Resource) string {
	if r.IsCephBacked() {
		return cephBackend + "/" + strconv.FormatUint(storageAvail(r), 10)
	}
	return r.StorageKey()
}

// storageAvail is the free space a storage row reports: what it holds
// subtracted from its size, never negative.
func storageAvail(r proxmox.Resource) uint64 {
	return availOf(asBytes(r.Disk.Int()), asBytes(r.MaxDisk.Int()))
}

func availOf(used, total uint64) uint64 {
	if total > used {
		return total - used
	}
	return 0
}

// backendAccum collects the figures of one storage backend.
type backendAccum struct {
	used  uint64
	avail uint64
	// guestDisks is set when at least one storage of the backend can hold
	// VM or container disks.
	guestDisks bool
	// names de-duplicates the rows of one storage, reported once per node
	// with readings that may differ by a few bytes between nodes.
	names map[string]bool
	// figures de-duplicates the storages of the backend on their reported
	// (used, total) pair: several CephFS storages mounted on the same file
	// system report the very same numbers, and must not be summed.
	figures map[[2]uint64]bool
}

// deriveStorage is the shared capacity usable for guest disks, which is what
// the cluster card asks about: how much room is left to host VMs.
//
// Only shared storages count. A shared storage is reported once per node by
// /cluster/resources, so it is de-duplicated on its name; the local storages
// of the nodes belong to the node screen. The Ceph-backed storages make up one
// backend per Ceph cluster, told apart by the free space their pools all
// report: the total is that available space, plus what each distinct pool has
// stored. Backends that cannot hold guest disks (backups, ISO images) are left
// out, and so are unavailable storages, whose figures are zero, and shared
// storages that report no size at all. A cluster with no shared guest storage
// left falls back to the local guest storages of its nodes, so that a
// standalone node still shows its capacity.
func deriveStorage(resources []proxmox.Resource) Usage {
	shared := make(map[string]*backendAccum)
	local := make(map[string]*backendAccum)
	for _, r := range resources {
		if r.Type != proxmox.ResourceTypeStorage || r.Status != proxmox.StatusAvailable {
			continue
		}
		backends := local
		if r.Shared.Bool() {
			backends = shared
		}
		key := storageBackend(r)
		b, ok := backends[key]
		if !ok {
			b = &backendAccum{names: make(map[string]bool), figures: make(map[[2]uint64]bool)}
			backends[key] = b
		}
		b.add(r)
	}
	if hasGuestBackend(shared) {
		return sumBackends(shared)
	}
	return sumBackends(local)
}

func (b *backendAccum) add(r proxmox.Resource) {
	if b.names[r.StorageKey()] {
		return
	}
	b.names[r.StorageKey()] = true
	used, total := asBytes(r.Disk.Int()), asBytes(r.MaxDisk.Int())
	if b.figures[[2]uint64{used, total}] {
		return
	}
	b.figures[[2]uint64{used, total}] = true
	// A storage that reports no size at all — an iSCSI target exposed
	// directly, for one — says nothing about the room left for guest disks.
	// Counting it as a backend would hide the local storages behind a
	// capacity of zero instead of falling back to them.
	if total > 0 {
		b.guestDisks = b.guestDisks || r.HoldsGuestDisks()
	}
	avail := availOf(used, total)
	// The pools of a backend share their free space; the smallest reading is
	// the one every pool could actually still fill.
	if len(b.figures) == 1 || avail < b.avail {
		b.avail = avail
	}
	b.used += used
}

func hasGuestBackend(backends map[string]*backendAccum) bool {
	for _, b := range backends {
		if b.guestDisks {
			return true
		}
	}
	return false
}

func sumBackends(backends map[string]*backendAccum) Usage {
	var used, total uint64
	for _, b := range backends {
		if !b.guestDisks {
			continue
		}
		used += b.used
		total += b.used + b.avail
	}
	return usage(used, total)
}

// deriveVMs counts the guests, QEMU and LXC alike. Templates are counted apart
// and excluded from Total: they consume no runtime resource.
func deriveVMs(resources []proxmox.Resource) VMCounts {
	var c VMCounts
	for _, r := range resources {
		if !r.IsGuest() {
			continue
		}
		switch {
		case r.Template.Bool():
			c.Templates++
		case r.Status == proxmox.StatusRunning:
			c.Running++
		default:
			c.Stopped++
		}
	}
	c.Total = c.Running + c.Stopped
	return c
}

// attachGuests fills in the guest list of every node, in place.
//
// The guests come from the same /cluster/resources call as the nodes, so no
// extra request is needed: the data was already on the wire. A guest whose node
// is not in the list is dropped rather than given a node of its own — PVE keeps
// reporting guests of a node that just left the cluster, and inventing a ghost
// node for them would contradict /cluster/status.
//
// Every node ends up with a non-nil slice, so the payload always carries an
// array, and the guests of a node are sorted by VMID: the sidebar tree must not
// shuffle between two identical polls.
func attachGuests(nodes []Node, resources []proxmox.Resource) {
	byNode := make(map[string][]Guest)
	for _, r := range resources {
		if !r.IsGuest() {
			continue
		}
		byNode[r.Node] = append(byNode[r.Node], guestOf(r))
	}
	for i := range nodes {
		guests := byNode[nodes[i].Name]
		if guests == nil {
			guests = []Guest{}
		}
		sort.SliceStable(guests, func(a, b int) bool {
			if guests[a].VMID != guests[b].VMID {
				return guests[a].VMID < guests[b].VMID
			}
			return guests[a].Name < guests[b].Name
		})
		nodes[i].Guests = guests
	}
}

// guestOf turns one /cluster/resources entry into a guest of the payload. It
// assumes the entry is a guest, which attachGuests has already checked.
func guestOf(r proxmox.Resource) Guest {
	tags := r.TagList()
	if tags == nil {
		tags = []string{}
	}
	return Guest{
		VMID:   int(r.VMID.Int()),
		Name:   r.Name,
		Kind:   guestKind(r.Type),
		Status: guestStatus(r),
		CPU:    CPU{Ratio: r.CPU.Float(), Cores: int(r.MaxCPU.Int())},
		Memory: usage(asBytes(r.Mem.Int()), asBytes(r.MaxMem.Int())),
		Tags:   tags,
	}
}

// guestKind maps a resource type to the kind of guest it denotes.
func guestKind(resourceType string) GuestKind {
	if resourceType == proxmox.ResourceTypeLXC {
		return GuestLXC
	}
	return GuestQemu
}

// guestStatus decides the state of one guest.
//
// Being a template wins over the reported state, exactly as in the counts of
// deriveVMs: a template PVE happens to report as running is still a template,
// and the two views of the same guest must not disagree.
func guestStatus(r proxmox.Resource) GuestStatus {
	if r.Template.Bool() {
		return GuestTemplate
	}
	if r.Status == proxmox.StatusRunning {
		return GuestRunning
	}
	return GuestStopped
}

// deriveQuorum reads corosync quorum from /cluster/status.
//
// A standalone node returns node entries only, with no "cluster" entry: its
// quorum is not lost, it simply does not exist. Reporting nil keeps it out of
// the health verdict.
func deriveQuorum(status []proxmox.ClusterStatusEntry) *Quorum {
	var q *Quorum
	online := 0
	for _, e := range status {
		switch e.Type {
		case proxmox.ClusterStatusTypeCluster:
			if q == nil {
				q = &Quorum{Quorate: e.Quorate.Bool(), Nodes: int(e.Nodes.Int())}
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

// deriveUpdates summarizes the pending packages of the cluster, or nil when
// nothing is known about them — typically a token without Sys.Modify on
// /nodes, where "unknown" must never be read as "up to date".
func deriveUpdates(data ClusterData) *Updates {
	if data.Updates == nil {
		return nil
	}
	names := make([]string, 0, len(data.Updates))
	for name := range data.Updates {
		names = append(names, name)
	}
	sort.Strings(names)

	pending := make([]string, 0, len(names))
	var version *string
	for _, name := range names {
		packages := data.Updates[name]
		if len(packages) == 0 {
			continue
		}
		pending = append(pending, name)
		// Nodes need not offer the same release. The banner announces the
		// HIGHEST one pending anywhere, not whichever node happens to sort
		// first by name — precisely the uneven cluster updates_uneven flags.
		if v, ok := proxmox.PVEManagerVersion(packages); ok {
			if version == nil || higherVersion(v, *version) {
				v := v
				version = &v
			}
		}
	}
	return &Updates{
		Nodes:             pending,
		PVEManagerVersion: version,
		CheckedAt:         data.UpdatesCheckedAt,
	}
}

// higherVersion reports whether a ranks above b. Debian versions are compared
// segment by segment on "." and "-", numerically wherever both segments are
// numbers: a lexical compare ranks 9.2.9 above 9.2.12, which is backwards.
func higherVersion(a, b string) bool {
	as := strings.FieldsFunc(a, isVersionSeparator)
	bs := strings.FieldsFunc(b, isVersionSeparator)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv string
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		an, aerr := strconv.Atoi(av)
		bn, berr := strconv.Atoi(bv)
		if aerr == nil && berr == nil {
			if an != bn {
				return an > bn
			}
			continue
		}
		// A non-numeric segment (a suffix such as "pve1") only decides when the
		// two differ; a shorter version loses to the longer one that extends it.
		if av != bv {
			return av > bv
		}
	}
	return false
}

func isVersionSeparator(r rune) bool {
	return r == '.' || r == '-'
}

// applyPendingUpdates fills in the per-node counts, in place. A node missing
// from the map keeps a nil count: a partial 403 leaves some nodes unknown
// while the others are perfectly well known.
func applyPendingUpdates(nodes []Node, updates map[string][]proxmox.AptUpdate) {
	if updates == nil {
		return
	}
	for i := range nodes {
		packages, ok := updates[nodes[i].Name]
		if !ok {
			continue
		}
		count := len(packages)
		nodes[i].PendingUpdates = &count
	}
}

// deriveAlerts builds the banners of a cluster card, in a fixed order so that
// the payload does not shuffle between two identical polls.
//
// Maintenance is never an alert: it is a state someone chose, not a fault. It
// does weigh on the health verdict, which is a different question.
func deriveAlerts(c ClusterOverview, memoryThreshold float64) []Alert {
	alerts := make([]Alert, 0, 5)

	if c.Quorum != nil && !c.Quorum.Quorate {
		alerts = append(alerts, Alert{Kind: AlertQuorumLost})
	}

	var down []string
	for _, n := range c.Nodes {
		if n.Status == NodeOffline || n.Status == NodeUnknown {
			down = append(down, n.Name)
		}
	}
	if len(down) > 0 {
		sort.Strings(down)
		alerts = append(alerts, Alert{Kind: AlertNodeOffline, Nodes: down})
	}

	var hot []string
	for _, n := range c.Nodes {
		if n.Status == NodeOnline && n.Memory != nil && n.Memory.Ratio > memoryThreshold {
			hot = append(hot, n.Name)
		}
	}
	if (c.Memory != nil && c.Memory.Ratio > memoryThreshold) || len(hot) > 0 {
		sort.Strings(hot)
		var ratio float64
		if c.Memory != nil {
			ratio = c.Memory.Ratio
		}
		alerts = append(alerts, Alert{Kind: AlertMemoryHigh, Nodes: hot, Ratio: &ratio})
	}

	// A node that is up but came without figures is not a cluster fault: it
	// is moxy's token that may not audit it. The banner says so, and the
	// health verdict ignores it.
	var blind []string
	for _, n := range c.Nodes {
		if countsTowardsCapacity(n) && !hasFigures(n) {
			blind = append(blind, n.Name)
		}
	}
	if len(blind) > 0 {
		sort.Strings(blind)
		alerts = append(alerts, Alert{Kind: AlertNodeStatsUnavailable, Nodes: blind})
	}

	// Uneven counts come BEFORE available updates: a card shows alerts[0] only,
	// and a cluster whose nodes diverge almost always has updates pending too.
	// The other order would hide the fault behind the news for good.
	if min, max, ok := pendingSpread(c.Nodes); ok && min != max {
		min, max := min, max
		alerts = append(alerts, Alert{
			Kind:       AlertUpdatesUneven,
			PendingMin: &min,
			PendingMax: &max,
		})
	}

	if c.Updates != nil && len(c.Updates.Nodes) > 0 {
		alerts = append(alerts, Alert{
			Kind:    AlertUpdatesAvailable,
			Nodes:   c.Updates.Nodes,
			Version: c.Updates.PVEManagerVersion,
		})
	}

	return alerts
}

// pendingSpread bounds the known pending package counts of the nodes that are
// up. A nil count means the question could not be asked — a token without
// Sys.Modify, a partial 403 — and is left out rather than read as zero, which
// would flag a perfectly even cluster the moment one node declines to answer.
// Fewer than two known counts make "uneven" meaningless, hence the flag.
func pendingSpread(nodes []Node) (min, max int, ok bool) {
	known := 0
	for _, n := range nodes {
		if !countsTowardsCapacity(n) || n.PendingUpdates == nil {
			continue
		}
		count := *n.PendingUpdates
		if known == 0 || count < min {
			min = count
		}
		if known == 0 || count > max {
			max = count
		}
		known++
	}
	return min, max, known >= 2
}

// deriveStatus is the health verdict of a cluster card.
//
// Available updates alone keep a cluster healthy — they are a piece of news,
// not an incident — which is what the mockups show: a cluster flagged "sain"
// under an update banner. Missing node statistics do not degrade it either:
// they describe moxy's token, not the cluster. Anything else, including a
// node in maintenance, degrades it.
func deriveStatus(c ClusterOverview) Status {
	for _, a := range c.Alerts {
		if a.Kind != AlertUpdatesAvailable && a.Kind != AlertNodeStatsUnavailable {
			return StatusDegraded
		}
	}
	for _, n := range c.Nodes {
		if n.Status != NodeOnline {
			return StatusDegraded
		}
	}
	return StatusHealthy
}

// ComputeTotals sums the header figures over every cluster card.
func ComputeTotals(clusters []ClusterOverview) Totals {
	t := Totals{Clusters: len(clusters)}
	for _, c := range clusters {
		t.Nodes += len(c.Nodes)
		for _, n := range c.Nodes {
			// A node in maintenance is online: it still answers, it still runs
			// its guests, it merely refuses to take new ones. Counting it as
			// down would raise a false alarm on a planned operation.
			if n.Status == NodeOnline || n.Status == NodeMaintenance {
				t.NodesOnline++
			}
		}
		t.VMs += c.VMs.Total
		t.Alerts += len(c.Alerts)
	}
	return t
}

// usage pairs a used and a total with their ratio, which is zero when the
// total is: a NaN would serialise as invalid JSON and break the frontend.
func usage(used, total uint64) Usage {
	u := Usage{Used: used, Total: total}
	if total > 0 {
		u.Ratio = float64(used) / float64(total)
	}
	return u
}

// usagePtr is usage for the fields that distinguish unknown from zero.
func usagePtr(used, total uint64) *Usage {
	u := usage(used, total)
	return &u
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
