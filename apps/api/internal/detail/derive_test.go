package detail

import (
	"encoding/json"
	"math"
	"reflect"
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
	if node.Uptime == nil || *node.Uptime != 3600 || node.CPU.Cores != 32 || node.CPU.Ratio != 0.31 {
		t.Fatalf("uptime/cpu are %v/%+v", node.Uptime, node.CPU)
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
	if len(node.Updates) != 2 {
		t.Fatalf("got %d pending packages, want the 2 the count announces", len(node.Updates))
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
	if node.Updates != nil {
		// An empty array would claim the node is up to date, which nobody knows.
		t.Fatalf("updates is %v, want nil when the question could not be asked", node.Updates)
	}
	if node.Guests == nil {
		t.Fatal("guests is nil, want an empty slice")
	}
}

// TestDeriveNodeListsPendingPackages covers what the count alone cannot say:
// which packages are waiting, and in which order the table shows them.
func TestDeriveNodeListsPendingPackages(t *testing.T) {
	node := deriveNode(nodeInput{
		Cluster:   "production",
		Node:      "pve-1",
		FetchedAt: fetchedAt,
		Status:    &proxmox.NodeStatus{},
		ClusterStatus: []proxmox.ClusterStatusEntry{
			{Type: proxmox.ClusterStatusTypeNode, Name: "pve-1", Online: true},
		},
		// Deliberately out of order: apt walks its lists as it pleases.
		Updates: []proxmox.AptUpdate{
			{Package: "systemd", Version: "257.4-1", OldVersion: "257.3-1", Title: "system and service manager"},
			{Package: "proxmox-firewall", Version: "1.2.0"},
			{Package: "pve-manager", Version: "9.2.12", OldVersion: "9.2.11"},
		},
		UpdatesKnown: true,
	})

	want := []string{"proxmox-firewall", "pve-manager", "systemd"}
	got := make([]string, 0, len(node.Updates))
	for _, u := range node.Updates {
		got = append(got, u.Package)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packages are %v, want %v sorted by name", got, want)
	}
	if node.PendingUpdates == nil || *node.PendingUpdates != len(want) {
		t.Fatalf("pending updates is %v, want %d: the count and the list are one answer", node.PendingUpdates, len(want))
	}

	systemd := node.Updates[2]
	if systemd.OldVersion == nil || *systemd.OldVersion != "257.3-1" || systemd.Version != "257.4-1" {
		t.Fatalf("systemd versions are %v -> %q", systemd.OldVersion, systemd.Version)
	}
	if systemd.Title == nil || *systemd.Title != "system and service manager" {
		t.Fatalf("systemd title is %v", systemd.Title)
	}

	// A package apt would install for the first time reports no old version,
	// and an empty string is not a version.
	firewall := node.Updates[0]
	if firewall.OldVersion != nil {
		t.Fatalf("proxmox-firewall old version is %v, want nil", firewall.OldVersion)
	}
	if firewall.Title != nil {
		t.Fatalf("proxmox-firewall title is %v, want nil rather than an empty string", firewall.Title)
	}
}

// TestDeriveNodeUpToDateIsNotUnknown pins the difference the whole payload
// rests on: [] means nothing is pending, null means nobody could ask.
func TestDeriveNodeUpToDateIsNotUnknown(t *testing.T) {
	node := deriveNode(nodeInput{
		Cluster:      "production",
		Node:         "pve-1",
		FetchedAt:    fetchedAt,
		Status:       &proxmox.NodeStatus{},
		UpdatesKnown: true,
	})

	if node.Updates == nil {
		t.Fatal("updates is nil for an up-to-date node, want an empty slice")
	}
	if len(node.Updates) != 0 {
		t.Fatalf("updates is %v, want empty", node.Updates)
	}
	encoded, err := json.Marshal(node.Updates)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("an up-to-date node serialises its updates as %s, want []", encoded)
	}
}

// TestDeriveGuestHAState covers the three answers: a managed guest gets the
// CRM's word, a guest the CRM does not know gets nothing, and a cluster with no
// HA manager gets nothing either. The last two both render as the em dash, and
// both mean the same thing to an operator: nothing will move this on its own.
func TestDeriveGuestHAState(t *testing.T) {
	manager := &proxmox.HAManagerStatus{
		ServiceStatus: map[string]proxmox.HAServiceStatus{
			"vm:102": {Node: "pve-2", State: proxmox.HAServiceError},
			"ct:105": {Node: "pve-2", State: proxmox.HAServiceStarted},
		},
	}
	tests := []struct {
		name string
		ha   *proxmox.HAManagerStatus
		kind string
		vmid proxmox.FlexInt
		want *string
	}{
		{"managed vm", manager, proxmox.ResourceTypeQemu, 102, strPtr(proxmox.HAServiceError)},
		{"managed container", manager, proxmox.ResourceTypeLXC, 105, strPtr(proxmox.HAServiceStarted)},
		{"not an ha resource", manager, proxmox.ResourceTypeQemu, 999, nil},
		{"no ha manager", nil, proxmox.ResourceTypeQemu, 102, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveGuestHAState(guestInput{
				Resource: proxmox.Resource{Type: tc.kind, VMID: tc.vmid},
				HA:       tc.ha,
			})
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("haState = %q, want nil", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("haState = nil, want %q", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Fatalf("haState = %q, want %q", *got, *tc.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

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
		HA: &proxmox.HAManagerStatus{
			ServiceStatus: map[string]proxmox.HAServiceStatus{
				"vm:102": {Node: "pve-2", State: proxmox.HAServiceStarted},
			},
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
	// Disk usage is unknown without an agent, and unknown is nil: a zero could
	// not be told apart from a volume that is genuinely empty. The size is
	// still known, since it is the declared one.
	if guest.Disk.Used != nil || guest.Disk.Ratio != nil || guest.Disk.Total != 32<<30 {
		t.Fatalf("disk is %+v", guest.Disk)
	}
	if guest.HostMemory == nil || *guest.HostMemory != 6<<30 {
		t.Fatalf("host memory is %v", guest.HostMemory)
	}
	// The CRM's own word, not a constant of our own: "error" or "fence" is
	// what somebody opening this page during an incident needs to read.
	if guest.HAState == nil || *guest.HAState != proxmox.HAServiceStarted {
		t.Fatalf("ha state is %v, want the crm state", guest.HAState)
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
	if series.CPUAverage == nil || math.Abs(*series.CPUAverage-0.3) > 1e-9 {
		t.Fatalf("cpu average is %v, want the mean of the two known points", series.CPUAverage)
	}
	if series.Timeframe != proxmox.TimeframeHour || series.Cluster != "preproduction" {
		t.Fatalf("series is %+v", series)
	}
}

func TestDeriveSeriesWithoutASingleReading(t *testing.T) {
	series := deriveSeries("preproduction", proxmox.TimeframeDay, []proxmox.RRDPoint{{Time: 1}, {Time: 2}}, fetchedAt)

	// Nil, not zero: nothing was measured, so there is no average to state.
	// A zero announced an idle object over a window nobody could read.
	if series.CPUAverage != nil {
		t.Fatalf("cpu average is %v, want nil", series.CPUAverage)
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
	if running.Outcome != TaskOutcomeRunning {
		t.Fatalf("outcome of a running task is %q, want %q", running.Outcome, TaskOutcomeRunning)
	}
	if running.Warnings != nil {
		t.Fatalf("warnings of a running task is %v, want nil", running.Warnings)
	}

	done := tasks.Entries[1]
	if done.Duration == nil || *done.Duration != 60 {
		t.Fatalf("duration is %v, want 60 seconds", done.Duration)
	}
	if done.Outcome != TaskOutcomeOK {
		t.Fatalf("outcome of an OK task is %q, want %q", done.Outcome, TaskOutcomeOK)
	}
	if !done.Start.Equal(time.Unix(200, 0).UTC()) || done.End == nil || !done.End.Equal(time.Unix(260, 0).UTC()) {
		t.Fatalf("start/end are %v/%v", done.Start, done.End)
	}

	failed := tasks.Entries[2]
	if failed.Status != "start failed: got timeout" {
		t.Fatalf("status of a failed task is %q, want the word PVE used", failed.Status)
	}
	if failed.Outcome != TaskOutcomeFailed {
		t.Fatalf("outcome of a failed task is %q, want %q", failed.Outcome, TaskOutcomeFailed)
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
	if entry.Outcome != TaskOutcomeFailed {
		t.Fatalf("outcome is %q, want %q", entry.Outcome, TaskOutcomeFailed)
	}
}

// TestDeriveTaskWithWarnings: PVE ends a job that warned with "WARNINGS: <n>",
// which used to fall in the "anything but OK" bucket and be rendered as a
// failure. A nightly vzdump that warns about one guest out of ninety is not a
// backup that did not happen, and the operators who watch that line hardest
// were the ones being cried wolf at.
func TestDeriveTaskWithWarnings(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		wantWarnings *int
	}{
		{name: "with a count", status: "WARNINGS: 2", wantWarnings: intPtr(2)},
		{name: "one warning", status: "WARNINGS: 1", wantWarnings: intPtr(1)},
		// The outcome does not depend on the count being readable: the prefix
		// is what PVE uses to say the job ran and warned.
		{name: "no readable count", status: "WARNINGS: many", wantWarnings: nil},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tasks := deriveTasks("preproduction", []proxmox.Task{
				{UPID: "a", Type: "vzdump", ID: "101", StartTime: 100, EndTime: flexPtr(160), Status: tt.status},
			}, fetchedAt)

			entry := tasks.Entries[0]
			if entry.Outcome != TaskOutcomeWarnings {
				t.Fatalf("outcome = %q, want %q", entry.Outcome, TaskOutcomeWarnings)
			}
			if entry.Outcome == TaskOutcomeFailed {
				t.Fatal("a job that warned was filed as a failure")
			}
			// The raw string stays available for the tooltip.
			if entry.Status != tt.status {
				t.Errorf("status = %q, want the word PVE used", entry.Status)
			}
			switch {
			case tt.wantWarnings == nil && entry.Warnings != nil:
				t.Errorf("warnings = %d, want nil: the count could not be read", *entry.Warnings)
			case tt.wantWarnings != nil && (entry.Warnings == nil || *entry.Warnings != *tt.wantWarnings):
				t.Errorf("warnings = %v, want %d", entry.Warnings, *tt.wantWarnings)
			}
			// A duration is still a duration: the job ran to completion.
			if entry.Duration == nil || *entry.Duration != 60 {
				t.Errorf("duration = %v, want 60", entry.Duration)
			}
		})
	}
}

func intPtr(v int) *int { return &v }

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
	if series.CPUAverage == nil || math.Abs(*series.CPUAverage-0.4) > 1e-9 {
		t.Fatalf("cpu average is %v, want 0.4", series.CPUAverage)
	}
}

func TestDeriveClusterSeriesOfNothingCarriesAnArray(t *testing.T) {
	series := deriveClusterSeries("preproduction", proxmox.TimeframeHour, nil, fetchedAt)
	if series.Points == nil {
		t.Fatal("points is nil, want an empty array")
	}
	if series.CPUAverage != nil || series.Timeframe != proxmox.TimeframeHour {
		t.Fatalf("series is %+v", series)
	}
}

// TestDeriveGuestDiskReportedByAnAgent is the other half of the rule the main
// guest test pins: when a guest agent IS there, the used half is a real figure
// and carries a ratio with it. Only the absence of one is nil.
func TestDeriveGuestDiskReportedByAnAgent(t *testing.T) {
	guest := deriveGuest(guestInput{
		Cluster:  "preproduction",
		Resource: proxmox.Resource{Type: proxmox.ResourceTypeQemu, Node: "pve-2", VMID: 102, Name: "web"},
		Status: &proxmox.GuestStatus{
			Status:  proxmox.StatusRunning,
			Disk:    8 << 30,
			MaxDisk: 32 << 30,
		},
		FetchedAt: fetchedAt,
	})

	if guest.Disk.Used == nil || *guest.Disk.Used != 8<<30 {
		t.Fatalf("used is %v, want 8 GiB", guest.Disk.Used)
	}
	if guest.Disk.Ratio == nil || *guest.Disk.Ratio != 0.25 {
		t.Fatalf("ratio is %v, want 0.25", guest.Disk.Ratio)
	}
}

// TestDeriveGuestUptimeOnlyWhenRunning: a stopped guest reports an uptime of
// zero, which is not a duration. The header appends the uptime only when
// there is one, and it can only tell from a nil.
func TestDeriveGuestUptimeOnlyWhenRunning(t *testing.T) {
	stopped := deriveGuest(guestInput{
		Cluster:   "preproduction",
		Resource:  proxmox.Resource{Type: proxmox.ResourceTypeQemu, Node: "pve-2", VMID: 102, Name: "web"},
		Status:    &proxmox.GuestStatus{Status: proxmox.StatusStopped},
		FetchedAt: fetchedAt,
	})
	if stopped.Uptime != nil {
		t.Errorf("stopped uptime = %d, want nil", *stopped.Uptime)
	}

	running := deriveGuest(guestInput{
		Cluster:   "preproduction",
		Resource:  proxmox.Resource{Type: proxmox.ResourceTypeQemu, Node: "pve-2", VMID: 102, Name: "web"},
		Status:    &proxmox.GuestStatus{Status: proxmox.StatusRunning, Uptime: 7200},
		FetchedAt: fetchedAt,
	})
	if running.Uptime == nil || *running.Uptime != 7200 {
		t.Errorf("running uptime = %v, want 7200", running.Uptime)
	}
}

func TestDeriveGuestDisks(t *testing.T) {
	guest := deriveGuest(guestInput{
		Cluster:  "preproduction",
		Resource: proxmox.Resource{Type: proxmox.ResourceTypeQemu, Node: "pve-2", VMID: 102, Name: "web"},
		Status:   &proxmox.GuestStatus{Status: proxmox.StatusRunning, MaxDisk: 32 << 30},
		Config: proxmox.GuestConfig{
			"scsi0":    "ceph-vm:vm-102-disk-0,size=32G",
			"scsi1":    "ceph-vm:vm-102-disk-1,size=2T",
			"ide2":     "local:iso/debian-13.iso,media=cdrom",
			"efidisk0": "ceph-vm:vm-102-disk-2,size=528K",
			"unused0":  "local-lvm:vm-102-disk-9",
		},
		FetchedAt: fetchedAt,
	})

	// The boot disk stays what the status endpoint says: it is one line of
	// the list, not the volumetry of the guest.
	if guest.Disk.Total != 32<<30 {
		t.Fatalf("boot disk is %d, want the 32 GiB of the status endpoint", guest.Disk.Total)
	}
	if len(guest.Disks) != 4 {
		t.Fatalf("disks are %+v, want four volumes with the CD-ROM left out", guest.Disks)
	}
	if guest.Allocated == nil {
		t.Fatal("allocation is nil with a readable configuration")
	}
	want := uint64(32<<30) + uint64(2<<40) + uint64(528<<10)
	if guest.Allocated.Bytes != want {
		t.Errorf("allocated bytes = %d, want %d: the attached volumes, the detached one left out", guest.Allocated.Bytes, want)
	}
	if guest.Allocated.Partial {
		t.Error("allocation is partial, want whole: every attached volume declares a size")
	}
	if guest.Allocated.Detached != 1 {
		t.Errorf("detached count = %d, want the one unused volume", guest.Allocated.Detached)
	}
	if guest.Allocated.DetachedBytes != 0 {
		t.Errorf("detached bytes = %d, want zero: PVE records no size for an unused volume", guest.Allocated.DetachedBytes)
	}

	storage := guest.Disks[1].Storage
	if guest.Disks[1].Key != "scsi0" || storage == nil || *storage != "ceph-vm" {
		t.Errorf("second disk is %+v, want scsi0 on ceph-vm, efidisk0 sorting first", guest.Disks[1])
	}
}

func TestDeriveGuestDisksPartialTotal(t *testing.T) {
	guest := deriveGuest(guestInput{
		Cluster:  "preproduction",
		Resource: proxmox.Resource{Type: proxmox.ResourceTypeQemu, Node: "pve-2", VMID: 103, Name: "db"},
		Status:   &proxmox.GuestStatus{Status: proxmox.StatusRunning},
		Config: proxmox.GuestConfig{
			"scsi0": "ceph-vm:vm-103-disk-0,size=64G",
			// A device handed straight to the guest declares no size, so the
			// total above it is a floor rather than the whole truth.
			"scsi1": "/dev/disk/by-id/ata-SAMSUNG_MZ7LH1T9",
		},
		FetchedAt: fetchedAt,
	})

	if guest.Allocated == nil {
		t.Fatal("allocation is nil with a readable configuration")
	}
	if guest.Allocated.Bytes != 64<<30 {
		t.Errorf("allocated bytes = %d, want the 64 GiB that is known", guest.Allocated.Bytes)
	}
	if !guest.Allocated.Partial {
		t.Error("allocation is not partial, want partial: one attached volume has no known size")
	}
	if size := guest.Disks[1].Size; size != nil {
		t.Errorf("the sizeless volume reports %d, want nil — unknown is not zero", *size)
	}
}

func TestDeriveGuestDisksUnreadableConfiguration(t *testing.T) {
	// Without VM.Audit on the guest the configuration cannot be read. The
	// list is then nil — nobody could ask — and never an empty slice, which
	// would claim the guest declares no volume.
	guest := deriveGuest(guestInput{
		Cluster:   "preproduction",
		Resource:  proxmox.Resource{Type: proxmox.ResourceTypeLXC, Node: "pve-1", VMID: 101, Name: "dns"},
		Status:    &proxmox.GuestStatus{Status: proxmox.StatusRunning, MaxDisk: 8 << 30},
		FetchedAt: fetchedAt,
	})

	if guest.Disks != nil {
		t.Errorf("disks are %+v, want nil when the configuration could not be read", guest.Disks)
	}
	if guest.Allocated != nil {
		t.Errorf("allocation is %+v, want nil when the configuration could not be read", guest.Allocated)
	}
	if guest.Disk.Total != 8<<30 {
		t.Errorf("boot disk is %d, want the figure of the status endpoint to stand alone", guest.Disk.Total)
	}
}

func TestDeriveGuestDisksEmptyConfiguration(t *testing.T) {
	// A diskless guest, read successfully: an EMPTY list, which is not the
	// nil of an unreadable configuration.
	guest := deriveGuest(guestInput{
		Cluster:   "preproduction",
		Resource:  proxmox.Resource{Type: proxmox.ResourceTypeQemu, Node: "pve-1", VMID: 104, Name: "pxe"},
		Status:    &proxmox.GuestStatus{Status: proxmox.StatusRunning},
		Config:    proxmox.GuestConfig{"net0": "virtio=BC:24:11:00:00:01,bridge=vmbr0"},
		FetchedAt: fetchedAt,
	})

	if guest.Disks == nil || len(guest.Disks) != 0 {
		t.Errorf("disks are %+v, want an empty list for a guest that declares none", guest.Disks)
	}
	if guest.Allocated == nil || guest.Allocated.Bytes != 0 || guest.Allocated.Partial {
		t.Errorf("allocation is %+v, want a whole zero", guest.Allocated)
	}
}
