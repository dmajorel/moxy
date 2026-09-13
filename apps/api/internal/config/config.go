// Package config loads and validates the multi-cluster configuration of moxy.
//
// The file is JSON: the standard library has no YAML decoder and the backend
// takes no external dependency. No secret is ever stored in it — each cluster
// names an environment variable holding its API token secret, which Load reads
// and wraps in a Secret.
package config

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Defaults applied when the corresponding field is absent from the file.
const (
	// DefaultMemoryThreshold is the memory ratio above which a cluster or a
	// node is reported as under pressure.
	DefaultMemoryThreshold = 0.80
	// DefaultTimeout is the per-call budget for a single Proxmox request.
	DefaultTimeout = 4 * time.Second
	// DefaultConnectTimeout bounds getting a connection up — TCP handshake
	// and TLS — as opposed to getting an answer. It is short because a node
	// that is switched off, or behind a firewall that drops rather than
	// refuses, costs exactly this much before the next url is tried. Under
	// one shared timeout that cost was the whole per-call budget, and the
	// failover ran out of round before it reached a second node.
	DefaultConnectTimeout = 2 * time.Second
	// MaxTimeout is where a per-call budget stops being a tuning knob and
	// becomes a way to hang the poller on one unresponsive node.
	MaxTimeout = 60 * time.Second
)

// TLSMode selects how the certificate of a cluster is verified.
type TLSMode string

// Supported TLS modes.
const (
	// TLSModeSystem verifies against the system trust store. It is the
	// default when the mode is left empty.
	TLSModeSystem TLSMode = "system"
	// TLSModePinned verifies against the PEM bundle named by CAFile, which
	// is typically the pve-root-ca of the cluster.
	TLSModePinned TLSMode = "pinned"
	// TLSModeInsecure skips verification entirely. It is accepted per
	// cluster only, and callers are expected to warn about it — see
	// Config.InsecureClusters.
	TLSModeInsecure TLSMode = "insecure"
)

