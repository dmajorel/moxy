package config

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
)

// Authentication modes moxy understands.
const (
	// AuthNone is the default and the historical behaviour: anyone who
	// reaches the port reads every cluster. It is only safe on loopback, or
	// behind a proxy that authenticates and that nothing can bypass.
	AuthNone = "none"
	// AuthProxyHeader trusts an identity asserted by a reverse proxy, and
	// only when the request comes from one of the proxies listed. It is the
	// deployment the README already recommends; this makes moxy able to
	// verify that it really is the one in place.
	AuthProxyHeader = "proxy-header"
	// AuthToken accepts one shared token, read from the environment like
	// every other secret. It exists for the deployment that has no
	// authenticating component in front of it -- moxy on an administration
	// workstation -- where the only other choice is AuthNone, which serves
	// the whole estate to whoever reaches the port.
	//
	// It is deliberately INFERIOR to AuthProxyHeader and must not become the
	// comfortable default: a shared token authorizes, it does not identify.
	// Everyone who holds it is the same caller, so nothing moxy could log
	// would ever say who asked.
	AuthToken = "token"
)

// DefaultAuthHeader is the header most authenticating proxies set.
const DefaultAuthHeader = "X-Forwarded-User"

// MinTokenLength is the shortest shared token the loader accepts.
//
// A static token never expires, is the only thing between the port and the
// estate, and nothing throttles the attempts: its length is what makes
// guessing it hopeless. 32 characters is what `openssl rand -hex 16` produces,
// which is the command the README gives.
const MinTokenLength = 32

// Auth is how moxy decides who is asking.
//
// It is deliberately not an authentication system of its own: moxy holds
// hypervisor tokens and has no business holding passwords as well. What it can
// do is refuse to serve a request that did not come through the component that
// does authenticate -- or, in token mode, that does not present the one secret
// the operator handed out.
type Auth struct {
	// Mode is "none", "proxy-header" or "token". Empty means "none".
	Mode string `json:"mode,omitempty"`
	// Header carries the authenticated user name in proxy-header mode.
	// Defaults to DefaultAuthHeader.
	Header string `json:"header,omitempty"`
	// TrustedProxies are the CIDR blocks a request must come from for Header
	// to be believed. Without this an attacker who reaches the port simply
	// sets the header themselves, which is worse than no check at all: the
	// log would then name whoever they claimed to be.
	TrustedProxies []string `json:"trustedProxies,omitempty"`
	// Trusted is TrustedProxies parsed, built once at load time.
	Trusted []netip.Prefix `json:"-"`
	// TokenEnv names the environment variable holding the shared token, in
	// token mode. The token lives in the environment for the same reason a
	// cluster secret does: a secret written in the configuration file is a
	// secret in every backup and every review of that file.
	TokenEnv string `json:"tokenEnv,omitempty"`
	// Token is TokenEnv read at load time, wrapped so that no format verb, no
	// encoder and no log line can spell it out. It is never served, never
	// echoed, and never compared with == -- see Secret.ConstantTimeEqual.
	Token Secret `json:"-"`
}

// NewProxyHeaderAuth builds a validated proxy-header policy. It exists so that
// callers outside this package -- the server tests, chiefly -- cannot assemble
// an Auth the loader would have refused.
func NewProxyHeaderAuth(header string, trustedProxies []string) (Auth, error) {
	auth := Auth{Mode: AuthProxyHeader, Header: header, TrustedProxies: trustedProxies}
	if err := auth.resolve(); err != nil {
		return Auth{}, err
	}
	return auth, nil
}

// NewTokenAuth builds a validated token policy from the token itself, for the
// same reason NewProxyHeaderAuth exists: a test must not be able to assemble a
// policy the loader would have refused. Load goes through the environment
// instead, which is where a real token comes from.
func NewTokenAuth(token string) (Auth, error) {
	auth := Auth{Mode: AuthToken, Token: NewSecret(token)}
	if err := auth.checkToken(); err != nil {
		return Auth{}, err
	}
	return auth, nil
}

// Enabled reports whether a request has to prove anything at all.
func (a Auth) Enabled() bool { return a.Mode == AuthProxyHeader || a.Mode == AuthToken }

