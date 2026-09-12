package detail

import (
	"math"
	"testing"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

var fetchedAt = time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)

func flexPtr(v int64) *proxmox.FlexInt {
	f := proxmox.FlexInt(v)
	return &f
}

func floatPtr(v float64) *float64 { return &v }

func uintPtr(v uint64) *uint64 { return &v }

func TestDeriveLoadAverage(t *testing.T) {
	tests := []struct {
		name string
		in   proxmox.LoadAvg
		want *[3]float64
	}{
		{
			name: "three readings",
			in:   proxmox.LoadAvg{0.53, 1.25, 0.87},
			want: &[3]float64{0.53, 1.25, 0.87},
		},
		{
			name: "an idle node really does report zeroes",
			in:   proxmox.LoadAvg{0, 0, 0},
			want: &[3]float64{0, 0, 0},
		},
		{
			name: "a negative figure is not a load average",
			in:   proxmox.LoadAvg{0.5, -1, 0.5},
			want: nil,
		},
		{
			name: "an unreadable figure yields nothing, not zeroes",
			in:   proxmox.LoadAvg{proxmox.FlexFloat(math.NaN()), 0.5, 0.5},
			want: nil,
		},
		{
			name: "an infinite figure yields nothing",
			in:   proxmox.LoadAvg{proxmox.FlexFloat(math.Inf(1)), 0.5, 0.5},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveLoadAverage(tt.in)
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("got %v, want nil", *got)
			case tt.want == nil:
				return
			case got == nil:
				t.Fatalf("got nil, want %v", *tt.want)
			case *got != *tt.want:
				t.Fatalf("got %v, want %v", *got, *tt.want)
			}
		})
	}
}

