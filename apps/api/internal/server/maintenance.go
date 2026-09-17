package server

import (
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/config"
	"github.com/dmajorel/moxy/apps/api/internal/detail"
	"github.com/dmajorel/moxy/apps/api/internal/maintenance"
	"github.com/dmajorel/moxy/apps/api/internal/metrics"
)

// This file serves POST /api/clusters/{cluster}/nodes/{node}/maintenance, the
// FIRST route of this daemon that writes anything.
//
// Everything below it -- the body, the authorization, the cross-site checks --
// exists because of that first. The API was read-only until now: a request
// that arrived from somewhere it should not have could read an estate, which
// the authentication modes already answer, but it could not act on one. It can
// now, and the three guards here are what keeps that from being something a
// page an operator happens to visit can trigger.

// maxMaintenanceBody bounds what the route will read. The body is one word out
// of a closed set of two; anything larger is a mistake or an attempt to make a
// daemon holding hypervisor tokens allocate.
const maxMaintenanceBody = 1 << 10

// anonymousCaller is what the audit line says when the authentication mode
// cannot name anybody. It is a word, not an empty field: a line that simply
// omits the identity reads like a line whose identity was lost.
const anonymousCaller = "anonymous"

// maintenanceRequest is the body of the route: {"action":"enable"|"disable"}.
// The two verbs are not checked here -- internal/maintenance owns that closed
// set, and re-stating it would be a second definition to keep in step.
type maintenanceRequest struct {
	Action string `json:"action"`
}

// handleMaintenance runs one maintenance request, or refuses it.
//
// The order of the checks is deliberate: what the request IS comes before who
// is asking, because a cross-site POST must be refused whether or not the
// browser it was fired from belongs to someone who would have been allowed.
func handleMaintenance(opts Options, guard *hostGuard, w http.ResponseWriter, r *http.Request, p detailPath) {
	audit := maintenanceAudit{cluster: p.cluster, node: p.node, user: anonymousCaller}

	// JSON, and only JSON -- the same refusal handleLogin makes, for the same
	// reason and now with teeth. An HTML form can post
	// application/x-www-form-urlencoded, multipart/form-data or text/plain
	// from another site with no preflight; it cannot post application/json.
	// Demanding it therefore means a cross-site POST has to come through
	// fetch(), which CORS holds at the door.
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		audit.write("unsupported_media_type", 0)
		writeError(w, http.StatusUnsupportedMediaType, "expected a JSON body")
		return
	}

	// And the origin has to be this service. Token mode sets its cookie
	// SameSite=Strict, so a browser would not attach it to a request fired
	// from another site at all; proxy-header mode is NOT covered, because the
	// session it rides on is a cookie of the proxy, set by something moxy does
	// not configure and cannot make strict. The check belongs here rather than
	// in the middleware for the same reason: it is worth making on the one
	// route where being wrong means a node is drained.
	if !sameOrigin(guard, r) {
		audit.write("cross_origin", 0)
		writeError(w, http.StatusForbidden, "cross-origin request refused")
		return
	}

	if opts.Detail == nil {
		// No source was wired: the route exists but nothing can execute. Said
		// plainly rather than as a 404, which the frontend would read as "this
		// cluster does not take part".
		writeError(w, http.StatusNotImplemented, "maintenance is not available without a cluster connection")
		return
	}

	var body maintenanceRequest
	// Bounded before the decoder sees it, and the decoder's own error is NOT
	// carried into the answer: it quotes the input it choked on, and a body
	// this daemon echoes back is a body an attacker chooses.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMaintenanceBody)).Decode(&body); err != nil {
		audit.write("malformed_body", 0)
		writeError(w, http.StatusBadRequest, "malformed body")
		return
	}
	audit.action = strings.TrimSpace(body.Action)

	// WHO IS ASKING, decided here and never only in the UI: the repository
	// rule on destructive actions is that the backend verifies.
	caller, named := maintenanceCaller(opts.Auth, r)
	if named {
		audit.user = caller
	}
	allowed, configured := opts.MaintenanceUsers[p.cluster]
	if !allowedToDrain(allowed, caller, named) {
		audit.write(metrics.OutcomeForbidden, 0)
		if configured {
			// Counted only for a cluster this deployment actually configured:
			// the id comes out of a URL, and an id nobody configured must not
			// be able to create a time series by being spelled into one.
			metrics.RecordMaintenanceCommand(p.cluster, audit.action, metrics.OutcomeForbidden, 0)
		}
		writeKindError(w, http.StatusForbidden, string(maintenance.KindForbidden),
			"not allowed to run maintenance on this cluster")
		return
	}

	started := time.Now()
	result, err := opts.Detail.ExecuteMaintenance(r.Context(), p.cluster, p.node, audit.action)
	elapsed := time.Since(started)

	if err != nil {
		answer := maintenanceAnswerFor(err)
		audit.write(answer.auditOutcome(), answer.exit)
		if answer.outcome != "" {
			metrics.RecordMaintenanceCommand(p.cluster, audit.action, answer.outcome, elapsed)
			// The cause stays in the log, where it may name a node or quote a
			// hypervisor; what leaves is the kind and a fixed sentence.
			log.Printf("maintenance on cluster %q failed: %v", p.cluster, err)
		}
		if answer.kind == "" {
			writeError(w, answer.status, answer.message)
			return
		}
		writeKindError(w, answer.status, answer.kind, answer.message)
		return
	}
	if result == nil {
		// A source that returns neither a result nor an error. Defensive, like
		// found() on the read routes: a typed nil would be serialised as the
		// JSON literal null under a 200.
		audit.write(metrics.Unclassified, 0)
		writeError(w, http.StatusBadGateway, "upstream unavailable")
		return
	}

	audit.via = result.Via
	audit.write(metrics.OutcomeOK, 0)
	metrics.RecordMaintenanceCommand(p.cluster, audit.action, metrics.OutcomeOK, elapsed)
	writeJSON(w, http.StatusOK, result)
}

