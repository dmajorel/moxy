package detail

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
	"github.com/dmajorel/moxy/apps/api/internal/config"
	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

// overviewSource is the part of the sample overview this mock needs.
type overviewSource interface {
	Overview(ctx context.Context) (*aggregate.Overview, error)
}

// Mock answers the per-object routes without contacting any cluster.
//
// Everything it serves is derived from the sample overview rather than made up
// separately, so a node opened from the tree carries the very figures its card
// showed. That consistency is the whole point: an invented node that matches
// nothing in the overview would be worse than no mock at all.
type Mock struct {
	source overviewSource
	// base anchors the synthetic series and task times, so two calls a second
	// apart do not redraw a different history.
	base time.Time
}

// NewMock builds a mock detail source on top of a sample overview.
func NewMock(source overviewSource) *Mock {
	return &Mock{source: source, base: time.Now().UTC().Truncate(time.Minute)}
}

// NewMockAt builds one anchored at base, so that every series, every task time
// and every fetchedAt is reproducible. It is what lets a test generate the
// frontend fixtures rather than have someone capture them by hand.
func NewMockAt(source overviewSource, base time.Time) *Mock {
	return &Mock{source: source, base: base.UTC().Truncate(time.Minute)}
}

func (m *Mock) Node(ctx context.Context, cluster, node string) (*Node, error) {
	found, _, err := m.findNode(ctx, cluster, node)
	if err != nil {
		return nil, err
	}

	pve := "9.2.11"
	kernel := "6.14.8-2-pve"
	cpu := cpuOf(found.node)
	memory := memoryOf(found.node)
	load := loadFrom(cpu)
	ha := proxmox.HANodeOnline

	// A drained node reports no HA activity of its own, which is what makes the
	// maintenance state visible on the node view as well as in the tree.
	if found.node.Status == aggregate.NodeMaintenance {
		ha = "maintenance"
	}

	return &Node{
		Cluster:     cluster,
		Name:        found.node.Name,
		Status:      found.node.Status,
		Uptime:      found.node.Uptime,
		FetchedAt:   m.base,
		PVEVersion:  &pve,
		KernelVer:   &kernel,
		CPU:         cpu,
		Memory:      memory,
		Swap:        swapFrom(memory),
		RootFS:      rootFSFrom(memory),
		LoadAverage: &load,
		Quorum:      found.cluster.Quorum,
		HAState:     &ha,
		// Nil stays nil: the sample overview already distinguishes "no pending
		// update" from "not allowed to ask", and so must this view.
		PendingUpdates: found.node.PendingUpdates,
		Updates:        mockUpdates(found.node.PendingUpdates),
		// append onto a nil slice yields nil when the source is empty, and the
		// model promises an array: a drained node must serialise as [].
		Guests: mockNodeGuests(found.node.Guests),
	}, nil
}

// mockNodeGuests copies the guests of the sample card, with one field dropped.
//
// The real node view leaves Agent nil — it never asks, the flag costing one
// call per VM that only the overview's slow sweep pays. A mock that carried it
// here would invite the frontend to read a field the daemon does not serve on
// this route.
func mockNodeGuests(guests []aggregate.Guest) []aggregate.Guest {
	out := make([]aggregate.Guest, 0, len(guests))
	for _, g := range guests {
		g.Agent = nil
		out = append(out, g)
	}
	return out
}

