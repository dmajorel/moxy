package aggregate

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

const testThreshold = 0.80

var testIdentity = Identity{ID: "preproduction", Name: "Preproduction"}

// gib is the unit the fixtures are written in: sizes travel as raw bytes.
const gib = 1 << 30

func nodeRes(node string, cpu float64, cores, mem, maxMem, uptime int64) proxmox.Resource {
	return proxmox.Resource{
		Type:   proxmox.ResourceTypeNode,
		ID:     "node/" + node,
		Node:   node,
		Status: proxmox.StatusOnline,
		CPU:    proxmox.FlexFloat(cpu),
		MaxCPU: proxmox.FlexInt(cores),
		Mem:    proxmox.FlexInt(mem),
		MaxMem: proxmox.FlexInt(maxMem),
		Uptime: proxmox.FlexInt(uptime),
	}
}

func guestRes(kind, node, name, status string, template bool) proxmox.Resource {
	return proxmox.Resource{
		Type:     kind,
		ID:       kind + "/" + name,
		Node:     node,
		Name:     name,
		Status:   status,
		Template: proxmox.FlexBool(template),
	}
}

func storageRes(node, name, status string, shared bool, disk, maxDisk int64) proxmox.Resource {
	return proxmox.Resource{
		Type:    proxmox.ResourceTypeStorage,
		ID:      "storage/" + node + "/" + name,
		Node:    node,
		Storage: name,
		Status:  status,
		Shared:  proxmox.FlexBool(shared),
		Disk:    proxmox.FlexInt(disk),
		MaxDisk: proxmox.FlexInt(maxDisk),
		Content: "images,rootdir",
	}
}

func statusNode(name string, online bool) proxmox.ClusterStatusEntry {
	return proxmox.ClusterStatusEntry{
		Type:   proxmox.ClusterStatusTypeNode,
		ID:     "node/" + name,
		Name:   name,
		Online: proxmox.FlexBool(online),
	}
}

func statusCluster(nodes int64, quorate bool) proxmox.ClusterStatusEntry {
	return proxmox.ClusterStatusEntry{
		Type:    proxmox.ClusterStatusTypeCluster,
		ID:      "cluster",
		Name:    "preproduction",
		Nodes:   proxmox.FlexInt(nodes),
		Quorate: proxmox.FlexBool(quorate),
	}
}

func haStatus(states map[string]string) *proxmox.HAManagerStatus {
	return &proxmox.HAManagerStatus{NodeStatus: states, ManagerStatus: "master"}
}

func nodeByName(t *testing.T, c ClusterOverview, name string) Node {
	t.Helper()
	for _, n := range c.Nodes {
		if n.Name == name {
			return n
		}
	}
	t.Fatalf("node %q missing from %+v", name, c.Nodes)
	return Node{}
}

func alertByKind(c ClusterOverview, kind AlertKind) (Alert, bool) {
	for _, a := range c.Alerts {
		if a.Kind == kind {
			return a, true
		}
	}
	return Alert{}, false
}

func alertKinds(c ClusterOverview) []AlertKind {
	kinds := make([]AlertKind, 0, len(c.Alerts))
	for _, a := range c.Alerts {
		kinds = append(kinds, a.Kind)
	}
	return kinds
}

func nodeNames(c ClusterOverview) []string {
	names := make([]string, 0, len(c.Nodes))
	for _, n := range c.Nodes {
		names = append(names, n.Name)
	}
	return names
}

func closeTo(got, want float64) bool { return math.Abs(got-want) < 1e-9 }