/* --------------------------------------------------------- authorization --- */

// maintenanceCaller names the caller, when the authentication mode can name
// anyone at all.
//
// Only "proxy-header" can: something in front authenticated the request and
// asserted the name, and requireIdentity has already checked that the
// something really is the configured proxy. Token mode authorizes with one
// shared secret and identifies nobody -- config/auth.go says so plainly,
// "everyone who holds it is the same caller" -- and mode "none" names nobody
// by construction.
func maintenanceCaller(auth config.Auth, r *http.Request) (string, bool) {
	if auth.Mode != config.AuthProxyHeader {
		return "", false
	}
	header := auth.Header
	if header == "" {
		// Auth.resolve fills this in; a hand-built Auth might not, and reading
		// an empty header name would silently turn every caller anonymous.
		header = config.DefaultAuthHeader
	}
	name := strings.TrimSpace(r.Header.Get(header))
	return name, name != ""
}

// allowedToDrain is the single authorization rule, and it FAILS CLOSED in both
// directions:
//
//   - a caller who CAN be named must appear in the list. An empty or missing
//     list therefore names nobody, and a cluster this deployment never
//     configured is refused rather than opened. The loader enforces the other
//     half of that bargain -- maintenance.enabled with an empty allowedUsers
//     is refused at start-up in proxy-header mode, so a list that names nobody
//     cannot be a typo nobody noticed.
//   - a caller who CANNOT be named is authorized only where the list names
//     nobody either. In token mode the shared secret is the whole
//     authorization, which the loader guarantees by refusing a list of names
//     there; anywhere else, a configuration that names people cannot be
//     honoured by a request that names no one, and the safe reading of a rule
//     that cannot be checked is to refuse.
func allowedToDrain(allowedUsers []string, caller string, named bool) bool {
	if !named {
		return len(allowedUsers) == 0
	}
	for _, allowed := range allowedUsers {
		if strings.TrimSpace(allowed) == caller {
			return true
		}
	}
	return false
}

