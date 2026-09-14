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
	// Uptime is in seconds, nil when the node reported none — the same rule as
	// the overview, which the two views must not disagree on. An offline node
	// has no uptime, and neither has one the token may not audit.
	Uptime     *int64          `json:"uptime"`
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
	// PendingUpdates is nil when the token may not ask. It is len(Updates) by
	// construction: the two are filled from the same answer, and nil together.
	PendingUpdates *int `json:"pendingUpdates"`
	// Updates lists those same pending packages, sorted by name. Nil means the
	// question could not be asked; an empty array means the node is up to date.
	// A count alone does not tell an operator whether to schedule a window: a
	// kernel and a manual page are both "1 en attente".
	Updates []Update `json:"updates"`
	// Guests hosted by this node, sorted by VMID. Never nil.
	Guests []aggregate.Guest `json:"guests"`
}

// Update is one pending package of a node.
//
// It is a narrowed view of proxmox.AptUpdate: the fields the node view shows,
// with the empty strings PVE sends turned into the nil this API uses for
// "unknown".
type Update struct {
	Package string `json:"package"`
	// Title is the one-line description apt carries, nil when PVE sent none.
	Title *string `json:"title"`
	// OldVersion is what is installed today, nil for a package apt would pull
	// in for the first time.
	OldVersion *string `json:"oldVersion"`
	// Version is what the upgrade would install.
	Version string `json:"version"`
}

// Guest is the payload of GET /api/clusters/{cluster}/guests/{vmid}.
type Guest struct {
	Cluster string `json:"cluster"`
	// Node is the node currently hosting the guest, which changes on migration.
	Node   string                `json:"node"`
	VMID   int                   `json:"vmid"`
	Name   string                `json:"name"`
	Kind   aggregate.GuestKind   `json:"kind"`
	Status aggregate.GuestStatus `json:"status"`
	// Uptime is in seconds, nil when the guest is not running: a stopped
	// guest and a template have none, and a zero would read as "started this
	// second".
	Uptime    *int64          `json:"uptime"`
	FetchedAt time.Time       `json:"fetchedAt"`
	CPU       aggregate.CPU   `json:"cpu"`
	Memory    aggregate.Usage `json:"memory"`
	// Disk is the BOOT disk alone — the rootfs of a container. Its Used is
	// nil more often than not: Proxmox only knows what a guest actually
	// consumes when the guest agent reports it, and a zero there used to be
	// indistinguishable from a genuinely empty volume. For everything the
	// guest allocates, see Disks.
	Disk DiskUsage `json:"disk"`
	// Disks is every volume the guest declares, boot disk included, ordered
	// by configuration key. It is nil — NOT empty — when the configuration
	// could not be read, which is what a token without VM.Audit gets; an
	// empty slice means the guest genuinely declares no volume.
	Disks []GuestDisk `json:"disks"`
	// Allocated is the total volumetry the guest declares, nil for the same
	// unreadable configuration that leaves Disks nil.
	Allocated *Allocation `json:"allocated"`
	// Nets is every network interface the guest declares, ordered by
	// configuration key. Like Disks it is nil — NOT empty — when the
	// configuration could not be read; an empty slice means the guest
	// genuinely has no card.
	Nets []GuestNet `json:"nets"`
	// HostMemory is what the hypervisor spends on this guest, which exceeds
	// what the guest itself sees.
	HostMemory *uint64  `json:"hostMemory"`
	Tags       []string `json:"tags"`
	// HAState is nil when the guest is not managed by HA.
	HAState *string `json:"haState"`
	// IPv4 comes from the guest agent and is nil without it.
	IPv4 *string `json:"ipv4"`
}

// GuestDisk is one volume of a guest, as its configuration declares it.
//
// It answers a question the native interface makes the operator assemble by
// hand: what does this guest actually occupy on the storages? The boot disk
// the status endpoint reports is only ever one line of this list.
type GuestDisk struct {
	// Key is the configuration key: "scsi0", "virtio1", "rootfs", "mp0", or
	// "unused2" for a volume left behind by a detach.
	Key string `json:"key"`
	// Storage is the storage holding the volume, nil for a host device
	// passed straight through, which belongs to no storage.
	Storage *string `json:"storage"`
	// Volume is the volume id, or the host path of a passed-through device.
	Volume string `json:"volume"`
	// Size is the declared size in BYTES, nil when the configuration carries
	// none: a passed-through device, or a detached volume, whose size PVE
	// does not record. Nil is UNKNOWN, and the UI renders it as a dash —
	// never as a zero, which would claim the volume takes no room.
	Size *uint64 `json:"size"`
	// Attached reports whether the guest can see the volume. A detached one
	// still occupies its storage.
	Attached bool `json:"attached"`
}

