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

	CPU     CPU      `json:"cpu"`
	Memory  Usage    `json:"memory"`
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
	Kind    string `json:"kind"`
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
	Uptime int64      `json:"uptime"`
	CPU    CPU        `json:"cpu"`
	Memory Usage      `json:"memory"`
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
	AlertQuorumLost       AlertKind = "quorum_lost"
	AlertNodeOffline      AlertKind = "node_offline"
	AlertMemoryHigh       AlertKind = "memory_high"
	AlertUpdatesAvailable AlertKind = "updates_available"
	AlertUnreachable      AlertKind = "unreachable"
)

// Alert is one banner on a cluster card. Fields beyond Kind are optional and
// depend on the kind: Nodes lists the nodes concerned, Ratio carries the
// measured value for memory_high, Version the offered release for
// updates_available.
type Alert struct {
	Kind    AlertKind `json:"kind"`
	Nodes   []string  `json:"nodes,omitempty"`
	Ratio   *float64  `json:"ratio,omitempty"`
	Version *string   `json:"version,omitempty"`
}
