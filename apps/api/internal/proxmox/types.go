package proxmox

// This file holds the wire types of the Proxmox VE API, and nothing else.
//
// Its reason to exist is the set of traps the PVE API sets for a naive
// consumer. They are documented on the fields concerned; the important ones,
// gathered here:
//
//   - "cpu" is a FRACTION in [0,1], not a percentage. A node at 42% reports
//     0.42. Multiply by 100 for display only, never for arithmetic.
//   - "mem", "maxmem", "disk" and "maxdisk" are BYTES. Never assume KiB or MiB.
//   - "uptime" is in seconds.
//   - "tags" is a single string whose elements are separated by ";" (see
//     SplitTags).
//   - "template", "shared", "quorate" and "online" are declared as booleans in
//     the API schema but PVE serialises them as 0/1 in practice, and the form
//     varies with the version. Hence FlexBool.
//   - numbers are sometimes serialised as JSON strings ("42", "0.5"), again
//     depending on the version and on the field. Hence FlexInt and FlexFloat.
//   - a SHARED storage appears ONCE PER NODE in /cluster/resources. Summing
//     the entries blindly multiplies the capacity of a cluster by its node
//     count: de-duplicate on Storage when Shared is true, on Node+Storage
//     otherwise.
//
// The fixtures in testdata/ were written from the PVE schema, not captured
// from a live cluster: the exact values of node_status, the presence of
// pve-manager in apt/update and the 0/1 serialisation of booleans are to be
// confirmed on a real cluster at first deployment.

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// errFlexDecode is returned when a tolerant type is handed something it cannot
// make sense of. It deliberately does NOT quote the offending value: decoding
// errors travel up to the caller and must never carry fragments of a response
// body. See errors.go.
var errFlexDecode = errors.New("proxmox: cannot decode JSON value")

// FlexInt is an integer that tolerates the three forms PVE produces for the
// same field across versions: a JSON number (42), a JSON string ("42") and a
// JSON boolean (true). JSON null decodes to zero.
//
// A number with a fractional part is truncated towards zero rather than
// rejected: some counters are exported through a float-valued RRD column.
type FlexInt int64

// Int returns the value as an int64.
func (f FlexInt) Int() int64 { return int64(f) }

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexInt) UnmarshalJSON(data []byte) error {
	s, ok, err := flexScalar(data)
	if err != nil {
		return err
	}
	if !ok {
		*f = 0
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = FlexInt(n)
		return nil
	}
	if x, err := strconv.ParseFloat(s, 64); err == nil {
		*f = FlexInt(int64(x))
		return nil
	}
	if b, err := strconv.ParseBool(s); err == nil {
		if b {
			*f = 1
		} else {
			*f = 0
		}
		return nil
	}
	return errFlexDecode
}

// FlexFloat is a float that tolerates a JSON number, a JSON string ("0.5") and
// a JSON boolean. JSON null decodes to zero.
//
// Beware of what the value means: Resource.CPU is a fraction in [0,1].
type FlexFloat float64

// Float returns the value as a float64.
func (f FlexFloat) Float() float64 { return float64(f) }

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexFloat) UnmarshalJSON(data []byte) error {
	s, ok, err := flexScalar(data)
	if err != nil {
		return err
	}
	if !ok {
		*f = 0
		return nil
	}
	if x, err := strconv.ParseFloat(s, 64); err == nil {
		*f = FlexFloat(x)
		return nil
	}
	if b, err := strconv.ParseBool(s); err == nil {
		if b {
			*f = 1
		} else {
			*f = 0
		}
		return nil
	}
	return errFlexDecode
}

// FlexBool is a boolean that tolerates every form PVE uses for a field its own
// schema declares as a boolean: 0/1, "0"/"1", true/false and "true"/"false".
// JSON null, an absent field and an empty string all decode to false.
//
// Any non-zero number is true, so that a counter repurposed as a flag ("2"
// paths shared) does not silently read as false.
type FlexBool bool

// Bool returns the value as a bool.
func (f FlexBool) Bool() bool { return bool(f) }

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexBool) UnmarshalJSON(data []byte) error {
	s, ok, err := flexScalar(data)
	if err != nil {
		return err
	}
	if !ok {
		*f = false
		return nil
	}
	if b, err := strconv.ParseBool(s); err == nil {
		*f = FlexBool(b)
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = FlexBool(n != 0)
		return nil
	}
	if x, err := strconv.ParseFloat(s, 64); err == nil {
		*f = FlexBool(x != 0)
		return nil
	}
	return errFlexDecode
}

