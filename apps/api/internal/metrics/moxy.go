package metrics

import (
	"strings"
	"time"
)

// The metric set of moxyd, declared once.
//
// Names follow the Prometheus conventions the ecosystem assumes: a moxy_
// prefix, a base unit in the name (_seconds, never milliseconds), _total on a
// counter, and a _timestamp_seconds gauge for "when did this last happen".
//
// Every label value below comes from a closed set. That is the whole design
// constraint: a metric endpoint is scraped every fifteen seconds forever, and
// a label taking a node name or a vmid would grow a time series per object of
// every cluster — the failure mode that takes a Prometheus down. It is also
// the rule that keeps host names out of a document that leaves the process.
var (
	// PVERequests counts the calls this daemon makes upstream, by cluster,
	// by the kind of endpoint, and by how they ended. It is what answers "how
	// much am I asking of my cluster", which the log cannot.
	PVERequests = Default.CounterVec(
		"moxy_pve_requests_total",
		"Calls made to a Proxmox cluster, by endpoint kind and outcome.",
		"cluster", "path_kind", "outcome",
	)

	// PVERequestSeconds is how long those calls take. The buckets are fixed
	// and few: they straddle the per-call timeout, which is what a reader
	// actually wants to know about — whether a cluster is answering in
	// milliseconds or grinding towards its budget.
	PVERequestSeconds = Default.HistogramVec(
		"moxy_pve_request_seconds",
		"Duration of the calls made to a Proxmox cluster.",
		[]float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		"cluster", "path_kind",
	)

	// PollLastSuccess is when a cluster last answered a full poll round. It
	// is the metric behind "why did the preproduction card go unreachable at
	// 03:12": the gauge stops moving at the moment it stopped answering.
	PollLastSuccess = Default.GaugeVec(
		"moxy_poll_last_success_timestamp_seconds",
		"UNIX time of the last successful poll of a cluster.",
		"cluster",
	)

	// ClusterStatus is the verdict of each card, one series per status with a
	// 0 or a 1. Three series per cluster rather than one numbered status: an
	// enum encoded as a number cannot be aggregated, and "how many clusters
	// are degraded" is a sum over a label.
	ClusterStatus = Default.GaugeVec(
		"moxy_cluster_status",
		"Current status of a cluster: 1 for the one it is in, 0 for the others.",
		"cluster", "status",
	)

	// DetailCacheEvents counts what the on-demand caches did with a request:
	// served it from memory, went upstream for it, or joined a call already in
	// flight. The third is the anti-stampede lock earning its keep, and it is
	// invisible from anywhere else.
	DetailCacheEvents = Default.CounterVec(
		"moxy_detail_cache_events_total",
		"Lookups in the on-demand detail caches, by cache and outcome.",
		"cache", "event",
	)

	// MaintenanceCommands counts the node maintenance commands this daemon
	// ran over SSH, by cluster, by action, and by how they ended. It is the
	// only trace of an action that leaves no PVE task behind, and the series
	// an operator watches after wiring the route to a button.
	//
	// The node is DELIBERATELY not a label. It is the one identifier a reader
	// would ask for and the one this exposition must not carry: a fleet of a
	// few hundred nodes would grow a series each, and a node name in a
	// scraped document is a host name leaving the process. The node of a
	// given command is in the task log and in the audit trail, where it
	// belongs.
	MaintenanceCommands = Default.CounterVec(
		"moxy_maintenance_commands_total",
		"Node maintenance commands run over SSH, by action and outcome.",
		"cluster", "action", "outcome",
	)

	// MaintenanceCommandSeconds is how long those commands take, end to end:
	// minting the credential, opening the session, and running the command.
	//
	// It carries the outcome, unlike the PVE histogram, because the failures
	// are what the budgets are set against — a timeout lands in the last
	// bucket and a refused command in the first, and averaging the two would
	// hide both. The cardinality stays bounded all the same: two actions and
	// one closed set of outcomes per cluster.
	//
	// The buckets straddle the SSH budgets of the configuration: a 2s dial, a
	// 20s run by default, 60s at the very most.
	MaintenanceCommandSeconds = Default.HistogramVec(
		"moxy_maintenance_command_seconds",
		"Duration of a node maintenance command, credential minting included.",
		[]float64{0.5, 1, 2.5, 5, 10, 20, 30, 60},
		"cluster", "action", "outcome",
	)

	// MaintenanceKeySource counts the attempts to obtain the credential one
	// session is opened with: a key read from disk, or a certificate minted
	// by OpenBao. It separates "the key source is down" from "the node is
	// down", which are one failure from the user's seat and two to fix.
	//
	// There is no cluster label: the key source is process-wide, exactly as
	// the maintenance mode is. And the ADDRESS of OpenBao appears NOWHERE —
	// not as a label, not in the help text. Same rule as the node name, same
	// two reasons.
	MaintenanceKeySource = Default.CounterVec(
		"moxy_maintenance_keysource_total",
		"Attempts to obtain the credential of a maintenance session, by mode and outcome.",
		"mode", "outcome",
	)

	// BuildInfo is the usual constant-1 gauge carrying the version in a label,
	// so that a dashboard can annotate a deployment.
	BuildInfo = Default.GaugeVec(
		"moxy_build_info",
		"Build identity of the running daemon; the value is always 1.",
		"version",
	)
)

