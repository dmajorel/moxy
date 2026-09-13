// Package aggregate polls every configured Proxmox cluster and derives the
// unified overview model that feeds the cluster overview screen.
package aggregate

import "time"

// Overview is the payload served by GET /api/overview.
//
// This shape is frozen: the frontend is built against it. All sizes are raw
// byte counts and all ratios are fractions in [0,1] — picking GiB or TiB,
// rounding and decimal separators is the frontend's job, so that no localized
// string ever leaves the backend.
type Overview struct {
	GeneratedAt time.Time         `json:"generatedAt"`
	Thresholds  Thresholds        `json:"thresholds"`
	Totals      Totals            `json:"totals"`
	Clusters    []ClusterOverview `json:"clusters"`
}

// Thresholds echoes the limits the backend applied, so the frontend can explain
// an alert without hardcoding the same numbers.
type Thresholds struct {
	Memory float64 `json:"memory"`
}

// Totals aggregates every cluster into the figures of the overview header.
type Totals struct {
	Clusters    int `json:"clusters"`
	Nodes       int `json:"nodes"`
	NodesOnline int `json:"nodesOnline"`
	VMs         int `json:"vms"`
	Alerts      int `json:"alerts"`
}

// Status is the health verdict of a cluster.
type Status string

const (
	// StatusHealthy means no alert other than available updates.
	StatusHealthy Status = "healthy"
	// StatusDegraded means at least one node or resource needs attention.
	StatusDegraded Status = "degraded"
	// StatusUnreachable means no fresh data could be fetched; the payload then
	// carries the last known snapshot, if any.
	StatusUnreachable Status = "unreachable"
)

// ClusterOverview is one cluster card of the overview screen.
type ClusterOverview struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Color  *string `json:"color"`
	Status Status  `json:"status"`

	// FetchedAt is when the data below was last successfully collected. It is
	// nil while no poll has ever succeeded.
	FetchedAt *time.Time `json:"fetchedAt"`
	// Error describes the last failed poll, even when stale data is still served.
	Error *Error `json:"error"`

	// Quorum is nil for a standalone node, which has no cluster quorum.
	Quorum *Quorum `json:"quorum"`

	// CPU and Memory are nil when no node reported any figure, which happens
	// when the token lacks Sys.Audit on /nodes: PVE then lists the nodes
	// without their statistics. Unknown is never served as zero.
	CPU    *CPU   `json:"cpu"`
	Memory *Usage `json:"memory"`
	// Storage is the shared capacity usable for guest disks, every Ceph-backed
	// storage of the cluster counted as one backend. See deriveStorage.
	Storage Usage    `json:"storage"`
	VMs     VMCounts `json:"vms"`
	Nodes   []Node   `json:"nodes"`

	// Updates is nil when the pending-update count is unknown, typically
	// because the token lacks Sys.Modify on /nodes.
	Updates *Updates `json:"updates"`
	Alerts  []Alert  `json:"alerts"`
}

// Error is a failed poll, reported without ever exposing credentials. Kind lets
// the frontend localize the cause; Message stays in English.
type Error struct {
	Kind string `json:"kind"`
	// Status is the HTTP status the cluster answered with, nil when there was
	// no answer at all. It is what separates a revoked token (401) from a
	// missing privilege (403), which "auth" alone cannot say and which call
	// for two different things to go and do.
	Status  *int   `json:"status"`
	Message string `json:"message"`
}

// Quorum reports corosync quorum as seen from the cluster.
type Quorum struct {
	Quorate bool `json:"quorate"`
	Nodes   int  `json:"nodes"`
	Online  int  `json:"online"`
}

// CPU is a load ratio together with the core count it was measured over.
type CPU struct {
	Ratio float64 `json:"ratio"`
	Cores int     `json:"cores"`
}

// Usage is a used/total pair in bytes, plus the ratio between them.
type Usage struct {
	Used  uint64  `json:"used"`
	Total uint64  `json:"total"`
	Ratio float64 `json:"ratio"`
}

// VMCounts counts guests, QEMU and LXC alike. Templates are counted apart and
// excluded from Total, since they consume no runtime resources.
type VMCounts struct {
	Running   int `json:"running"`
	Stopped   int `json:"stopped"`
	Templates int `json:"templates"`
	Total     int `json:"total"`
}

// NodeStatus is the state of a single node.
type NodeStatus string

const (
	NodeOnline  NodeStatus = "online"
	NodeOffline NodeStatus = "offline"
	// NodeMaintenance is a deliberate state, not a fault: it is never an alert.
	NodeMaintenance NodeStatus = "maintenance"
	// NodeUnknown means the node appeared in neither authoritative source.
	NodeUnknown NodeStatus = "unknown"
)