func TestDeriveNodeStatus(t *testing.T) {
	entries := []proxmox.ClusterStatusEntry{
		{Type: proxmox.ClusterStatusTypeNode, Name: "up", Online: true},
		{Type: proxmox.ClusterStatusTypeNode, Name: "down", Online: false},
		{Type: proxmox.ClusterStatusTypeNode, Name: "draining", Online: true},
	}
	ha := &proxmox.HAManagerStatus{NodeStatus: map[string]string{
		"draining": proxmox.HANodeMaintenance,
		"up":       proxmox.HANodeOnline,
	}}

	tests := []struct {
		name string
		node string
		ha   *proxmox.HAManagerStatus
		want aggregate.NodeStatus
	}{
		{name: "online", node: "up", ha: ha, want: aggregate.NodeOnline},
		{name: "offline", node: "down", ha: ha, want: aggregate.NodeOffline},
		{name: "maintenance wins over the online it still reports", node: "draining", ha: ha, want: aggregate.NodeMaintenance},
		{name: "without the ha manager a draining node is only online", node: "draining", ha: nil, want: aggregate.NodeOnline},
		{name: "absent from both sources", node: "ghost", ha: ha, want: aggregate.NodeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveNodeStatus(tt.node, entries, tt.ha); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeriveNodeRatioOfAnEmptyTotalIsZeroNotNaN(t *testing.T) {
	node := deriveNode(nodeInput{
		Cluster:   "preproduction",
		Node:      "pve-1",
		FetchedAt: fetchedAt,
		Status: &proxmox.NodeStatus{
			Memory: proxmox.Usage{Used: 8 << 30, Total: 16 << 30},
			// A node with no swap reports a total of zero, which is not an
			// error and must not produce a division by zero.
			Swap:   proxmox.Usage{Used: 0, Total: 0},
			RootFS: proxmox.Usage{Used: 0, Total: 0},
		},
	})

	for name, u := range map[string]aggregate.Usage{"swap": node.Swap, "rootfs": node.RootFS} {
		if math.IsNaN(u.Ratio) || math.IsInf(u.Ratio, 0) {
			// encoding/json refuses NaN outright: the whole response would fail.
			t.Fatalf("%s ratio is %v, want 0", name, u.Ratio)
		}
		if u.Ratio != 0 {
			t.Fatalf("%s ratio is %v, want 0", name, u.Ratio)
		}
	}
	if node.Memory.Ratio != 0.5 {
		t.Fatalf("memory ratio is %v, want 0.5", node.Memory.Ratio)
	}
}

func TestDeriveNodeGathersWhatTheNodeReports(t *testing.T) {
	pending := []proxmox.AptUpdate{{Package: "pve-manager"}, {Package: "libc6"}}
	node := deriveNode(nodeInput{
		Cluster:   "preproduction",
		Node:      "pve-1",
		FetchedAt: fetchedAt,
		Status: &proxmox.NodeStatus{
			Uptime:     3600,
			CPU:        0.31,
			CPUInfo:    proxmox.NodeCPUInfo{CPUs: 32},
			Memory:     proxmox.Usage{Used: 4, Total: 8},
			LoadAvg:    proxmox.LoadAvg{1, 2, 3},
			PVEVersion: "pve-manager/9.2.9/abc",
			KVersion:   "Linux 6.14.8-2-pve",
		},
		ClusterStatus: []proxmox.ClusterStatusEntry{
			{Type: proxmox.ClusterStatusTypeCluster, Name: "pprd", Nodes: 2, Quorate: true},
			{Type: proxmox.ClusterStatusTypeNode, Name: "pve-1", Online: true},
			{Type: proxmox.ClusterStatusTypeNode, Name: "pve-2", Online: false},
		},
		Resources: []proxmox.Resource{
			{Type: proxmox.ResourceTypeQemu, Node: "pve-1", VMID: 102, Name: "web", Status: proxmox.StatusRunning, Tags: "prod;web"},
			{Type: proxmox.ResourceTypeLXC, Node: "pve-1", VMID: 101, Name: "dns", Status: proxmox.StatusStopped},
			{Type: proxmox.ResourceTypeQemu, Node: "pve-2", VMID: 200, Name: "elsewhere", Status: proxmox.StatusRunning},
		},
		HA:           &proxmox.HAManagerStatus{NodeStatus: map[string]string{"pve-1": proxmox.HANodeOnline}},
		Updates:      pending,
		UpdatesKnown: true,
	})

	if node.Status != aggregate.NodeOnline {
		t.Fatalf("status is %q, want online", node.Status)
	}
	if node.Uptime != 3600 || node.CPU.Cores != 32 || node.CPU.Ratio != 0.31 {
		t.Fatalf("uptime/cpu are %d/%+v", node.Uptime, node.CPU)
	}
	if node.PVEVersion == nil || *node.PVEVersion != "pve-manager/9.2.9/abc" {
		t.Fatalf("pve version is %v", node.PVEVersion)
	}
	if node.KernelVer == nil || *node.KernelVer != "Linux 6.14.8-2-pve" {
		t.Fatalf("kernel version is %v", node.KernelVer)
	}
	if node.Quorum == nil || !node.Quorum.Quorate || node.Quorum.Nodes != 2 || node.Quorum.Online != 1 {
		t.Fatalf("quorum is %+v", node.Quorum)
	}
	if node.HAState == nil || *node.HAState != proxmox.HANodeOnline {
		t.Fatalf("ha state is %v", node.HAState)
	}
	if node.PendingUpdates == nil || *node.PendingUpdates != 2 {
		t.Fatalf("pending updates is %v, want 2", node.PendingUpdates)
	}
	if len(node.Guests) != 2 {
		t.Fatalf("got %d guests, want the 2 hosted here", len(node.Guests))
	}
	if node.Guests[0].VMID != 101 || node.Guests[1].VMID != 102 {
		t.Fatalf("guests are not sorted by vmid: %+v", node.Guests)
	}
	if node.Guests[0].Kind != aggregate.GuestLXC || node.Guests[1].Kind != aggregate.GuestQemu {
		t.Fatalf("guest kinds are %q and %q", node.Guests[0].Kind, node.Guests[1].Kind)
	}
	if got := node.Guests[0].Tags; got == nil || len(got) != 0 {
		t.Fatalf("tags of an untagged guest are %v, want an empty slice", got)
	}
}

func TestDeriveNodeWithoutAnythingOptional(t *testing.T) {
	node := deriveNode(nodeInput{
		Cluster:   "standalone",
		Node:      "pve-1",
		FetchedAt: fetchedAt,
		Status:    &proxmox.NodeStatus{},
		ClusterStatus: []proxmox.ClusterStatusEntry{
			{Type: proxmox.ClusterStatusTypeNode, Name: "pve-1", Online: true},
		},
	})

	if node.Quorum != nil {
		// A standalone node has no cluster entry: its quorum does not exist
		// rather than being lost.
		t.Fatalf("quorum is %+v, want nil", node.Quorum)
	}
	if node.HAState != nil {
		t.Fatalf("ha state is %v, want nil", node.HAState)
	}
	if node.PendingUpdates != nil {
		t.Fatalf("pending updates is %v, want nil when the question could not be asked", node.PendingUpdates)
	}
	if node.Guests == nil {
		t.Fatal("guests is nil, want an empty slice")
	}
}

func TestDeriveGuest(t *testing.T) {
	address := "10.18.160.4"
	guest := deriveGuest(guestInput{
		Cluster: "preproduction",
		Resource: proxmox.Resource{
			Type: proxmox.ResourceTypeQemu, Node: "pve-2", VMID: 102, Name: "web", Tags: "prod;web",
		},
		Status: &proxmox.GuestStatus{
			Status:  proxmox.StatusRunning,
			Name:    "web",
			Uptime:  7200,
			CPU:     0.12,
			CPUs:    4,
			Mem:     2 << 30,
			MaxMem:  4 << 30,
			MaxDisk: 32 << 30,
			Balloon: 6 << 30,
			HA:      proxmox.GuestHA{Managed: true},
		},
		IPv4:      &address,
		FetchedAt: fetchedAt,
	})

	if guest.Node != "pve-2" {
		t.Fatalf("node is %q, want the one hosting it now", guest.Node)
	}
	if guest.Status != aggregate.GuestRunning || guest.Kind != aggregate.GuestQemu {
		t.Fatalf("status/kind are %q/%q", guest.Status, guest.Kind)
	}
	if guest.Memory.Ratio != 0.5 {
		t.Fatalf("memory ratio is %v, want 0.5", guest.Memory.Ratio)
	}
	if guest.Disk.Ratio != 0 || guest.Disk.Total != 32<<30 {
		// Disk usage is unknown without an agent; the size is still known.
		t.Fatalf("disk is %+v", guest.Disk)
	}
	if guest.HostMemory == nil || *guest.HostMemory != 6<<30 {
		t.Fatalf("host memory is %v", guest.HostMemory)
	}
	if guest.HAState == nil || *guest.HAState != haStateManaged {
		t.Fatalf("ha state is %v, want a managed guest to have one", guest.HAState)
	}
	if guest.IPv4 == nil || *guest.IPv4 != address {
		t.Fatalf("ipv4 is %v", guest.IPv4)
	}
	if len(guest.Tags) != 2 || guest.Tags[0] != "prod" {
		t.Fatalf("tags are %v", guest.Tags)
	}
}

func TestDeriveGuestLeavesTheUnknownUnknown(t *testing.T) {
	guest := deriveGuest(guestInput{
		Cluster:   "preproduction",
		Resource:  proxmox.Resource{Type: proxmox.ResourceTypeLXC, Node: "pve-1", VMID: 101, Name: "dns"},
		Status:    &proxmox.GuestStatus{Status: proxmox.StatusStopped},
		FetchedAt: fetchedAt,
	})

	if guest.IPv4 != nil {
		t.Fatalf("ipv4 is %v, want nil without an agent", guest.IPv4)
	}
	if guest.HAState != nil {
		t.Fatalf("ha state is %v, want nil for an unmanaged guest", guest.HAState)
	}
	if guest.HostMemory != nil {
		t.Fatalf("host memory is %v, want nil when ballooning says nothing", guest.HostMemory)
	}
	if guest.Tags == nil {
		t.Fatal("tags is nil, want an empty slice")
	}
	if guest.Name != "dns" {
		t.Fatalf("name is %q, want the one from the listing when the status omits it", guest.Name)
	}
}

func TestDeriveGuestTemplateIsNotStopped(t *testing.T) {
	guest := deriveGuest(guestInput{
		Resource: proxmox.Resource{Type: proxmox.ResourceTypeQemu, VMID: 9000},
		Status:   &proxmox.GuestStatus{Status: proxmox.StatusStopped, Template: true},
	})
	if guest.Status != aggregate.GuestTemplate {
		t.Fatalf("status is %q, want template", guest.Status)
	}
}

func TestDeriveSeriesKeepsHolesAndAveragesWhatIsKnown(t *testing.T) {
	raw := []proxmox.RRDPoint{
		{Time: 100, CPU: floatPtr(0.2), MemUsed: uintPtr(2), MemTotal: uintPtr(8), NetIn: uintPtr(10)},
		// The node was down for this step: RRD simply omits the columns.
		{Time: 200},
		{Time: 300, CPU: floatPtr(0.4), Mem: uintPtr(4), MaxMem: uintPtr(8), NetOut: uintPtr(20)},
	}

	series := deriveSeries("preproduction", proxmox.TimeframeHour, raw, fetchedAt)

	if len(series.Points) != 3 {
		t.Fatalf("got %d points, want 3: a hole is a point without values, not a missing point", len(series.Points))
	}
	if series.Points[1].CPU != nil || series.Points[1].MemUsed != nil || series.Points[1].NetIn != nil {
		t.Fatalf("the hole carries values: %+v", series.Points[1])
	}
	if !series.Points[1].Time.Equal(time.Unix(200, 0).UTC()) {
		t.Fatalf("the hole is at %v", series.Points[1].Time)
	}
	// The guest columns are named mem/maxmem and the node ones memused/memtotal.
	if series.Points[2].MemUsed == nil || *series.Points[2].MemUsed != 4 {
		t.Fatalf("mem of the guest-shaped sample is %v", series.Points[2].MemUsed)
	}
	if series.Points[2].MemTotal == nil || *series.Points[2].MemTotal != 8 {
		t.Fatalf("maxmem of the guest-shaped sample is %v", series.Points[2].MemTotal)
	}
	// 0.2 and 0.4 over TWO points, not three: counting the hole as zero would
	// report 0.2 and invent an idleness nobody measured.
	if math.Abs(series.CPUAverage-0.3) > 1e-9 {
		t.Fatalf("cpu average is %v, want the mean of the two known points", series.CPUAverage)
	}
	if series.Timeframe != proxmox.TimeframeHour || series.Cluster != "preproduction" {
		t.Fatalf("series is %+v", series)
	}
}

func TestDeriveSeriesWithoutASingleReading(t *testing.T) {
	series := deriveSeries("preproduction", proxmox.TimeframeDay, []proxmox.RRDPoint{{Time: 1}, {Time: 2}}, fetchedAt)

	if series.CPUAverage != 0 {
		t.Fatalf("cpu average is %v, want 0", series.CPUAverage)
	}
	if len(series.Points) != 2 {
		t.Fatalf("got %d points, want the 2 empty ones: the chart shows its gaps", len(series.Points))
	}
}

func TestDeriveSeriesOfNothingCarriesAnArray(t *testing.T) {
	series := deriveSeries("preproduction", proxmox.TimeframeYear, nil, fetchedAt)
	if series.Points == nil {
		t.Fatal("points is nil, want an empty slice")
	}
}

func TestDeriveTasks(t *testing.T) {
	raw := []proxmox.Task{
		{UPID: "b", Node: "pve-1", Type: "vzdump", ID: "101", User: "root@pam", StartTime: 200, EndTime: flexPtr(260), Status: proxmox.TaskStatusOK},
		{UPID: "a", Node: "pve-2", Type: "qmstart", ID: "102", User: "ops@pve", StartTime: 100, EndTime: flexPtr(103), Status: "start failed: got timeout"},
		{UPID: "c", Node: "pve-1", Type: "migrateall", ID: "", User: "root@pam", StartTime: 300},
	}

	tasks := deriveTasks("preproduction", raw, fetchedAt)

	if len(tasks.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(tasks.Entries))
	}
	if got := []string{tasks.Entries[0].UPID, tasks.Entries[1].UPID, tasks.Entries[2].UPID}; got[0] != "c" || got[1] != "b" || got[2] != "a" {
		t.Fatalf("entries are ordered %v, want the most recent first", got)
	}

	running := tasks.Entries[0]
	if running.Status != taskStatusRunning {
		t.Fatalf("status of a running task is %q, want %q", running.Status, taskStatusRunning)
	}
	if running.End != nil {
		t.Fatalf("end of a running task is %v, want nil", running.End)
	}
	if running.Duration != nil {
		t.Fatalf("duration of a running task is %v, want nil rather than a 0 that claims it took no time", running.Duration)
	}
	if running.OK != nil {
		t.Fatalf("outcome of a running task is %v, want nil", running.OK)
	}

	done := tasks.Entries[1]
	if done.Duration == nil || *done.Duration != 60 {
		t.Fatalf("duration is %v, want 60 seconds", done.Duration)
	}
	if done.OK == nil || !*done.OK {
		t.Fatalf("outcome of an OK task is %v, want true", done.OK)
	}
	if !done.Start.Equal(time.Unix(200, 0).UTC()) || done.End == nil || !done.End.Equal(time.Unix(260, 0).UTC()) {
		t.Fatalf("start/end are %v/%v", done.Start, done.End)
	}

	failed := tasks.Entries[2]
	if failed.Status != "start failed: got timeout" {
		t.Fatalf("status of a failed task is %q, want the word PVE used", failed.Status)
	}
	if failed.OK == nil || *failed.OK {
		t.Fatalf("outcome of a failed task is %v, want false", failed.OK)
	}
	if failed.Duration == nil || *failed.Duration != 3 {
		t.Fatalf("duration is %v, want 3 seconds", failed.Duration)
	}
}

func TestDeriveTaskOfAnUnknownOutcome(t *testing.T) {
	tasks := deriveTasks("preproduction", []proxmox.Task{
		{UPID: "a", StartTime: 100, EndTime: flexPtr(120)},
	}, fetchedAt)

	entry := tasks.Entries[0]
	if entry.Status != taskStatusUnknown {
		t.Fatalf("status is %q, want %q rather than a silent success", entry.Status, taskStatusUnknown)
	}
	if entry.OK == nil || *entry.OK {
		t.Fatalf("outcome is %v, want false", entry.OK)
	}
}

func TestDeriveTaskClampsClockSkew(t *testing.T) {
	tasks := deriveTasks("preproduction", []proxmox.Task{
		// The two timestamps come from two nodes whose clocks disagree.
		{UPID: "a", StartTime: 500, EndTime: flexPtr(498), Status: proxmox.TaskStatusOK},
	}, fetchedAt)

	if d := tasks.Entries[0].Duration; d == nil || *d != 0 {
		t.Fatalf("duration is %v, want 0", d)
	}
}

func TestDeriveTasksOfAnEmptyLogCarriesAnArray(t *testing.T) {
	tasks := deriveTasks("preproduction", nil, fetchedAt)
	if tasks.Entries == nil {
		t.Fatal("entries is nil, want an empty slice")
	}
}

func TestDeriveClusterSeriesWeightsCPUByCores(t *testing.T) {
	// A small node at full load and a large one at rest: averaging the two
	// fractions would report 50 %, when the cluster is really spending 4 of
	// its 36 cores.
	small := []proxmox.RRDPoint{{Time: 100, CPU: floatPtr(1), MaxCPU: floatPtr(4), MemUsed: uintPtr(3), MemTotal: uintPtr(8)}}
	large := []proxmox.RRDPoint{{Time: 100, CPU: floatPtr(0), MaxCPU: floatPtr(32), MemUsed: uintPtr(1), MemTotal: uintPtr(64)}}

	series := deriveClusterSeries("preproduction", proxmox.TimeframeHour, [][]proxmox.RRDPoint{small, large}, fetchedAt)

	if len(series.Points) != 1 {
		t.Fatalf("got %d points, want the single shared step", len(series.Points))
	}
	got := series.Points[0]
	if got.CPU == nil || math.Abs(*got.CPU-4.0/36.0) > 1e-9 {
		t.Fatalf("cpu is %v, want the core-weighted mean 4/36", got.CPU)
	}
	if got.MemUsed == nil || *got.MemUsed != 4 || got.MemTotal == nil || *got.MemTotal != 72 {
		t.Fatalf("memory is %v/%v, want the sum of both nodes", got.MemUsed, got.MemTotal)
	}
	if got.NetIn != nil || got.NetOut != nil {
		t.Fatalf("network is %v/%v, want nil: the card draws no network line", got.NetIn, got.NetOut)
	}
}

func TestDeriveClusterSeriesMatchesStepsOnTheirTimestamp(t *testing.T) {
	// The second node joined an hour in and has fewer samples: pairing the
	// i-th sample of the two would add readings taken minutes apart.
	older := []proxmox.RRDPoint{
		{Time: 100, CPU: floatPtr(0.5), MaxCPU: floatPtr(2)},
		{Time: 200, CPU: floatPtr(0.5), MaxCPU: floatPtr(2)},
	}
	younger := []proxmox.RRDPoint{
		{Time: 200, CPU: floatPtr(0.1), MaxCPU: floatPtr(2)},
	}

	series := deriveClusterSeries("preproduction", proxmox.TimeframeHour, [][]proxmox.RRDPoint{older, younger}, fetchedAt)

	if len(series.Points) != 2 {
		t.Fatalf("got %d points, want the union of the two grids", len(series.Points))
	}
	if !series.Points[0].Time.Equal(time.Unix(100, 0).UTC()) || !series.Points[1].Time.Equal(time.Unix(200, 0).UTC()) {
		t.Fatalf("points are out of order: %v then %v", series.Points[0].Time, series.Points[1].Time)
	}
	if series.Points[0].CPU == nil || math.Abs(*series.Points[0].CPU-0.5) > 1e-9 {
		t.Fatalf("the first step is %v, want the only node that was there", series.Points[0].CPU)
	}
	if series.Points[1].CPU == nil || math.Abs(*series.Points[1].CPU-0.3) > 1e-9 {
		t.Fatalf("the shared step is %v, want the mean of both nodes", series.Points[1].CPU)
	}
}

func TestDeriveClusterSeriesKeepsAStepNoNodeMeasuredAsAHole(t *testing.T) {
	// Both nodes were down at 200, and one of them reports no maxcpu at 300:
	// without a weight its reading cannot enter a weighted mean.
	first := []proxmox.RRDPoint{
		{Time: 100, CPU: floatPtr(0.4), MaxCPU: floatPtr(8), MemUsed: uintPtr(2), MemTotal: uintPtr(8)},
		{Time: 200},
		{Time: 300, CPU: floatPtr(0.9)},
	}
	second := []proxmox.RRDPoint{{Time: 200}, {Time: 300}}

	series := deriveClusterSeries("preproduction", proxmox.TimeframeHour, [][]proxmox.RRDPoint{first, second}, fetchedAt)

	if len(series.Points) != 3 {
		t.Fatalf("got %d points, want 3: a hole is a point without values", len(series.Points))
	}
	if series.Points[1].CPU != nil || series.Points[1].MemUsed != nil {
		t.Fatalf("the hole carries values: %+v", series.Points[1])
	}
	if series.Points[2].CPU != nil {
		t.Fatalf("a reading without maxcpu is %v, want nil: it has no weight", series.Points[2].CPU)
	}
	// The average runs over the one step that was measured.
	if math.Abs(series.CPUAverage-0.4) > 1e-9 {
		t.Fatalf("cpu average is %v, want 0.4", series.CPUAverage)
	}
}

func TestDeriveClusterSeriesOfNothingCarriesAnArray(t *testing.T) {
	series := deriveClusterSeries("preproduction", proxmox.TimeframeHour, nil, fetchedAt)
	if series.Points == nil {
		t.Fatal("points is nil, want an empty array")
	}
	if series.CPUAverage != 0 || series.Timeframe != proxmox.TimeframeHour {
		t.Fatalf("series is %+v", series)
	}
}