// TestDeriveCPUIsWeightedByCoreCount is the case a plain mean gets wrong: two
// nodes of very different sizes, the small one idle and the big one busy.
func TestDeriveCPUIsWeightedByCoreCount(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("small", 0.10, 8, 4*gib, 32*gib, 1000),
			nodeRes("big", 0.90, 64, 200*gib, 256*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(2, true),
			statusNode("small", true),
			statusNode("big", true),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	// (0.10*8 + 0.90*64) / 72 = 58.4 / 72
	want := 58.4 / 72
	if !closeTo(c.CPU.Ratio, want) {
		t.Errorf("cpu ratio = %v, want %v", c.CPU.Ratio, want)
	}
	// The mean of the fractions is 0.50: half the load the cluster really has.
	if closeTo(c.CPU.Ratio, 0.50) {
		t.Errorf("cpu ratio = %v, which is the unweighted mean, not the weighted one", c.CPU.Ratio)
	}
	if c.CPU.Cores != 72 {
		t.Errorf("cores = %d, want 72", c.CPU.Cores)
	}
}

func TestDeriveSharedStorageCountedOnce(t *testing.T) {
	const (
		sharedUsed  = 4 * gib
		sharedTotal = 16 * gib
		localUsed   = 1 * gib
		localTotal  = 4 * gib
	)
	var resources []proxmox.Resource
	for _, node := range []string{"n1", "n2", "n3"} {
		// The same shared storage, reported once by every node.
		resources = append(resources,
			storageRes(node, "nfs-shared", proxmox.StatusAvailable, true, sharedUsed, sharedTotal),
			storageRes(node, "local-lvm", proxmox.StatusAvailable, false, localUsed, localTotal),
		)
	}
	data := ClusterData{Resources: resources}

	c := Derive(testIdentity, data, testThreshold)

	// The shared storage counts once; the local ones belong to the node
	// screen and stay out of the cluster figure.
	if c.Storage.Used != sharedUsed || c.Storage.Total != sharedTotal {
		t.Errorf("storage = %d/%d, want %d/%d (the shared storage must count once, local ones not at all)",
			c.Storage.Used, c.Storage.Total, sharedUsed, sharedTotal)
	}
}

// TestDeriveLocalStorageIsTheFallback covers the standalone node: with no
// shared storage at all, its local guest storages are the capacity there is.
func TestDeriveLocalStorageIsTheFallback(t *testing.T) {
	data := ClusterData{Resources: []proxmox.Resource{
		storageRes("n1", "local-lvm", proxmox.StatusAvailable, false, 1*gib, 4*gib),
		storageRes("n2", "local-lvm", proxmox.StatusAvailable, false, 2*gib, 4*gib),
	}}

	c := Derive(testIdentity, data, testThreshold)

	if c.Storage.Used != 3*gib || c.Storage.Total != 8*gib {
		t.Errorf("storage = %d/%d, want %d/%d", c.Storage.Used, c.Storage.Total, 3*gib, 8*gib)
	}
}

// TestDeriveStorageWithoutGuestDisksExcluded: a shared backup target is not
// room for VMs, however large.
func TestDeriveStorageWithoutGuestDisksExcluded(t *testing.T) {
	backups := storageRes("n1", "pbs", proxmox.StatusAvailable, true, 10*gib, 100*gib)
	backups.Content = "backup"
	backups.Plugintype = "pbs"
	data := ClusterData{Resources: []proxmox.Resource{
		backups,
		storageRes("n1", "nfs-vm", proxmox.StatusAvailable, true, 2*gib, 8*gib),
	}}

	c := Derive(testIdentity, data, testThreshold)

	if c.Storage.Used != 2*gib || c.Storage.Total != 8*gib {
		t.Errorf("storage = %d/%d, want %d/%d (backup-only storage must not count)",
			c.Storage.Used, c.Storage.Total, 2*gib, 8*gib)
	}
}

// TestDeriveCephStoragesAreOneBackend runs the storage rows captured on the
// six-node qualification cluster: three RBD pools and four CephFS mounts, all
// on one Ceph cluster and all reporting the same free space, plus local-lvm
// and local on every node. Summing them showed 262 TiB for a 37 TiB Ceph.
func TestDeriveCephStoragesAreOneBackend(t *testing.T) {
	data := ClusterData{
		Resources: fixture[[]proxmox.Resource](t, "cluster_resources_ceph.json"),
	}

	c := Derive(testIdentity, data, testThreshold)

	// The free space every Ceph storage reports (maxdisk - disk), identical
	// across the seven of them, plus what each distinct pool has stored: the
	// three RBD pools and the one CephFS the four mounts share.
	const avail = 40766311545569 - 309985
	const used = 309985 + 224846 + 24789497727 + 39933968384
	if c.Storage.Used != used {
		t.Errorf("storage used = %d, want %d", c.Storage.Used, uint64(used))
	}
	if c.Storage.Total != used+avail {
		t.Errorf("storage total = %d, want %d (one Ceph backend, no local storage)",
			c.Storage.Total, uint64(used+avail))
	}
	if c.Storage.Total > 41*1000*1000*1000*1000 {
		t.Errorf("storage total = %d: the Ceph capacity is still being multiplied", c.Storage.Total)
	}
}

// TestDeriveTwoCephClustersAreTwoBackends covers the RBD pool backed by an
// external Ceph, next to the cluster's own: a shared Ceph reports a different
// free space, and folding the two into one backend kept the smaller of the two
// capacities while still summing what both had stored.
func TestDeriveTwoCephClustersAreTwoBackends(t *testing.T) {
	local := storageRes("n1", "rbd-local", proxmox.StatusAvailable, true, 10*gib, 100*gib)
	local.Plugintype = proxmox.PluginRBD
	fs := storageRes("n1", "cephfs-local", proxmox.StatusAvailable, true, 5*gib, 95*gib)
	fs.Plugintype = proxmox.PluginCephFS
	external := storageRes("n1", "rbd-external", proxmox.StatusAvailable, true, 20*gib, 420*gib)
	external.Plugintype = proxmox.PluginRBD
	data := ClusterData{Resources: []proxmox.Resource{local, fs, external}}

	c := Derive(testIdentity, data, testThreshold)

	// rbd-local and cephfs-local both report 90 GiB free: one Ceph, counted
	// once, holding 15 GiB. rbd-external reports 400 GiB free and 20 GiB
	// stored, on a Ceph of its own.
	const used = 35 * gib
	const total = used + 90*gib + 400*gib
	if c.Storage.Used != used || c.Storage.Total != total {
		t.Errorf("storage = %d/%d, want %d/%d (two Ceph clusters are two backends)",
			c.Storage.Used, c.Storage.Total, uint64(used), uint64(total))
	}
}

// TestDeriveSharedStorageWithoutSizeFallsBackToLocal: an iSCSI target exposed
// directly accepts images but reports no size, and used to claim the whole
// cluster figure as a shared backend of 0 bytes, hiding the local-lvm where
// the disks actually live.
func TestDeriveSharedStorageWithoutSizeFallsBackToLocal(t *testing.T) {
	iscsi := storageRes("n1", "iscsi-direct", proxmox.StatusAvailable, true, 0, 0)
	iscsi.Plugintype = "iscsi"
	data := ClusterData{Resources: []proxmox.Resource{
		iscsi,
		storageRes("n1", "local-lvm", proxmox.StatusAvailable, false, 1*gib, 4*gib),
		storageRes("n2", "local-lvm", proxmox.StatusAvailable, false, 2*gib, 4*gib),
	}}

	c := Derive(testIdentity, data, testThreshold)

	if c.Storage.Used != 3*gib || c.Storage.Total != 8*gib {
		t.Errorf("storage = %d/%d, want %d/%d (a sizeless shared storage must not shadow the local ones)",
			c.Storage.Used, c.Storage.Total, uint64(3*gib), uint64(8*gib))
	}
}

// TestDeriveNodesWithoutFiguresAreUnknown is the token that may list the nodes
// but not audit them: PVE then returns the node rows without cpu, maxcpu, mem
// or maxmem. The card must say "unknown" and explain, not show an idle
// cluster with no memory.
func TestDeriveNodesWithoutFiguresAreUnknown(t *testing.T) {
	bare := func(name string) proxmox.Resource {
		return proxmox.Resource{Type: proxmox.ResourceTypeNode, ID: "node/" + name, Node: name, Status: proxmox.StatusOnline}
	}
	data := ClusterData{
		Resources: []proxmox.Resource{bare("n1"), bare("n2")},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(2, true),
			statusNode("n1", true),
			statusNode("n2", true),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if c.CPU != nil || c.Memory != nil {
		t.Errorf("cpu = %+v, memory = %+v, want both nil", c.CPU, c.Memory)
	}
	for _, name := range []string{"n1", "n2"} {
		n := nodeByName(t, c, name)
		if n.Status != NodeOnline {
			t.Errorf("%s status = %q, want %q: the node is up, only its figures are missing", name, n.Status, NodeOnline)
		}
		if n.CPU != nil || n.Memory != nil {
			t.Errorf("%s cpu = %+v, memory = %+v, want both nil", name, n.CPU, n.Memory)
		}
	}
	a, ok := alertByKind(c, AlertNodeStatsUnavailable)
	if !ok || !reflect.DeepEqual(a.Nodes, []string{"n1", "n2"}) {
		t.Fatalf("node_stats_unavailable alert = %+v, present=%v", a, ok)
	}
	if _, ok := alertByKind(c, AlertMemoryHigh); ok {
		t.Error("memory_high raised on unknown memory")
	}
	// The cluster itself is fine; the banner is about moxy's token.
	if c.Status != StatusHealthy {
		t.Errorf("status = %q, want %q", c.Status, StatusHealthy)
	}
}

// TestDerivePartialFiguresCountTheKnownNodes: when only some nodes come with
// figures, the totals cover those and the banner names the others.
func TestDerivePartialFiguresCountTheKnownNodes(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("seen", 0.50, 16, 8*gib, 16*gib, 1000),
			{Type: proxmox.ResourceTypeNode, ID: "node/blind", Node: "blind", Status: proxmox.StatusOnline},
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(2, true),
			statusNode("seen", true),
			statusNode("blind", true),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if c.CPU == nil || c.CPU.Cores != 16 || !closeTo(c.CPU.Ratio, 0.50) {
		t.Errorf("cpu = %+v, want ratio 0.5 over 16 cores", c.CPU)
	}
	if c.Memory == nil || c.Memory.Used != 8*gib || c.Memory.Total != 16*gib {
		t.Errorf("memory = %+v, want %d/%d", c.Memory, 8*gib, 16*gib)
	}
	if a, ok := alertByKind(c, AlertNodeStatsUnavailable); !ok || !reflect.DeepEqual(a.Nodes, []string{"blind"}) {
		t.Errorf("node_stats_unavailable alert = %+v, present=%v", a, ok)
	}
}

func TestDeriveUnavailableStorageExcluded(t *testing.T) {
	data := ClusterData{Resources: []proxmox.Resource{
		storageRes("n1", "local-lvm", proxmox.StatusAvailable, false, 1*gib, 4*gib),
		storageRes("n1", "backup-nfs", "unavailable", true, 9*gib, 99*gib),
	}}

	c := Derive(testIdentity, data, testThreshold)

	if c.Storage.Used != 1*gib || c.Storage.Total != 4*gib {
		t.Errorf("storage = %d/%d, want %d/%d", c.Storage.Used, c.Storage.Total, 1*gib, 4*gib)
	}
}

func TestDeriveGuestCounts(t *testing.T) {
	data := ClusterData{Resources: []proxmox.Resource{
		guestRes(proxmox.ResourceTypeQemu, "n1", "vm-web", proxmox.StatusRunning, false),
		guestRes(proxmox.ResourceTypeQemu, "n1", "vm-batch", proxmox.StatusStopped, false),
		guestRes(proxmox.ResourceTypeQemu, "n1", "tpl-debian", proxmox.StatusStopped, true),
		guestRes(proxmox.ResourceTypeLXC, "n2", "ct-proxy", proxmox.StatusRunning, false),
		guestRes(proxmox.ResourceTypeLXC, "n2", "ct-old", proxmox.StatusStopped, false),
		guestRes(proxmox.ResourceTypeLXC, "n2", "tpl-alpine", proxmox.StatusStopped, true),
		// Neither of these is a guest.
		{Type: proxmox.ResourceTypeSDN, Node: "n1", Status: proxmox.StatusAvailable},
		{Type: proxmox.ResourceTypePool},
	}}

	c := Derive(testIdentity, data, testThreshold)

	want := VMCounts{Running: 2, Stopped: 2, Templates: 2, Total: 4}
	if c.VMs != want {
		t.Errorf("vms = %+v, want %+v", c.VMs, want)
	}
}

// TestDeriveRunningTemplateCountsAsTemplate guards the precedence: a template
// is a template even if PVE reports it as running.
func TestDeriveRunningTemplateCountsAsTemplate(t *testing.T) {
	data := ClusterData{Resources: []proxmox.Resource{
		guestRes(proxmox.ResourceTypeQemu, "n1", "tpl-weird", proxmox.StatusRunning, true),
	}}

	c := Derive(testIdentity, data, testThreshold)

	want := VMCounts{Templates: 1}
	if c.VMs != want {
		t.Errorf("vms = %+v, want %+v", c.VMs, want)
	}
}

// TestDeriveOfflineNodeExcludedFromTotals feeds the offline node stale but
// non-zero figures, so the test fails if exclusion relies on PVE reporting
// zeros rather than on the node status.
func TestDeriveOfflineNodeExcludedFromTotals(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("up", 0.50, 16, 8*gib, 16*gib, 1000),
			nodeRes("down", 0.90, 64, 200*gib, 256*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(2, true),
			statusNode("up", true),
			statusNode("down", false),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if c.CPU.Cores != 16 || !closeTo(c.CPU.Ratio, 0.50) {
		t.Errorf("cpu = %+v, want ratio 0.5 over 16 cores", c.CPU)
	}
	if c.Memory.Used != 8*gib || c.Memory.Total != 16*gib {
		t.Errorf("memory = %d/%d, want %d/%d", c.Memory.Used, c.Memory.Total, 8*gib, 16*gib)
	}
	if got := nodeByName(t, c, "down").Status; got != NodeOffline {
		t.Errorf("node down status = %q, want %q", got, NodeOffline)
	}
	if a, ok := alertByKind(c, AlertNodeOffline); !ok || !reflect.DeepEqual(a.Nodes, []string{"down"}) {
		t.Errorf("node_offline alert = %+v, present=%v", a, ok)
	}
	if c.Status != StatusDegraded {
		t.Errorf("status = %q, want %q", c.Status, StatusDegraded)
	}
}

func TestDeriveMaintenanceFromHAManagerStatus(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.40, 32, 8*gib, 64*gib, 1000),
			nodeRes("n2", 0.02, 32, 2*gib, 64*gib, 1000),
			nodeRes("n3", 0.30, 32, 6*gib, 64*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(3, true),
			statusNode("n1", true),
			// PVE still reports a node being drained as online: only the HA
			// manager status tells maintenance apart.
			statusNode("n2", true),
			statusNode("n3", true),
		},
		HA: haStatus(map[string]string{
			"n1": proxmox.HANodeOnline,
			"n2": proxmox.HANodeMaintenance,
			"n3": proxmox.HANodeOnline,
		}),
	}

	c := Derive(testIdentity, data, testThreshold)

	if got := nodeByName(t, c, "n2").Status; got != NodeMaintenance {
		t.Errorf("n2 status = %q, want %q", got, NodeMaintenance)
	}
	// A node in maintenance is still up: its capacity stays in the totals.
	if c.CPU.Cores != 96 || c.Memory.Total != 192*gib {
		t.Errorf("cores = %d, memory total = %d; maintenance must count towards capacity",
			c.CPU.Cores, c.Memory.Total)
	}
	// Maintenance is a choice, not a fault: no alert, but the cluster is
	// no longer nominal.
	if len(c.Alerts) != 0 {
		t.Errorf("alerts = %+v, want none", c.Alerts)
	}
	if c.Status != StatusDegraded {
		t.Errorf("status = %q, want %q", c.Status, StatusDegraded)
	}
}

// TestDeriveMemoryAlertThresholdIsStrict pins the boundary: the threshold is
// the highest acceptable value, not the first alerting one.
func TestDeriveMemoryAlertThresholdIsStrict(t *testing.T) {
	cases := []struct {
		name      string
		used      int64
		total     int64
		wantAlert bool
	}{
		{name: "below", used: 700, total: 1000, wantAlert: false},
		{name: "exactly at threshold", used: 800, total: 1000, wantAlert: false},
		{name: "just above", used: 801, total: 1000, wantAlert: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			data := ClusterData{
				Resources: []proxmox.Resource{nodeRes("n1", 0.1, 8, tc.used, tc.total, 1000)},
				Status: []proxmox.ClusterStatusEntry{
					statusCluster(1, true),
					statusNode("n1", true),
				},
			}

			c := Derive(testIdentity, data, testThreshold)

			a, ok := alertByKind(c, AlertMemoryHigh)
			if ok != tc.wantAlert {
				t.Fatalf("memory_high present = %v, want %v (ratio %v)", ok, tc.wantAlert, c.Memory.Ratio)
			}
			if !ok {
				if c.Status != StatusHealthy {
					t.Errorf("status = %q, want %q", c.Status, StatusHealthy)
				}
				return
			}
			if !reflect.DeepEqual(a.Nodes, []string{"n1"}) {
				t.Errorf("alert nodes = %v, want [n1]", a.Nodes)
			}
			if a.Ratio == nil || !closeTo(*a.Ratio, c.Memory.Ratio) {
				t.Errorf("alert ratio = %v, want the cluster ratio %v", a.Ratio, c.Memory.Ratio)
			}
			if c.Status != StatusDegraded {
				t.Errorf("status = %q, want %q", c.Status, StatusDegraded)
			}
		})
	}
}

