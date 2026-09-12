// Package detail serves the per-object views: one node, one guest, and the
// recent task log of a cluster.
//
// Unlike the overview, which a background poller refreshes for every cluster,
// these are fetched on demand: polling six nodes and a hundred and fifty guests
// every five seconds would cost far more than it is worth, and nobody is
// looking at more than one object at a time. A short cache absorbs the repeat
// requests a five-second UI refresh produces.
package detail

import (
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
)

// Node is the payload of GET /api/clusters/{cluster}/nodes/{node}.
type Node struct {
	Cluster string `json:"cluster"`
	Name    string `json:"name"`
	// Status reuses the overview vocabulary: online, offline, maintenance,
	// unknown. The two views must never disagree about a node's state.
	Status aggregate.NodeStatus `json:"status"`
	// Uptime is in seconds.
	Uptime     int64           `json:"uptime"`
	FetchedAt  time.Time       `json:"fetchedAt"`
	PVEVersion *string         `json:"pveVersion"`
	KernelVer  *string         `json:"kernelVersion"`
	CPU        aggregate.CPU   `json:"cpu"`
	Memory     aggregate.Usage `json:"memory"`
	Swap       aggregate.Usage `json:"swap"`
	RootFS     aggregate.Usage `json:"rootfs"`
	// LoadAverage holds the 1, 5 and 15 minute figures, in that order. It is
	// nil when the node did not report them.
	LoadAverage *[3]float64 `json:"loadAverage"`
	// Quorum is nil for a standalone node.
	Quorum *aggregate.Quorum `json:"quorum"`
	// HAState is the CRM's own word for this node, or nil when the cluster runs
	// no HA manager.
	HAState *string `json:"haState"`
	// PendingUpdates is nil when the token may not ask.
	PendingUpdates *int `json:"pendingUpdates"`
	// Guests hosted by this node, sorted by VMID. Never nil.
	Guests []aggregate.Guest `json:"guests"`
}

// Guest is the payload of GET /api/clusters/{cluster}/guests/{vmid}.
type Guest struct {
	Cluster string `json:"cluster"`
	// Node is the node currently hosting the guest, which changes on migration.
	Node      string                `json:"node"`
	VMID      int                   `json:"vmid"`
	Name      string                `json:"name"`
	Kind      aggregate.GuestKind   `json:"kind"`
	Status    aggregate.GuestStatus `json:"status"`
	Uptime    int64                 `json:"uptime"`
	FetchedAt time.Time             `json:"fetchedAt"`
	CPU       aggregate.CPU         `json:"cpu"`
	Memory    aggregate.Usage       `json:"memory"`
	// Disk is the boot disk. Its Used is often zero: Proxmox only knows what a
	// guest actually consumes when the guest agent reports it.
	Disk aggregate.Usage `json:"disk"`
	// HostMemory is what the hypervisor spends on this guest, which exceeds
	// what the guest itself sees.
	HostMemory *uint64  `json:"hostMemory"`
	Tags       []string `json:"tags"`
	// HAState is nil when the guest is not managed by HA.
	HAState *string `json:"haState"`
	// IPv4 comes from the guest agent and is nil without it.
	IPv4 *string `json:"ipv4"`
}

// Series is the payload of the rrd endpoints: a fixed-height sparkline needs
// points, not a rendered picture.
//
// The handoff document is explicit on this: the CPU chart is drawn at a fixed
// height and never auto-scaled, because auto-scaling turns 0.6 % into a peak.
// The backend therefore ships the raw fractions and lets the frontend decide
// the scale.
type Series struct {
	Cluster string `json:"cluster"`
	// Timeframe echoes the requested window: hour, day, week, month or year.
	Timeframe string    `json:"timeframe"`
	FetchedAt time.Time `json:"fetchedAt"`
	Points    []Point   `json:"points"`
	// Average of the CPU ratio over the window, which the mockup shows as a
	// label next to the chart.
	CPUAverage float64 `json:"cpuAverage"`
}

// Point is one sample. Missing values are nil rather than zero: RRD returns
// gaps, and drawing a gap as zero would invent a drop that never happened.
type Point struct {
	Time     time.Time `json:"time"`
	CPU      *float64  `json:"cpu"`
	MemUsed  *uint64   `json:"memUsed"`
	MemTotal *uint64   `json:"memTotal"`
	NetIn    *uint64   `json:"netIn"`
	NetOut   *uint64   `json:"netOut"`
}

// Tasks is the payload of GET /api/clusters/{cluster}/tasks.
type Tasks struct {
	Cluster   string    `json:"cluster"`
	FetchedAt time.Time `json:"fetchedAt"`
	Entries   []Task    `json:"entries"`
}

// Task is one line of the cluster log.
type Task struct {
	UPID  string     `json:"upid"`
	Node  string     `json:"node"`
	Type  string     `json:"type"`
	ID    string     `json:"id"`
	User  string     `json:"user"`
	Start time.Time  `json:"start"`
	End   *time.Time `json:"end"`
	// Duration in seconds, computed here so the UI never has to subtract two
	// timestamps in its head — the native interface's exact failing.
	Duration *int64 `json:"duration"`
	// Status is "running" while End is nil, "OK" on success, or the raw PVE
	// error string otherwise.
	Status string `json:"status"`
	OK     *bool  `json:"ok"`
}
