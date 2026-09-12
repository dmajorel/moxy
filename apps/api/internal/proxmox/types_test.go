package proxmox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFlexIntUnmarshal(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int64
	}{
		{"number", `42`, 42},
		{"negative number", `-7`, -7},
		{"zero", `0`, 0},
		{"string", `"42"`, 42},
		{"string with spaces", `" 42 "`, 42},
		{"float number truncated", `4.9`, 4},
		{"float string truncated", `"4.9"`, 4},
		{"exponent", `1e3`, 1000},
		{"bool true", `true`, 1},
		{"bool false", `false`, 0},
		{"string bool", `"true"`, 1},
		{"null", `null`, 0},
		{"empty string", `""`, 0},
		{"large byte count", `8796093022208`, 8796093022208},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got FlexInt
			if err := json.Unmarshal([]byte(tc.input), &got); err != nil {
				t.Fatalf("Unmarshal(%s) returned error: %v", tc.input, err)
			}
			if got.Int() != tc.want {
				t.Errorf("Unmarshal(%s) = %d, want %d", tc.input, got.Int(), tc.want)
			}
		})
	}
}

func TestFlexIntUnmarshalError(t *testing.T) {
	for _, input := range []string{`"abc"`, `[1]`, `{"a":1}`} {
		var got FlexInt
		if err := json.Unmarshal([]byte(input), &got); err == nil {
			t.Errorf("Unmarshal(%s) = %d, want an error", input, got.Int())
		}
	}
}

func TestFlexFloatUnmarshal(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  float64
	}{
		{"number", `0.5`, 0.5},
		{"integer number", `1`, 1},
		{"cpu fraction", `0.042`, 0.042},
		{"string", `"0.5"`, 0.5},
		{"string integer", `"1"`, 1},
		{"bool true", `true`, 1},
		{"bool false", `false`, 0},
		{"null", `null`, 0},
		{"empty string", `""`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got FlexFloat
			if err := json.Unmarshal([]byte(tc.input), &got); err != nil {
				t.Fatalf("Unmarshal(%s) returned error: %v", tc.input, err)
			}
			if got.Float() != tc.want {
				t.Errorf("Unmarshal(%s) = %v, want %v", tc.input, got.Float(), tc.want)
			}
		})
	}
}

func TestFlexFloatUnmarshalError(t *testing.T) {
	for _, input := range []string{`"abc"`, `[0.5]`, `{}`} {
		var got FlexFloat
		if err := json.Unmarshal([]byte(input), &got); err == nil {
			t.Errorf("Unmarshal(%s) = %v, want an error", input, got.Float())
		}
	}
}

func TestFlexBoolUnmarshal(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"number one", `1`, true},
		{"number zero", `0`, false},
		{"other number", `2`, true},
		{"string one", `"1"`, true},
		{"string zero", `"0"`, false},
		{"bool true", `true`, true},
		{"bool false", `false`, false},
		{"string true", `"true"`, true},
		{"string false", `"false"`, false},
		{"float", `1.0`, true},
		{"null", `null`, false},
		{"empty string", `""`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got FlexBool
			if err := json.Unmarshal([]byte(tc.input), &got); err != nil {
				t.Fatalf("Unmarshal(%s) returned error: %v", tc.input, err)
			}
			if got.Bool() != tc.want {
				t.Errorf("Unmarshal(%s) = %v, want %v", tc.input, got.Bool(), tc.want)
			}
		})
	}
}

func TestFlexBoolUnmarshalError(t *testing.T) {
	for _, input := range []string{`"yolo"`, `[true]`, `{}`} {
		var got FlexBool
		if err := json.Unmarshal([]byte(input), &got); err == nil {
			t.Errorf("Unmarshal(%s) = %v, want an error", input, got.Bool())
		}
	}
}

