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

// IsCephBacked reports whether the storage draws on the cluster's Ceph
// capacity. Every RBD pool and every CephFS of a PVE cluster reports the same
// available space, that of the one Ceph cluster behind them: summing them
// multiplies the capacity by the number of storages.
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