// mockPackages is the catalogue the sample pending updates are drawn from: a
// kernel, the Proxmox stack and a handful of Debian packages, because that is
// what a real apt/update answer looks like. It is long enough to cover the
// largest count the sample overview reports.
//
// pve-manager comes first so that every node with a pending update shows the
// release the overview's alert announces: 9.2.11 installed, 9.2.12 pending.
var mockPackages = []Update{
	{Package: "pve-manager", Title: mockPtr("Proxmox Virtual Environment Management Tools"), OldVersion: mockPtr("9.2.11"), Version: "9.2.12"},
	{Package: "proxmox-kernel-6.14", Title: mockPtr("Proxmox Kernel Image"), OldVersion: mockPtr("6.14.8-2"), Version: "6.14.9-1"},
	{Package: "pve-qemu-kvm", Title: mockPtr("Full virtualization on x86 hardware"), OldVersion: mockPtr("9.2.0-5"), Version: "9.2.0-6"},
	{Package: "qemu-server", Title: mockPtr("Qemu Server Tools"), OldVersion: mockPtr("9.0.12"), Version: "9.0.13"},
	{Package: "pve-container", Title: mockPtr("Proxmox VE Container management tool"), OldVersion: mockPtr("6.0.9"), Version: "6.0.10"},
	{Package: "libpve-common-perl", Title: mockPtr("Proxmox VE base library"), OldVersion: mockPtr("9.0.6"), Version: "9.0.7"},
	{Package: "libpve-storage-perl", Title: mockPtr("Proxmox VE storage management library"), OldVersion: mockPtr("9.0.8"), Version: "9.0.9"},
	{Package: "proxmox-backup-client", Title: mockPtr("Proxmox Backup Client tools"), OldVersion: mockPtr("4.0.6-1"), Version: "4.0.7-1"},
	{Package: "ceph-common", Title: mockPtr("common utilities to mount and interact with a ceph storage cluster"), OldVersion: mockPtr("19.2.1-pve2"), Version: "19.2.2-pve1"},
	{Package: "openssh-server", Title: mockPtr("secure shell (SSH) server, for secure access from remote machines"), OldVersion: mockPtr("1:9.9p1-4"), Version: "1:9.9p1-5"},
	{Package: "libssl3", Title: mockPtr("Secure Sockets Layer toolkit - shared libraries"), OldVersion: mockPtr("3.4.1-1"), Version: "3.4.2-1"},
	{Package: "systemd", Title: mockPtr("system and service manager"), OldVersion: mockPtr("257.3-1"), Version: "257.4-1"},
	{Package: "curl", Title: mockPtr("command line tool for transferring data with URL syntax"), OldVersion: mockPtr("8.12.1-2"), Version: "8.12.1-3"},
	{Package: "zfsutils-linux", Title: mockPtr("command-line tools to manage OpenZFS filesystems"), OldVersion: mockPtr("2.3.1-pve1"), Version: "2.3.2-pve1"},
	// A package apt would install for the first time: no old version, which the
	// node view must render as a dash rather than as an arrow out of nothing.
	{Package: "proxmox-firewall", Title: mockPtr("Proxmox nftables firewall implementation"), OldVersion: nil, Version: "1.2.0"},
}

// mockUpdates draws the first pending packages of the catalogue, so that the
// list a node serves always agrees with the count its card shows. It mirrors
// the three states of the real thing: nil for a node nobody may ask about, an
// empty array for a node that is up to date, the packages otherwise.
func mockUpdates(pending *int) []Update {
	if pending == nil {
		return nil
	}
	count := *pending
	if count > len(mockPackages) {
		count = len(mockPackages)
	}
	out := make([]Update, 0, count)
	for _, u := range mockPackages[:count] {
		out = append(out, cloneUpdate(u))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Package < out[j].Package })
	return out
}

// cloneUpdate copies what the pointers point at as well as the struct, so that
// a caller writing through them cannot reach the shared catalogue.
func cloneUpdate(u Update) Update {
	out := u
	if u.Title != nil {
		out.Title = mockPtr(*u.Title)
	}
	if u.OldVersion != nil {
		out.OldVersion = mockPtr(*u.OldVersion)
	}
	return out
}

// mockPtr returns a pointer to a copy of v.
func mockPtr[T any](v T) *T {
	return &v
}

func (m *Mock) Guest(ctx context.Context, cluster string, vmid int) (*Guest, error) {
	found, err := m.findGuest(ctx, cluster, vmid)
	if err != nil {
		return nil, err
	}
	guest := found.guest

	result := &Guest{
		Cluster:   cluster,
		Node:      found.node.Name,
		VMID:      guest.VMID,
		Name:      guest.Name,
		Kind:      guest.Kind,
		Status:    guest.Status,
		FetchedAt: m.base,
		CPU:       guest.CPU,
		Memory:    guest.Memory,
		Disk:      diskFrom(guest),
		Tags:      append(make([]string, 0, len(guest.Tags)), guest.Tags...),
	}
	result.Disks, result.Allocated = disksFrom(guest, result.Disk.Total)
	result.Nets = netsFrom(guest)

	if guest.Status == aggregate.GuestRunning {
		// Uptimes are staggered by VMID so the list does not look cloned.
		// Only a running guest has one: the others must serialise as null,
		// which is the case the header has to render without a duration.
		uptime := int64(3600 * (24 + guest.VMID%72))
		result.Uptime = &uptime
		host := guest.Memory.Used + uint64(guest.VMID%7+1)*64*1024*1024
		result.HostMemory = &host
		// The CRM's own vocabulary, and only for the guests the sample says
		// HA manages: a mock that gives every guest an HA state would let an
		// interface through that cannot render a guest without one.
		if state, managed := mockHAServiceState(guest); managed {
			result.HAState = &state
		}
		// Every third guest has no agent: the view must handle a missing
		// address, which is the common case on a real estate.
		if guest.VMID%3 != 0 {
			ip := fmt.Sprintf("10.18.%d.%d", 160+guest.VMID%4, guest.VMID%254+1)
			result.IPv4 = &ip
		}
	}
	return result, nil
}

