package aggregate

import (
	"testing"

	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

// TestNodeStatusOf is the rule the overview and the node view now share. It
// lived in internal/detail, against a copy of this function; the copy is gone
// and the table came with it, because a rule tested on one of two definitions
// is a rule tested on neither.
func TestNodeStatusOf(t *testing.T) {
	status := []proxmox.ClusterStatusEntry{
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
		want NodeStatus
	}{
		{name: "online", node: "up", ha: ha, want: NodeOnline},
		{name: "offline", node: "down", ha: ha, want: NodeOffline},
		{name: "maintenance wins over the online it still reports", node: "draining", ha: ha, want: NodeMaintenance},
		{name: "without the ha manager a draining node is only online", node: "draining", ha: nil, want: NodeOnline},
		{name: "absent from both sources", node: "ghost", ha: ha, want: NodeUnknown},
		// A node the CRM drains but /cluster/status has not heard of is still
		// in maintenance: the manager is the authority on that one word.
		{name: "maintenance wins over absence too", node: "unlisted", ha: &proxmox.HAManagerStatus{
			NodeStatus: map[string]string{"unlisted": proxmox.HANodeMaintenance},
		}, want: NodeMaintenance},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := NodeStatusOf(tt.node, status, tt.ha); got != tt.want {
				t.Fatalf("NodeStatusOf = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNodeStatusFromAgreesWithNodeStatusOf: the poller reads the two sources
// as it walks them and calls NodeStatusFrom; every other caller hands over the
// payloads and calls NodeStatusOf. The two must answer the same thing, or the
// split that exists for performance would reintroduce the very divergence the
// shared rule was written to kill.
func TestNodeStatusFromAgreesWithNodeStatusOf(t *testing.T) {
	ha := &proxmox.HAManagerStatus{NodeStatus: map[string]string{
		"drained": proxmox.HANodeMaintenance,
	}}

	cases := []struct {
		node     string
		status   []proxmox.ClusterStatusEntry
		inStatus bool
		online   bool
	}{
		{node: "up", status: []proxmox.ClusterStatusEntry{{Type: proxmox.ClusterStatusTypeNode, Name: "up", Online: true}}, inStatus: true, online: true},
		{node: "down", status: []proxmox.ClusterStatusEntry{{Type: proxmox.ClusterStatusTypeNode, Name: "down", Online: false}}, inStatus: true, online: false},
		{node: "ghost", status: nil, inStatus: false, online: false},
		{node: "drained", status: []proxmox.ClusterStatusEntry{{Type: proxmox.ClusterStatusTypeNode, Name: "drained", Online: true}}, inStatus: true, online: true},
		{node: "drained", status: nil, inStatus: false, online: false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.node, func(t *testing.T) {
			from := NodeStatusFrom(c.node == "drained", c.inStatus, c.online)
			of := NodeStatusOf(c.node, c.status, ha)
			if from != of {
				t.Errorf("NodeStatusFrom = %q, NodeStatusOf = %q", from, of)
			}
		})
	}
}

func TestQuorumOf(t *testing.T) {
	tests := []struct {
		name   string
		status []proxmox.ClusterStatusEntry
		want   *Quorum
	}{
		{
			name: "a quorate cluster counts the nodes that are up",
			status: []proxmox.ClusterStatusEntry{
				{Type: proxmox.ClusterStatusTypeCluster, Quorate: true, Nodes: 3},
				{Type: proxmox.ClusterStatusTypeNode, Name: "a", Online: true},
				{Type: proxmox.ClusterStatusTypeNode, Name: "b", Online: true},
				{Type: proxmox.ClusterStatusTypeNode, Name: "c", Online: false},
			},
			want: &Quorum{Quorate: true, Nodes: 3, Online: 2},
		},
		{
			// A standalone node sends node entries only. Its quorum is not
			// lost, it does not exist, and a one-node vote invented here would
			// put a healthy machine in the degraded column forever.
			name: "a standalone node has no quorum at all",
			status: []proxmox.ClusterStatusEntry{
				{Type: proxmox.ClusterStatusTypeNode, Name: "solo", Online: true},
			},
			want: nil,
		},
		{name: "nothing at all", status: nil, want: nil},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := QuorumOf(tt.status)
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("QuorumOf = %+v, want nil", got)
			case tt.want == nil:
			case got == nil:
				t.Fatalf("QuorumOf = nil, want %+v", tt.want)
			case *got != *tt.want:
				t.Fatalf("QuorumOf = %+v, want %+v", *got, *tt.want)
			}
		})
	}
}

func TestGuestKindOf(t *testing.T) {
	if got := GuestKindOf(proxmox.ResourceTypeLXC); got != GuestLXC {
		t.Errorf("lxc = %q, want %q", got, GuestLXC)
	}
	if got := GuestKindOf(proxmox.ResourceTypeQemu); got != GuestQemu {
		t.Errorf("qemu = %q, want %q", got, GuestQemu)
	}
	// Anything that is not a container is a virtual machine: PVE adds
	// resource types over time, and a guest with an unknown kind must still
	// render rather than fall out of the list.
	if got := GuestKindOf("something-else"); got != GuestQemu {
		t.Errorf("unknown = %q, want %q", got, GuestQemu)
	}
}

func TestGuestStatusOf(t *testing.T) {
	tests := []struct {
		name     string
		template bool
		status   string
		want     GuestStatus
	}{
		{name: "running", status: proxmox.StatusRunning, want: GuestRunning},
		{name: "stopped", status: proxmox.StatusStopped, want: GuestStopped},
		{name: "anything else is stopped", status: "paused", want: GuestStopped},
		{name: "template", template: true, status: proxmox.StatusStopped, want: GuestTemplate},
		// PVE has been seen reporting a template as running. It is still a
		// template, and the counts of the card say so: a view that disagreed
		// would show one more running guest than its own node card.
		{name: "a template reported running is still a template", template: true, status: proxmox.StatusRunning, want: GuestTemplate},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := GuestStatusOf(tt.template, tt.status); got != tt.want {
				t.Errorf("GuestStatusOf(%v, %q) = %q, want %q", tt.template, tt.status, got, tt.want)
			}
			r := proxmox.Resource{Status: tt.status}
			if tt.template {
				r.Template = proxmox.FlexBool(true)
			}
			if got := GuestStatusOfResource(r); got != tt.want {
				t.Errorf("GuestStatusOfResource = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUsageOfAndAsBytes(t *testing.T) {
	// The ratio of an empty total is zero, not NaN: encoding/json refuses a
	// NaN outright, so the whole response would be a 500 in place of a page.
	if got := UsageOf(0, 0); got.Ratio != 0 {
		t.Errorf("UsageOf(0, 0).Ratio = %v, want 0", got.Ratio)
	}
	if got := UsageOf(5, 0); got.Ratio != 0 {
		t.Errorf("UsageOf(5, 0).Ratio = %v, want 0", got.Ratio)
	}
	if got := UsageOf(1, 4); got.Ratio != 0.25 || got.Used != 1 || got.Total != 4 {
		t.Errorf("UsageOf(1, 4) = %+v", got)
	}

	// PVE has no negative sizes, but a tolerant decode could produce one, and
	// an unsigned conversion would turn it into an absurdly large total.
	if got := AsBytes(-1); got != 0 {
		t.Errorf("AsBytes(-1) = %d, want 0", got)
	}
	if got := AsBytes(1 << 40); got != 1<<40 {
		t.Errorf("AsBytes(1 TiB) = %d", got)
	}
}
