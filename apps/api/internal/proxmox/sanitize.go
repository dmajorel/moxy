package proxmox

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
)

// This file keeps the promise unwrapURL only half kept.
//
// unwrapURL strips the *url.Error http.Client wraps a transport failure in,
// because its message repeats the full URL and a URL names a node. But the
// cause underneath carries the same information in its own words:
//
//	dial tcp 127.0.0.1:36305: connect: connection refused
//	dial tcp: lookup pve-03.internal on 169.254.1.1:53: no such host
//	x509: certificate is valid for pve-03.internal, not moxy.example
//
// an address and a port, a node name and the resolver that was asked about it,
// the names on a certificate. That message ends up in aggregate.Error.Message,
// which is served in the JSON of /api/overview and shown by the frontend under
// "Détail technique". moxy has no authentication and is meant to be reachable
// on loopback, so that document is as good as public.
//
// sanitize therefore replaces the message with a label saying what went wrong
// and nothing about where. The cause stays reachable through Unwrap, so
// Classify, errors.Is and errors.As are unaffected, and Unsanitized gives the
// full text back for the server log — the one place the host may appear.

// sanitized is a transport error whose message names no host.
type sanitized struct {
	msg   string
	cause error
}

// Error implements error with the host-free label.
func (s *sanitized) Error() string { return s.msg }

// Unwrap keeps errors.Is, errors.As and therefore Classify working through the
// wrapper: every test they make is made against the real cause.
func (s *sanitized) Unwrap() error { return s.cause }

// sanitize wraps err when it is a transport failure whose message names a
// host, and returns it unchanged otherwise. An error this package builds
// itself, or one that comes from decoding a body, names nothing and is left
// alone.
func sanitize(err error) error {
	if err == nil {
		return nil
	}
	msg, ok := sanitizedMessage(err)
	if !ok {
		return err
	}
	return &sanitized{msg: msg, cause: err}
}

// sanitizedMessage returns the label for err, and false when err is not one of
// the shapes that names a host.
//
// ORDER MATTERS. A *net.DNSError usually arrives inside a *net.OpError, and it
// is the DNS error that names both the node and the resolver, so it is tested
// first. The x509 types come before the generic net.OpError for the same
// reason they come before KindNetwork in Classify: they are the more specific
// diagnosis.
func sanitizedMessage(err error) (string, bool) {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		switch {
		case dnsErr.IsNotFound:
			return "dns: no such host", true
		case dnsErr.IsTimeout:
			return "dns: timeout", true
		}
		return "dns: lookup failed", true
	}

	var recordHeader tls.RecordHeaderError
	if errors.As(err, &recordHeader) {
		return "tls: not a tls server", true
	}

	var hostname x509.HostnameError
	if errors.As(err, &hostname) {
		return "x509: hostname mismatch", true
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return "x509: unknown authority", true
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) {
		if invalid.Reason == x509.Expired {
			return "x509: certificate expired", true
		}
		return "x509: certificate invalid", true
	}
	var systemRoots x509.SystemRootsError
	if errors.As(err, &systemRoots) {
		return "x509: no system roots", true
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		op := opErr.Op
		if op == "" {
			op = "connect"
		}
		if opErr.Timeout() {
			return op + ": timeout", true
		}
		// A syscall error spells out the operation and the errno — "connect:
		// connection refused" — and neither names a host. Anything else is
		// reported generically rather than guessed at: *net.AddrError, for
		// one, carries the address in its message.
		var syscallErr *os.SyscallError
		if errors.As(opErr.Err, &syscallErr) {
			return op + ": " + syscallErr.Err.Error(), true
		}
		return op + ": failed", true
	}

	return "", false
}

// Unsanitized returns err's message with the host-free labels expanded back
// into the causes they replaced.
//
// It exists for the SERVER LOG, which is the one place an operator is entitled
// to read the address that was dialled and the name that failed to resolve. It
// must never reach an API response: everything it adds over err.Error() is
// exactly what sanitize took out.
func Unsanitized(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	var s *sanitized
	if errors.As(err, &s) && s.cause != nil {
		msg += ": " + s.cause.Error()
	}
	return msg
}