func (m *Mock) NodeSeries(ctx context.Context, cluster, node, timeframe string) (*Series, error) {
	found, _, err := m.findNode(ctx, cluster, node)
	if err != nil {
		return nil, err
	}
	return m.series(cluster, timeframe, cpuOf(found.node).Ratio, memoryOf(found.node), int64(len(found.node.Name)), true)
}

func (m *Mock) GuestSeries(ctx context.Context, cluster string, vmid int, timeframe string) (*Series, error) {
	found, err := m.findGuest(ctx, cluster, vmid)
	if err != nil {
		return nil, err
	}
	return m.series(cluster, timeframe, found.guest.CPU.Ratio, found.guest.Memory, int64(vmid), true)
}

// ClusterSeries answers the overview card with an hour built around the
// figures its own card shows, holes included: a mock too clean would let
// through a chart unable to draw a gap.
func (m *Mock) ClusterSeries(ctx context.Context, cluster, timeframe string) (*Series, error) {
	overview, err := m.overviewOf(ctx, cluster)
	if err != nil {
		return nil, err
	}
	if overview.CPU == nil || overview.Memory == nil {
		// No node reported a figure. Nothing is drawn, which is what the real
		// service serves too; an invented hour would say the cluster was fine.
		if _, _, err := windowOf(timeframe); err != nil {
			return nil, err
		}
		return &Series{
			Cluster:   cluster,
			Timeframe: timeframe,
			FetchedAt: m.base,
			Points:    []Point{},
		}, nil
	}
	// The cluster series carries no network columns, as the real one does not:
	// a sum over whichever nodes answered is a figure nobody asked for.
	return m.series(cluster, timeframe, overview.CPU.Ratio, *overview.Memory, int64(len(overview.Name)), false)
}

func (m *Mock) Tasks(ctx context.Context, cluster string, limit int) (*Tasks, error) {
	overview, err := m.overviewOf(ctx, cluster)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = DefaultTaskLimit
	}

	guests := allGuests(overview)
	if len(overview.Nodes) == 0 {
		// An unreachable cluster has no node to file a task against; an empty
		// journal is the truthful answer.
		return &Tasks{Cluster: cluster, FetchedAt: m.base, Entries: []Task{}}, nil
	}
	entries := make([]Task, 0, limit)
	for i := 0; i < limit; i++ {
		kind, subject := taskShape(i, guests)
		node := overview.Nodes[i%len(overview.Nodes)].Name
		entries = append(entries, m.task(i, 0, node, kind, subject))
	}

	return &Tasks{Cluster: cluster, FetchedAt: m.base, Entries: entries}, nil
}

// GuestTasks answers the per-guest log the live route serves: the jobs filed
// against this guest alone, on the node hosting it.
//
// They are BUILT rather than sieved out of the cluster journal above. Sieving
// a log of fifty lines by vmid yields nothing for most guests — which is the
// very emptiness this route exists to fix — and a mock reproducing the bug
// would let a broken view through unnoticed.
func (m *Mock) GuestTasks(ctx context.Context, cluster string, vmid, limit int) (*Tasks, error) {
	found, err := m.findGuest(ctx, cluster, vmid)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = DefaultTaskLimit
	}

	// A handful of entries, deterministic per guest: a machine's own history is
	// short, and a page of fifty identical backups would say nothing about how
	// the table reads.
	count := vmid%4 + 3
	if count > limit {
		count = limit
	}

	kinds := []string{"vzdump", "qmstart", "qmsnapshot", "qmigrate"}
	subject := strconv.Itoa(vmid)
	entries := make([]Task, 0, count)
	for i := 0; i < count; i++ {
		kind := kinds[i%len(kinds)]
		if found.guest.Kind == aggregate.GuestLXC {
			kind = strings.Replace(kind, "qm", "vz", 1)
		}
		// The vmid seeds the outcome so that the demonstration holds failures
		// as well as successes: over a log of four lines, i%11 alone never
		// fails once.
		entries = append(entries, m.task(i, vmid, found.node.Name, kind, subject))
	}

	return &Tasks{Cluster: cluster, FetchedAt: m.base, Entries: entries}, nil
}

