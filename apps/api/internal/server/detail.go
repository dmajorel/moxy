package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmajorel/moxy/apps/api/internal/detail"
	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
)

// detailPrefix is the ServeMux pattern the per-object views are mounted under.
//
// Go 1.19 has no path parameters — they landed in 1.22 — so the prefix is
// registered as a subtree and everything after it is split by hand in
// matchDetailPath. Nothing below relies on ServeMux having validated the shape
// of the path.
const detailPrefix = "/api/clusters/"

// The bounds of the two query parameters this layer parses. None of them is
// defined here: a second copy of a number is a second definition, and the two
// drift the day one of them is tuned.
//
// The window comes from proxmox, which is the package that knows which strings
// the RRD endpoints accept; the task limits from detail, which is what applies
// them. An unknown timeframe is a 400 HERE, and the accepted string is the
// only thing that can reach the hypervisor — never a request upstream to learn
// what a closed set already says.
const (
	defaultTimeframe = proxmox.TimeframeHour
	defaultTaskLimit = detail.DefaultTaskLimit
	maxTaskLimit     = detail.MaxTaskLimit
)

// DetailSource serves the per-object views. It is implemented by
// detail.Service; the interface keeps the HTTP layer testable without one.
type DetailSource interface {
	Node(ctx context.Context, cluster, node string) (*detail.Node, error)
	Guest(ctx context.Context, cluster string, vmid int) (*detail.Guest, error)
	NodeSeries(ctx context.Context, cluster, node, timeframe string) (*detail.Series, error)
	// ClusterSeries is the history the overview card draws in place of a pair
	// of gauges. PVE has no cluster-wide RRD: it is folded from the nodes.
	ClusterSeries(ctx context.Context, cluster, timeframe string) (*detail.Series, error)
	GuestSeries(ctx context.Context, cluster string, vmid int, timeframe string) (*detail.Series, error)
	Tasks(ctx context.Context, cluster string, limit int) (*detail.Tasks, error)
	// GuestTasks is the log of one guest, read from its hosting node rather
	// than sieved out of Tasks: the cluster log takes no filter, so a busy
	// cluster pushes a guest's own lines out of any tail worth fetching.
	GuestTasks(ctx context.Context, cluster string, vmid, limit int) (*detail.Tasks, error)
	// MaintenancePlan is read-only: it says what draining a node would entail,
	// and changes nothing. Executing the drain is not part of this interface,
	// and cannot be: PVE exposes no REST route for node maintenance.
	MaintenancePlan(ctx context.Context, cluster, node string) (*detail.MaintenancePlan, error)
}

// detailKind is which of the views a request matched.
type detailKind int

const (
	routeNode detailKind = iota
	routeNodeSeries
	routeGuest
	routeGuestSeries
	routeTasks
	routeGuestTasks
	routeClusterSeries
	routeMaintenancePlan
)

// String names the route in log lines. It never carries user input.
func (k detailKind) String() string {
	switch k {
	case routeNode:
		return "node"
	case routeNodeSeries:
		return "node rrd"
	case routeGuest:
		return "guest"
	case routeGuestSeries:
		return "guest rrd"
	case routeTasks:
		return "tasks"
	case routeGuestTasks:
		return "guest tasks"
	case routeClusterSeries:
		return "cluster rrd"
	case routeMaintenancePlan:
		return "maintenance plan"
	}
	return "unknown"
}

// detailPath is a request whose path matched one of the routes matchDetailPath
// lists.
//
// The path segments are filled in by matchDetailPath, already percent-decoded
// and checked. vmidRaw stays text at that point because a malformed vmid is a
// 400, and that must not be decided before the method check; the remaining
// fields are filled in by the handler once the query string is validated.
type detailPath struct {
	kind    detailKind
	cluster string
	node    string
	vmidRaw string

	vmid      int
	timeframe string
	limit     int
}