// TestDeriveMemoryAlertFromClusterRatioAlone covers the other half of the rule:
// the cluster as a whole is over the threshold although no online node is.
func TestDeriveMemoryAlertFromClusterRatioAlone(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 75, 100, 1000),
			nodeRes("n2", 0.1, 8, 950, 1000, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(2, true),
			statusNode("n1", true),
			statusNode("n2", true),
		},
		// n2 is being drained, so it is not itself a concern.
		HA: haStatus(map[string]string{"n2": proxmox.HANodeMaintenance}),
	}

	c := Derive(testIdentity, data, testThreshold)

	a, ok := alertByKind(c, AlertMemoryHigh)
	if !ok {
		t.Fatalf("memory_high missing, cluster ratio %v", c.Memory.Ratio)
	}
	if len(a.Nodes) != 0 {
		t.Errorf("alert nodes = %v, want none: only a node that is online can be listed", a.Nodes)
	}
	if a.Ratio == nil || !closeTo(*a.Ratio, 1025.0/1100.0) {
		t.Errorf("alert ratio = %v, want %v", a.Ratio, 1025.0/1100.0)
	}
}

func TestDeriveStandaloneNodeHasNoQuorum(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{nodeRes("solo", 0.1, 8, 4*gib, 32*gib, 1000)},
		// A standalone node returns node entries only.
		Status: []proxmox.ClusterStatusEntry{statusNode("solo", true)},
	}

	c := Derive(testIdentity, data, testThreshold)

	if c.Quorum != nil {
		t.Errorf("quorum = %+v, want nil for a standalone node", c.Quorum)
	}
	if _, ok := alertByKind(c, AlertQuorumLost); ok {
		t.Error("quorum_lost raised on a node that has no quorum to lose")
	}
	if c.Status != StatusHealthy {
		t.Errorf("status = %q, want %q", c.Status, StatusHealthy)
	}
}

func TestDeriveQuorumLost(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(3, false),
			statusNode("n1", true),
			statusNode("n2", true),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	want := &Quorum{Quorate: false, Nodes: 3, Online: 2}
	if !reflect.DeepEqual(c.Quorum, want) {
		t.Errorf("quorum = %+v, want %+v", c.Quorum, want)
	}
	if _, ok := alertByKind(c, AlertQuorumLost); !ok {
		t.Error("quorum_lost missing")
	}
	if c.Status != StatusDegraded {
		t.Errorf("status = %q, want %q", c.Status, StatusDegraded)
	}
}

