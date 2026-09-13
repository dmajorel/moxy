package server

import (
	"encoding/json"
	"mime"
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

// LoginPath is where a token-mode client exchanges the shared token for the
// cookie that carries it afterwards. It is served by the guard itself rather
// than by the mux: it is the one API path that must answer before an identity
// exists, and in every other mode it does not exist at all.
const LoginPath = "/api/login"

// TokenCookie is the cookie the token mode sets and reads.
//
// The cookie holds the token and nothing else. There is NO server-side
// session, on purpose: a session is state to store, expire and invalidate, and
// a daemon that would rather hold no passwords has no business holding a
// session table either. The cost is stated plainly in the README -- the only
// way to revoke is to change the token and restart.
const TokenCookie = "moxy_token"

// maxLoginBody bounds what POST /api/login will read. A token is a few dozen
// characters; anything larger is either a mistake or an attempt to make the
// daemon allocate.
const maxLoginBody = 4 << 10

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
//
// Token mode is the answer for the deployment that has no such component in
// front of it, and it is a lesser answer: one shared secret, presented in a
// cookie or a bearer header, which authorizes without identifying anyone. It
// is here because the alternative that deployment actually had was AuthNone.
func requireIdentity(auth config.Auth, next http.Handler) http.Handler {
	if !auth.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exemptFromAuth[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		if auth.Mode == config.AuthToken {
			// Compared verbatim, before anything cleans the path: the guard
			// and the caller must never disagree about which path this is.
			if r.URL.Path == LoginPath {
				handleLogin(auth, w, r)
				return
			}
			if servesLoginScreen(r) {
				next.ServeHTTP(w, r)
				return
			}
		}
		if !authorized(auth, r) {
			// Nothing about what was presented is logged or echoed: in
			// proxy-header mode it is a user name asserted by someone who may
			// have no business asserting it, and in token mode it is the
			// secret itself.
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorized reports whether this request carries what its mode requires.
func authorized(auth config.Auth, r *http.Request) bool {
	switch auth.Mode {
	case config.AuthProxyHeader:
		return trustedPeer(auth.Trusted, r.RemoteAddr) && r.Header.Get(auth.Header) != ""
	case config.AuthToken:
		return auth.Token.ConstantTimeEqual(presentedToken(r))
	default:
		// AuthNone never reaches here: requireIdentity returns next as is.
		return true
	}
}

// presentedToken reads the token out of a request, from the cookie the login
// route set or from an Authorization header.
//
// The header is not there for the browser -- it is there for everything that
// is not one. /metrics sits inside the authenticated chain, and a scraper
// speaks bearer tokens, not cookie jars; so does curl in a runbook.
func presentedToken(r *http.Request) string {
	if cookie, err := r.Cookie(TokenCookie); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	const bearer = "Bearer "
	if value := r.Header.Get("Authorization"); len(value) > len(bearer) &&
		strings.EqualFold(value[:len(bearer)], bearer) {
		return strings.TrimSpace(value[len(bearer):])
	}
	return ""
}

// servesLoginScreen reports whether this request is the frontend bundle being
// fetched so that the token can be typed into it.
//
// This is the one concession token mode makes, and it is a deliberate one. In
// proxy-header mode the bundle is guarded like the API, because something in
// front has already authenticated the caller; in token mode there is nothing
// in front, so guarding the bundle would mean serving a 401 to a browser that
// has no way to ask for the token yet. What is served is static
// JavaScript and CSS, identical in every deployment: no cluster name, no node,
// no reading. Everything that carries the estate -- /api and /metrics -- stays
// behind the token, which is what the guard is for.
func servesLoginScreen(r *http.Request) bool {
	if !isReadMethod(r.Method) {
		return false
	}
	p := r.URL.Path
	for _, guarded := range [...]string{"/api", "/metrics"} {
		if p == guarded || strings.HasPrefix(p, guarded+"/") {
			return false
		}
	}
	return true
}

// loginRequest is the body of POST /api/login.
type loginRequest struct {
	Token string `json:"token"`
}

// handleLogin exchanges the shared token for the cookie that carries it.
//
// The token is never written anywhere but into the cookie of the caller who
// already knew it: not into the log, not into the answer, not into an error.
// Both outcomes are the same shape and the same work -- the comparison itself
// is constant-time -- so a wrong token and a right one are told apart only by
// the status code.
func handleLogin(auth config.Auth, w http.ResponseWriter, r *http.Request) {
	// An answer carrying a Set-Cookie, or a refusal of one, must never be
	// held by a cache on the way.
	w.Header().Set("Cache-Control", "no-store")

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// JSON, and only JSON. A form content type is what an HTML form posted
	// from another site can send without a preflight; refusing it means a page
	// the operator happens to visit cannot log their browser in to a token of
	// its own choosing. SameSite=Strict already keeps the resulting cookie
	// from being sent anywhere, but the cheapest way to not have to reason
	// about that is to refuse the request.
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "expected a JSON body")
		return
	}

	var body loginRequest
	// The reader is bounded before the decoder sees it: a login route that
	// reads whatever it is given is a memory tap that needs no credentials.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLoginBody)).Decode(&body); err != nil {
		// The error is NOT wrapped into the answer: a decoder message quotes
		// the input it choked on, and the input here is the token.
		writeError(w, http.StatusBadRequest, "malformed body")
		return
	}

	if !auth.Token.ConstantTimeEqual(strings.TrimSpace(body.Token)) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:  TokenCookie,
		Value: strings.TrimSpace(body.Token),
		Path:  "/",
		// No Expires and no MaxAge: a session cookie, gone when the browser
		// is. A token that outlives the session on disk is a token left on a
		// workstation.
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		// Secure only under TLS: setting it on a plain http:// answer would
		// produce a cookie the browser refuses to store, and moxy on a
		// workstation is reached over loopback http. Whether the request is
		// TLS is read from the connection, never from a forwarded header --
		// in this mode there is no proxy whose word could be taken for it.
		Secure: r.TLS != nil,
	})
	// Nothing to say that the status does not already say, and anything said
	// here would be one more place the token could end up.
	w.WriteHeader(http.StatusNoContent)
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