/* ------------------------------------------------------------------ CSRF --- */

// sameOrigin reports whether this request was fired from a page served by moxy
// itself.
//
// An ABSENT Origin is accepted, and that is not a hole: browsers send it on
// every POST, so a request without one did not come from a page. What it is
// there for is everything that is not a browser -- curl in a runbook, a script
// draining a node before a reboot -- which has no origin to declare and is not
// subject to the attack this guard is about.
//
// A present one has to name moxy: the Host of the request, which checkHost has
// already vetted, or one of the declared -allowed-hosts, for the proxy that
// rewrites Host to an internal name and passes the public one through.
func sameOrigin(guard *hostGuard, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	// A sandboxed iframe or a redirected form posts the literal "null", which
	// names no host and must never be read as "no origin at all".
	if origin == "null" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	host := normalizeHost(u.Host)
	if host == "" {
		return false
	}
	if host == normalizeHost(r.Host) {
		return true
	}
	if guard == nil || guard.allowed == nil {
		return false
	}
	_, ok := guard.allowed[host]
	return ok
}

/* -------------------------------------------------------- error mapping --- */

// maintenanceAnswer is one failure, turned into what the client is told, what
// /metrics counts and what the audit line records.
type maintenanceAnswer struct {
	status int
	// kind is the vocabulary the frontend translates by, and is empty for the
	// two failures that carry none: a cluster or node that is not there, and a
	// request the caller got wrong. Neither is a class of failure to explain
	// in a sentence.
	kind string
	// message stays in English. Translating it is the frontend's job.
	message string
	// outcome is the metric label, and is empty when this failure must NOT be
	// counted: a cluster that is not configured would put an id nobody chose
	// into a time series, and a malformed request is not an attempt on the
	// estate.
	outcome string
	exit    int
}

// auditOutcome is what the audit line records, which is the metric outcome
// where there is one and a word of its own where there is not: the audit is
// prose for an operator and may say more than a closed label set.
func (a maintenanceAnswer) auditOutcome() string {
	if a.outcome != "" {
		return a.outcome
	}
	if a.status == http.StatusNotFound {
		return "not_found"
	}
	return "invalid_request"
}

// maintenanceStatus maps each Kind of internal/maintenance onto a status code.
// The table is the one of the issue, stated once, as data: a switch spread
// through the handler is how two of these drift apart.
var maintenanceStatus = map[maintenance.Kind]int{
	maintenance.KindForbidden:            http.StatusForbidden,
	maintenance.KindNoQuorum:             http.StatusConflict,
	maintenance.KindNoHAManager:          http.StatusConflict,
	maintenance.KindNoOtherNode:          http.StatusConflict,
	maintenance.KindAlreadyRunning:       http.StatusConflict,
	maintenance.KindKeySourceUnavailable: http.StatusBadGateway,
	maintenance.KindKeySourceDenied:      http.StatusBadGateway,
	maintenance.KindUnreachable:          http.StatusBadGateway,
	maintenance.KindHostKeyMismatch:      http.StatusBadGateway,
	maintenance.KindAuthFailed:           http.StatusBadGateway,
	maintenance.KindTimeout:              http.StatusGatewayTimeout,
	maintenance.KindCommandRefused:       http.StatusBadGateway,
	maintenance.KindCommandFailed:        http.StatusBadGateway,
}

