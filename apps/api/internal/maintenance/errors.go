package maintenance

import (
	"errors"
	"strconv"
	"strings"
)

// Kind classifies a failed maintenance request. It exists so that the HTTP
// layer can pick a status code without parsing a sentence, the frontend can
// translate the failure into French, and /metrics can count it under a CLOSED
// set of outcome values.
type Kind string

// The vocabulary. It is closed: every failure this package produces carries one
// of these, and nothing else may be invented downstream.
const (
	// KindForbidden is the caller, not the cluster: unauthenticated, or absent
	// from clusters[].maintenance.allowedUsers. It is decided by the HTTP
	// layer, which knows the identity; it lives here because it belongs to the
	// same closed set of metric outcomes as the rest. -> 403
	KindForbidden Kind = "maintenance_forbidden"

	// KindNoQuorum is a cluster without corosync quorum: pmxcfs is read-only,
	// so the CRM command cannot even be written, let alone honoured. -> 409
	KindNoQuorum Kind = "no_quorum"
	// KindNoHAManager is a cluster running no HA manager: there is no CRM to
	// act on the command. Refusing beats opening a session to write an
	// instruction nobody reads. -> 409
	KindNoHAManager Kind = "no_ha_manager"
	// KindNoOtherNode is a cluster with no member to run the command on
	// besides the target itself. Never connect to the node being drained: it
	// is often precisely the machine about to be stopped, it may already be
	// unreachable, and the session can die with it. -> 409
	KindNoOtherNode Kind = "no_other_node"
	// KindAlreadyRunning is a second execution on the same (cluster, node)
	// while the first is still open. Ten tabs clicking do not make ten
	// sessions. -> 409
	KindAlreadyRunning Kind = "already_running"

	// KindKeySourceUnavailable is the source of the credential not answering:
	// OpenBao sealed, unreachable, or timing out. -> 502
	KindKeySourceUnavailable Kind = "keysource_unavailable"
	// KindKeySourceDenied is the source of the credential refusing: expired
	// secret_id, role denied, signature refused. -> 502
	//
	// Both keysource kinds exist to say "the KEY SOURCE did not answer", not
	// "the node did not answer". Collapsing them into ssh_unreachable would
	// send an operator probing port 22 on a node that is perfectly fine.
	KindKeySourceDenied Kind = "keysource_denied"

	// KindUnreachable is a node that did not answer on its SSH port. -> 502
	KindUnreachable Kind = "ssh_unreachable"
	// KindHostKeyMismatch is a host key absent from known_hosts, or different
	// from the one recorded. There is no setting that waves this away, and
	// there will not be one: an unverified host key runs a privileged command
	// on a machine that may not be the one it claims to be. -> 502
	KindHostKeyMismatch Kind = "ssh_host_key_mismatch"
	// KindAuthFailed is the node refusing the credential. -> 502
	KindAuthFailed Kind = "ssh_auth_failed"
	// KindTimeout is a budget running out: connection, handshake or command.
	// -> 504
	KindTimeout Kind = "ssh_timeout"

	// KindCommandRefused is the node-side validator rejecting the request:
	// exit 64 (grammar) or 65 (no such node on this cluster). -> 502
	KindCommandRefused Kind = "command_refused"
	// KindCommandFailed is ha-manager exiting non-zero. -> 502
	KindCommandFailed Kind = "command_failed"
)

// Exit codes of the node-side validator. They are distinct on purpose: the
// backend turns them into a different kind from a failure of ha-manager
// itself, and an operator reading "command refused" looks at the deployment
// while one reading "command failed" looks at the cluster.
const (
	exitBadRequest  = 64 // grammar: not three words, unknown verb, bad action
	exitUnknownNode = 65 // a node name this cluster does not have
)