// task builds entry i of a demonstration log, counting backwards from m.base.
//
// seed offsets the rule deciding which entry failed, so that two logs drawn
// from the same shape — the cluster journal, one guest's history — do not both
// fail on the same line.
func (m *Mock) task(i, seed int, node, kind, subject string) Task {
	start := m.base.Add(-time.Duration(i*17+3) * time.Minute)
	task := Task{
		UPID:  fmt.Sprintf("UPID:%s:%08X:%08X:%s:%s:root@pam:", node, i+1, start.Unix(), kind, subject),
		Node:  node,
		Type:  kind,
		ID:    subject,
		User:  "root@pam",
		Start: start,
	}

	// The most recent task is left running so the view exercises a null
	// duration, which is not the same as a duration of zero.
	if i == 0 {
		task.Status = "running"
		task.Outcome = TaskOutcomeRunning
		return task
	}

	seconds := int64(i%9 + 1)
	end := start.Add(time.Duration(seconds) * time.Second)
	task.End = &end
	task.Duration = &seconds

	// One entry in thirteen finished with no status at all. PVE does that, and
	// an unknown outcome is not a success: the view has to have a rendering
	// for it, or it will paint it green.
	if (i+seed)%13 == 7 {
		task.Status = taskStatusUnknown
		task.Outcome = TaskOutcomeFailed
		return task
	}

	// One entry in seven finished WITH WARNINGS, which is the third outcome
	// and the one the log used to paint as a failure: a nightly vzdump that
	// warns about a single guest is not a backup that did not happen.
	if (i+seed)%7 == 3 {
		warnings := (i+seed)%3 + 1
		task.Status = fmt.Sprintf("%s %d", proxmox.TaskStatusWarningsPrefix, warnings)
		task.Outcome = TaskOutcomeWarnings
		task.Warnings = &warnings
		return task
	}

	if (i+seed)%11 == 0 {
		task.Status = "storage 'nfs-shared' is not online"
		task.Outcome = TaskOutcomeFailed
		return task
	}
	task.Status = proxmox.TaskStatusOK
	task.Outcome = TaskOutcomeOK
	return task
}

/* ------------------------------------------------------------------ lookup */

type foundNode struct {
	cluster aggregate.ClusterOverview
	node    aggregate.Node
}

type foundGuest struct {
	node  aggregate.Node
	guest aggregate.Guest
}

func (m *Mock) overviewOf(ctx context.Context, cluster string) (aggregate.ClusterOverview, error) {
	overview, err := m.source.Overview(ctx)
	if err != nil {
		return aggregate.ClusterOverview{}, err
	}
	for _, candidate := range overview.Clusters {
		if candidate.ID == cluster {
			return candidate, nil
		}
	}
	return aggregate.ClusterOverview{}, notFoundf("cluster %q", cluster)
}

func (m *Mock) findNode(ctx context.Context, cluster, node string) (foundNode, aggregate.ClusterOverview, error) {
	view, err := m.overviewOf(ctx, cluster)
	if err != nil {
		return foundNode{}, view, err
	}
	for _, candidate := range view.Nodes {
		if candidate.Name == node {
			return foundNode{cluster: view, node: candidate}, view, nil
		}
	}
	return foundNode{}, view, notFoundf("node %q in cluster %q", node, cluster)
}

func (m *Mock) findGuest(ctx context.Context, cluster string, vmid int) (foundGuest, error) {
	view, err := m.overviewOf(ctx, cluster)
	if err != nil {
		return foundGuest{}, err
	}
	for _, node := range view.Nodes {
		for _, guest := range node.Guests {
			if guest.VMID == vmid {
				return foundGuest{node: node, guest: guest}, nil
			}
		}
	}
	return foundGuest{}, notFoundf("guest %d in cluster %q", vmid, cluster)
}

