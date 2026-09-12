package detail

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
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
	ha := "actif"

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
		// append onto a nil slice yields nil when the source is empty, and the
		// model promises an array: a drained node must serialise as [].
		Guests: append(make([]aggregate.Guest, 0, len(found.node.Guests)), found.node.Guests...),
	}, nil
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

	if guest.Status == aggregate.GuestRunning {
		// Uptimes are staggered by VMID so the list does not look cloned.
		result.Uptime = int64(3600 * (24 + guest.VMID%72))
		host := guest.Memory.Used + uint64(guest.VMID%7+1)*64*1024*1024
		result.HostMemory = &host
		ha := "started"
		result.HAState = &ha
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
	return m.series(cluster, timeframe, cpuOf(found.node).Ratio, memoryOf(found.node), int64(len(found.node.Name)))
}

func (m *Mock) GuestSeries(ctx context.Context, cluster string, vmid int, timeframe string) (*Series, error) {
	found, err := m.findGuest(ctx, cluster, vmid)
	if err != nil {
		return nil, err
	}
	return m.series(cluster, timeframe, found.guest.CPU.Ratio, found.guest.Memory, int64(vmid))
}

func (m *Mock) Tasks(ctx context.Context, cluster string, limit int) (*Tasks, error) {
	overview, err := m.overviewOf(ctx, cluster)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 25
	}

	guests := allGuests(overview)
	if len(overview.Nodes) == 0 {
		// An unreachable cluster has no node to file a task against; an empty
		// journal is the truthful answer.
		return &Tasks{Cluster: cluster, FetchedAt: m.base, Entries: []Task{}}, nil
	}
	entries := make([]Task, 0, limit)
	for i := 0; i < limit; i++ {
		start := m.base.Add(-time.Duration(i*17+3) * time.Minute)
		kind, subject := taskShape(i, guests)
		node := overview.Nodes[i%len(overview.Nodes)].Name

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
		if i > 0 {
			seconds := int64(i%9 + 1)
			end := start.Add(time.Duration(seconds) * time.Second)
			ok := i%11 != 0
			task.End = &end
			task.Duration = &seconds
			task.OK = &ok
			if ok {
				task.Status = "OK"
			} else {
				task.Status = "storage 'nfs-shared' is not online"
			}
		} else {
			task.Status = "running"
		}
		entries = append(entries, task)
	}

	return &Tasks{Cluster: cluster, FetchedAt: m.base, Entries: entries}, nil
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
func (m *Mock) series(cluster, timeframe string, current float64, memory aggregate.Usage, seed int64) (*Series, error) {
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
		points = append(points, point)

		sum += ratio
		samples++
	}

	average := 0.0
	if samples > 0 {
		average = sum / float64(samples)
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
		return 0, 0, notFoundf("timeframe %q", timeframe)
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
	used := total / 5
	return aggregate.Usage{Used: used, Total: total, Ratio: float64(used) / float64(total)}
}

// diskFrom keeps the boot disk allocated but unmeasured, which is what Proxmox
// reports without a guest agent — the case the view must not draw as empty.
func diskFrom(guest aggregate.Guest) aggregate.Usage {
	total := guest.Memory.Total * 4
	if total == 0 {
		total = 32 * 1024 * 1024 * 1024
	}
	if guest.VMID%4 == 0 {
		used := total / 3
		return aggregate.Usage{Used: used, Total: total, Ratio: float64(used) / float64(total)}
	}
	return aggregate.Usage{Used: 0, Total: total, Ratio: 0}
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