// flexScalar reduces a JSON scalar to its textual form. It reports ok=false
// for null and for the empty string, which every tolerant type maps to its
// zero value. An array or an object is rejected: silently accepting one would
// hide a schema change.
func flexScalar(data []byte) (s string, ok bool, err error) {
	s = strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		return "", false, nil
	}
	if s[0] == '"' {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return "", false, err
		}
		str = strings.TrimSpace(str)
		if str == "" {
			return "", false, nil
		}
		return str, true, nil
	}
	if s[0] == '[' || s[0] == '{' {
		return "", false, errFlexDecode
	}
	return s, true, nil
}

// Resource types returned by /cluster/resources in the "type" field.
const (
	ResourceTypeNode    = "node"
	ResourceTypeQemu    = "qemu"
	ResourceTypeLXC     = "lxc"
	ResourceTypeStorage = "storage"
	ResourceTypeSDN     = "sdn"
	ResourceTypePool    = "pool"
)

// Statuses seen on a Resource. Nodes and guests use "online"/"running", a
// storage uses "available".
const (
	StatusOnline    = "online"
	StatusOffline   = "offline"
	StatusRunning   = "running"
	StatusStopped   = "stopped"
	StatusAvailable = "available"
	StatusUnknown   = "unknown"
)

// Resource is one entry of /cluster/resources, the single call that yields
// nodes, guests and storages at once. The fields are a strict subset of what
// PVE returns: only what the overview screen derives from.
//
// A field is populated only for the types that carry it; the others stay at
// their zero value. There is no discriminated union here on purpose, a flat
// struct is what the API actually sends.
type Resource struct {
	// Type is one of the ResourceType* constants. An unknown type must be
	// skipped, not treated as an error: PVE adds resource types over time.
	Type string `json:"type"`
	// ID is the opaque identifier, e.g. "node/prox-pprd-2301-cit",
	// "qemu/101" or "storage/prox-pprd-2301-cit/nfs-shared".
	ID string `json:"id"`
	// Node is the node the resource lives on. For a shared storage this is
	// the node reporting it, and the same storage is reported by every node.
	Node string `json:"node"`
	// Name is the guest name. Node entries carry their name in Node, not here.
	Name string `json:"name"`
	// Status is "online"/"offline" for a node, "running"/"stopped" for a
	// guest, "available"/"unavailable" for a storage. It may be empty.
	Status string `json:"status"`
	// VMID is the guest identifier, zero for the other types.
	VMID FlexInt `json:"vmid"`

	// CPU is a FRACTION in [0,1] of the resource's total CPU capacity, NOT a
	// percentage. Averaging fractions across nodes of different sizes is
	// wrong: weight by MaxCPU.
	CPU FlexFloat `json:"cpu"`
	// MaxCPU is a number of cores (or of assigned vCPUs for a guest).
	MaxCPU FlexInt `json:"maxcpu"`

	// Mem and MaxMem are BYTES. An offline node reports MaxMem as 0, so it
	// must be excluded from cluster totals rather than counted as empty.
	Mem    FlexInt `json:"mem"`
	MaxMem FlexInt `json:"maxmem"`
	// Disk and MaxDisk are BYTES. For a storage they are the used and total
	// capacity; for a guest, Disk is usually 0 (PVE does not track it).
	Disk    FlexInt `json:"disk"`
	MaxDisk FlexInt `json:"maxdisk"`
	// Uptime is in SECONDS, zero when the resource is not running.
	Uptime FlexInt `json:"uptime"`

	// Template marks a guest as a template. Declared boolean, serialised 0/1.
	// Templates must be counted apart from running and stopped guests.
	Template FlexBool `json:"template"`
	// Shared marks a storage visible from every node. Declared boolean,
	// serialised 0/1. A shared storage appears ONCE PER NODE in the response:
	// de-duplicate on Storage when it is true, on Node+Storage otherwise,
	// or the cluster capacity is multiplied by the node count.
	Shared FlexBool `json:"shared"`

	// Storage is the storage name, the de-duplication key of a shared storage.
	Storage string `json:"storage"`
	// Content is the comma-separated list of allowed content types
	// ("images,rootdir,iso,backup,vztmpl,snippets").
	Content string `json:"content"`
	// Tags is a single string, elements separated by ";". Use SplitTags.
	Tags string `json:"tags"`
	// Plugintype is the storage backend ("dir", "lvmthin", "nfs", "rbd"...).
	Plugintype string `json:"plugintype"`
}