func handleDetail(src DetailSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// These routes describe live infrastructure, error answers included: a
		// cached 404 would outlive the guest that was being created.
		w.Header().Set("Cache-Control", "no-store")

		p, ok := matchDetailPath(r.URL.EscapedPath())
		if !ok {
			// No route has this shape. Answering 404 here keeps the /api/
			// namespace closed exactly as handleNotFound does elsewhere.
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if !isReadMethod(r.Method) {
			w.Header().Set("Allow", allowReadMethods)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if p.vmidRaw != "" {
			vmid, err := parseVMID(p.vmidRaw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "vmid must be a positive integer")
				return
			}
			p.vmid = vmid
		}
		if p.kind == routeNodeSeries || p.kind == routeGuestSeries || p.kind == routeClusterSeries {
			timeframe, err := parseTimeframe(r.URL.Query().Get("timeframe"))
			if err != nil {
				writeError(w, http.StatusBadRequest, "timeframe must be one of hour, day, week, month, year")
				return
			}
			p.timeframe = timeframe
		}
		if p.kind == routeTasks || p.kind == routeGuestTasks {
			limit, err := parseLimit(r.URL.Query().Get("limit"))
			if err != nil {
				writeError(w, http.StatusBadRequest, "limit must be a positive integer")
				return
			}
			p.limit = limit
		}

		if src == nil {
			// Mock mode configures no source: the routes exist but nothing can
			// answer them. 501 says exactly that, and keeps the frontend from
			// reading the answer as "this node does not exist".
			writeError(w, http.StatusNotImplemented, "detail views are not available without a cluster connection")
			return
		}

		payload, err := fetchDetail(r.Context(), src, p)
		if err != nil {
			writeDetailError(w, r, p, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

// fetchDetail calls the single source method the matched route names.
func fetchDetail(ctx context.Context, src DetailSource, p detailPath) (interface{}, error) {
	switch p.kind {
	case routeNode:
		return found(src.Node(ctx, p.cluster, p.node))
	case routeNodeSeries:
		return found(src.NodeSeries(ctx, p.cluster, p.node, p.timeframe))
	case routeClusterSeries:
		return found(src.ClusterSeries(ctx, p.cluster, p.timeframe))
	case routeGuest:
		return found(src.Guest(ctx, p.cluster, p.vmid))
	case routeGuestSeries:
		return found(src.GuestSeries(ctx, p.cluster, p.vmid, p.timeframe))
	case routeTasks:
		return found(src.Tasks(ctx, p.cluster, p.limit))
	case routeGuestTasks:
		return found(src.GuestTasks(ctx, p.cluster, p.vmid, p.limit))
	case routeMaintenancePlan:
		return found(src.MaintenancePlan(ctx, p.cluster, p.node))
	}
	// Unreachable: matchDetailPath returns no other kind.
	return nil, detail.ErrNotFound
}

// found guards against a source that returns neither an error nor a value: a
// typed nil pointer would be encoded as the JSON literal null under a 200, which
// the frontend cannot tell from a real payload. It is reported as a missing
// object instead.
func found[T any](v *T, err error) (interface{}, error) {
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, detail.ErrNotFound
	}
	return v, nil
}

// writeDetailError maps a source failure onto the API's error shape.
//
// Only two answers ever leave this function. An upstream error may name an
// internal host or quote a hypervisor reply, so it is logged and replaced by a
// fixed message, exactly as handleOverview does.
func writeDetailError(w http.ResponseWriter, r *http.Request, p detailPath, err error) {
	if errors.Is(err, detail.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	// The caller got the request wrong, not the object. This layer parses the
	// query itself and answers 400 before ever calling the service, so nothing
	// reaches here today; it is wired all the same, because the alternative is
	// a future caller told 404 about a cluster that is perfectly well there.
	if errors.Is(err, detail.ErrInvalidArgument) {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// The caller went away -- closed the tab, navigated on. Nothing failed
	// upstream, nobody is waiting for an answer, and logging it as an outage
	// filled the journal with false alarms on every quick navigation.
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		return
	}

	log.Printf("detail %s unavailable for cluster %q: %v", p.kind, p.cluster, err)

	// A refusal upstream is not an outage, and telling them apart matters: the
	// generic answer sends an operator looking at the network for what is a
	// missing privilege on the token. The distinction costs one status code and
	// saves a long hunt. No detail is added — the cause stays in the log.
	if kind, ok := proxmox.KindOf(err); ok {
		switch kind {
		case proxmox.KindAuth:
			writeError(w, http.StatusForbidden, "insufficient privileges")
			return
		case proxmox.KindTimeout:
			// Not the same thing as unreachable, and not the same thing to do
			// about it: a cluster that is answering too slowly is a different
			// hunt from one that is not answering.
			writeError(w, http.StatusGatewayTimeout, "upstream timeout")
			return
		}
	}
	writeError(w, http.StatusBadGateway, "upstream unavailable")
}

// matchDetailPath splits the escaped path of a request into one of the
// routes below, or reports that it matches none of them:
//
//	{cluster}/rrd
//	{cluster}/nodes/{node}
//	{cluster}/nodes/{node}/rrd
//	{cluster}/guests/{vmid}
//	{cluster}/guests/{vmid}/rrd
//	{cluster}/guests/{vmid}/tasks
//	{cluster}/tasks
//	{cluster}/nodes/{node}/maintenance/plan
//
// It works on the escaped form and unescapes each segment separately, so that a
// node name containing a slash (sent as %2F) stays one segment instead of
// silently becoming two. Anything else — a missing, empty, dotted or otherwise
// unusable segment, an unknown collection, too few or too many segments — is
// rejected rather than guessed at.
func matchDetailPath(escaped string) (detailPath, bool) {
	rest := strings.TrimPrefix(escaped, detailPrefix)
	if len(rest) == len(escaped) {
		// The handler was reached on a path outside its own subtree.
		return detailPath{}, false
	}

	parts := strings.Split(rest, "/")
	if len(parts) < 2 || len(parts) > 5 {
		return detailPath{}, false
	}
	for i, raw := range parts {
		seg, err := url.PathUnescape(raw)
		if err != nil || !validSegment(seg) {
			return detailPath{}, false
		}
		parts[i] = seg
	}

	cluster := parts[0]
	switch {
	case len(parts) == 2 && parts[1] == "tasks":
		return detailPath{kind: routeTasks, cluster: cluster}, true
	case len(parts) == 2 && parts[1] == "rrd":
		return detailPath{kind: routeClusterSeries, cluster: cluster}, true
	case len(parts) == 3 && parts[1] == "nodes":
		return detailPath{kind: routeNode, cluster: cluster, node: parts[2]}, true
	case len(parts) == 4 && parts[1] == "nodes" && parts[3] == "rrd":
		return detailPath{kind: routeNodeSeries, cluster: cluster, node: parts[2]}, true
	case len(parts) == 3 && parts[1] == "guests":
		return detailPath{kind: routeGuest, cluster: cluster, vmidRaw: parts[2]}, true
	case len(parts) == 4 && parts[1] == "guests" && parts[3] == "rrd":
		return detailPath{kind: routeGuestSeries, cluster: cluster, vmidRaw: parts[2]}, true
	case len(parts) == 4 && parts[1] == "guests" && parts[3] == "tasks":
		return detailPath{kind: routeGuestTasks, cluster: cluster, vmidRaw: parts[2]}, true
	case len(parts) == 5 && parts[1] == "nodes" && parts[3] == "maintenance" && parts[4] == "plan":
		return detailPath{kind: routeMaintenancePlan, cluster: cluster, node: parts[2]}, true
	}
	return detailPath{}, false
}

// Bounds of an identifier segment and of a vmid.
const (
	// maxSegment is the length of the longest hostname, which is what a PVE
	// node name is. Nothing legitimate comes near it; the bound exists so a
	// multi-kilobyte segment cannot be concatenated into a PVE request path.
	maxSegment = 253

	// minVMID and maxVMID are the range PVE itself allows for a guest id.
	minVMID = 100
	maxVMID = 999999999
)

// validSegment reports whether a decoded path segment may be used as an
// identifier.
//
// An empty segment comes from a double slash or a trailing one; "." and ".."
// are traversal, and so is any segment that still holds a separator once
// decoded — %2F%2E%2E%2F decodes to "/../", which must never be concatenated
// into a PVE request path. Control characters are refused too: they have no
// place in a node name and would let a caller forge lines in the server log.
// Length is bounded for the same reason: a segment no name could ever have is
// refused here rather than sent upstream to be refused there.
func validSegment(seg string) bool {
	if seg == "" || seg == "." || seg == ".." || len(seg) > maxSegment {
		return false
	}
	for _, r := range seg {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// parseVMID accepts the integers PVE uses to identify a guest.
//
// The spelling must be canonical: strconv.Atoi reads "+101" and "0101" as 101,
// which would give one guest several URLs and so several cache entries and
// several shapes in the log. The range is PVE's own, so a value no guest could
// have costs a 400 here rather than an upstream call before the 404.
func parseVMID(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if strconv.Itoa(n) != raw {
		return 0, errors.New("vmid must be written in canonical form")
	}
	if n < minVMID || n > maxVMID {
		return 0, errors.New("vmid out of range")
	}
	return n, nil
}

// parseTimeframe defaults an absent window to the hour and refuses anything
// outside the known set.
func parseTimeframe(raw string) (string, error) {
	if raw == "" {
		return defaultTimeframe, nil
	}
	if !proxmox.ValidTimeframe(raw) {
		return "", errors.New("unknown timeframe")
	}
	return raw, nil
}

// parseLimit defaults an absent limit and caps an oversized one. Only an
// unreadable or non-positive value is an error: capping keeps a large but
// well-formed request working, which a 400 would not.
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return defaultTaskLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, errors.New("limit must be positive")
	}
	if n > maxTaskLimit {
		return maxTaskLimit, nil
	}
	return n, nil
}
