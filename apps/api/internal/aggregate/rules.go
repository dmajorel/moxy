package aggregate

import (
	"strings"

	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

// Rules of the overview that the per-object views must follow to the letter.
//
// The two families of routes describe the SAME objects. An operator who reads
// "en maintenance" on a card and "hors ligne" on that node's own page has been
// lied to by one of the two, and there is no way to tell which. These rules
// used to be stated twice — once here, once in internal/detail — under a
// comment asking that they never be changed on one side alone. Nothing
// enforced that; this file does, by leaving one definition to change.
//
// Everything here is pure: no clock, no network, no state. That is what lets
// both packages call it from the middle of their own derivations.

// NodeStatusOf decides the state of one node from the two authoritative
// sources: the HA manager status, and /cluster/status.
//
// MAINTENANCE WINS. A node being drained still answers /cluster/status as
// online, and reporting it as online would hide the very state the screen
// exists to show. It is not offline either — it runs, it holds its guests, it
// merely refuses new ones.
//
// A node known from /cluster/resources ALONE is unknown, never assumed online:
// PVE keeps a stale entry there for a node that has just left, and it lists a
// node that has just joined before either authoritative source mentions it.
func NodeStatusOf(name string, status []proxmox.ClusterStatusEntry, ha *proxmox.HAManagerStatus) NodeStatus {
	for _, e := range status {
		if e.Type != proxmox.ClusterStatusTypeNode || e.Name != name {
			continue
		}
		return NodeStatusFrom(inMaintenance(name, ha), true, e.Online.Bool())
	}
	return NodeStatusFrom(inMaintenance(name, ha), false, false)
}

// NodeStatusFrom is NodeStatusOf for a caller that has already read the two
// sources — the poller, which accumulates them per node as it walks the
// payloads rather than scanning the list once per node.
func NodeStatusFrom(maintenance, inStatus, online bool) NodeStatus {
	if maintenance {
		return NodeMaintenance
	}
	if !inStatus {
		return NodeUnknown
	}
	if online {
		return NodeOnline
	}
	return NodeOffline
}

// inMaintenance reports whether the CRM says this node is being drained.
func inMaintenance(name string, ha *proxmox.HAManagerStatus) bool {
	return ha != nil && ha.NodeState(name) == proxmox.HANodeMaintenance
}

// QuorumOf reads corosync quorum from /cluster/status.
//
// A standalone node returns node entries only, with no "cluster" entry: its
// quorum is not lost, it simply does not exist, and nil is how the payload
// says so. Reporting a one-node vote instead would put a healthy machine in
// the degraded column forever.
func QuorumOf(status []proxmox.ClusterStatusEntry) *Quorum {
	var q *Quorum
	online := 0
	for _, e := range status {
		switch e.Type {
		case proxmox.ClusterStatusTypeCluster:
			if q == nil {
				q = &Quorum{Quorate: e.Quorate.Bool(), Nodes: int(e.Nodes.Int())}
			}
		case proxmox.ClusterStatusTypeNode:
			if e.Online.Bool() {
				online++
			}
		}
	}
	if q == nil {
		return nil
	}
	q.Online = online
	return q
}

// GuestKindOf maps a PVE resource type to the kind of guest it denotes.
// Anything that is not an LXC container is a virtual machine.
func GuestKindOf(resourceType string) GuestKind {
	if resourceType == proxmox.ResourceTypeLXC {
		return GuestLXC
	}
	return GuestQemu
}

// GuestStatusOf decides the state of one guest.
//
// BEING A TEMPLATE WINS over the reported state, exactly as in the counts of
// deriveVMs: a template PVE happens to report as running is still a template,
// and a view that counted it among the running guests would disagree with the
// card of its own node.
func GuestStatusOf(template bool, status string) GuestStatus {
	if template {
		return GuestTemplate
	}
	if status == proxmox.StatusRunning {
		return GuestRunning
	}
	return GuestStopped
}

// GuestStatusOfResource is GuestStatusOf read from a /cluster/resources row.
func GuestStatusOfResource(r proxmox.Resource) GuestStatus {
	return GuestStatusOf(r.Template.Bool(), r.Status)
}

// UsageOf pairs a used and a total with the ratio between them, which is ZERO
// when the total is: a division by zero would put a NaN in the payload, and
// NaN is not valid JSON — encoding/json refuses it and the whole response
// fails, which is a 500 in place of a page.
func UsageOf(used, total uint64) Usage {
	u := Usage{Used: used, Total: total}
	if total > 0 {
		u.Ratio = float64(used) / float64(total)
	}
	return u
}

// AsBytes clamps a byte count to zero. PVE has no negative sizes, but a
// tolerant decode of an unexpected payload could produce one, and an unsigned
// conversion would turn it into an absurdly large total.
func AsBytes(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}

// PVEVersionOf turns the pveversion banner a node reports into the bare
// version number it carries: "pve-manager/9.2.9/ec4c0cbd8a1d5b3a" becomes
// "9.2.9".
//
// PVE answers with the banner, never with the number. Both the cluster card
// and the node page display that version, so the cut belongs here rather than
// in either of them: the two views must write the same string for the same
// node, and a split performed twice is a split that will diverge once.
//
// An empty banner is unknown, hence nil — an offline node reports none, and
// neither does a node the token may not audit. An unexpected shape is returned
// WHOLE rather than dropped: this is a display value and never a comparison
// key, so showing something odd beats showing nothing at all.
func PVEVersionOf(banner string) *string {
	banner = strings.TrimSpace(banner)
	if banner == "" {
		return nil
	}
	// "pve-manager/<version>/<commit>". The commit is of no interest here, and
	// the package name is the same on every node.
	const prefix = "pve-manager/"
	if strings.HasPrefix(banner, prefix) {
		if version, _, found := strings.Cut(banner[len(prefix):], "/"); found && version != "" {
			return &version
		}
	}
	return &banner
}