// THE RULE OF THIS PACKAGE, and the one a distracted refactoring will erase.
//
// A TRANSPORT failure -- unreachable, timeout, host key, auth -- means the
// command never reached ha-manager, so running it on the next node is safe and
// is what we do. An APPLICATIVE failure -- command_refused, command_failed --
// means the node side answered: the command may have taken effect, or may be
// refused identically everywhere, and trying another node would at best repeat
// a refusal and at worst issue a second privileged instruction. So we stop,
// on the first one, and report it.
//
// In practice "node-maintenance enable" is idempotent for the CRM, which makes
// a replay harmless even when the session breaks after the command was sent.
// The invariant is written for the day a second, non-idempotent command is
// added: replaying on another node is safe up to the point where ha-manager is
// reached, and only there.
//
// keysource_* failures happen before any session exists, so they are neither:
// no node has been contacted and none will be.

// transportKinds is the retryable half of the rule above, as data, so that the
// rule is stated once and read by Retryable, by the service and by the tests.
var transportKinds = map[Kind]bool{
	KindUnreachable:     true,
	KindHostKeyMismatch: true,
	KindAuthFailed:      true,
	KindTimeout:         true,
}

// Retryable reports whether this kind of failure lets the next node be tried.
func (k Kind) Retryable() bool { return transportKinds[k] }

// Error is the single error type this package returns.
//
// SECURITY. It carries the cluster id, the node, the kind, an exit code and a
// cause. It never carries, and must never be made to carry, the credential,
// the full command line sent to the node, or anything read from a key source's
// response body: an error of this type is logged, counted and, through its
// kind, serialised to the browser.
type Error struct {
	// Cluster is the configured cluster id, never a URL.
	Cluster string
	// Node is the node the failure is about: the target for a precondition,
	// the node the session was attempted on for a transport or applicative
	// failure. Empty when the failure belongs to neither -- a key source that
	// did not answer, say.
	Node string
	// Kind is the class of failure.
	Kind Kind
	// ExitCode is the status the node-side command ended with, and is 0 unless
	// Kind is KindCommandRefused or KindCommandFailed. The audit line reports
	// it.
	ExitCode int
	// Err is the cause, wrapped for errors.Is and errors.As. It may be nil
	// when the kind alone describes the failure.
	Err error
}

// Error implements error: "cluster <id>: node <name>: <kind>: exit <n>:
// <cause>", each segment omitted when empty.
func (e *Error) Error() string {
	var parts []string
	if e.Cluster != "" {
		parts = append(parts, "cluster "+e.Cluster)
	}
	if e.Node != "" {
		parts = append(parts, "node "+e.Node)
	}
	if e.Kind != "" {
		parts = append(parts, string(e.Kind))
	}
	if e.ExitCode != 0 {
		parts = append(parts, "exit "+strconv.Itoa(e.ExitCode))
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	if len(parts) == 0 {
		return "maintenance: unknown error"
	}
	return strings.Join(parts, ": ")
}

// Unwrap gives errors.Is and errors.As access to the cause.
func (e *Error) Unwrap() error { return e.Err }

// Retryable reports whether another node may be tried after this failure. It
// is the method callers should use: it answers false for an error with no kind
// at all, which is the safe reading -- an unclassified failure is one we cannot
// prove stopped short of ha-manager.
func (e *Error) Retryable() bool { return e.Kind.Retryable() }

// KindOf returns the Kind of the first *Error in the chain, and false when
// there is none. It spares every caller the errors.As dance.
func KindOf(err error) (Kind, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind, true
	}
	return "", false
}

// Retryable reports whether err is a transport failure, which is to say
// whether the next node may be tried. An error this package did not classify
// is NOT retryable: see the rule above.
func Retryable(err error) bool {
	kind, ok := KindOf(err)
	return ok && kind.Retryable()
}

// newError builds an *Error.
func newError(cluster, node string, kind Kind, cause error) *Error {
	return &Error{Cluster: cluster, Node: node, Kind: kind, Err: cause}
}

// ErrNotFound is the cluster or node not existing, or the cluster having no
// maintenance block at all. It carries no Kind: the HTTP layer answers 404 and
// the UI shows no button, so there is nothing for the frontend to translate
// and nothing for the metric to count under an outcome.
var ErrNotFound = errors.New("not found")

// ErrInvalidAction is an action outside the two verbs. The HTTP layer answers
// 400: the caller got the request wrong, it did not name something absent.
var ErrInvalidAction = errors.New("invalid action")