// IsGuest reports whether the resource is a QEMU VM or an LXC container.
func (r Resource) IsGuest() bool {
	return r.Type == ResourceTypeQemu || r.Type == ResourceTypeLXC
}

// TagList returns the tags of the resource as a slice. See SplitTags.
func (r Resource) TagList() []string { return SplitTags(r.Tags) }

// StorageKey returns the de-duplication key of a storage resource: the storage
// name alone when it is shared, since the same storage is reported once per
// node, and node/storage otherwise. It is meaningless for other types.
func (r Resource) StorageKey() string {
	if r.Shared.Bool() {
		return r.Storage
	}
	return r.Node + "/" + r.Storage
}

// Storage plugin types with a meaning for the capacity figures.
const (
	PluginRBD    = "rbd"
	PluginCephFS = "cephfs"
)

// Content types a storage may hold, as they appear in Resource.Content.
const (
	ContentImages  = "images"
	ContentRootDir = "rootdir"
)

// HasContent reports whether the storage accepts the given content type.
func (r Resource) HasContent(content string) bool {
	for _, c := range strings.Split(r.Content, ",") {
		if strings.TrimSpace(c) == content {
			return true
		}
	}
	return false
}

// HoldsGuestDisks reports whether the storage can hold VM or container disks,
// as opposed to ISO images, templates or backups only.
func (r Resource) HoldsGuestDisks() bool {
	return r.HasContent(ContentImages) || r.HasContent(ContentRootDir)
}

// IsCephBacked reports whether the storage draws on a Ceph cluster's capacity.
// Every RBD pool and every CephFS carved out of one Ceph reports the same
// available space, that of the Ceph behind them: summing them multiplies the
// capacity by the number of storages.
func (r Resource) IsCephBacked() bool {
	return r.Plugintype == PluginRBD || r.Plugintype == PluginCephFS
}

