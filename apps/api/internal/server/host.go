package server

import (
	"net"
	"net/http"
	"strings"
)

// This file closes DNS rebinding, which is the one browser-side attack a
// loopback service without authentication is actually exposed to.
//
// moxy is meant to run on 127.0.0.1:8080 with no login (see the README). A
// hostile page the operator visits can point attacker.example at 127.0.0.1
// after its first load; the browser then sends its requests to moxy with
// "Host: attacker.example", from an origin the page controls, and reads the
// answer. There is no CORS to stop it — deliberately, since same-origin
// requests need none — and no credential to be missing, since there is none.
//
// Checking Host is the classic mitigation, and the only one available until
// moxy authenticates its callers. It is NOT authentication: it identifies
// nobody. It only keeps a foreign origin from talking to moxy through the
// operator's own browser.

// hostGuard decides whether a Host header names moxy.
//
// A nil allowed set means the check is off, which is what a generic listen
// address with no configured list produces: moxy cannot then guess its own
// name, and refusing everything would break the container deployment.
type hostGuard struct {
	allowed map[string]struct{}
}

// genericHosts are the listen addresses that name no host in particular. When
// moxyd listens on one of them, its own name is whatever the operator put in
// front of it, which only the operator can say.
var genericHosts = map[string]bool{"": true, "0.0.0.0": true, "::": true}

// newHostGuard builds the guard from the listen address and the configured
// list.
//
// localhost, 127.0.0.1 and ::1 are always allowed: they are what an operator
// types, and they are the deployment the README recommends. The host of -addr
// joins them when it names something, so binding to a fixed address is enough
// to be reachable by that name. Everything else has to be declared, which is
// what -allowed-hosts is for: a reverse proxy that passes the public Host
// through needs the public name here.
func newHostGuard(addr string, allowedHosts []string) *hostGuard {
	declared := normalizeHosts(allowedHosts)
	listen := listenHost(addr)

	if listen == "" && len(declared) == 0 {
		// Nothing to compare against: the check is off. The daemon says so at
		// startup, see HostCheckDisabled.
		return &hostGuard{}
	}

	allowed := map[string]struct{}{
		"localhost": {},
		"127.0.0.1": {},
		"::1":       {},
	}
	if listen != "" {
		allowed[listen] = struct{}{}
	}
	for _, h := range declared {
		allowed[h] = struct{}{}
	}
	return &hostGuard{allowed: allowed}
}

// HostCheckDisabled reports whether the Host check is inactive for this
// configuration, so that moxyd can say so at startup rather than leave an
// operator believing in a protection that is not running.
func HostCheckDisabled(addr string, allowedHosts []string) bool {
	return newHostGuard(addr, allowedHosts).allowed == nil
}

// listenHost extracts the host of a listen address, or "" when it names no
// host in particular.
func listenHost(addr string) string {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	host = normalizeHost(host)
	if genericHosts[host] {
		return ""
	}
	return host
}

// normalizeHosts splits and cleans a comma-separated list, dropping what is
// empty after trimming.
func normalizeHosts(raw []string) []string {
	var out []string
	for _, entry := range raw {
		for _, item := range strings.Split(entry, ",") {
			if h := normalizeHost(item); h != "" {
				out = append(out, h)
			}
		}
	}
	return out
}

// normalizeHost puts one host into the form the map is keyed by: no
// surrounding space, no port, no brackets around an IPv6 literal, lowercase.
//
// A trailing dot is removed too: "moxy.example." and "moxy.example" are the
// same name to DNS, and a browser will happily send either.
func normalizeHost(raw string) string {
	host := strings.TrimSpace(raw)
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	host = strings.TrimSuffix(host, ".")
	return strings.ToLower(host)
}

// allows reports whether r names moxy.
func (g *hostGuard) allows(r *http.Request) bool {
	if g.allowed == nil {
		return true
	}
	host := normalizeHost(r.Host)
	if host == "" {
		// HTTP/1.1 requires a Host, and a request without one cannot be shown
		// to be addressed to moxy.
		return false
	}
	_, ok := g.allowed[host]
	return ok
}

// checkHost rejects a request whose Host header does not name moxy.
//
// /healthz is EXEMPT, and that is a deliberate choice rather than an oversight.
// A liveness probe is the one caller whose Host header the operator does not
// control — kubelet sends the pod IP, HAProxy's httpchk sends whatever it was
// configured with, some send none at all — and a 421 there turns a healthy
// daemon into a failing one. What it gives up is small: /healthz answers a
// status and a build identifier, and nothing about any cluster. Every route
// that describes infrastructure is checked.
func checkHost(guard *hostGuard, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" && !guard.allows(r) {
			// 421 is the status for a request sent to a server that is not
			// authoritative for the requested host, which is exactly this.
			writeError(w, http.StatusMisdirectedRequest, "misdirected request")
			return
		}
		next.ServeHTTP(w, r)
	})
}
