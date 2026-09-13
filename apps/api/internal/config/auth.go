package config

import (
	"fmt"
	"net/netip"
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
)

// DefaultAuthHeader is the header most authenticating proxies set.
const DefaultAuthHeader = "X-Forwarded-User"

// Auth is how moxy decides who is asking.
//
// It is deliberately not an authentication system of its own: moxy holds
// hypervisor tokens and has no business holding passwords as well. What it can
// do is refuse to serve a request that did not come through the component that
// does authenticate.
type Auth struct {
	// Mode is "none" or "proxy-header". Empty means "none".
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

// Enabled reports whether a request has to carry an identity.
func (a Auth) Enabled() bool { return a.Mode == AuthProxyHeader }

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
		return nil
	case AuthProxyHeader:
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
	default:
		return fmt.Errorf("auth: mode %q is unknown, want %q or %q", a.Mode, AuthNone, AuthProxyHeader)
	}
}