// resolve applies the defaults and validates the block.
func (a *Auth) resolve() error {
	if a.Mode == "" {
		a.Mode = AuthNone
	}
	switch a.Mode {
	case AuthNone:
		if a.Header != "" || len(a.TrustedProxies) > 0 {
			return fmt.Errorf("auth: header and trustedProxies are only used in %q mode", AuthProxyHeader)
		}
		if a.TokenEnv != "" {
			return fmt.Errorf("auth: tokenEnv is only used in %q mode", AuthToken)
		}
		return nil
	case AuthProxyHeader:
		if a.TokenEnv != "" {
			return fmt.Errorf("auth: tokenEnv is only used in %q mode", AuthToken)
		}
		if a.Header == "" {
			a.Header = DefaultAuthHeader
		}
		if strings.TrimSpace(a.Header) != a.Header || strings.ContainsAny(a.Header, " \t\r\n:") {
			return fmt.Errorf("auth: header %q is not a valid header name", a.Header)
		}
		if len(a.TrustedProxies) == 0 {
			return fmt.Errorf("auth: %q mode needs at least one entry in trustedProxies, "+
				"otherwise anyone reaching the port can assert any identity", AuthProxyHeader)
		}
		a.Trusted = make([]netip.Prefix, 0, len(a.TrustedProxies))
		for _, raw := range a.TrustedProxies {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
			if err != nil {
				return fmt.Errorf("auth: trustedProxies: %q is not a CIDR block: %w", raw, err)
			}
			a.Trusted = append(a.Trusted, prefix.Masked())
		}
		return nil
	case AuthToken:
		// A setting that does nothing in the chosen mode makes a file read as
		// though it protected something it does not.
		if a.Header != "" || len(a.TrustedProxies) > 0 {
			return fmt.Errorf("auth: header and trustedProxies are only used in %q mode", AuthProxyHeader)
		}
		if a.TokenEnv == "" {
			return fmt.Errorf("auth: %q mode needs tokenEnv, naming the environment variable that holds the token", AuthToken)
		}
		if !envNamePattern.MatchString(a.TokenEnv) {
			return fmt.Errorf("auth: tokenEnv %q is not a valid environment variable name", a.TokenEnv)
		}
		value, ok := os.LookupEnv(a.TokenEnv)
		if !ok {
			return fmt.Errorf("auth: environment variable %s is not set", a.TokenEnv)
		}
		// A secret pasted through a shell often carries a trailing newline.
		a.Token = NewSecret(strings.TrimSpace(value))
		if err := a.checkToken(); err != nil {
			// The variable is named, the value never is: these errors are
			// printed by the startup log.
			return fmt.Errorf("auth: environment variable %s: %w", a.TokenEnv, err)
		}
		return nil
	default:
		return fmt.Errorf("auth: mode %q is unknown, want %q, %q or %q", a.Mode, AuthNone, AuthProxyHeader, AuthToken)
	}
}

// checkToken validates the shared token itself. Nothing it returns carries the
// value -- only what is wrong with it, and where.
//
// The character set is not cosmetic. The token travels back to the browser in
// a cookie, and net/http drops the bytes a cookie value may not carry AFTER
// logging the offending byte: a token holding a space or a semicolon would
// have written pieces of itself into the daemon's log and then failed to
// authenticate anyway. Refusing it at load time is what keeps both from
// happening.
func (a Auth) checkToken() error {
	// plaintext, not Reveal: Reveal has exactly one legitimate caller in the
	// whole tree, the transport that builds the Authorization header for PVE,
	// and this is not it. Inside this package the value is reachable without
	// widening that door.
	token := a.Token.plaintext()
	if token == "" {
		return fmt.Errorf("the token is empty")
	}
	if len(token) < MinTokenLength {
		return fmt.Errorf("the token is %d characters, shorter than the %d required: "+
			"generate one with `openssl rand -hex 16`", len(token), MinTokenLength)
	}
	for i := 0; i < len(token); i++ {
		if !validCookieByte(token[i]) {
			return fmt.Errorf("the token holds a character a cookie cannot carry, at offset %d: "+
				"use printable ASCII without space, comma, semicolon, backslash or double quote", i)
		}
	}
	return nil
}

// validCookieByte is the byte set RFC 6265 allows in a cookie value, which is
// also the set net/http accepts without rewriting -- and complaining about --
// the value.
func validCookieByte(b byte) bool {
	if b <= 0x20 || b >= 0x7f {
		return false
	}
	switch b {
	case '"', ',', ';', '\\':
		return false
	}
	return true
}