// Cache event labels.
const (
	// EventHit is an answer served from memory.
	EventHit = "hit"
	// EventMiss is a call that had to go upstream.
	EventMiss = "miss"
	// EventJoin is a caller that waited on a call someone else had started —
	// the anti-stampede lock doing its job.
	EventJoin = "join"
)

// Path kinds. They name a FAMILY of endpoint, never one object: the point of
// the classification is that a thousand guests produce one label value.
const (
	PathResources   = "resources"
	PathStatus      = "status"
	PathHA          = "ha"
	PathApt         = "apt"
	PathNodeStatus  = "node_status"
	PathGuestStatus = "guest_status"
	PathGuestConfig = "guest_config"
	PathRRD         = "rrd"
	PathTasks       = "tasks"
	PathAgent       = "agent"
	PathSDN         = "sdn"
	PathNetwork     = "network"
	// PathOther is the fallback. It exists so that a route added without a
	// case here still counts, under a label that says it was not classified.
	PathOther = "other"
)

// ClassifyPath maps a relative PVE path to one of the kinds above.
//
// It works on the SHAPE of the path, never on its content: "/nodes/pve-3/
// status" and "/nodes/pve-7/status" are one label value, and the node name is
// dropped rather than carried. That is not only a cardinality choice — a node
// name in a scraped document is a host name leaving the process, which the
// rest of this daemon is careful never to do.
func ClassifyPath(path string) string {
	// The query string is never part of the kind: it carries the timeframe,
	// the vmid and the limit.
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	switch {
	case path == "/cluster/resources":
		return PathResources
	case path == "/cluster/status":
		return PathStatus
	case strings.HasPrefix(path, "/cluster/ha/"):
		return PathHA
	case strings.HasPrefix(path, "/cluster/sdn/"):
		return PathSDN
	case strings.HasSuffix(path, "/network"):
		return PathNetwork
	case path == "/cluster/tasks" || strings.HasSuffix(path, "/tasks"):
		return PathTasks
	case strings.HasSuffix(path, "/apt/update"):
		return PathApt
	case strings.HasSuffix(path, "/rrddata"):
		return PathRRD
	case strings.Contains(path, "/agent/"):
		return PathAgent
	case strings.HasSuffix(path, "/config"):
		return PathGuestConfig
	case strings.HasSuffix(path, "/status"):
		// Everything left ending in /status is per-object, and a guest path
		// carries its kind: /nodes/{node}/qemu/{vmid}/status.
		if strings.Contains(path, "/qemu/") || strings.Contains(path, "/lxc/") {
			return PathGuestStatus
		}
		return PathNodeStatus
	}
	return PathOther
}

// OutcomeOK labels a call that succeeded. The failing outcomes are the Kind
// values of the proxmox package, passed through as they are: the two
// vocabularies must not drift, and this package cannot import that one
// without a cycle.
const OutcomeOK = "ok"

// Unclassified is what a label value outside its closed set folds to. It is
// never dropped and never passed through: a miswired call must still count,
// under a value that says it was not one of the declared ones — the same
// choice as PathOther, for the same reason.
const Unclassified = "other"

