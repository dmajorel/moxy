package metrics

import "strings"

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