// TestDerivePartialUpdatePermission is the partial 403: the token may ask some
// nodes and not others. Only the nodes actually refused stay unknown.
func TestDerivePartialUpdatePermission(t *testing.T) {
	checkedAt := time.Date(2026, 9, 12, 9, 50, 0, 0, time.UTC)
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n3", 0.1, 8, 4*gib, 32*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(3, true),
			statusNode("n1", true),
			statusNode("n2", true),
			statusNode("n3", true),
		},
		Updates: map[string][]proxmox.AptUpdate{
			"n1": {
				{Package: "pve-manager", Version: "9.2.12"},
				{Package: "qemu-server", Version: "9.0.21"},
			},
			// n2 answered 403 and is absent from the map.
			"n3": {},
		},
		UpdatesCheckedAt: checkedAt,
	}

	c := Derive(testIdentity, data, testThreshold)

	if got := nodeByName(t, c, "n1").PendingUpdates; got == nil || *got != 2 {
		t.Errorf("n1 pendingUpdates = %v, want 2", got)
	}
	if got := nodeByName(t, c, "n2").PendingUpdates; got != nil {
		t.Errorf("n2 pendingUpdates = %v, want nil: the node was not answered for", *got)
	}
	if got := nodeByName(t, c, "n3").PendingUpdates; got == nil || *got != 0 {
		t.Errorf("n3 pendingUpdates = %v, want 0, which is not the same as unknown", got)
	}
	if c.Updates == nil {
		t.Fatal("updates = nil, want a summary")
	}
	if !reflect.DeepEqual(c.Updates.Nodes, []string{"n1"}) {
		t.Errorf("updates nodes = %v, want [n1]", c.Updates.Nodes)
	}
	if c.Updates.PVEManagerVersion == nil || *c.Updates.PVEManagerVersion != "9.2.12" {
		t.Errorf("pveManagerVersion = %v, want 9.2.12", c.Updates.PVEManagerVersion)
	}
	if !c.Updates.CheckedAt.Equal(checkedAt) {
		t.Errorf("checkedAt = %v, want %v", c.Updates.CheckedAt, checkedAt)
	}

	a, ok := alertByKind(c, AlertUpdatesAvailable)
	if !ok {
		t.Fatal("updates_available missing")
	}
	if !reflect.DeepEqual(a.Nodes, []string{"n1"}) {
		t.Errorf("alert nodes = %v, want [n1]", a.Nodes)
	}
	if a.Version == nil || *a.Version != "9.2.12" {
		t.Errorf("alert version = %v, want 9.2.12", a.Version)
	}
}

// TestDeriveUpdatesWithoutPVEManager keeps "unknown version" distinct from
// "no update": the frontend then shows a count instead of a release.
func TestDeriveUpdatesWithoutPVEManager(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000)},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(1, true),
			statusNode("n1", true),
		},
		Updates: map[string][]proxmox.AptUpdate{
			"n1": {{Package: "curl", Version: "8.5.0"}},
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if c.Updates == nil || c.Updates.PVEManagerVersion != nil {
		t.Errorf("pveManagerVersion = %v, want nil", c.Updates)
	}
	if a, ok := alertByKind(c, AlertUpdatesAvailable); !ok || a.Version != nil {
		t.Errorf("alert = %+v, present=%v, want an alert without a version", a, ok)
	}
}

// TestDeriveUpdatesUnknown is the full 403: nothing is known, and nothing is
// claimed.
func TestDeriveUpdatesUnknown(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(2, true),
			statusNode("n1", true),
			statusNode("n2", true),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if c.Updates != nil {
		t.Errorf("updates = %+v, want nil", c.Updates)
	}
	for _, n := range c.Nodes {
		if n.PendingUpdates != nil {
			t.Errorf("%s pendingUpdates = %d, want nil", n.Name, *n.PendingUpdates)
		}
	}
	if _, ok := alertByKind(c, AlertUpdatesAvailable); ok {
		t.Error("updates_available raised while no update is known")
	}
	if c.Status != StatusHealthy {
		t.Errorf("status = %q, want %q", c.Status, StatusHealthy)
	}
}

// TestDeriveUpdatesAloneStayHealthy matches the mockups: a cluster carrying an
// update banner is still reported as healthy.
func TestDeriveUpdatesAloneStayHealthy(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000)},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(1, true),
			statusNode("n1", true),
		},
		Updates: map[string][]proxmox.AptUpdate{
			"n1": {{Package: "pve-manager", Version: "9.2.12"}},
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if got := alertKinds(c); !reflect.DeepEqual(got, []AlertKind{AlertUpdatesAvailable}) {
		t.Fatalf("alerts = %v, want only updates_available", got)
	}
	if c.Status != StatusHealthy {
		t.Errorf("status = %q, want %q", c.Status, StatusHealthy)
	}
}

// aptUpdates builds n distinct pending packages, so that only the COUNT the
// helper produces matters to the caller.
func aptUpdates(n int) []proxmox.AptUpdate {
	out := make([]proxmox.AptUpdate, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, proxmox.AptUpdate{
			Package: "pkg" + strconv.Itoa(i),
			Version: "1.0",
		})
	}
	return out
}

// TestDeriveUpdatesUnevenAcrossNodes is the case the banner exists for: nodes
// sitting at different package levels, which updates_available alone never
// says.
func TestDeriveUpdatesUnevenAcrossNodes(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n3", 0.1, 8, 4*gib, 32*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(3, true),
			statusNode("n1", true),
			statusNode("n2", true),
			statusNode("n3", true),
		},
		Updates: map[string][]proxmox.AptUpdate{
			"n1": aptUpdates(11),
			"n2": aptUpdates(14),
			"n3": aptUpdates(12),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	a, ok := alertByKind(c, AlertUpdatesUneven)
	if !ok {
		t.Fatalf("alerts = %v, want updates_uneven", alertKinds(c))
	}
	if a.PendingMin == nil || *a.PendingMin != 11 {
		t.Errorf("pendingMin = %v, want 11", a.PendingMin)
	}
	if a.PendingMax == nil || *a.PendingMax != 14 {
		t.Errorf("pendingMax = %v, want 14", a.PendingMax)
	}
	// The alert is about the spread, not about one node: naming a node would
	// read as "the problem is there".
	if len(a.Nodes) != 0 {
		t.Errorf("nodes = %v, want none", a.Nodes)
	}
	// An unprecedented alert degrades the cluster, and that is the intent:
	// nodes at different levels is an inconsistency someone acts on.
	if c.Status != StatusDegraded {
		t.Errorf("status = %q, want %q", c.Status, StatusDegraded)
	}
}

// TestDeriveUpdatesUnevenComesBeforeAvailable pins the banner order: a card
// shows alerts[0] only, and an uneven cluster nearly always has updates
// pending too, so the other order would hide the fault behind the news.
func TestDeriveUpdatesUnevenComesBeforeAvailable(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(2, true),
			statusNode("n1", true),
			statusNode("n2", true),
		},
		Updates: map[string][]proxmox.AptUpdate{
			"n1": aptUpdates(3),
			"n2": aptUpdates(9),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	want := []AlertKind{AlertUpdatesUneven, AlertUpdatesAvailable}
	if got := alertKinds(c); !reflect.DeepEqual(got, want) {
		t.Errorf("alerts = %v, want %v", got, want)
	}
}

// TestDeriveUpdatesEvenRaisesNothing is the other half of the contract: a
// cluster whose nodes agree shows nothing more than it shows today.
func TestDeriveUpdatesEvenRaisesNothing(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(2, true),
			statusNode("n1", true),
			statusNode("n2", true),
		},
		Updates: map[string][]proxmox.AptUpdate{
			"n1": aptUpdates(12),
			"n2": aptUpdates(12),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if got := alertKinds(c); !reflect.DeepEqual(got, []AlertKind{AlertUpdatesAvailable}) {
		t.Fatalf("alerts = %v, want only updates_available", got)
	}
	if c.Status != StatusHealthy {
		t.Errorf("status = %q, want %q", c.Status, StatusHealthy)
	}
}