func allGuests(view aggregate.ClusterOverview) []aggregate.Guest {
	var guests []aggregate.Guest
	for _, node := range view.Nodes {
		guests = append(guests, node.Guests...)
	}
	sort.Slice(guests, func(i, j int) bool { return guests[i].VMID < guests[j].VMID })
	return guests
}

/* ------------------------------------------------------------- derivation */

// series builds a deterministic history around the object's current load.
//
// Two samples are deliberately left nil: RRD returns gaps, and a chart that
// cannot draw one here would not draw one against a real cluster either.
func (m *Mock) series(cluster, timeframe string, current float64, memory aggregate.Usage, seed int64, network bool) (*Series, error) {
	count, step, err := windowOf(timeframe)
	if err != nil {
		return nil, err
	}

	points := make([]Point, 0, count)
	var sum float64
	var samples int

	for i := 0; i < count; i++ {
		at := m.base.Add(-time.Duration(count-1-i) * step)
		point := Point{Time: at}

		if isGap(i, count, seed) {
			points = append(points, point)
			continue
		}

		ratio := wobble(current, i, seed)
		used := float64(memory.Used) * (0.94 + 0.12*noise(i, seed+7))
		memUsed := uint64(math.Max(0, used))

		point.CPU = &ratio
		point.MemUsed = &memUsed
		point.MemTotal = &memory.Total
		if network {
			// Bytes per second, as RRD reports them. Present so that a reader
			// of these series is exercised against columns that exist; the
			// cluster series leaves them nil, like the real one.
			in := uint64(math.Max(0, 120_000*(0.4+noise(i, seed+11))))
			out := uint64(math.Max(0, 90_000*(0.3+noise(i, seed+13))))
			point.NetIn = &in
			point.NetOut = &out
		}
		points = append(points, point)

		sum += ratio
		samples++
	}

	// A window whose every sample is a gap has no average to state. The mock
	// produces one on purpose: an interface that renders "moy. 0 %" there
	// would be announcing a measurement nobody took.
	var average *float64
	if samples > 0 {
		mean := sum / float64(samples)
		average = &mean
	}

	return &Series{
		Cluster:    cluster,
		Timeframe:  timeframe,
		FetchedAt:  m.base,
		Points:     points,
		CPUAverage: average,
	}, nil
}

func windowOf(timeframe string) (count int, step time.Duration, err error) {
	switch timeframe {
	case "hour":
		return 60, time.Minute, nil
	case "day":
		return 96, 15 * time.Minute, nil
	case "week":
		return 84, 2 * time.Hour, nil
	case "month":
		return 90, 8 * time.Hour, nil
	case "year":
		return 73, 120 * time.Hour, nil
	default:
		return 0, 0, invalidf("timeframe %q", timeframe)
	}
}

// isGap marks two holes per window, always in the same places for a given
// object, so screenshots and tests stay reproducible.
func isGap(index, count int, seed int64) bool {
	if count < 8 {
		return false
	}
	first := int(seed%7) + count/4
	return index == first || index == first+1
}

// wobble varies a value around its current level without ever leaving [0,1].
func wobble(current float64, index int, seed int64) float64 {
	amplitude := math.Max(current*0.45, 0.004)
	value := current + amplitude*(noise(index, seed)-0.5)*2
	return math.Min(1, math.Max(0, value))
}

// noise is a cheap deterministic pseudo-random in [0,1): no global state, no
// rand seeding, identical on every run.
func noise(index int, seed int64) float64 {
	x := math.Sin(float64(index)*12.9898 + float64(seed)*78.233)
	return x - math.Floor(x)
}

// cpuOf and memoryOf unwrap the optional node figures. The sample overview
// always fills them, but the model allows nil — PVE omits them when the token
// may not audit the node — and a mock that ignored that would compile against a
// contract it does not honour.
func cpuOf(node aggregate.Node) aggregate.CPU {
	if node.CPU == nil {
		return aggregate.CPU{}
	}
	return *node.CPU
}

func memoryOf(node aggregate.Node) aggregate.Usage {
	if node.Memory == nil {
		return aggregate.Usage{}
	}
	return *node.Memory
}

