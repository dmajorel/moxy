package proxmox

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Kind classifies a failure of a call to a Proxmox VE cluster. It exists so
// that the aggregator can decide what to do (retry another URL, keep the last
// known snapshot, surface a distinct alert) and so that the frontend can
// translate the failure into French without parsing an English sentence.
type Kind string

// The five classes of failure. They are exhaustive by construction: Classify
// never returns an empty Kind.
const (
	// KindAuth is an HTTP 401 or 403: the token is wrong, expired, or lacks a
	// privilege. Retrying another node of the same cluster is pointless, every
	// node shares the same user database.
	KindAuth Kind = "auth"
	// KindTLS is a certificate that could not be verified: unknown authority
	// (self-signed cluster CA and tls.mode "system"), wrong hostname, expired
	// certificate. Actionable: pin the cluster CA, or relax verification for
	// that one cluster.
	KindTLS Kind = "tls"
	// KindTimeout is the per-call budget running out, whether from the context
	// deadline or from a net.Error reporting a timeout.
	KindTimeout Kind = "timeout"
	// KindNetwork is everything that kept the request from completing without
	// being a timeout or a certificate problem: connection refused, no route,
	// DNS failure, connection reset.
	KindNetwork Kind = "network"
	// KindProtocol is a cluster that answered, but not with what was expected:
	// a 5xx, any other unexpected status, or a body that would not decode.
	KindProtocol Kind = "protocol"
)

// Error is the single error type this package returns. It carries the context
// the aggregator needs, and NOTHING ELSE.
//
// SECURITY. The message is built from four fields only: the cluster id, the
// request path, the HTTP status code, and the cause. It never contains, and
// must never be made to contain:
//
//   - a response body, which PVE fills with server-side detail;
//   - a request or response header, Authorization first among them;
//   - the API token, its secret, or a URL carrying credentials.
//
// This holds structurally rather than by discipline: Classify is handed a
// status code and an error, never an *http.Response, so a body cannot reach it
// even by accident. The one remaining way to leak is for a caller to build the
// cause itself out of response content, which is why the cause must always be
// a transport error or a sentinel of this package. An error of this type is
// serialised into the JSON of /api/overview and therefore reaches the browser.
type Error struct {
	// Cluster is the configured cluster id ("preproduction"), never a URL:
	// a URL could carry credentials, and identifies a node rather than a
	// cluster.
	Cluster string
	// Path is the API path relative to /api2/json ("/cluster/status"). It
	// must not carry a query string holding anything sensitive.
	Path string
	// Kind is the class of failure, as returned by Classify.
	Kind Kind
	// Status is the HTTP status code, 0 when the request never got an answer.
	Status int
	// Err is the cause, wrapped for errors.Is and errors.As. It may be nil
	// when the status alone describes the failure.
	Err error
}

// Error implements error. The format is "cluster <id>: <path>: http <code>
// <text>: <cause>", each segment omitted when empty.
func (e *Error) Error() string {
	var parts []string
	if e.Cluster != "" {
		parts = append(parts, "cluster "+e.Cluster)
	}
	if e.Path != "" {
		parts = append(parts, e.Path)
	}
	if e.Status != 0 {
		s := "http " + strconv.Itoa(e.Status)
		if text := http.StatusText(e.Status); text != "" {
			s += " " + text
		}
		parts = append(parts, s)
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	if len(parts) == 0 {
		return "proxmox: unknown error"
	}
	return strings.Join(parts, ": ")
}

// Unwrap gives errors.Is and errors.As access to the cause.
func (e *Error) Unwrap() error { return e.Err }

// Classify builds the *Error for a failed call and deduces its Kind.
//
// It is called only on failure; on success the caller has nothing to build.
// Status is 0 when no response was received, err is nil when the status alone
// describes the failure.
//
// ORDER OF THE TESTS, and why:
//
//  1. HTTP 401/403 first. An authenticated rejection is authoritative and
//     unambiguous: the cluster answered, it understood the request, and it
//     refused it. No transport-level detail can change that reading, and it is
//     the one class that must not trigger a failover to another node.
//  2. Timeout next, before TLS. A handshake aborted by the budget running out
//     can surface as both a deadline and a certificate-shaped error; the cause
//     is the deadline, and the remedy is a longer timeout, not a new CA.
//     Testing TLS first would send the operator after an imaginary
//     certificate problem.
//  3. TLS next, before the remaining statuses and before network. A
//     certificate failure is the most specific and most actionable diagnosis
//     left, it has an obvious fix, and it is otherwise indistinguishable from
//     a plain connection failure once folded into KindNetwork.
//  4. Protocol next: a 5xx, any other unexpected status, or a body that would
//     not decode. The cluster answered, so this cannot be a network failure,
//     and reaching this point means no more specific class applied.
//  5. Network last, as the default. It is the residual class, which is why it
//     must be tested last: anything specific has already been named.
func Classify(cluster, path string, status int, err error) *Error {
	e := &Error{Cluster: cluster, Path: path, Status: status, Err: err}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		e.Kind = KindAuth
	case isTimeout(err):
		e.Kind = KindTimeout
	case isTLSError(err):
		e.Kind = KindTLS
	case status >= 400 || isDecodeError(err):
		e.Kind = KindProtocol
	default:
		e.Kind = KindNetwork
	}
	return e
}

// isTimeout reports whether err is the per-call budget running out, either as
// the context deadline or as a net.Error that says so. http.Client wraps both
// in a *url.Error, hence errors.As rather than a type assertion.
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// isTLSError reports whether the chain holds a certificate verification
// failure.
//
// The x509 types are matched rather than tls.CertificateVerificationError,
// which only exists from Go 1.20 and this project builds with 1.19. This is
// not merely a fallback: from 1.20 onwards that wrapper unwraps to exactly
// these x509 errors, so matching them keeps working unchanged, and it also
// catches a verification done outside of a handshake.
func isTLSError(err error) bool {
	if err == nil {
		return false
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return true
	}
	var hostname x509.HostnameError
	if errors.As(err, &hostname) {
		return true
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) {
		return true
	}
	var systemRoots x509.SystemRootsError
	if errors.As(err, &systemRoots) {
		return true
	}
	// A peer that is not speaking TLS at all: plain HTTP on port 8006.
	var recordHeader tls.RecordHeaderError
	return errors.As(err, &recordHeader)
}

// isDecodeError reports whether err comes from reading or decoding the body:
// malformed JSON, a field of an unexpected type, or a body cut short. The
// cluster answered, so this is a protocol failure, not a network one.
func isDecodeError(err error) bool {
	if err == nil {
		return false
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return true
	}
	var unmarshalType *json.UnmarshalTypeError
	if errors.As(err, &unmarshalType) {
		return true
	}
	if errors.Is(err, errFlexDecode) {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

// KindOf returns the Kind of the first *Error in the chain, and false when
// there is none. It spares every caller the errors.As dance.
func KindOf(err error) (Kind, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind, true
	}
	return "", false
}