// TestDeriveUpdatesUnevenIgnoresUnknownCounts is the false positive to avoid: a
// node whose count could not be read — a partial 403 — is left out of the
// comparison rather than counted as zero, which would flag an even cluster.
func TestDeriveUpdatesUnevenIgnoresUnknownCounts(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n3", 0.1, 8, 4*gib, 32*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(3, true),
			statusNode("n1", true),
			statusNode("n2", true),
			statusNode("n3", true),
		},
		// n3 is missing from the map: unknown, not up to date.
		Updates: map[string][]proxmox.AptUpdate{
			"n1": aptUpdates(7),
			"n2": aptUpdates(7),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if nodeByName(t, c, "n3").PendingUpdates != nil {
		t.Error("n3 pendingUpdates is set, want nil")
	}
	if _, ok := alertByKind(c, AlertUpdatesUneven); ok {
		t.Errorf("alerts = %v, want no updates_uneven: n3 is unknown, not zero", alertKinds(c))
	}
}

// TestDeriveUpdatesUnevenNeedsTwoKnownCounts: "uneven" means nothing with a
// single measured node, so a lone count raises nothing.
func TestDeriveUpdatesUnevenNeedsTwoKnownCounts(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(2, true),
			statusNode("n1", true),
			statusNode("n2", true),
		},
		Updates: map[string][]proxmox.AptUpdate{
			"n1": aptUpdates(5),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if _, ok := alertByKind(c, AlertUpdatesUneven); ok {
		t.Errorf("alerts = %v, want no updates_uneven: only one node is measured", alertKinds(c))
	}
}

// TestDeriveUpdatesUnevenCountsMaintenanceNode: a drained node is still up and
// its packages are real, so it is compared like any other — and it is exactly
// the node likely to have been left behind.
func TestDeriveUpdatesUnevenCountsMaintenanceNode(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(2, true),
			statusNode("n1", true),
			statusNode("n2", true),
		},
		HA: haStatus(map[string]string{"n2": proxmox.HANodeMaintenance}),
		Updates: map[string][]proxmox.AptUpdate{
			"n1": aptUpdates(4),
			"n2": aptUpdates(21),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if got := nodeByName(t, c, "n2").Status; got != NodeMaintenance {
		t.Fatalf("n2 status = %q, want %q", got, NodeMaintenance)
	}
	a, ok := alertByKind(c, AlertUpdatesUneven)
	if !ok {
		t.Fatalf("alerts = %v, want updates_uneven", alertKinds(c))
	}
	if a.PendingMin == nil || *a.PendingMin != 4 || a.PendingMax == nil || *a.PendingMax != 21 {
		t.Errorf("spread = %v..%v, want 4..21", a.PendingMin, a.PendingMax)
	}
}

// TestDeriveUpdatesUnevenSkipsOfflineNode: an offline node has no count to
// compare, and a leftover one must not make an even cluster look uneven.
func TestDeriveUpdatesUnevenSkipsOfflineNode(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n3", 0, 0, 0, 0, 0),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(3, true),
			statusNode("n1", true),
			statusNode("n2", true),
			statusNode("n3", false),
		},
		Updates: map[string][]proxmox.AptUpdate{
			"n1": aptUpdates(6),
			"n2": aptUpdates(6),
			"n3": aptUpdates(30),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if _, ok := alertByKind(c, AlertUpdatesUneven); ok {
		t.Errorf("alerts = %v, want no updates_uneven: n3 is offline", alertKinds(c))
	}
}

// TestDeriveUpdatesKeepsTheHighestVersion: nodes need not offer the same
// release, and the banner must announce the highest one rather than whichever
// node sorts first by name. Numerically, since 9.2.9 sorts above 9.2.12.
func TestDeriveUpdatesKeepsTheHighestVersion(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(2, true),
			statusNode("n1", true),
			statusNode("n2", true),
		},
		Updates: map[string][]proxmox.AptUpdate{
			"n1": {{Package: "pve-manager", Version: "9.2.9"}},
			"n2": {{Package: "pve-manager", Version: "9.2.12"}},
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if c.Updates == nil || c.Updates.PVEManagerVersion == nil {
		t.Fatalf("updates = %+v, want a version", c.Updates)
	}
	if got := *c.Updates.PVEManagerVersion; got != "9.2.12" {
		t.Errorf("pveManagerVersion = %q, want 9.2.12", got)
	}
	if a, ok := alertByKind(c, AlertUpdatesAvailable); !ok || a.Version == nil || *a.Version != "9.2.12" {
		t.Errorf("alert = %+v, present=%v, want version 9.2.12", a, ok)
	}
}

func TestHigherVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"9.2.12", "9.2.9", true},
		{"9.2.9", "9.2.12", false},
		{"9.2.12", "9.2.12", false},
		{"10.0.0", "9.9.9", true},
		{"9.2.12-1", "9.2.12", true},
		{"9.2.12", "9.2.12-1", false},
		{"8.3.0-2", "8.3.0-10", false},
		{"9.2.12-pve1", "9.2.12-pve1", false},
		{"", "9.2.12", false},
		{"9.2.12", "", true},
	}
	for _, tc := range cases {
		if got := higherVersion(tc.a, tc.b); got != tc.want {
			t.Errorf("higherVersion(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestDeriveNodeKnownFromResourcesOnly covers the stale entry: /cluster/status
// is authoritative, so a node it does not list is not declared online on the
// strength of a leftover /cluster/resources row.
func TestDeriveNodeKnownFromResourcesOnly(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("ghost", 0.9, 64, 60*gib, 64*gib, 1000),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(1, true),
			statusNode("n1", true),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if got := nodeByName(t, c, "ghost").Status; got != NodeUnknown {
		t.Errorf("ghost status = %q, want %q", got, NodeUnknown)
	}
	if c.CPU.Cores != 8 {
		t.Errorf("cores = %d, want 8: an unknown node must not weigh on the totals", c.CPU.Cores)
	}
	if a, ok := alertByKind(c, AlertNodeOffline); !ok || !reflect.DeepEqual(a.Nodes, []string{"ghost"}) {
		t.Errorf("node_offline alert = %+v, present=%v", a, ok)
	}
}

// TestDeriveOrderIsDeterministic pins both orders the payload depends on:
// nodes by name, alerts by severity.
func TestDeriveOrderIsDeterministic(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n3", 0.1, 8, 31*gib, 32*gib, 1000),
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 0, 0, 0),
		},
		Status: []proxmox.ClusterStatusEntry{
			statusNode("n3", true),
			statusCluster(3, false),
			statusNode("n1", true),
			statusNode("n2", false),
		},
		Updates: map[string][]proxmox.AptUpdate{
			"n3": {{Package: "pve-manager", Version: "9.2.12"}},
			"n1": {{Package: "pve-manager", Version: "9.2.12"}},
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if got := nodeNames(c); !reflect.DeepEqual(got, []string{"n1", "n2", "n3"}) {
		t.Errorf("nodes = %v, want them sorted by name", got)
	}
	want := []AlertKind{AlertQuorumLost, AlertNodeOffline, AlertMemoryHigh, AlertUpdatesAvailable}
	if got := alertKinds(c); !reflect.DeepEqual(got, want) {
		t.Errorf("alerts = %v, want %v", got, want)
	}
	if a, _ := alertByKind(c, AlertUpdatesAvailable); !reflect.DeepEqual(a.Nodes, []string{"n1", "n3"}) {
		t.Errorf("updates_available nodes = %v, want them sorted", a.Nodes)
	}

	// Same input, same output: nothing here depends on map iteration order.
	for i := 0; i < 10; i++ {
		if again := Derive(testIdentity, data, testThreshold); !reflect.DeepEqual(again, c) {
			t.Fatalf("derive is not stable across calls: %+v vs %+v", again, c)
		}
	}
}

// TestDeriveEmptyClusterIsSerialisable is the division-by-zero guard: a ratio
// must never be NaN, which encoding/json refuses outright.
func TestDeriveEmptyClusterIsSerialisable(t *testing.T) {
	c := Derive(testIdentity, ClusterData{}, testThreshold)

	if c.Storage.Total != 0 || c.Storage.Used != 0 || c.Storage.Ratio != 0 {
		t.Errorf("storage = %+v, want a zero usage", c.Storage)
	}
	// No node, no figure: unknown is null, never a zero dressed as a measure.
	if c.CPU != nil || c.Memory != nil {
		t.Errorf("cpu = %+v, memory = %+v, want both nil", c.CPU, c.Memory)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"cpu":null`, `"memory":null`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("payload lacks %s: %s", want, raw)
		}
	}
}

// TestDeriveNeverReturnsNilSlices keeps the JSON carrying arrays rather than
// nulls, which the frontend would have to special-case.
func TestDeriveNeverReturnsNilSlices(t *testing.T) {
	c := Derive(testIdentity, ClusterData{}, testThreshold)

	if c.Nodes == nil {
		t.Error("nodes = nil, want an empty slice")
	}
	if c.Alerts == nil {
		t.Error("alerts = nil, want an empty slice")
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Nodes  []Node  `json:"nodes"`
		Alerts []Alert `json:"alerts"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Nodes == nil || decoded.Alerts == nil {
		t.Errorf("payload = %s, want [] for nodes and alerts", raw)
	}
}

// TestDeriveLeavesPollerFieldsAlone: freshness is the poller's business, since
// deciding it needs a clock and Derive reads none.
func TestDeriveLeavesPollerFieldsAlone(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000)},
		Status: []proxmox.ClusterStatusEntry{
			statusCluster(1, true),
			statusNode("n1", true),
		},
	}

	c := Derive(testIdentity, data, testThreshold)

	if c.FetchedAt != nil {
		t.Errorf("fetchedAt = %v, want nil", c.FetchedAt)
	}
	if c.Error != nil {
		t.Errorf("error = %+v, want nil", c.Error)
	}
	if c.Status == StatusUnreachable {
		t.Error("status = unreachable, which only the poller may decide")
	}
}

