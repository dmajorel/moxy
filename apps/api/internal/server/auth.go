package server

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/dmajorel/moxy/apps/api/internal/config"
)

// exemptFromAuth are the paths that answer whatever the identity is.
//
// The probes have to: an orchestrator has no identity to present, and a
// liveness check that fails on authentication would restart a daemon that is
// working perfectly. They give away a status and a build identifier, which is
// the whole of what they carry.
var exemptFromAuth = map[string]bool{
	"/healthz": true, "/healthz/": true,
	"/readyz": true, "/readyz/": true,
}

// requireIdentity refuses a request that did not come through the component
// that authenticates.
//
// moxy does not authenticate anyone itself, and should not: it holds
// hypervisor tokens and has no business holding passwords as well. What it can
// do -- and could not before -- is verify that the authenticating proxy the
// README tells operators to put in front really is in front. Without that,
// "behind a proxy that authenticates" is a hope, not a property: a published
// port, a container on a shared network, a proxy rule that stops matching, and
// the whole estate is readable.
//
// TWO conditions, and both are needed. The header alone proves nothing, since
// anyone reaching the port can set it -- and believing it would be worse than
// no check at all, because the log would then name whoever they claimed to be.
// The address alone proves nothing either: the proxy forwards for everyone.
func requireIdentity(auth config.Auth, next http.Handler) http.Handler {
	if !auth.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exemptFromAuth[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		if !trustedPeer(auth.Trusted, r.RemoteAddr) || r.Header.Get(auth.Header) == "" {
			// The header VALUE is never logged or echoed: it is a user name
			// asserted by someone who may have no business asserting it.
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// trustedPeer reports whether the request came from one of the configured
// proxies. RemoteAddr is the peer of the TCP connection, which no header can
// change -- that is the whole reason the check uses it.
func trustedPeer(trusted []netip.Prefix, remoteAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	// A v4-mapped v6 peer must match a v4 prefix: Go reports 127.0.0.1 as
	// ::ffff:127.0.0.1 on a dual-stack listener.
	addr = addr.Unmap()
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// AuthDisabled reports whether the daemon is serving everything to anyone who
// reaches the port. The caller warns; this package does not log.
func AuthDisabled(auth config.Auth) bool { return !auth.Enabled() }

// ListensBeyondLoopback reports whether addr accepts connections from outside
// the machine. Combined with AuthDisabled it is the one startup warning that
// matters most: the difference between a development default and an estate
// readable by anyone who finds the port.
func ListensBeyondLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	if host == "" {
		// ":8080" binds every interface.
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		// A name: it has to resolve to something, and moxy cannot tell what.
		return true
	}
	// Loopback is the safe case and the only one: the unspecified address
	// binds every interface, and any other literal is an interface address.
	return !ip.Unmap().IsLoopback()
}