// Node is one node of a cluster.
type Node struct {
	Name   string     `json:"name"`
	Status NodeStatus `json:"status"`
	// Uptime is in seconds, nil when the node reported none. PVE omits it
	// together with the other figures when the token may not audit the node,
	// and an offline node has none to report. A zero would say the node
	// rebooted this very second, which is the opposite of what has happened.
	Uptime *int64 `json:"uptime"`
	// CPU and Memory are nil when /cluster/resources listed the node without
	// figures, which is what PVE does when the token may not audit it.
	CPU    *CPU   `json:"cpu"`
	Memory *Usage `json:"memory"`
	// PendingUpdates is nil when unknown rather than 0, so the frontend can
	// tell "nothing pending" from "not allowed to ask".
	PendingUpdates *int `json:"pendingUpdates"`
	// Guests are the VMs and containers hosted by this node, sorted by VMID.
	// It is never nil, so the payload always carries an array: the sidebar
	// tree renders "no guest" and "field missing" the same way, and the
	// frontend should not have to tell them apart.
	Guests []Guest `json:"guests"`
}

// GuestKind distinguishes a full virtual machine from a container.
type GuestKind string

const (
	GuestQemu GuestKind = "qemu"
	GuestLXC  GuestKind = "lxc"
)

// GuestStatus is the runtime state of a guest. A template is reported as such
// rather than as stopped: it consumes nothing and cannot be started.
type GuestStatus string

const (
	GuestRunning  GuestStatus = "running"
	GuestStopped  GuestStatus = "stopped"
	GuestTemplate GuestStatus = "template"
)

// Guest is one VM or container, as listed under the node that hosts it.
type Guest struct {
	VMID   int         `json:"vmid"`
	Name   string      `json:"name"`
	Kind   GuestKind   `json:"kind"`
	Status GuestStatus `json:"status"`
	CPU    CPU         `json:"cpu"`
	Memory Usage       `json:"memory"`
	Tags   []string    `json:"tags"`
}

// Updates summarizes pending package updates across a cluster.
type Updates struct {
	Nodes []string `json:"nodes"`
	// PVEManagerVersion is the version offered by the pve-manager package, when
	// that package is among the pending ones; nil otherwise.
	PVEManagerVersion *string   `json:"pveManagerVersion"`
	CheckedAt         time.Time `json:"checkedAt"`
}

// AlertKind identifies an alert without carrying localized text.
type AlertKind string

const (
	AlertQuorumLost AlertKind = "quorum_lost"
	// AlertNodeOffline flags the nodes /cluster/status reports as down. It
	// does NOT cover the ones no authoritative source mentioned at all: see
	// AlertNodeUnknown.
	AlertNodeOffline AlertKind = "node_offline"
	// AlertNodeUnknown flags the nodes that appeared in /cluster/resources
	// alone, which is NodeUnknown. A node that has just joined looks like this
	// for a few seconds, and a row of a node that no longer exists looks like
	// it until PVE reaps it. Neither is an outage, and calling both "offline"
	// sent operators hunting one.
	AlertNodeUnknown      AlertKind = "node_unknown"
	AlertMemoryHigh       AlertKind = "memory_high"
	AlertUpdatesAvailable AlertKind = "updates_available"
	AlertUnreachable      AlertKind = "unreachable"
	// AlertUpdatesUneven flags nodes that do not all have the same number of
	// pending packages. A cluster whose nodes sit at different package levels
	// is an inconsistent cluster, which updates_available alone never says.
	AlertUpdatesUneven AlertKind = "updates_uneven"
	// AlertNodeStatsUnavailable flags nodes that are up but reported no CPU
	// or memory figure. It points at moxy's own token, not at the cluster:
	// PVE strips the statistics when Sys.Audit is missing on /nodes/{node}.
	AlertNodeStatsUnavailable AlertKind = "node_stats_unavailable"
)

// Alert is one banner on a cluster card. Fields beyond Kind are optional and
// depend on the kind: Nodes lists the nodes concerned, Ratio carries the
// measured value for memory_high, Version the offered release for
// updates_available, PendingMin and PendingMax the spread for updates_uneven.
type Alert struct {
	Kind  AlertKind `json:"kind"`
	Nodes []string  `json:"nodes,omitempty"`
	// Ratio is the memory ratio of memory_high, and it always describes what
	// Nodes names: the HIGHEST ratio among the listed nodes when there are
	// any, the cluster ratio only when there is none — a cluster full on
	// average with no individual node over the threshold. Rendering the
	// cluster figure next to a list of nodes would state it of each of them.
	Ratio   *float64 `json:"ratio,omitempty"`
	Version *string  `json:"version,omitempty"`
	// PendingMin and PendingMax bound the per-node pending package counts of
	// updates_uneven. Nodes whose count is unknown are left out of both.
	PendingMin *int `json:"pendingMin,omitempty"`
	PendingMax *int `json:"pendingMax,omitempty"`
}