func TestDeriveCarriesIdentity(t *testing.T) {
	color := "#378ADD"
	id := Identity{ID: "qualification", Name: "Qualification", Color: &color}

	c := Derive(id, ClusterData{}, testThreshold)

	if c.ID != id.ID || c.Name != id.Name || c.Color != id.Color {
		t.Errorf("identity = %q/%q/%v, want %q/%q/%v", c.ID, c.Name, c.Color, id.ID, id.Name, id.Color)
	}
}

func TestComputeTotals(t *testing.T) {
	clusters := []ClusterOverview{
		{
			Nodes: []Node{
				{Name: "a1", Status: NodeOnline},
				{Name: "a2", Status: NodeOnline},
			},
			VMs:    VMCounts{Running: 12, Stopped: 1, Templates: 1, Total: 13},
			Alerts: []Alert{},
		},
		{
			Nodes: []Node{
				{Name: "b1", Status: NodeOnline},
				// Maintenance counts as online: the node is up, it merely
				// refuses new guests.
				{Name: "b2", Status: NodeMaintenance},
				{Name: "b3", Status: NodeOffline},
				{Name: "b4", Status: NodeUnknown},
			},
			VMs:    VMCounts{Running: 44, Stopped: 2, Total: 46},
			Alerts: []Alert{{Kind: AlertMemoryHigh}, {Kind: AlertNodeOffline}},
		},
		{
			Nodes:  []Node{{Name: "c1", Status: NodeOnline}},
			VMs:    VMCounts{Running: 89, Templates: 3, Total: 89},
			Alerts: []Alert{{Kind: AlertUpdatesAvailable}},
		},
	}

	got := ComputeTotals(clusters)

	want := Totals{Clusters: 3, Nodes: 7, NodesOnline: 5, VMs: 148, Alerts: 3}
	if got != want {
		t.Errorf("totals = %+v, want %+v", got, want)
	}
}

func TestComputeTotalsOfNothing(t *testing.T) {
	if got := ComputeTotals(nil); got != (Totals{}) {
		t.Errorf("totals = %+v, want zero", got)
	}
}

// fixture decodes a PVE response captured under proxmox/testdata, envelope
// included, so that the derivation is exercised on the shape of a real answer.
func fixture[T any](t *testing.T, name string) T {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "proxmox", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return envelope.Data
}

// TestDeriveFromFixtures runs one whole poll of the three-node preproduction
// cluster: mixed string and number encodings, a node in maintenance, a shared
// storage reported once per node and an unavailable one.
func TestDeriveFromFixtures(t *testing.T) {
	ha := fixture[proxmox.HAManagerStatus](t, "ha_manager_status.json")
	data := ClusterData{
		Resources: fixture[[]proxmox.Resource](t, "cluster_resources.json"),
		Status:    fixture[[]proxmox.ClusterStatusEntry](t, "cluster_status.json"),
		HA:        &ha,
	}

	c := Derive(testIdentity, data, testThreshold)

	wantNodes := []string{"prox-pprd-2301-cit", "prox-pprd-2302-cit", "prox-pprd-2303-cit"}
	if got := nodeNames(c); !reflect.DeepEqual(got, wantNodes) {
		t.Fatalf("nodes = %v, want %v", got, wantNodes)
	}
	if got := nodeByName(t, c, "prox-pprd-2302-cit").Status; got != NodeMaintenance {
		t.Errorf("2302 status = %q, want %q", got, NodeMaintenance)
	}
	if got := nodeByName(t, c, "prox-pprd-2301-cit").Uptime; got != 3542400 {
		t.Errorf("2301 uptime = %d, want 3542400", got)
	}

	// (0.42 + 0.03 + 0.28) * 32 / 96
	if want := 0.73 / 3; !closeTo(c.CPU.Ratio, want) {
		t.Errorf("cpu ratio = %v, want %v", c.CPU.Ratio, want)
	}
	if c.CPU.Cores != 96 {
		t.Errorf("cores = %d, want 96", c.CPU.Cores)
	}

	const wantMemUsed = 76000000000 + 8589934592 + 82000000000
	const wantMemTotal = 3 * 103079215104
	if c.Memory.Used != wantMemUsed || c.Memory.Total != wantMemTotal {
		t.Errorf("memory = %d/%d, want %d/%d", c.Memory.Used, c.Memory.Total, uint64(wantMemUsed), uint64(wantMemTotal))
	}

	// nfs-shared once; local-lvm is per node and backup-nfs unavailable, and
	// neither belongs in the shared capacity.
	const wantStoUsed = 4288125337600
	const wantStoTotal = 8796093022208
	if c.Storage.Used != wantStoUsed || c.Storage.Total != wantStoTotal {
		t.Errorf("storage = %d/%d, want %d/%d", c.Storage.Used, c.Storage.Total, uint64(wantStoUsed), uint64(wantStoTotal))
	}

	wantVMs := VMCounts{Running: 3, Stopped: 1, Templates: 1, Total: 4}
	if c.VMs != wantVMs {
		t.Errorf("vms = %+v, want %+v", c.VMs, wantVMs)
	}
	wantQuorum := &Quorum{Quorate: true, Nodes: 3, Online: 3}
	if !reflect.DeepEqual(c.Quorum, wantQuorum) {
		t.Errorf("quorum = %+v, want %+v", c.Quorum, wantQuorum)
	}
	// Every node is below the threshold, and maintenance raises no alert.
	if len(c.Alerts) != 0 {
		t.Errorf("alerts = %+v, want none", c.Alerts)
	}
	if c.Status != StatusDegraded {
		t.Errorf("status = %q, want %q (2302 is in maintenance)", c.Status, StatusDegraded)
	}
	if c.Updates != nil {
		t.Errorf("updates = %+v, want nil: the fixture carries no apt result", c.Updates)
	}
}