func loadFrom(cpu aggregate.CPU) [3]float64 {
	base := cpu.Ratio * float64(cpu.Cores)
	return [3]float64{
		math.Round(base*100) / 100,
		math.Round(base*1.08*100) / 100,
		math.Round(base*0.96*100) / 100,
	}
}

func swapFrom(memory aggregate.Usage) aggregate.Usage {
	total := memory.Total / 16
	return aggregate.Usage{Used: 0, Total: total, Ratio: 0}
}

func rootFSFrom(memory aggregate.Usage) aggregate.Usage {
	total := memory.Total * 14
	return ratioUsage(total/5, total)
}

// ratioUsage builds a used/total pair without dividing by zero. A node PVE
// listed without its figures has a memory total of zero, and every size
// derived from it is zero too: the quotient is a NaN, which encoding/json
// refuses outright -- so the whole node view came back as a 500.
func ratioUsage(used, total uint64) aggregate.Usage {
	if total == 0 {
		return aggregate.Usage{}
	}
	return aggregate.Usage{Used: used, Total: total, Ratio: float64(used) / float64(total)}
}

// diskFrom keeps the boot disk allocated but unmeasured, which is what Proxmox
// reports without a guest agent — the case the view must not draw as empty.
// Unmeasured is a nil Used, never a zero: a volume carrying a filesystem is
// never genuinely empty, so the two could not be told apart.
func diskFrom(guest aggregate.Guest) DiskUsage {
	total := guest.Memory.Total * 4
	if total == 0 {
		total = 32 * 1024 * 1024 * 1024
	}
	if guest.VMID%4 == 0 {
		used := total / 3
		ratio := float64(used) / float64(total)
		return DiskUsage{Used: &used, Total: total, Ratio: &ratio}
	}
	return DiskUsage{Total: total}
}

// disksFrom builds the volume list of a demonstration guest around the boot
// disk of its card, so a guest opened from the tree carries the figure it
// showed there, plus the rest of what it allocates.
//
// It writes a real PVE configuration and hands it to deriveDisks rather than
// assembling the payload itself. That is the point: the mock then exercises
// the parsing, the ordering and the totals the live path uses, instead of
// agreeing with them by accident.
//
// The configuration is deliberately UNTIDY. Every shape the view has to
// survive is in it: a container with a single rootfs, a VM whose data disk
// dwarfs its system disk, a CD-ROM drive that must not be counted, a
// passed-through device whose size nobody knows, and a volume left behind by
// someone who detached a disk without deleting it. A clean mock would let
// through a UI that renders none of them.
func disksFrom(guest aggregate.Guest, boot uint64) ([]GuestDisk, *Allocation) {
	// One guest in five has an unreadable configuration, which is what a
	// token without VM.Audit gets: no list at all, not an empty one. The
	// remainder is 3 rather than 0 so that the first guest of the tree still
	// shows its volumes — a demonstration whose opening screen is a degraded
	// one teaches the wrong thing.
	if guest.VMID%5 == 3 {
		return nil, nil
	}

	config := proxmox.GuestConfig{}
	if guest.Kind == aggregate.GuestLXC {
		config["rootfs"] = fmt.Sprintf("local-zfs:subvol-%d-disk-0,size=%d", guest.VMID, boot)
		if guest.VMID%3 == 0 {
			config["mp0"] = fmt.Sprintf("cephfs:subvol-%d-disk-1,mp=/srv/data,size=%d", guest.VMID, boot*6)
		}
	} else {
		config["scsi0"] = fmt.Sprintf("ceph-vm:vm-%d-disk-0,iothread=1,size=%d", guest.VMID, boot)
		config["efidisk0"] = fmt.Sprintf("ceph-vm:vm-%d-disk-1,efitype=4m,size=528K", guest.VMID)
		// The drive an installation was left plugged into. It allocates
		// nothing, and the list must not show it.
		config["ide2"] = "local:iso/debian-13.2.0-amd64-netinst.iso,media=cdrom"
		if guest.VMID%2 == 0 {
			config["scsi1"] = fmt.Sprintf("ceph-vm:vm-%d-disk-2,backup=0,size=%d", guest.VMID, boot*11)
		}
		// A raw device handed to the guest declares no size at all: the total
		// is then a floor, and the view has to say so.
		if guest.VMID%7 == 0 {
			config["scsi2"] = "/dev/disk/by-id/ata-SAMSUNG_MZ7LH1T9HMLT_S45NNA0N"
		}
	}
	// Something left behind by a detach, still occupying its storage.
	if guest.VMID%4 == 0 {
		volume := fmt.Sprintf("local-lvm:vm-%d-disk-3", guest.VMID)
		if guest.Kind == aggregate.GuestLXC {
			volume = fmt.Sprintf("local-zfs:subvol-%d-disk-3", guest.VMID)
		}
		config["unused0"] = volume
	}

	return deriveDisks(config)
}