// SplitTags splits the PVE tag string into its elements. The documented
// separator is ";"; "," is also accepted because older versions and the web UI
// have both been known to produce it. Empty elements are dropped and the
// result is nil for an empty string.
func SplitTags(tags string) []string {
	fields := strings.FieldsFunc(tags, func(r rune) bool { return r == ';' || r == ',' })
	var out []string
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// Cluster status entry types returned by /cluster/status.
const (
	ClusterStatusTypeCluster = "cluster"
	ClusterStatusTypeNode    = "node"
)

// ClusterStatusEntry is one entry of /cluster/status. The response mixes a
// single entry of type "cluster" with one entry of type "node" per member.
//
// A STANDALONE node (no cluster configured) returns node entries only, with no
// "cluster" entry at all: quorum is then unknown, not lost, and must not count
// towards cluster health.
type ClusterStatusEntry struct {
	// Type is ClusterStatusTypeCluster or ClusterStatusTypeNode.
	Type string `json:"type"`
	// Name is the cluster name or the node name, depending on Type.
	Name string `json:"name"`
	// ID is "cluster" or "node/<name>".
	ID string `json:"id"`
	// Nodes is the number of members, on the "cluster" entry only.
	Nodes FlexInt `json:"nodes"`
	// Quorate is set on the "cluster" entry. Declared boolean, serialised 0/1.
	Quorate FlexBool `json:"quorate"`
	// Online is set on "node" entries. Declared boolean, serialised 0/1.
	// This is the authoritative source for node reachability: /cluster/resources
	// keeps reporting a stale entry for a node that just went away.
	Online FlexBool `json:"online"`
	// Local marks the node that answered the call.
	Local FlexBool `json:"local"`
	// NodeID is the corosync node id.
	NodeID FlexInt `json:"nodeid"`
	// IP is the corosync link address of the node.
	IP string `json:"ip"`
}

// HA node states observed in HAManagerStatus.NodeStatus.
//
// HANodeMaintenance is the primary and only structured source for "this node
// is in maintenance": /cluster/ha/status/current merely mentions it as the
// substring "maintenance mode" inside a free-form message.
const (
	HANodeOnline      = "online"
	HANodeMaintenance = "maintenance"
	HANodeFence       = "fence"
	HANodeUnknown     = "unknown"
	HANodeGone        = "gone"
)

// HAManagerStatus is the decoded /cluster/ha/status/manager_status, which needs
// Sys.Audit only. A cluster without an HA manager returns an empty NodeStatus;
// that is not an error, it means no node is in maintenance.
type HAManagerStatus struct {
	// NodeStatus maps a node name to one of the HANode* states.
	NodeStatus map[string]string `json:"node_status"`
	// ManagerStatus is the state of the HA master itself ("master",
	// "wait_for_quorum", "lost_manager_lock", ...), empty when unknown.
	ManagerStatus string `json:"manager_status"`
	// MasterNode is the node currently holding the manager lock.
	MasterNode string `json:"master_node"`
	// Timestamp is the UNIX time of the last manager round, in seconds.
	Timestamp FlexInt `json:"timestamp"`
}

// UnmarshalJSON implements json.Unmarshaler.
//
// The endpoint has been seen in two shapes depending on the version: a flat
// object carrying node_status, and an object whose "manager_status" member is
// itself the manager state hash holding node_status, master_node and
// timestamp. Both are accepted, and a flat field wins over its nested
// counterpart, so that the caller never has to care.
func (s *HAManagerStatus) UnmarshalJSON(data []byte) error {
	var raw struct {
		NodeStatus    map[string]string `json:"node_status"`
		MasterNode    string            `json:"master_node"`
		Timestamp     FlexInt           `json:"timestamp"`
		ManagerStatus json.RawMessage   `json:"manager_status"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = HAManagerStatus{
		NodeStatus: raw.NodeStatus,
		MasterNode: raw.MasterNode,
		Timestamp:  raw.Timestamp,
	}
	if len(raw.ManagerStatus) == 0 {
		return nil
	}
	var str string
	if err := json.Unmarshal(raw.ManagerStatus, &str); err == nil {
		s.ManagerStatus = str
		return nil
	}
	var nested struct {
		NodeStatus    map[string]string `json:"node_status"`
		MasterNode    string            `json:"master_node"`
		ManagerStatus string            `json:"manager_status"`
		Timestamp     FlexInt           `json:"timestamp"`
	}
	if err := json.Unmarshal(raw.ManagerStatus, &nested); err != nil {
		// Neither a string nor the known object: not worth failing the whole
		// decode over a field the overview only reads opportunistically.
		return nil
	}
	s.ManagerStatus = nested.ManagerStatus
	if s.NodeStatus == nil {
		s.NodeStatus = nested.NodeStatus
	}
	if s.MasterNode == "" {
		s.MasterNode = nested.MasterNode
	}
	if s.Timestamp == 0 {
		s.Timestamp = nested.Timestamp
	}
	return nil
}

// NodeState returns the HA state of a node, or HANodeUnknown when the node is
// absent from the manager status, which is the normal case on a cluster
// without HA.
func (s HAManagerStatus) NodeState(node string) string {
	if state, ok := s.NodeStatus[node]; ok && state != "" {
		return state
	}
	return HANodeUnknown
}

// PackagePVEManager is the package whose pending version feeds the "update
// available" banner of the overview screen.
const PackagePVEManager = "pve-manager"

// AptUpdate is one pending package of /nodes/{node}/apt/update.
//
// The endpoint requires Sys.Modify on /nodes, which a read-only audit token
// does not have: a 403 means "unknown", never "up to date". Its JSON keys are
// capitalised, unlike every other PVE endpoint.
type AptUpdate struct {
	Package    string `json:"Package"`
	Version    string `json:"Version"`
	OldVersion string `json:"OldVersion"`
	Title      string `json:"Title"`
	Priority   string `json:"Priority"`
	Section    string `json:"Section"`
	Origin     string `json:"Origin"`
	Arch       string `json:"Arch"`
}

// PVEManagerVersion returns the pending version of pve-manager in the list, and
// false when the package is not among the pending updates.
func PVEManagerVersion(updates []AptUpdate) (string, bool) {
	for _, u := range updates {
		if u.Package == PackagePVEManager {
			return u.Version, u.Version != ""
		}
	}
	return "", false
}

// errEmptyPayload is the cause of an answer whose "data" member is null where
// an object was expected. Like errFlexDecode it quotes nothing of the body.
var errEmptyPayload = errors.New("proxmox: empty payload")

// Timeframes accepted by the RRD endpoints in the "timeframe" parameter. The
// list is closed: anything else is a caller mistake and must be rejected
// before a request leaves for PVE, which would answer with a 400 the operator
// then has to decipher.
const (
	TimeframeHour  = "hour"
	TimeframeDay   = "day"
	TimeframeWeek  = "week"
	TimeframeMonth = "month"
	TimeframeYear  = "year"
)

// ValidTimeframe reports whether tf is one of the Timeframe* constants.
func ValidTimeframe(tf string) bool {
	switch tf {
	case TimeframeHour, TimeframeDay, TimeframeWeek, TimeframeMonth, TimeframeYear:
		return true
	}
	return false
}

// ValidGuestKind reports whether kind is one of the two guest types that have
// per-guest endpoints, "qemu" and "lxc". Every other resource type — a node, a
// storage, a pool — has no /nodes/{node}/{kind}/{vmid} tree at all.
func ValidGuestKind(kind string) bool {
	return kind == ResourceTypeQemu || kind == ResourceTypeLXC
}

// GuestKindSupportsAgent reports whether guests of this kind can answer the
// guest agent endpoints. Only QEMU can: the /nodes/{node}/{kind}/{vmid}/agent
// tree does not exist for LXC at all, and a container's addresses come from
// its configuration instead. Callers use it to avoid asking a question that
// has no endpoint to answer it.
func GuestKindSupportsAgent(kind string) bool { return kind == ResourceTypeQemu }

// NodeCPUInfo is the "cpuinfo" member of a node status.
type NodeCPUInfo struct {
	// CPUs is the number of logical processors, the denominator of the CPU
	// fraction reported next to it.
	CPUs FlexInt `json:"cpus"`
	// Cores and Sockets describe the physical layout; either may be absent.
	Cores   FlexInt `json:"cores"`
	Sockets FlexInt `json:"sockets"`
	// Model is the marketing name of the processor.
	Model string `json:"model"`
	// MHz is the nominal frequency. PVE serialises it as a string
	// ("2100.000"), hence FlexFloat.
	MHz FlexFloat `json:"mhz"`
}

// Usage is a used/total pair in BYTES. It is the shape of the "memory",
// "swap" and "rootfs" members of a node status; "ksm" uses it too.
//
// Free and Avail are not always sent, and Total - Used is not always Free:
// read them as reported rather than recomputing one from the others.
type Usage struct {
	Used  FlexInt `json:"used"`
	Total FlexInt `json:"total"`
	Free  FlexInt `json:"free"`
	Avail FlexInt `json:"avail"`
}

// LoadAvg is the 1, 5 and 15 minute load averages of a node.
//
// PVE sends them as an array of three STRINGS ("0.53"), not of numbers, which
// is why the elements are FlexFloat: a [3]float64 would fail to decode against
// a real cluster.
type LoadAvg [3]FlexFloat

// One, Five and Fifteen return the three averages as float64.
func (l LoadAvg) One() float64     { return l[0].Float() }
func (l LoadAvg) Five() float64    { return l[1].Float() }
func (l LoadAvg) Fifteen() float64 { return l[2].Float() }

// NodeStatus is the decoded /nodes/{node}/status: what a node reports about
// itself, which /cluster/resources does not carry — swap, root filesystem,
// load average, and the versions actually running.
//
// Sizes are BYTES, Uptime is SECONDS, and CPU is a FRACTION in [0,1] like
// everywhere else in this package.
type NodeStatus struct {
	// Uptime is in SECONDS.
	Uptime FlexInt `json:"uptime"`
	// CPU is a FRACTION in [0,1] of the node's total capacity, NOT a
	// percentage. Wait is the iowait share, same convention.
	CPU  FlexFloat `json:"cpu"`
	Wait FlexFloat `json:"wait"`
	// CPUInfo carries the core count the fraction above is relative to.
	CPUInfo NodeCPUInfo `json:"cpuinfo"`
	// Memory, Swap and RootFS are in BYTES. A node with no swap reports a
	// total of 0, which is not an error.
	Memory Usage `json:"memory"`
	Swap   Usage `json:"swap"`
	RootFS Usage `json:"rootfs"`
	// LoadAvg is an array of three STRINGS on the wire. See LoadAvg.
	LoadAvg LoadAvg `json:"loadavg"`
	// PVEVersion is the pve-manager version ("pve-manager/9.2.9/..."), and
	// KVersion the running kernel banner. Both are free-form strings meant
	// for display, never for comparison.
	PVEVersion string `json:"pveversion"`
	KVersion   string `json:"kversion"`
}

// GuestHA is the "ha" member of a guest status. Its only field of interest is
// whether the guest is managed by the HA stack, since a managed guest must not
// be stopped or migrated the way an unmanaged one is.
type GuestHA struct {
	Managed FlexBool `json:"managed"`
}

// GuestStatus is the decoded /nodes/{node}/{kind}/{vmid}/status/current for a
// QEMU VM or an LXC container. The two endpoints answer with the same shape
// for everything read here; the fields one of them omits stay at their zero
// value.
//
// TRAP. MaxDisk is the SIZE of the guest's disk, while Disk is what the guest
// actually uses — and Disk is 0 for a QEMU VM unless the guest agent reports
// it. A zero Disk therefore means "unknown", not "empty", and must never be
// rendered as 0 % of MaxDisk. An LXC container does report its real usage.
type GuestStatus struct {
	// Status is StatusRunning or StatusStopped.
	Status string `json:"status"`
	// Name and VMID identify the guest. Name may be absent on LXC.
	Name string  `json:"name"`
	VMID FlexInt `json:"vmid"`
	// Uptime is in SECONDS, zero when the guest is stopped.
	Uptime FlexInt `json:"uptime"`
	// CPU is a FRACTION in [0,1], CPUs the number of assigned vCPUs.
	CPU  FlexFloat `json:"cpu"`
	CPUs FlexInt   `json:"cpus"`
	// Mem and MaxMem are BYTES.
	Mem    FlexInt `json:"mem"`
	MaxMem FlexInt `json:"maxmem"`
	// Disk and MaxDisk are BYTES. See the trap on the type.
	Disk    FlexInt `json:"disk"`
	MaxDisk FlexInt `json:"maxdisk"`
	// Balloon is the current balloon target in BYTES, 0 when ballooning is
	// disabled or unsupported.
	Balloon FlexInt `json:"balloon"`
	// Cumulative counters since the guest started, in BYTES.
	NetIn     FlexInt `json:"netin"`
	NetOut    FlexInt `json:"netout"`
	DiskRead  FlexInt `json:"diskread"`
	DiskWrite FlexInt `json:"diskwrite"`
	// HA says whether the HA stack manages this guest.
	HA GuestHA `json:"ha"`
	// Tags is a single string, elements separated by ";". Use TagList.
	Tags string `json:"tags"`
	// Template marks a template. Declared boolean, serialised 0/1.
	Template FlexBool `json:"template"`
	// Lock is the pending operation holding the guest ("backup",
	// "migrate", "snapshot"), empty when there is none. A locked guest
	// refuses most actions.
	Lock string `json:"lock"`
	// QMPStatus is the QEMU-level state ("running", "paused",
	// "prelaunch"), empty on LXC.
	QMPStatus string `json:"qmpstatus"`
	// Agent is set when the QEMU guest agent is configured. It does not
	// promise the agent is actually answering.
	Agent FlexBool `json:"agent"`
}

// TagList returns the tags of the guest as a slice. See SplitTags.
func (g GuestStatus) TagList() []string { return SplitTags(g.Tags) }

// RRDPoint is one sample of /nodes/{node}/rrddata or of its per-guest
// counterpart. It is the union of the two column sets: a node sample carries
// the load average and the root filesystem, a guest sample carries the disk
// counters, and each leaves the other's columns nil.
//
// TRAP, and the reason every value is a pointer. RRD does not pad its holes:
// when a series has no data for a step — the node was down, the guest did not
// exist yet, the counter was only added in a later PVE version — the FIELD IS
// SIMPLY ABSENT from the object. Decoding into float64 would turn that gap
// into a perfectly plausible 0, which reads as "the CPU was idle" rather than
// "nothing is known about this step". nil means unknown; it must be rendered
// as a break in a sparkline, never as a point at zero.
//
// Time is the exception: it is present on every sample, so it is a value.
type RRDPoint struct {
	// Time is the UNIX timestamp of the sample, in SECONDS.
	Time int64
	// CPU is a FRACTION in [0,1]; MaxCPU is the core count it is relative
	// to. IOWait is the iowait share, same convention, nodes only.
	CPU    *float64
	MaxCPU *float64
	IOWait *float64
	// LoadAvg is the 1 minute load average, nodes only.
	LoadAvg *float64
	// Memory in BYTES. A node fills MemTotal/MemUsed, a guest Mem/MaxMem.
	Mem      *uint64
	MaxMem   *uint64
	MemTotal *uint64
	MemUsed  *uint64
	// Swap and root filesystem in BYTES, nodes only.
	SwapTotal *uint64
	SwapUsed  *uint64
	RootTotal *uint64
	RootUsed  *uint64
	// Disk in BYTES, guests only. As in GuestStatus, Disk is usually
	// absent: PVE does not track what a VM consumes inside its volume.
	Disk    *uint64
	MaxDisk *uint64
	// Traffic and I/O, in BYTES PER SECOND averaged over the step — these
	// are rates, not the cumulative counters of GuestStatus.
	NetIn     *uint64
	NetOut    *uint64
	DiskRead  *uint64
	DiskWrite *uint64
}

// rrdPoint is the wire form of a sample. Every column is a POINTER to a
// tolerant type, which is what tells an absent column (nil) from a zero one:
// encoding/json leaves a pointer alone when the key is missing, and sets it to
// nil on an explicit null.
type rrdPoint struct {
	Time      *FlexInt   `json:"time"`
	CPU       *FlexFloat `json:"cpu"`
	MaxCPU    *FlexFloat `json:"maxcpu"`
	IOWait    *FlexFloat `json:"iowait"`
	LoadAvg   *FlexFloat `json:"loadavg"`
	Mem       *FlexInt   `json:"mem"`
	MaxMem    *FlexInt   `json:"maxmem"`
	MemTotal  *FlexInt   `json:"memtotal"`
	MemUsed   *FlexInt   `json:"memused"`
	SwapTotal *FlexInt   `json:"swaptotal"`
	SwapUsed  *FlexInt   `json:"swapused"`
	RootTotal *FlexInt   `json:"roottotal"`
	RootUsed  *FlexInt   `json:"rootused"`
	Disk      *FlexInt   `json:"disk"`
	MaxDisk   *FlexInt   `json:"maxdisk"`
	NetIn     *FlexInt   `json:"netin"`
	NetOut    *FlexInt   `json:"netout"`
	DiskRead  *FlexInt   `json:"diskread"`
	DiskWrite *FlexInt   `json:"diskwrite"`
}

// UnmarshalJSON implements json.Unmarshaler.
//
// It decodes through the pointer-valued wire form above and then converts,
// so that the exported type speaks in plain *float64 and *uint64 while the
// tolerance for PVE's string-serialised numbers stays where it belongs.
func (p *RRDPoint) UnmarshalJSON(data []byte) error {
	var raw rrdPoint
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = RRDPoint{
		CPU:       rrdFloat(raw.CPU),
		MaxCPU:    rrdFloat(raw.MaxCPU),
		IOWait:    rrdFloat(raw.IOWait),
		LoadAvg:   rrdFloat(raw.LoadAvg),
		Mem:       rrdUint(raw.Mem),
		MaxMem:    rrdUint(raw.MaxMem),
		MemTotal:  rrdUint(raw.MemTotal),
		MemUsed:   rrdUint(raw.MemUsed),
		SwapTotal: rrdUint(raw.SwapTotal),
		SwapUsed:  rrdUint(raw.SwapUsed),
		RootTotal: rrdUint(raw.RootTotal),
		RootUsed:  rrdUint(raw.RootUsed),
		Disk:      rrdUint(raw.Disk),
		MaxDisk:   rrdUint(raw.MaxDisk),
		NetIn:     rrdUint(raw.NetIn),
		NetOut:    rrdUint(raw.NetOut),
		DiskRead:  rrdUint(raw.DiskRead),
		DiskWrite: rrdUint(raw.DiskWrite),
	}
	if raw.Time != nil {
		p.Time = raw.Time.Int()
	}
	return nil
}

// rrdFloat converts an optional tolerant float, keeping nil as nil.
func rrdFloat(v *FlexFloat) *float64 {
	if v == nil {
		return nil
	}
	f := v.Float()
	return &f
}

// rrdUint converts an optional tolerant integer to an unsigned one, keeping
// nil as nil. A negative value is clamped to zero: these columns are sizes and
// byte rates, and RRD has been seen to produce a very small negative average
// on a counter reset rather than a gap.
func rrdUint(v *FlexInt) *uint64 {
	if v == nil {
		return nil
	}
	n := v.Int()
	if n < 0 {
		n = 0
	}
	u := uint64(n)
	return &u
}

// TaskStatusOK is the exit status of a task that succeeded. Anything else is
// the failure message itself, free-form and meant for display.
const TaskStatusOK = "OK"

// Task is one entry of /cluster/tasks: a job that ran, or is still running,
// somewhere on the cluster.
//
// TRAP. EndTime is a POINTER because a RUNNING task has no "endtime" key at
// all. A plain FlexInt would decode that absence into 0, which is a valid
// timestamp (1 January 1970) and would make every running task look like an
// ancient finished one. nil means "still running", and Running says so.
// Status is empty while the task runs, TaskStatusOK when it succeeded, and the
// error message when it failed.
type Task struct {
	// UPID is the unique process identifier, the handle used to fetch the
	// log of the task.
	UPID string `json:"upid"`
	// Node is where the task runs, Type its kind ("qmstart", "vzdump",
	// "migrateall"), ID the object it acts on (a vmid, a storage name).
	Node string `json:"node"`
	Type string `json:"type"`
	ID   string `json:"id"`
	// User is the identity that started it ("root@pam").
	User string `json:"user"`
	// StartTime is a UNIX timestamp in SECONDS.
	StartTime FlexInt `json:"starttime"`
	// EndTime is absent while the task runs. See the trap on the type.
	EndTime *FlexInt `json:"endtime"`
	// Status is "" while running, TaskStatusOK on success, and the failure
	// message otherwise.
	Status string `json:"status"`
	// PID is the process id on Node, of no use outside of it.
	PID FlexInt `json:"pid"`
}

// Running reports whether the task has not finished yet, which is exactly the
// absence of an end time.
func (t Task) Running() bool { return t.EndTime == nil }

// Succeeded reports whether the task finished with TaskStatusOK. A running
// task has succeeded neither way: it is neither Succeeded nor Failed.
func (t Task) Succeeded() bool { return !t.Running() && t.Status == TaskStatusOK }

// Failed reports whether the task finished with anything other than
// TaskStatusOK. An empty status on a finished task is treated as a failure of
// unknown cause rather than as a success.
func (t Task) Failed() bool { return !t.Running() && t.Status != TaskStatusOK }

// ErrNoGuestIPv4 is returned by GuestIPv4 when the guest agent answered but
// reported no usable IPv4 address: only loopback, only IPv6, or no interface
// at all. It is a sentinel rather than an *Error because nothing failed — the
// answer is simply "unknown", which the caller renders as a dash.
var ErrNoGuestIPv4 = errors.New("proxmox: no ipv4 address reported by the guest agent")

// guestAgentInterfaces is the payload of the QEMU guest agent's
// network-get-interfaces command. Its keys are the agent's own, hyphenated,
// and unlike the rest of the API they do not follow the PVE naming.
type guestAgentInterfaces struct {
	Name        string            `json:"name"`
	HardwareAdr string            `json:"hardware-address"`
	IPAddresses []guestAgentIPAdr `json:"ip-addresses"`
}

// guestAgentIPAdr is one address of one interface as the agent reports it.
type guestAgentIPAdr struct {
	Type    string  `json:"ip-address-type"`
	Address string  `json:"ip-address"`
	Prefix  FlexInt `json:"prefix"`
}