// Maintenance actions. The command sent to a node is "node-maintenance
// <action> <node>", and the action is the half of it that may be a label.
const (
	ActionEnable  = "enable"
	ActionDisable = "disable"
)

// Maintenance key source modes, mirroring the configuration.
const (
	KeySourceSSHKey  = "ssh-key"
	KeySourceOpenBao = "openbao"
)

// Maintenance outcomes. They mirror the Kind vocabulary of the maintenance
// package, plus OutcomeOK, and are written out here rather than imported:
// that package counts what it does through this one, so importing it back
// would be a cycle. The two lists must not drift, which is why they are
// spelled identically and asserted in the test.
const (
	OutcomeForbidden            = "maintenance_forbidden"
	OutcomeNoQuorum             = "no_quorum"
	OutcomeNoHAManager          = "no_ha_manager"
	OutcomeNoOtherNode          = "no_other_node"
	OutcomeAlreadyRunning       = "already_running"
	OutcomeKeySourceUnavailable = "keysource_unavailable"
	OutcomeKeySourceDenied      = "keysource_denied"
	OutcomeUnreachable          = "ssh_unreachable"
	OutcomeHostKeyMismatch      = "ssh_host_key_mismatch"
	OutcomeAuthFailed           = "ssh_auth_failed"
	OutcomeTimeout              = "ssh_timeout"
	OutcomeCommandRefused       = "command_refused"
	OutcomeCommandFailed        = "command_failed"
)

// maintenanceOutcomes is the closed set the outcome label is taken from. A
// map rather than a switch so that the test can walk it: what must be proven
// is that the set is closed, not that a particular value is in it.
var maintenanceOutcomes = map[string]bool{
	OutcomeOK:                   true,
	OutcomeForbidden:            true,
	OutcomeNoQuorum:             true,
	OutcomeNoHAManager:          true,
	OutcomeNoOtherNode:          true,
	OutcomeAlreadyRunning:       true,
	OutcomeKeySourceUnavailable: true,
	OutcomeKeySourceDenied:      true,
	OutcomeUnreachable:          true,
	OutcomeHostKeyMismatch:      true,
	OutcomeAuthFailed:           true,
	OutcomeTimeout:              true,
	OutcomeCommandRefused:       true,
	OutcomeCommandFailed:        true,
}

// MaintenanceOutcome folds an outcome to the closed set above.
//
// The argument is a Kind of the maintenance package, or OutcomeOK. Anything
// else — an error string that reached here by accident, a Kind added without
// a constant here — becomes Unclassified rather than a label value of its
// own: an error message carries a host name, a path and a cause, and none of
// the three may become a time series.
func MaintenanceOutcome(outcome string) string {
	if maintenanceOutcomes[outcome] {
		return outcome
	}
	return Unclassified
}

// MaintenanceAction folds an action to {enable, disable}.
func MaintenanceAction(action string) string {
	if action == ActionEnable || action == ActionDisable {
		return action
	}
	return Unclassified
}

// KeySourceMode folds a mode to {ssh-key, openbao}.
func KeySourceMode(mode string) string {
	if mode == KeySourceSSHKey || mode == KeySourceOpenBao {
		return mode
	}
	return Unclassified
}

// RecordMaintenanceCommand counts one maintenance command and records how
// long it took, under one folded label set. The two families move together
// because a count without a duration, or the reverse, makes the pair
// unreadable; the caller therefore has one call to make and one chance to
// pass the labels through the closed sets.
func RecordMaintenanceCommand(cluster, action, outcome string, d time.Duration) {
	a, o := MaintenanceAction(action), MaintenanceOutcome(outcome)
	MaintenanceCommands.Inc(cluster, a, o)
	MaintenanceCommandSeconds.Duration(d, cluster, a, o)
}

// RecordKeySource counts one attempt at obtaining a credential. Neither the
// address of the key source nor the identity it authenticated with is a
// label, ever.
func RecordKeySource(mode, outcome string) {
	MaintenanceKeySource.Inc(KeySourceMode(mode), MaintenanceOutcome(outcome))
}