// netsFrom builds the interface list of a demonstration guest.
//
// Like disksFrom it writes a real PVE configuration and hands it to deriveNets
// with a real alias table, rather than assembling the payload itself: the mock
// then exercises the two guest syntaxes, the ordering and the alias lookup that
// the live path uses, instead of agreeing with them by accident.
//
// The sample is deliberately uneven, because every shape the view has to
// survive has to be in it:
//
//   - a guest with TWO cards, so the block is never assumed to hold one line;
//   - a bridge WITH an alias and a bridge WITHOUT, which must render its own
//     name rather than a dash;
//   - a VLAN tag on one card and none on the other;
//   - a container, whose configuration names the interface its guest sees
//     ("eth0") where a VM's never does.
func netsFrom(guest aggregate.Guest) []GuestNet {
	// The same guests whose configuration is unreadable have no card list
	// either: it is one call that failed, not two. Keeping the two fields in
	// step is what makes the degraded state believable.
	if guest.VMID%5 == 3 {
		return nil
	}

	// The names an administrator gave these networks. "vmbr0" is deliberately
	// absent: the ordinary bridge nobody bothered to comment is the case the
	// interface must render without inventing a dash.
	aliases := map[string]string{
		"vmbr1":    "DMZ publique",
		"vnet-adm": "Administration",
	}

	config := proxmox.GuestConfig{}
	if guest.Kind == aggregate.GuestLXC {
		config["net0"] = fmt.Sprintf("name=eth0,bridge=vmbr0,hwaddr=BC:24:11:%02X:%02X:01,ip=dhcp,type=veth", guest.VMID%256, guest.VMID/256%256)
		if guest.VMID%3 == 0 {
			config["net1"] = fmt.Sprintf("name=eth1,bridge=vnet-adm,hwaddr=BC:24:11:%02X:%02X:02,tag=42,type=veth", guest.VMID%256, guest.VMID/256%256)
		}
	} else {
		// The QEMU shorthand, which is what PVE actually writes: the model is
		// the key of the pair and the MAC is its value.
		config["net0"] = fmt.Sprintf("virtio=BC:24:11:%02X:%02X:01,bridge=vmbr0,firewall=1", guest.VMID%256, guest.VMID/256%256)
		if guest.VMID%2 == 0 {
			config["net1"] = fmt.Sprintf("virtio=BC:24:11:%02X:%02X:02,bridge=vmbr1,tag=120", guest.VMID%256, guest.VMID/256%256)
		}
	}

	return deriveNets(config, aliases)
}

// taskShape spreads the journal over the kinds an operator actually sees.
func taskShape(index int, guests []aggregate.Guest) (kind, subject string) {
	if len(guests) == 0 || index%4 == 3 {
		return "aptupdate", ""
	}
	guest := guests[index%len(guests)]
	kinds := []string{"vzdump", "qmstart", "qmigrate", "qmsnapshot"}
	kind = kinds[index%len(kinds)]
	if guest.Kind == aggregate.GuestLXC {
		kind = strings.Replace(kind, "qm", "vz", 1)
	}
	return kind, fmt.Sprintf("%d", guest.VMID)
}

// MaintenancePlan derives a drain plan from the sample overview, using the same
// placement as the live service so the dialog can be exercised without a
// cluster — including the case where the cluster has no room.
func (m *Mock) MaintenancePlan(ctx context.Context, cluster, node string) (*MaintenancePlan, error) {
	view, err := m.overviewOf(ctx, cluster)
	if err != nil {
		return nil, err
	}

	// The mock reads no configuration, so it shows the default.
	plan := buildPlan(cluster, node, m.clusterViewOf(view), config.DefaultThreshold)
	if plan == nil {
		return nil, notFoundf("node %q in cluster %q", node, cluster)
	}
	plan.FetchedAt = m.base
	return plan, nil
}