var (
	// clusterIDPattern is the accepted shape of a cluster identifier: it
	// ends up in URLs and in JSON keys, so it stays lowercase and terse.
	clusterIDPattern = regexp.MustCompile(`^[a-z0-9-]+$`)
	// tokenIDPattern is the Proxmox API token identifier, user@realm!name.
	tokenIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+![A-Za-z0-9._-]+$`)
	// envNamePattern is the shape of a portable environment variable name.
	envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// colorPattern is the accent colour of a cluster. It is passed through to
	// the frontend and ends up in a style attribute, so the shape is pinned
	// here rather than trusted there.
	colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

// Config is the whole configuration, defaults applied and secrets resolved.
type Config struct {
	// Auth is how moxy decides who is asking. Absent means nobody is asked.
	Auth       Auth       `json:"auth"`
	Thresholds Thresholds `json:"thresholds"`
	Clusters   []Cluster  `json:"clusters"`
}

// Thresholds holds the ratios shared by the overview and the capacity checks.
type Thresholds struct {
	// Memory is the used/total memory ratio above which an alert is raised,
	// in ]0,1]. Defaults to DefaultMemoryThreshold.
	Memory float64 `json:"memory"`
}

// TLS describes how one cluster is verified.
type TLS struct {
	// Mode is system, pinned or insecure. Empty means system.
	Mode TLSMode `json:"mode"`
	// CAFile is the PEM bundle to pin against, required in pinned mode.
	CAFile string `json:"caFile,omitempty"`
	// Pool is CAFile parsed, built once at load time so that the proxmox
	// client never reads the file again. It is nil unless Mode is pinned.
	Pool *x509.CertPool `json:"-"`
}

// Cluster is a single Proxmox VE cluster: one API endpoint per node, one API
// token, one TLS policy.
type Cluster struct {
	// ID is a stable, lowercase identifier, unique across the file.
	ID string `json:"id"`
	// Name is the label shown to the user.
	Name string `json:"name"`
	// Color is an optional accent, passed through to the frontend as is.
	Color *string `json:"color,omitempty"`
	// URLs are the node endpoints, at least one, all https and without a
	// path: the client appends /api2/json itself.
	URLs []string `json:"urls"`
	// TokenID is the non-sensitive half of the API token, user@realm!name.
	TokenID string `json:"tokenId"`
	// SecretEnv names the environment variable holding the token secret.
	SecretEnv string `json:"secretEnv"`
	// TLS is the certificate policy of this cluster.
	TLS TLS `json:"tls"`
	// Proxy is an optional HTTP proxy to reach this cluster through, as an
	// http, https or socks5 URL. Empty — the normal case — means a direct
	// connection: the proxy environment of the process is deliberately NOT
	// honoured, so that an intranet HTTPS_PROXY cannot silently insert an
	// intermediary between moxy and an hypervisor token.
	Proxy string `json:"proxy,omitempty"`
	// ProxyURL is Proxy parsed, built once at load time. It is nil unless
	// Proxy is set.
	ProxyURL *url.URL `json:"-"`
	// Timeout is the per-call budget as written in the file, for instance
	// "4s". Use RequestTimeout, its parsed form, at run time.
	Timeout string `json:"timeout,omitempty"`
	// RequestTimeout is Timeout parsed, defaulted to DefaultTimeout.
	RequestTimeout time.Duration `json:"-"`
	// ConnectTimeout is the budget for establishing a connection, as written
	// in the file. Use DialTimeout, its parsed form, at run time.
	ConnectTimeout string `json:"connectTimeout,omitempty"`
	// DialTimeout is ConnectTimeout parsed, defaulted to
	// DefaultConnectTimeout and never above RequestTimeout.
	DialTimeout time.Duration `json:"-"`
	// Secret is the token secret read from SecretEnv at load time. The field
	// is exported on purpose: fmt only redacts through Secret's methods when
	// it can reach the value, which it cannot do on an unexported field.
	Secret Secret `json:"-"`
}

// ValidationErrors gathers every problem found in one configuration file, so
// that a single run reports them all instead of one per attempt. Go 1.19 has no
// errors.Join, hence the explicit type.
type ValidationErrors []error

// Error implements error.
func (v ValidationErrors) Error() string {
	msgs := make([]string, 0, len(v))
	for _, err := range v {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// Load reads the configuration file at path, decodes it, resolves every cluster
// secret from the environment, applies the defaults and validates the result.
// It fails outright rather than starting with a half-usable cluster.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	cfg := &Config{}
	// STRICT. An unknown field is a typo, and a typo that decodes silently is
	// a setting the operator believes is applied. "memroy" left the memory
	// threshold at its default, "cafile" made a pinned cluster complain about
	// a missing caFile written two lines above it. JSON has no comments, so a
	// "_comment" key is itself a typo waiting to hide one.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	// One document, not a stream: trailing JSON means the file was edited into
	// something its author did not mean to write.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config %s: unexpected data after the configuration object", path)
	}
	// A relative caFile is resolved against the configuration file, not the
	// working directory: the container image has no WORKDIR, so "ca/x.pem"
	// next to /etc/moxy/config.json was looked up in /ca.
	if err := cfg.resolve(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	// The secrets are in the Secret values now. Dropping the variables keeps
	// them out of os.Environ() and out of anything this process may later
	// hand to a child. It is a narrow gain -- /proc/<pid>/environ still holds
	// the block the process started with -- and it costs the ability to
	// re-read a secret without a restart, which nothing does.
	cfg.unsetSecretEnv()
	return cfg, nil
}

// unsetSecretEnv drops every variable a cluster took its secret from. It runs
// once every cluster has been resolved, since two clusters may legitimately
// name the same variable.
func (c *Config) unsetSecretEnv() {
	for i := range c.Clusters {
		if name := c.Clusters[i].SecretEnv; name != "" {
			_ = os.Unsetenv(name)
		}
	}
}

// InsecureClusters lists the identifiers of the clusters whose TLS
// verification is disabled. The package deliberately does not log: it is up to
// the caller to warn, naming each cluster.
func (c *Config) InsecureClusters() []string {
	var ids []string
	for i := range c.Clusters {
		if c.Clusters[i].TLS.Mode == TLSModeInsecure {
			ids = append(ids, c.Clusters[i].ID)
		}
	}
	return ids
}

// normalizeURL is the comparison form of a node endpoint: scheme and host
// lowercased, trailing slash dropped. It is only ever used to tell two
// configured urls apart, never to build a request.
func normalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.ToLower(strings.TrimRight(strings.TrimSpace(raw), "/"))
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

// resolve applies the defaults, reads the secrets and validates everything,
// collecting the problems instead of stopping at the first one.
func (c *Config) resolve(baseDir string) error {
	var errs ValidationErrors

	if c.Thresholds.Memory == 0 {
		c.Thresholds.Memory = DefaultMemoryThreshold
	}
	if c.Thresholds.Memory <= 0 || c.Thresholds.Memory > 1 {
		errs = append(errs, fmt.Errorf("thresholds.memory: %v is out of range, want a ratio in ]0,1]", c.Thresholds.Memory))
	}

	if err := c.Auth.resolve(); err != nil {
		errs = append(errs, err)
	}

	if len(c.Clusters) == 0 {
		errs = append(errs, fmt.Errorf("clusters: at least one cluster is required"))
	}

	seen := make(map[string]bool, len(c.Clusters))
	// Across clusters, not only within one: the same endpoint declared twice
	// means moxy polls one cluster under two names, doubling the load on it
	// and counting its nodes and guests twice in the totals.
	seenURL := make(map[string]string)
	for i := range c.Clusters {
		cl := &c.Clusters[i]
		where := fmt.Sprintf("clusters[%d]", i)
		if cl.ID != "" {
			where = fmt.Sprintf("cluster %q", cl.ID)
		}
		switch {
		case cl.ID == "":
			errs = append(errs, fmt.Errorf("%s: id is required", where))
		case !clusterIDPattern.MatchString(cl.ID):
			errs = append(errs, fmt.Errorf("%s: id must match %s", where, clusterIDPattern))
		case seen[cl.ID]:
			errs = append(errs, fmt.Errorf("%s: duplicate id", where))
		default:
			seen[cl.ID] = true
		}
		errs = append(errs, cl.resolve(where, baseDir)...)

		for _, raw := range cl.URLs {
			key := normalizeURL(raw)
			if key == "" {
				continue
			}
			if owner, ok := seenURL[key]; ok {
				errs = append(errs, fmt.Errorf("%s: urls: %q is already declared by %s", where, raw, owner))
				continue
			}
			seenURL[key] = where
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// resolve fills in the defaults of a single cluster and returns its problems.
// where is the prefix identifying the cluster in every message.
func (cl *Cluster) resolve(where, baseDir string) []error {
	var errs []error

	if cl.Name == "" {
		errs = append(errs, fmt.Errorf("%s: name is required", where))
	}

	if len(cl.URLs) == 0 {
		errs = append(errs, fmt.Errorf("%s: urls: at least one url is required", where))
	}
	withinCluster := make(map[string]bool, len(cl.URLs))
	for _, raw := range cl.URLs {
		if err := checkURL(raw); err != nil {
			errs = append(errs, fmt.Errorf("%s: urls: %w", where, err))
			continue
		}
		// A list that repeats an endpoint promises a failover it cannot do:
		// the next attempt goes back to the node that just failed.
		key := normalizeURL(raw)
		if withinCluster[key] {
			errs = append(errs, fmt.Errorf("%s: urls: %q appears twice", where, raw))
			continue
		}
		withinCluster[key] = true
	}

	if cl.Color != nil && !colorPattern.MatchString(*cl.Color) {
		errs = append(errs, fmt.Errorf("%s: color %q must be a #rrggbb value", where, *cl.Color))
	}

	if cl.TokenID == "" {
		errs = append(errs, fmt.Errorf("%s: tokenId is required", where))
	} else if !tokenIDPattern.MatchString(cl.TokenID) {
		errs = append(errs, fmt.Errorf("%s: tokenId %q is malformed, want user@realm!tokenid", where, cl.TokenID))
	}

	if err := cl.resolveSecret(where); err != nil {
		errs = append(errs, err)
	}

	if err := cl.resolveTLS(where, baseDir); err != nil {
		errs = append(errs, err)
	}

	if err := cl.resolveTimeout(where); err != nil {
		errs = append(errs, err)
	}

	if err := cl.resolveProxy(where); err != nil {
		errs = append(errs, err)
	}

	return errs
}

// resolveSecret reads the token secret from the environment. The value itself
// never appears in an error message.
func (cl *Cluster) resolveSecret(where string) error {
	if cl.SecretEnv == "" {
		return fmt.Errorf("%s: secretEnv is required", where)
	}
	if !envNamePattern.MatchString(cl.SecretEnv) {
		return fmt.Errorf("%s: secretEnv %q is not a valid environment variable name", where, cl.SecretEnv)
	}
	value, ok := os.LookupEnv(cl.SecretEnv)
	if !ok {
		return fmt.Errorf("%s: environment variable %s is not set", where, cl.SecretEnv)
	}
	// A secret pasted through a shell often carries a trailing newline.
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s: environment variable %s is empty", where, cl.SecretEnv)
	}
	cl.Secret = NewSecret(value)
	return nil
}

// resolveTLS defaults the mode and, in pinned mode, builds the certificate pool
// once so that the client never reads the CA file again.
func (cl *Cluster) resolveTLS(where, baseDir string) error {
	if cl.TLS.Mode == "" {
		cl.TLS.Mode = TLSModeSystem
	}
	switch cl.TLS.Mode {
	case TLSModeSystem, TLSModeInsecure:
		if cl.TLS.CAFile != "" {
			return fmt.Errorf("%s: tls.caFile is only used in %q mode", where, TLSModePinned)
		}
		return nil
	case TLSModePinned:
		if cl.TLS.CAFile == "" {
			return fmt.Errorf("%s: tls.caFile is required in %q mode", where, TLSModePinned)
		}
		if !filepath.IsAbs(cl.TLS.CAFile) && baseDir != "" {
			cl.TLS.CAFile = filepath.Join(baseDir, cl.TLS.CAFile)
		}
		pem, err := os.ReadFile(cl.TLS.CAFile)
		if err != nil {
			return fmt.Errorf("%s: tls.caFile: %w", where, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return fmt.Errorf("%s: tls.caFile %s contains no valid PEM certificate", where, cl.TLS.CAFile)
		}
		cl.TLS.Pool = pool
		return nil
	default:
		return fmt.Errorf("%s: tls.mode %q is unknown, want %q, %q or %q", where, cl.TLS.Mode, TLSModeSystem, TLSModePinned, TLSModeInsecure)
	}
}

// resolveTimeout parses the per-call budget, defaulting to DefaultTimeout.
func (cl *Cluster) resolveTimeout(where string) error {
	cl.RequestTimeout = DefaultTimeout
	if cl.Timeout != "" {
		d, err := time.ParseDuration(cl.Timeout)
		if err != nil {
			return fmt.Errorf("%s: timeout %q is malformed: %w", where, cl.Timeout, err)
		}
		if d <= 0 {
			return fmt.Errorf("%s: timeout %q must be positive", where, cl.Timeout)
		}
		if d > MaxTimeout {
			return fmt.Errorf("%s: timeout %q is above the %s maximum", where, cl.Timeout, MaxTimeout)
		}
		cl.RequestTimeout = d
	}

	cl.DialTimeout = DefaultConnectTimeout
	if cl.ConnectTimeout != "" {
		d, err := time.ParseDuration(cl.ConnectTimeout)
		if err != nil {
			return fmt.Errorf("%s: connectTimeout %q is malformed: %w", where, cl.ConnectTimeout, err)
		}
		if d <= 0 {
			return fmt.Errorf("%s: connectTimeout %q must be positive", where, cl.ConnectTimeout)
		}
		cl.DialTimeout = d
	}
	// Connecting is part of answering, so a connect budget above the call
	// budget can never be reached and only misleads whoever reads the file.
	if cl.DialTimeout > cl.RequestTimeout {
		return fmt.Errorf("%s: connectTimeout %s is above timeout %s", where, cl.DialTimeout, cl.RequestTimeout)
	}
	return nil
}

// resolveProxy parses the optional per-cluster proxy. An absent proxy is the
// normal case and leaves ProxyURL nil, which the client reads as "connect
// directly".
func (cl *Cluster) resolveProxy(where string) error {
	raw := strings.TrimSpace(cl.Proxy)
	cl.Proxy = raw
	if raw == "" {
		cl.ProxyURL = nil
		return nil
	}
	u, err := checkProxyURL(raw)
	if err != nil {
		return fmt.Errorf("%s: proxy: %w", where, err)
	}
	cl.ProxyURL = u
	return nil
}

// proxySchemes are the schemes net/http knows how to reach a proxy with.
var proxySchemes = map[string]bool{"http": true, "https": true, "socks5": true}

// checkProxyURL enforces an absolute proxy URL of a supported scheme, without
// credentials: like the rest of the file, a proxy password would be a secret at
// rest in a file that gets copied around, and belongs in an environment
// variable — which moxy does not read for a proxy, on purpose.
func checkProxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%q is not a valid url: %w", raw, err)
	}
	if !u.IsAbs() {
		return nil, fmt.Errorf("%q must be an absolute url", raw)
	}
	if !proxySchemes[u.Scheme] {
		return nil, fmt.Errorf("%q must use http, https or socks5, got %q", raw, u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%q has no host", raw)
	}
	if u.User != nil {
		// Redacted, not raw: the message is going to a log, and the point of
		// the rule is precisely that this URL may carry a password.
		return nil, fmt.Errorf("%q must not carry credentials", u.Redacted())
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		return nil, fmt.Errorf("%q must not have a path", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%q must not have a query or a fragment", raw)
	}
	return u, nil
}

// checkURL enforces an absolute https endpoint without a path: the proxmox
// client appends /api2/json to it, so anything else would silently break.
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%q is not a valid url: %w", raw, err)
	}
	if !u.IsAbs() {
		return fmt.Errorf("%q must be an absolute url", raw)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%q must use https, got %q", raw, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%q has no host", raw)
	}
	if u.User != nil {
		// Redacted, not raw, exactly as checkProxyURL does: this message is
		// going to a log, and the whole reason for the rule is that the URL
		// it quotes may carry a password.
		return fmt.Errorf("%q must not carry credentials", u.Redacted())
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		return fmt.Errorf("%q must not have a path, the client appends /api2/json", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%q must not have a query or a fragment", raw)
	}
	return nil
}