// TestFlexAbsentField checks that a field missing from the payload keeps its
// zero value: UnmarshalJSON is not called at all in that case.
func TestFlexAbsentField(t *testing.T) {
	var r Resource
	if err := json.Unmarshal([]byte(`{"type":"node","node":"n1"}`), &r); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if r.MaxMem.Int() != 0 || r.CPU.Float() != 0 || r.Template.Bool() {
		t.Errorf("absent fields decoded to %d, %v, %v, want zero values",
			r.MaxMem.Int(), r.CPU.Float(), r.Template.Bool())
	}
}

func TestSplitTags(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"single", "web", []string{"web"}},
		{"semicolons", "pprd;web;edge", []string{"pprd", "web", "edge"}},
		{"commas", "pprd,web", []string{"pprd", "web"}},
		{"mixed separators", "pprd;web,edge", []string{"pprd", "web", "edge"}},
		{"empty elements", ";pprd;;web;", []string{"pprd", "web"}},
		{"spaces trimmed", "pprd; web ", []string{"pprd", "web"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SplitTags(tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("SplitTags(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestResourceStorageKey(t *testing.T) {
	shared := Resource{Type: ResourceTypeStorage, Node: "n1", Storage: "nfs", Shared: true}
	if got, want := shared.StorageKey(), "nfs"; got != want {
		t.Errorf("shared StorageKey() = %q, want %q", got, want)
	}
	local := Resource{Type: ResourceTypeStorage, Node: "n1", Storage: "local-lvm"}
	if got, want := local.StorageKey(), "n1/local-lvm"; got != want {
		t.Errorf("local StorageKey() = %q, want %q", got, want)
	}
}

// readFixture decodes the {"data": ...} envelope of a testdata file into v.
func readFixture(t *testing.T, name string, v interface{}) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decoding envelope of %s: %v", name, err)
	}
	if err := json.Unmarshal(envelope.Data, v); err != nil {
		t.Fatalf("decoding data of %s: %v", name, err)
	}
}

func TestClusterResourcesFixture(t *testing.T) {
	var resources []Resource
	readFixture(t, "cluster_resources.json", &resources)

	byID := make(map[string]Resource, len(resources))
	for _, r := range resources {
		byID[r.ID] = r
	}

	// A node whose numeric fields come as JSON numbers.
	node := byID["node/prox-pprd-2301-cit"]
	if node.Type != ResourceTypeNode || node.Status != StatusOnline {
		t.Errorf("node 2301: type %q status %q, want %q and %q",
			node.Type, node.Status, ResourceTypeNode, StatusOnline)
	}
	if got, want := node.CPU.Float(), 0.42; got != want {
		t.Errorf("node 2301 cpu = %v, want %v (a fraction, not a percentage)", got, want)
	}
	if got, want := node.MaxCPU.Int(), int64(32); got != want {
		t.Errorf("node 2301 maxcpu = %d, want %d", got, want)
	}
	if got, want := node.MaxMem.Int(), int64(103079215104); got != want {
		t.Errorf("node 2301 maxmem = %d, want %d bytes", got, want)
	}
	if got, want := node.Uptime.Int(), int64(3542400); got != want {
		t.Errorf("node 2301 uptime = %d, want %d seconds", got, want)
	}

	// The same fields, sent as JSON strings by the very same cluster.
	drained := byID["node/prox-pprd-2302-cit"]
	if got, want := drained.MaxCPU.Int(), int64(32); got != want {
		t.Errorf("node 2302 maxcpu from string = %d, want %d", got, want)
	}
	if got, want := drained.Mem.Int(), int64(8589934592); got != want {
		t.Errorf("node 2302 mem from string = %d, want %d", got, want)
	}
	if got, want := drained.CPU.Float(), 0.03; got != want {
		t.Errorf("node 2302 cpu from string = %v, want %v", got, want)
	}
	if got, want := drained.Uptime.Int(), int64(86400); got != want {
		t.Errorf("node 2302 uptime from string = %d, want %d", got, want)
	}

	// Guests: templates are serialised 0/1 and must be counted apart.
	var running, stopped, templates int
	for _, r := range resources {
		if !r.IsGuest() {
			continue
		}
		switch {
		case r.Template.Bool():
			templates++
		case r.Status == StatusRunning:
			running++
		default:
			stopped++
		}
	}
	if running != 3 || stopped != 1 || templates != 1 {
		t.Errorf("guests: %d running, %d stopped, %d templates; want 3, 1, 1",
			running, stopped, templates)
	}
	if tpl := byID["qemu/9000"]; !tpl.Template.Bool() {
		t.Error("qemu/9000 template = false, want true (serialised as 1)")
	}
	if vm := byID["qemu/101"]; vm.Template.Bool() {
		t.Error("qemu/101 template = true, want false (serialised as 0)")
	}
	if got, want := byID["qemu/102"].VMID.Int(), int64(102); got != want {
		t.Errorf("qemu/102 vmid from string = %d, want %d", got, want)
	}
	if got, want := byID["lxc/200"].TagList(), []string{"pprd", "net", "edge"}; !reflect.DeepEqual(got, want) {
		t.Errorf("lxc/200 tags = %#v, want %#v", got, want)
	}
	if got := byID["qemu/103"].TagList(); got != nil {
		t.Errorf("qemu/103 tags = %#v, want nil", got)
	}

	// The de-duplication trap: one shared storage, reported once per node.
	var sharedEntries int
	for _, r := range resources {
		if r.Type == ResourceTypeStorage && r.Storage == "nfs-shared" && r.Shared.Bool() {
			sharedEntries++
		}
	}
	if sharedEntries != 3 {
		t.Fatalf("nfs-shared appears %d times, want 3 (once per node)", sharedEntries)
	}
	totals := make(map[string]int64)
	for _, r := range resources {
		if r.Type != ResourceTypeStorage || r.Status != StatusAvailable {
			continue
		}
		totals[r.StorageKey()] = r.MaxDisk.Int()
	}
	if len(totals) != 4 {
		t.Errorf("de-duplicated storages = %d, want 4 (nfs-shared + 3 local-lvm)", len(totals))
	}
	var capacity int64
	for _, v := range totals {
		capacity += v
	}
	if want := int64(8796093022208 + 3*536870912000); capacity != want {
		t.Errorf("de-duplicated capacity = %d, want %d bytes", capacity, want)
	}
	if _, ok := totals["prox-pprd-2302-cit/backup-nfs"]; ok {
		t.Error("an unavailable storage was counted")
	}
}

func TestClusterStatusFixture(t *testing.T) {
	var entries []ClusterStatusEntry
	readFixture(t, "cluster_status.json", &entries)

	var cluster *ClusterStatusEntry
	nodes := make(map[string]ClusterStatusEntry)
	for i, e := range entries {
		switch e.Type {
		case ClusterStatusTypeCluster:
			cluster = &entries[i]
		case ClusterStatusTypeNode:
			nodes[e.Name] = e
		}
	}
	if cluster == nil {
		t.Fatal("no entry of type cluster, want one")
	}
	if !cluster.Quorate.Bool() {
		t.Error("quorate = false, want true (serialised as 1)")
	}
	if got, want := cluster.Nodes.Int(), int64(3); got != want {
		t.Errorf("cluster nodes = %d, want %d", got, want)
	}
	if len(nodes) != 3 {
		t.Fatalf("node entries = %d, want 3", len(nodes))
	}
	for name, n := range nodes {
		if !n.Online.Bool() {
			t.Errorf("node %s online = false, want true", name)
		}
	}
	local := nodes["prox-pprd-2301-cit"]
	if !local.Local.Bool() || local.NodeID.Int() != 1 || local.IP != "10.20.30.1" {
		t.Errorf("node 2301 = %+v, want the local node, nodeid 1, ip 10.20.30.1", local)
	}
	// The last node mixes the string forms of nodeid and online.
	last := nodes["prox-pprd-2303-cit"]
	if got, want := last.NodeID.Int(), int64(3); got != want {
		t.Errorf("node 2303 nodeid from string = %d, want %d", got, want)
	}
	if !last.Online.Bool() {
		t.Error(`node 2303 online = false, want true (serialised as "1")`)
	}
}

func TestHAManagerStatusFixture(t *testing.T) {
	var status HAManagerStatus
	readFixture(t, "ha_manager_status.json", &status)

	want := map[string]string{
		"prox-pprd-2301-cit": HANodeOnline,
		"prox-pprd-2302-cit": HANodeMaintenance,
		"prox-pprd-2303-cit": HANodeOnline,
	}
	if !reflect.DeepEqual(status.NodeStatus, want) {
		t.Errorf("node_status = %#v, want %#v", status.NodeStatus, want)
	}
	if got := status.NodeState("prox-pprd-2302-cit"); got != HANodeMaintenance {
		t.Errorf("NodeState(2302) = %q, want %q", got, HANodeMaintenance)
	}
	if got := status.NodeState("prox-pprd-9999-cit"); got != HANodeUnknown {
		t.Errorf("NodeState(absent node) = %q, want %q", got, HANodeUnknown)
	}
	if got, want := status.MasterNode, "prox-pprd-2301-cit"; got != want {
		t.Errorf("master_node = %q, want %q", got, want)
	}
	if got, want := status.Timestamp.Int(), int64(1757671200); got != want {
		t.Errorf("timestamp = %d, want %d", got, want)
	}
}

// TestHAManagerStatusShapes covers the two shapes the endpoint has been seen
// in: a flat object, and one nesting the manager state under manager_status.
func TestHAManagerStatusShapes(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{
			name:  "flat",
			input: `{"node_status":{"n1":"maintenance"},"manager_status":"master","timestamp":42}`,
		},
		{
			name:  "nested",
			input: `{"manager_status":{"node_status":{"n1":"maintenance"},"manager_status":"master","timestamp":42}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var status HAManagerStatus
			if err := json.Unmarshal([]byte(tc.input), &status); err != nil {
				t.Fatalf("Unmarshal returned error: %v", err)
			}
			if got := status.NodeState("n1"); got != HANodeMaintenance {
				t.Errorf("NodeState(n1) = %q, want %q", got, HANodeMaintenance)
			}
			if got, want := status.ManagerStatus, "master"; got != want {
				t.Errorf("manager_status = %q, want %q", got, want)
			}
			if got, want := status.Timestamp.Int(), int64(42); got != want {
				t.Errorf("timestamp = %d, want %d", got, want)
			}
		})
	}
}

// TestHAManagerStatusEmpty checks that a cluster without HA decodes cleanly:
// no node in maintenance, no error.
func TestHAManagerStatusEmpty(t *testing.T) {
	var status HAManagerStatus
	if err := json.Unmarshal([]byte(`{}`), &status); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if len(status.NodeStatus) != 0 {
		t.Errorf("node_status = %#v, want empty", status.NodeStatus)
	}
	if got := status.NodeState("n1"); got != HANodeUnknown {
		t.Errorf("NodeState(n1) = %q, want %q", got, HANodeUnknown)
	}
}

func TestAptUpdateFixture(t *testing.T) {
	var updates []AptUpdate
	readFixture(t, "apt_update.json", &updates)

	if len(updates) != 4 {
		t.Fatalf("pending packages = %d, want 4", len(updates))
	}
	first := updates[0]
	if first.Package != PackagePVEManager {
		t.Errorf("first package = %q, want %q", first.Package, PackagePVEManager)
	}
	if first.OldVersion != "9.2.9" || first.Origin != "Proxmox" || first.Arch != "amd64" {
		t.Errorf("pve-manager = %+v, want old version 9.2.9 from Proxmox on amd64", first)
	}
	version, ok := PVEManagerVersion(updates)
	if !ok || version != "9.2.12" {
		t.Errorf("PVEManagerVersion() = %q, %v; want %q, true", version, ok, "9.2.12")
	}
	if _, ok := PVEManagerVersion(updates[1:]); ok {
		t.Error("PVEManagerVersion() without pve-manager = true, want false")
	}
	if _, ok := PVEManagerVersion(nil); ok {
		t.Error("PVEManagerVersion(nil) = true, want false")
	}
}