// clusterViewOf turns a sample cluster card back into the raw shape buildPlan
// consumes, so the mock and the live service run the very same placement rather
// than two implementations that could disagree.
func (m *Mock) clusterViewOf(view aggregate.ClusterOverview) clusterView {
	var raw clusterView
	haStatus := make(map[string]string, len(view.Nodes))
	services := make(map[string]proxmox.HAServiceStatus)

	for _, node := range view.Nodes {
		memory := memoryOf(node)
		cpu := cpuOf(node)
		raw.Resources = append(raw.Resources, proxmox.Resource{
			Type:   proxmox.ResourceTypeNode,
			Node:   node.Name,
			Name:   node.Name,
			Status: string(node.Status),
			Mem:    proxmox.FlexInt(int64(memory.Used)),
			MaxMem: proxmox.FlexInt(int64(memory.Total)),
			MaxCPU: proxmox.FlexInt(int64(cpu.Cores)),
		})
		raw.Status = append(raw.Status, proxmox.ClusterStatusEntry{
			Type: proxmox.ClusterStatusTypeNode,
			Name: node.Name,
			// A node in maintenance is still online: it is reachable, it simply
			// refuses new guests.
			Online: proxmox.FlexBool(node.Status == aggregate.NodeOnline || node.Status == aggregate.NodeMaintenance),
		})
		haStatus[node.Name] = haStateOf(node.Status)

		for _, guest := range node.Guests {
			kind := proxmox.ResourceTypeQemu
			if guest.Kind == aggregate.GuestLXC {
				kind = proxmox.ResourceTypeLXC
			}
			raw.Resources = append(raw.Resources, proxmox.Resource{
				Type:     kind,
				Node:     node.Name,
				Name:     guest.Name,
				VMID:     proxmox.FlexInt(int64(guest.VMID)),
				Status:   guestResourceStatus(guest.Status),
				Mem:      proxmox.FlexInt(int64(guest.Memory.Used)),
				MaxMem:   proxmox.FlexInt(int64(guest.Memory.Total)),
				Template: proxmox.FlexBool(guest.Status == aggregate.GuestTemplate),
			})
			if state, managed := mockHAServiceState(guest); managed {
				services[proxmox.HAServiceID(kind, guest.VMID)] = proxmox.HAServiceStatus{
					Node:  node.Name,
					State: state,
				}
			}
		}
	}

	if view.Quorum != nil {
		raw.Status = append(raw.Status, proxmox.ClusterStatusEntry{
			Type:    proxmox.ClusterStatusTypeCluster,
			Name:    view.ID,
			Nodes:   proxmox.FlexInt(int64(view.Quorum.Nodes)),
			Quorate: proxmox.FlexBool(view.Quorum.Quorate),
		})
	}
	raw.HA = &proxmox.HAManagerStatus{NodeStatus: haStatus, ServiceStatus: services}
	return raw
}

// mockHAServiceState decides which sample guests the CRM manages, and in what
// state. Two guests in five are left out on purpose: the plan has to show both
// "the CRM will move this" and "somebody has to move this by hand", and the
// guest view has to render an unmanaged guest as the em dash.
func mockHAServiceState(guest aggregate.Guest) (string, bool) {
	if guest.Status == aggregate.GuestTemplate {
		return "", false
	}
	switch guest.VMID % 5 {
	case 0, 1:
		return "", false
	case 2:
		// Managed on paper, left where it is in practice.
		return proxmox.HAServiceDisabled, true
	case 3:
		if guest.Status == aggregate.GuestStopped {
			return proxmox.HAServiceStopped, true
		}
		return proxmox.HAServiceStarted, true
	default:
		return proxmox.HAServiceStarted, true
	}
}

func haStateOf(status aggregate.NodeStatus) string {
	switch status {
	case aggregate.NodeMaintenance:
		return proxmox.HANodeMaintenance
	case aggregate.NodeOffline:
		return proxmox.HANodeGone
	default:
		return proxmox.HANodeOnline
	}
}

func guestResourceStatus(status aggregate.GuestStatus) string {
	if status == aggregate.GuestRunning {
		return proxmox.StatusRunning
	}
	return proxmox.StatusStopped
}