// GuestNet is one network interface of a guest, as its configuration declares
// it, with the network's human name resolved.
//
// It answers the question the native interface leaves the operator to answer
// from memory: "vmbr12" is not a network anybody recognises, "DMZ publique"
// is.
type GuestNet struct {
	// Key is the configuration key: "net0". It names the card on the
	// hypervisor side.
	Key string `json:"key"`
	// Name is the interface name inside the guest, "eth0". Containers
	// declare it; a VM does not, so it is nil for QEMU — the guest operating
	// system names its own cards and PVE never learns of it.
	Name *string `json:"name"`
	// Bridge is the Linux bridge or SDN VNet the card is attached to, nil
	// for a card attached to nothing.
	Bridge *string `json:"bridge"`
	// Alias is the human name of that network, nil when there is none to be
	// had — either the network carries no alias, or the lookup could not be
	// made. Both leave the UI showing the bridge, which is why they need not
	// be told apart here: what must NOT happen is rendering a dash where a
	// perfectly good bridge name exists.
	Alias *string `json:"alias"`
	// Tag is the VLAN the card's traffic carries, nil when untagged. Two
	// cards on one bridge with different tags are on different networks.
	Tag *int `json:"tag"`
	// MAC is the hardware address, nil when the configuration declares none.
	MAC *string `json:"mac"`
}

// Allocation is the total volumetry of a guest.
//
// It is a small object rather than a bare number because a total alone would
// lie by omission twice over: about the volumes whose size nobody knows, and
// about the detached ones that occupy a storage without belonging to the
// running guest.
type Allocation struct {
	// Bytes is the sum of the ATTACHED volumes whose size is known. Detached
	// volumes are deliberately left out of it.
	Bytes uint64 `json:"bytes"`
	// Partial says that at least one attached volume declares no size, so
	// Bytes is a floor rather than the whole truth and the UI must say so.
	Partial bool `json:"partial"`
	// Detached is how many volumes are parked in an "unused" slot. They
	// still cost storage, which is why they are counted, and they are not
	// part of the guest, which is why they are counted apart.
	Detached int `json:"detached"`
	// DetachedBytes is the sum of the detached volumes whose size is known —
	// usually zero, since PVE records no size for an unused volume.
	DetachedBytes uint64 `json:"detachedBytes"`
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
	// CPUAverage is the mean CPU ratio over the samples that carry one, which
	// the mockup shows as a label next to the chart. It is nil when not one
	// sample does — an empty window, or a series that is nothing but gaps.
	// A zero there announced an idle node that was never measured at all.
	CPUAverage *float64 `json:"cpuAverage"`
}

// DiskUsage is a Usage whose used half may be unknown.
//
// It exists for the one figure of this API that Proxmox reports only when a
// guest agent is there to report it. Total is always known — it is the
// declared size of the volume — so only Used and the Ratio derived from it
// can be nil.
type DiskUsage struct {
	// Used is BYTES, nil when nothing reported it.
	Used  *uint64 `json:"used"`
	Total uint64  `json:"total"`
	// Ratio is Used over Total, nil whenever Used is: a fill level computed
	// from an unknown is a guess drawn as a bar.
	Ratio *float64 `json:"ratio"`
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
	// Status is the raw PVE string: "running" while End is nil, "OK" on
	// success, "WARNINGS: 2" on a job that warned, or the error message
	// otherwise. One value is written here rather than read: "unknown", for a
	// task PVE reports as finished with no status at all, which Outcome calls
	// failed. The UI shows Status as a tooltip; it never decides anything from
	// it, that is what Outcome is for.
	Status string `json:"status"`
	// Outcome is the verdict, one of the TaskOutcome* constants. It exists
	// because there are FOUR of them and a boolean can only carry two: a task
	// that finished with warnings is neither a success nor a failure, and
	// calling it one turned every nightly backup that warned into a red line.
	Outcome string `json:"outcome"`
	// Warnings is how many the task reported, nil when it reported none or
	// when the count could not be read. It is only ever set alongside
	// TaskOutcomeWarnings.
	Warnings *int `json:"warnings"`
}

// The verdicts of a task, mirrored by TaskOutcome in apps/web/src/api/types.ts.
const (
	// TaskOutcomeRunning is a task that has not finished: it has no verdict
	// yet, which is not the same as not having succeeded.
	TaskOutcomeRunning = "running"
	TaskOutcomeOK      = "ok"
	// TaskOutcomeWarnings is a job that ran to completion and reported
	// something worth a look. PVE's own interface renders it in amber; it is
	// not a failure.
	TaskOutcomeWarnings = "warnings"
	TaskOutcomeFailed   = "failed"
)