// TestDeriveFixtureWithUpdates adds the apt/update fixture on top of the poll,
// so the update banner is exercised on a captured answer too.
func TestDeriveFixtureWithUpdates(t *testing.T) {
	ha := fixture[proxmox.HAManagerStatus](t, "ha_manager_status.json")
	pending := fixture[[]proxmox.AptUpdate](t, "apt_update.json")
	checkedAt := time.Date(2026, 9, 12, 9, 50, 0, 0, time.UTC)
	data := ClusterData{
		Resources: fixture[[]proxmox.Resource](t, "cluster_resources.json"),
		Status:    fixture[[]proxmox.ClusterStatusEntry](t, "cluster_status.json"),
		HA:        &ha,
		Updates: map[string][]proxmox.AptUpdate{
			"prox-pprd-2301-cit": pending,
			"prox-pprd-2302-cit": nil,
			"prox-pprd-2303-cit": nil,
		},
		UpdatesCheckedAt: checkedAt,
	}

	c := Derive(testIdentity, data, testThreshold)

	a, ok := alertByKind(c, AlertUpdatesAvailable)
	if !ok {
		t.Fatal("updates_available missing")
	}
	if !reflect.DeepEqual(a.Nodes, []string{"prox-pprd-2301-cit"}) {
		t.Errorf("alert nodes = %v", a.Nodes)
	}
	if a.Version == nil || *a.Version != "9.2.12" {
		t.Errorf("alert version = %v, want 9.2.12", a.Version)
	}
	if got := nodeByName(t, c, "prox-pprd-2301-cit").PendingUpdates; got == nil || *got != len(pending) {
		t.Errorf("2301 pendingUpdates = %v, want %d", got, len(pending))
	}
	if got := nodeByName(t, c, "prox-pprd-2303-cit").PendingUpdates; got == nil || *got != 0 {
		t.Errorf("2303 pendingUpdates = %v, want 0", got)
	}
}

// guestWithVMID builds a guest resource carrying the fields the guest list
// derives from, which the counting tests do not need.
func guestWithVMID(kind, node, name, status string, vmid int64, template bool) proxmox.Resource {
	r := guestRes(kind, node, name, status, template)
	r.VMID = proxmox.FlexInt(vmid)
	r.ID = fmt.Sprintf("%s/%d", kind, vmid)
	return r
}

// guestVMIDs lists the identifiers of a node's guests, in payload order.
func guestVMIDs(n Node) []int {
	ids := make([]int, 0, len(n.Guests))
	for _, g := range n.Guests {
		ids = append(ids, g.VMID)
	}
	return ids
}

func TestDeriveGuestsAttachedToTheirNode(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
			guestWithVMID(proxmox.ResourceTypeQemu, "n1", "vm-web", proxmox.StatusRunning, 101, false),
			guestWithVMID(proxmox.ResourceTypeLXC, "n2", "ct-proxy", proxmox.StatusRunning, 200, false),
			guestWithVMID(proxmox.ResourceTypeQemu, "n2", "vm-batch", proxmox.StatusStopped, 103, false),
		},
		Status: []proxmox.ClusterStatusEntry{statusNode("n1", true), statusNode("n2", true)},
	}

	c := Derive(testIdentity, data, testThreshold)

	if got := guestVMIDs(nodeByName(t, c, "n1")); !reflect.DeepEqual(got, []int{101}) {
		t.Errorf("n1 guests = %v, want [101]", got)
	}
	if got := guestVMIDs(nodeByName(t, c, "n2")); !reflect.DeepEqual(got, []int{103, 200}) {
		t.Errorf("n2 guests = %v, want [103 200] sorted by vmid", got)
	}
	if got := nodeByName(t, c, "n2").Guests[1].Kind; got != GuestLXC {
		t.Errorf("ct-proxy kind = %q, want %q", got, GuestLXC)
	}
	if got := nodeByName(t, c, "n2").Guests[0].Status; got != GuestStopped {
		t.Errorf("vm-batch status = %q, want %q", got, GuestStopped)
	}
}

// TestDeriveGuestFieldsAreCopiedRaw checks the guest keeps the figures PVE
// reported, in the units of the wire: a CPU fraction and byte counts.
func TestDeriveGuestFieldsAreCopiedRaw(t *testing.T) {
	g := guestWithVMID(proxmox.ResourceTypeQemu, "n1", "vm-web", proxmox.StatusRunning, 101, false)
	g.CPU = proxmox.FlexFloat(0.052)
	g.MaxCPU = proxmox.FlexInt(4)
	g.Mem = proxmox.FlexInt(4 * gib)
	g.MaxMem = proxmox.FlexInt(8 * gib)
	g.Tags = "pprd;web"
	data := ClusterData{
		Resources: []proxmox.Resource{nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000), g},
		Status:    []proxmox.ClusterStatusEntry{statusNode("n1", true)},
	}

	c := Derive(testIdentity, data, testThreshold)

	want := Guest{
		VMID:   101,
		Name:   "vm-web",
		Kind:   GuestQemu,
		Status: GuestRunning,
		CPU:    CPU{Ratio: 0.052, Cores: 4},
		Memory: Usage{Used: 4 * gib, Total: 8 * gib, Ratio: 0.5},
		Tags:   []string{"pprd", "web"},
	}
	if got := nodeByName(t, c, "n1").Guests[0]; !reflect.DeepEqual(got, want) {
		t.Errorf("guest = %+v, want %+v", got, want)
	}
}

// TestDeriveGuestTemplateWinsOverStatus guards the same precedence as the
// counters: a template PVE reports as running is still a template, and the list
// must not contradict VMCounts.
func TestDeriveGuestTemplateWinsOverStatus(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			guestWithVMID(proxmox.ResourceTypeQemu, "n1", "tpl-weird", proxmox.StatusRunning, 9000, true),
		},
		Status: []proxmox.ClusterStatusEntry{statusNode("n1", true)},
	}

	c := Derive(testIdentity, data, testThreshold)

	if got := nodeByName(t, c, "n1").Guests[0].Status; got != GuestTemplate {
		t.Errorf("status = %q, want %q", got, GuestTemplate)
	}
	if want := (VMCounts{Templates: 1}); c.VMs != want {
		t.Errorf("vms = %+v, want %+v", c.VMs, want)
	}
}

// TestDeriveGuestsSortedByVMID pins the order of the sidebar tree: it must not
// follow the order PVE happened to answer in.
func TestDeriveGuestsSortedByVMID(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			guestWithVMID(proxmox.ResourceTypeQemu, "n1", "vm-c", proxmox.StatusRunning, 9000, true),
			guestWithVMID(proxmox.ResourceTypeLXC, "n1", "vm-a", proxmox.StatusRunning, 150, false),
			guestWithVMID(proxmox.ResourceTypeQemu, "n1", "vm-b", proxmox.StatusStopped, 101, false),
		},
		Status: []proxmox.ClusterStatusEntry{statusNode("n1", true)},
	}

	c := Derive(testIdentity, data, testThreshold)

	if got := guestVMIDs(nodeByName(t, c, "n1")); !reflect.DeepEqual(got, []int{101, 150, 9000}) {
		t.Errorf("guests = %v, want [101 150 9000]", got)
	}
	for i := 0; i < 10; i++ {
		if again := Derive(testIdentity, data, testThreshold); !reflect.DeepEqual(again, c) {
			t.Fatalf("guest order is not stable across calls")
		}
	}
}