// maintenanceMessage is the English sentence each Kind is served with.
//
// NONE OF THEM CARRIES ANYTHING READ FROM UPSTREAM: no node name, no key
// source address, no line of a hypervisor's answer. They are fixed strings, so
// that what an operator reads in a browser cannot be chosen by whatever the
// far end replied. The two keysource ones say "key source" and never "node",
// which is the whole reason they are two kinds and not one: an operator told
// "unreachable" spends the evening probing port 22 on a node that is fine.
var maintenanceMessage = map[maintenance.Kind]string{
	maintenance.KindForbidden:            "not allowed to run maintenance on this cluster",
	maintenance.KindNoQuorum:             "the cluster has no quorum, so the command cannot be written",
	maintenance.KindNoHAManager:          "the cluster runs no HA manager to honour the command",
	maintenance.KindNoOtherNode:          "no other node of the cluster is available to run the command",
	maintenance.KindAlreadyRunning:       "a maintenance command is already running on this node",
	maintenance.KindKeySourceUnavailable: "the key source did not answer",
	maintenance.KindKeySourceDenied:      "the key source refused to issue a credential",
	maintenance.KindUnreachable:          "no node answered on the ssh port",
	maintenance.KindHostKeyMismatch:      "the host key does not match the one on record",
	maintenance.KindAuthFailed:           "the node refused the credential",
	maintenance.KindTimeout:              "the session ran out of its budget",
	maintenance.KindCommandRefused:       "the node refused the command",
	maintenance.KindCommandFailed:        "the command failed on the node",
}

// maintenanceAnswerFor turns an error of the execution path into an answer.
//
// The errors arrive UNWRAPPED, which is what lets this read a Kind instead of
// a sentence. Anything it cannot classify is 502: the request reached a
// cluster this deployment configured and something upstream failed, which is
// the same thing writeDetailError says about a read.
func maintenanceAnswerFor(err error) maintenanceAnswer {
	if kind, ok := maintenance.KindOf(err); ok {
		answer := maintenanceAnswer{
			status:  maintenanceStatus[kind],
			kind:    string(kind),
			message: maintenanceMessage[kind],
			outcome: string(kind),
		}
		if answer.status == 0 {
			// A Kind added upstream without a row here. Counted under the
			// folded label rather than dropped, and served as an upstream
			// failure rather than as a 200 nobody checked.
			answer.status, answer.message = http.StatusBadGateway, "upstream unavailable"
			answer.outcome = metrics.Unclassified
		}
		var known *maintenance.Error
		if errors.As(err, &known) {
			answer.exit = known.ExitCode
		}
		return answer
	}

	switch {
	case errors.Is(err, maintenance.ErrNotFound), errors.Is(err, detail.ErrNotFound):
		// The cluster does not take part, or the node is not one it has.
		// There is no button for it in the UI and no time series for it here.
		return maintenanceAnswer{status: http.StatusNotFound, message: "not found"}
	case errors.Is(err, maintenance.ErrInvalidAction), errors.Is(err, detail.ErrInvalidArgument):
		// The caller wrote something outside the two verbs. The body is NOT
		// echoed back: it is the one part of the request an attacker chooses.
		return maintenanceAnswer{status: http.StatusBadRequest, message: "malformed body"}
	}
	return maintenanceAnswer{
		status:  http.StatusBadGateway,
		message: "upstream unavailable",
		outcome: metrics.Unclassified,
	}
}

/* ----------------------------------------------------------------- audit --- */

// maintenanceAudit is one line of the audit trail, filled in as the request
// makes its way through and written exactly once, whatever the outcome.
//
// WHAT IT MUST NEVER CARRY: the credential, the command line sent to a node,
// the body of the request, or a formatted *http.Request -- the last one is a
// repository rule of its own, since the Authorization header holds a token.
// What it does carry is who, what, where and how it ended, which is what an
// operator reading back a night of drains needs.
type maintenanceAudit struct {
	user    string
	cluster string
	node    string
	action  string
	via     string
}

// write emits the line.
//
// The timestamp is written out rather than left to the log prefix: an audit
// line must not stop being an audit line because somebody called log.SetFlags.
// Every value is quoted, which is not cosmetic -- a user name comes from a
// header, and an unquoted newline in one would let a caller forge a line of
// their own in this very trail.
func (a maintenanceAudit) write(outcome string, exit int) {
	user := a.user
	if user == "" {
		user = anonymousCaller
	}
	log.Printf("maintenance audit: at=%s user=%q cluster=%q node=%q action=%q via=%q outcome=%q exit=%d",
		time.Now().UTC().Format(time.RFC3339), user, a.cluster, a.node, a.action, a.via, outcome, exit)
}