// TestDeriveGuestOnUnknownNodeIgnored: PVE keeps reporting the guests of a node
// that has left the cluster. They must not conjure a node of their own, which
// would contradict /cluster/status and put a ghost in the tree.
func TestDeriveGuestOnUnknownNodeIgnored(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			guestWithVMID(proxmox.ResourceTypeQemu, "n1", "vm-web", proxmox.StatusRunning, 101, false),
			guestWithVMID(proxmox.ResourceTypeQemu, "ghost", "vm-gone", proxmox.StatusRunning, 102, false),
		},
		Status: []proxmox.ClusterStatusEntry{statusNode("n1", true)},
	}

	c := Derive(testIdentity, data, testThreshold)

	if got := nodeNames(c); !reflect.DeepEqual(got, []string{"n1"}) {
		t.Fatalf("nodes = %v, want [n1] alone", got)
	}
	if got := guestVMIDs(nodeByName(t, c, "n1")); !reflect.DeepEqual(got, []int{101}) {
		t.Errorf("n1 guests = %v, want [101]", got)
	}
	// The counters look at the resources, not at the node list: the orphan is
	// still a guest of the cluster.
	if want := (VMCounts{Running: 2, Total: 2}); c.VMs != want {
		t.Errorf("vms = %+v, want %+v", c.VMs, want)
	}
}

// TestDeriveGuestWithoutMemoryTotal is the division-by-zero guard on the guest
// ratio: a NaN is not valid JSON and would break the frontend outright.
func TestDeriveGuestWithoutMemoryTotal(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			guestWithVMID(proxmox.ResourceTypeQemu, "n1", "vm-new", proxmox.StatusStopped, 101, false),
		},
		Status: []proxmox.ClusterStatusEntry{statusNode("n1", true)},
	}

	c := Derive(testIdentity, data, testThreshold)

	got := nodeByName(t, c, "n1").Guests[0].Memory
	if got.Ratio != 0 || got.Used != 0 || got.Total != 0 {
		t.Errorf("memory = %+v, want a zero usage", got)
	}
	if math.IsNaN(got.Ratio) {
		t.Fatal("memory ratio is NaN")
	}
	if _, err := json.Marshal(c); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}

// TestDeriveGuestSlicesAreNeverNil keeps the JSON carrying arrays: a node with
// no guest, and a guest with no tag, must both serialise as [].
func TestDeriveGuestSlicesAreNeverNil(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
			guestWithVMID(proxmox.ResourceTypeQemu, "n1", "vm-web", proxmox.StatusRunning, 101, false),
		},
		Status: []proxmox.ClusterStatusEntry{statusNode("n1", true), statusNode("n2", true)},
	}

	c := Derive(testIdentity, data, testThreshold)

	if got := nodeByName(t, c, "n2").Guests; got == nil {
		t.Error("n2 guests = nil, want an empty slice")
	} else if len(got) != 0 {
		t.Errorf("n2 guests = %+v, want none", got)
	}
	if got := nodeByName(t, c, "n1").Guests[0].Tags; got == nil {
		t.Error("tags = nil, want an empty slice")
	}

	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Nodes []struct {
			Guests []struct {
				Tags []string `json:"tags"`
			} `json:"guests"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for i, n := range decoded.Nodes {
		if n.Guests == nil {
			t.Errorf("node %d: guests decoded as null, want []", i)
		}
		for j, g := range n.Guests {
			if g.Tags == nil {
				t.Errorf("node %d guest %d: tags decoded as null, want []", i, j)
			}
		}
	}
}

// TestDeriveGuestListAgreesWithVMCounts checks the two views of the same guests
// cannot diverge: what the list shows is what the counters count.
func TestDeriveGuestListAgreesWithVMCounts(t *testing.T) {
	data := ClusterData{
		Resources: []proxmox.Resource{
			nodeRes("n1", 0.1, 8, 4*gib, 32*gib, 1000),
			nodeRes("n2", 0.1, 8, 4*gib, 32*gib, 1000),
			guestWithVMID(proxmox.ResourceTypeQemu, "n1", "vm-web", proxmox.StatusRunning, 101, false),
			guestWithVMID(proxmox.ResourceTypeQemu, "n1", "vm-batch", proxmox.StatusStopped, 102, false),
			guestWithVMID(proxmox.ResourceTypeQemu, "n1", "tpl-debian", proxmox.StatusStopped, 9000, true),
			guestWithVMID(proxmox.ResourceTypeLXC, "n2", "ct-proxy", proxmox.StatusRunning, 200, false),
			guestWithVMID(proxmox.ResourceTypeLXC, "n2", "ct-old", proxmox.StatusStopped, 201, false),
		},
		Status: []proxmox.ClusterStatusEntry{statusNode("n1", true), statusNode("n2", true)},
	}

	c := Derive(testIdentity, data, testThreshold)

	assertGuestsMatchCounts(t, c)
}

// assertGuestsMatchCounts checks the guests listed under the nodes of a cluster
// add up to its VMCounts, templates apart.
func assertGuestsMatchCounts(t *testing.T, c ClusterOverview) {
	t.Helper()
	var got VMCounts
	for _, n := range c.Nodes {
		for _, g := range n.Guests {
			switch g.Status {
			case GuestRunning:
				got.Running++
			case GuestStopped:
				got.Stopped++
			case GuestTemplate:
				got.Templates++
			default:
				t.Errorf("guest %d has status %q", g.VMID, g.Status)
			}
		}
	}
	got.Total = got.Running + got.Stopped
	if got != c.VMs {
		t.Errorf("guests add up to %+v, VMCounts = %+v", got, c.VMs)
	}
}

// TestDeriveGuestsFromFixtures runs the guest list over the shape of a real
// answer: mixed string and number encodings, a template and a container.
func TestDeriveGuestsFromFixtures(t *testing.T) {
	ha := fixture[proxmox.HAManagerStatus](t, "ha_manager_status.json")
	data := ClusterData{
		Resources: fixture[[]proxmox.Resource](t, "cluster_resources.json"),
		Status:    fixture[[]proxmox.ClusterStatusEntry](t, "cluster_status.json"),
		HA:        &ha,
	}

	c := Derive(testIdentity, data, testThreshold)

	want := map[string][]Guest{
		"prox-pprd-2301-cit": {
			{
				VMID: 101, Name: "vm-web-01", Kind: GuestQemu, Status: GuestRunning,
				CPU:    CPU{Ratio: 0.052, Cores: 4},
				Memory: Usage{Used: 4 * gib, Total: 8 * gib, Ratio: 0.5},
				Tags:   []string{"pprd", "web"},
			},
			{
				VMID: 9000, Name: "tpl-debian12", Kind: GuestQemu, Status: GuestTemplate,
				CPU:    CPU{Ratio: 0, Cores: 2},
				Memory: Usage{Used: 0, Total: 2 * gib, Ratio: 0},
				Tags:   []string{"template", "debian"},
			},
		},
		// vmid, maxcpu, mem and maxmem all arrive as JSON strings here.
		"prox-pprd-2302-cit": {
			{
				VMID: 102, Name: "vm-db-01", Kind: GuestQemu, Status: GuestRunning,
				CPU:    CPU{Ratio: 0.113, Cores: 8},
				Memory: Usage{Used: 12 * gib, Total: 16 * gib, Ratio: 0.75},
				Tags:   []string{"pprd", "db"},
			},
		},
		"prox-pprd-2303-cit": {
			{
				VMID: 103, Name: "vm-batch-01", Kind: GuestQemu, Status: GuestStopped,
				CPU:    CPU{Ratio: 0, Cores: 2},
				Memory: Usage{Used: 0, Total: 4 * gib, Ratio: 0},
				// An empty tag string yields an empty list, never nil.
				Tags: []string{},
			},
			{
				VMID: 200, Name: "ct-proxy-01", Kind: GuestLXC, Status: GuestRunning,
				CPU:    CPU{Ratio: 0.007, Cores: 2},
				Memory: Usage{Used: 512 * 1024 * 1024, Total: 1 * gib, Ratio: 0.5},
				Tags:   []string{"pprd", "net", "edge"},
			},
		},
	}
	for name, guests := range want {
		if got := nodeByName(t, c, name).Guests; !reflect.DeepEqual(got, guests) {
			t.Errorf("%s guests = %+v, want %+v", name, got, guests)
		}
	}
	assertGuestsMatchCounts(t, c)
}
