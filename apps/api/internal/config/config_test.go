package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const secretEnv = "MOXY_SECRET_QUALIFICATION"

// baseCluster is a minimal valid cluster; tests mutate a copy of it.
func baseCluster() map[string]any {
	return map[string]any{
		"id":        "qualification",
		"name":      "Qualification",
		"urls":      []any{"https://prox-qual-2201-cit:8006"},
		"tokenId":   "moxy@pve!ro",
		"secretEnv": secretEnv,
	}
}

func doc(clusters ...map[string]any) map[string]any {
	list := make([]any, 0, len(clusters))
	for _, c := range clusters {
		list = append(list, c)
	}
	return map[string]any{"clusters": list}
}

// writeConfig marshals the document to a file in a temporary directory and
// returns its path.
func writeConfig(t *testing.T, document map[string]any) string {
	t.Helper()
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// writeCA writes a self-signed certificate authority in PEM form and returns
// its path. No network and no fixture file are involved.
func writeCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "moxy test ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, block, 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	return path
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	cfg, err := Load(writeConfig(t, doc(baseCluster())))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Thresholds.Memory != DefaultMemoryThreshold {
		t.Errorf("thresholds.memory = %v, want %v", cfg.Thresholds.Memory, DefaultMemoryThreshold)
	}
	if len(cfg.Clusters) != 1 {
		t.Fatalf("got %d clusters, want 1", len(cfg.Clusters))
	}
	cl := cfg.Clusters[0]
	if cl.RequestTimeout != DefaultTimeout {
		t.Errorf("timeout = %v, want %v", cl.RequestTimeout, DefaultTimeout)
	}
	if cl.TLS.Mode != TLSModeSystem {
		t.Errorf("tls.mode = %q, want %q", cl.TLS.Mode, TLSModeSystem)
	}
	if cl.TLS.Pool != nil {
		t.Error("tls.pool is set in system mode")
	}
	if cl.Color != nil {
		t.Errorf("color = %v, want nil", cl.Color)
	}
	// No proxy by default: nodes are reached directly, whatever the
	// environment of the process says.
	if cl.ProxyURL != nil {
		t.Errorf("proxyURL = %v, want nil", cl.ProxyURL)
	}
	if cl.Secret.Reveal() != sentinel {
		t.Error("the secret was not read from the environment")
	}
	if got := cfg.InsecureClusters(); len(got) != 0 {
		t.Errorf("InsecureClusters() = %v, want none", got)
	}
}

func TestLoadReadsExplicitValues(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	cl := baseCluster()
	cl["color"] = "#378ADD"
	cl["timeout"] = "9s"
	cl["proxy"] = "http://proxy.invalid:3128"
	cl["urls"] = []any{"https://prox-qual-2201-cit:8006", "https://prox-qual-2202-cit:8006"}
	document := doc(cl)
	document["thresholds"] = map[string]any{"memory": 0.9}

	cfg, err := Load(writeConfig(t, document))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Thresholds.Memory != 0.9 {
		t.Errorf("thresholds.memory = %v, want 0.9", cfg.Thresholds.Memory)
	}
	got := cfg.Clusters[0]
	if got.RequestTimeout != 9*time.Second {
		t.Errorf("timeout = %v, want 9s", got.RequestTimeout)
	}
	if got.Color == nil || *got.Color != "#378ADD" {
		t.Errorf("color = %v, want #378ADD", got.Color)
	}
	if len(got.URLs) != 2 {
		t.Errorf("got %d urls, want 2", len(got.URLs))
	}
	if got.ProxyURL == nil || got.ProxyURL.String() != "http://proxy.invalid:3128" {
		t.Errorf("proxyURL = %v, want http://proxy.invalid:3128", got.ProxyURL)
	}
}

func TestLoadPinnedTLSBuildsPool(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	cl := baseCluster()
	cl["tls"] = map[string]any{"mode": "pinned", "caFile": writeCA(t)}

	cfg, err := Load(writeConfig(t, doc(cl)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	pool := cfg.Clusters[0].TLS.Pool
	if pool == nil {
		t.Fatal("tls.pool is nil in pinned mode")
	}
	want := x509.NewCertPool()
	raw, err := os.ReadFile(cfg.Clusters[0].TLS.CAFile)
	if err != nil {
		t.Fatalf("read ca: %v", err)
	}
	if !want.AppendCertsFromPEM(raw) {
		t.Fatal("the generated ca is not a valid PEM bundle")
	}
	if !pool.Equal(want) {
		t.Error("tls.pool does not hold the pinned certificate")
	}
}

func TestLoadInsecureClusters(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	t.Setenv("MOXY_SECRET_PRODUCTION", sentinel)
	lax := baseCluster()
	lax["id"] = "production"
	lax["name"] = "Production"
	lax["secretEnv"] = "MOXY_SECRET_PRODUCTION"
	lax["tls"] = map[string]any{"mode": "insecure"}

	cfg, err := Load(writeConfig(t, doc(baseCluster(), lax)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := cfg.InsecureClusters()
	if len(got) != 1 || got[0] != "production" {
		t.Errorf("InsecureClusters() = %v, want [production]", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("Load() of a missing file returned no error")
	}
}

func TestLoadMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() of malformed JSON returned no error")
	}
}

func TestLoadValidation(t *testing.T) {
	caFile := writeCA(t)

	withCluster := func(mutate func(map[string]any)) map[string]any {
		cl := baseCluster()
		mutate(cl)
		return doc(cl)
	}

	cases := []struct {
		name     string
		document map[string]any
		unset    []string
		env      map[string]string
		want     string
	}{
		{
			name:     "no cluster",
			document: doc(),
			want:     "at least one cluster",
		},
		{
			name: "duplicate id",
			document: func() map[string]any {
				second := baseCluster()
				second["name"] = "Qualification bis"
				return doc(baseCluster(), second)
			}(),
			want: "duplicate id",
		},
		{
			name:     "malformed id",
			document: withCluster(func(c map[string]any) { c["id"] = "Qualification_1" }),
			want:     "id must match",
		},
		{
			name:     "missing id",
			document: withCluster(func(c map[string]any) { delete(c, "id") }),
			want:     "id is required",
		},
		{
			name:     "missing name",
			document: withCluster(func(c map[string]any) { delete(c, "name") }),
			want:     "name is required",
		},
		{
			name:     "no url",
			document: withCluster(func(c map[string]any) { c["urls"] = []any{} }),
			want:     "at least one url",
		},
		{
			name:     "url not https",
			document: withCluster(func(c map[string]any) { c["urls"] = []any{"http://prox-qual-2201-cit:8006"} }),
			want:     "must use https",
		},
		{
			name:     "url with a path",
			document: withCluster(func(c map[string]any) { c["urls"] = []any{"https://prox-qual-2201-cit:8006/api2/json"} }),
			want:     "must not have a path",
		},
		{
			name:     "relative url",
			document: withCluster(func(c map[string]any) { c["urls"] = []any{"//prox-qual-2201-cit:8006"} }),
			want:     "absolute url",
		},
		{
			name:     "url without a host",
			document: withCluster(func(c map[string]any) { c["urls"] = []any{"https://"} }),
			want:     "has no host",
		},
		{
			name:     "malformed tokenId",
			document: withCluster(func(c map[string]any) { c["tokenId"] = "moxy@pve" }),
			want:     "tokenId",
		},
		{
			name:     "missing secretEnv",
			document: withCluster(func(c map[string]any) { delete(c, "secretEnv") }),
			want:     "secretEnv is required",
		},
		{
			name:     "secret absent from the environment",
			document: withCluster(func(c map[string]any) { c["secretEnv"] = "MOXY_SECRET_NEVER_SET" }),
			unset:    []string{"MOXY_SECRET_NEVER_SET"},
			want:     "is not set",
		},
		{
			name:     "secret empty in the environment",
			document: withCluster(func(c map[string]any) {}),
			env:      map[string]string{secretEnv: "  "},
			want:     "is empty",
		},
		{
			name:     "unknown tls mode",
			document: withCluster(func(c map[string]any) { c["tls"] = map[string]any{"mode": "lax"} }),
			want:     "tls.mode",
		},
		{
			name:     "pinned without caFile",
			document: withCluster(func(c map[string]any) { c["tls"] = map[string]any{"mode": "pinned"} }),
			want:     "tls.caFile is required",
		},
		{
			name: "unreadable caFile",
			document: withCluster(func(c map[string]any) {
				c["tls"] = map[string]any{"mode": "pinned", "caFile": filepath.Join(t.TempDir(), "absent.pem")}
			}),
			want: "tls.caFile",
		},
		{
			name: "caFile is not a certificate",
			document: withCluster(func(c map[string]any) {
				path := filepath.Join(t.TempDir(), "broken.pem")
				if err := os.WriteFile(path, []byte("-----BEGIN CERTIFICATE-----\nnot base64\n"), 0o600); err != nil {
					t.Fatalf("write broken pem: %v", err)
				}
				c["tls"] = map[string]any{"mode": "pinned", "caFile": path}
			}),
			want: "no valid PEM certificate",
		},
		{
			name: "caFile outside pinned mode",
			document: withCluster(func(c map[string]any) {
				c["tls"] = map[string]any{"mode": "system", "caFile": caFile}
			}),
			want: "only used in",
		},
		{
			name:     "malformed timeout",
			document: withCluster(func(c map[string]any) { c["timeout"] = "4" }),
			want:     "timeout",
		},
		{
			name:     "zero timeout",
			document: withCluster(func(c map[string]any) { c["timeout"] = "0s" }),
			want:     "must be positive",
		},
		{
			name:     "schemeless proxy",
			document: withCluster(func(c map[string]any) { c["proxy"] = "//proxy.invalid:3128" }),
			want:     "absolute url",
		},
		{
			// "host:port" parses as a scheme with an opaque part, not as a
			// host: it is the shape net/http would have accepted from the
			// environment, and the one an operator will try first.
			name:     "proxy without a scheme",
			document: withCluster(func(c map[string]any) { c["proxy"] = "proxy.invalid:3128" }),
			want:     "http, https or socks5",
		},
		{
			name:     "unsupported proxy scheme",
			document: withCluster(func(c map[string]any) { c["proxy"] = "ftp://proxy.invalid:3128" }),
			want:     "http, https or socks5",
		},
		{
			name:     "proxy with credentials",
			document: withCluster(func(c map[string]any) { c["proxy"] = "http://user:pass@proxy.invalid:3128" }),
			want:     "must not carry credentials",
		},
		{
			name:     "proxy with a path",
			document: withCluster(func(c map[string]any) { c["proxy"] = "http://proxy.invalid:3128/pac" }),
			want:     "must not have a path",
		},
		{
			name: "negative threshold",
			document: func() map[string]any {
				d := doc(baseCluster())
				d["thresholds"] = map[string]any{"memory": -0.1}
				return d
			}(),
			want: "out of range",
		},
		{
			name: "threshold above one",
			document: func() map[string]any {
				d := doc(baseCluster())
				d["thresholds"] = map[string]any{"memory": 1.5}
				return d
			}(),
			want: "out of range",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(secretEnv, sentinel)
			for name, value := range tc.env {
				t.Setenv(name, value)
			}
			for _, name := range tc.unset {
				t.Setenv(name, "placeholder")
				if err := os.Unsetenv(name); err != nil {
					t.Fatalf("unset %s: %v", name, err)
				}
			}
			_, err := Load(writeConfig(t, tc.document))
			if err == nil {
				t.Fatalf("Load() returned no error, want one mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Load() error = %v, want it to mention %q", err, tc.want)
			}
			if strings.Contains(err.Error(), sentinel) {
				t.Errorf("Load() error leaked the secret: %v", err)
			}
		})
	}
}

// TestValidationNamesEveryFaultyCluster checks that the problems are collected
// rather than reported one per run, and that each message names its cluster.
func TestValidationNamesEveryFaultyCluster(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	t.Setenv("MOXY_SECRET_PRODUCTION", sentinel)
	first := baseCluster()
	first["urls"] = []any{"http://prox-qual-2201-cit:8006"}
	second := baseCluster()
	second["id"] = "production"
	second["name"] = "Production"
	second["secretEnv"] = "MOXY_SECRET_PRODUCTION"
	second["tokenId"] = "moxy@pve"

	_, err := Load(writeConfig(t, doc(first, second)))
	if err == nil {
		t.Fatal("Load() returned no error")
	}
	for _, want := range []string{`cluster "qualification"`, `cluster "production"`, "https", "tokenId"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
}

// TestZeroThresholdFallsBackToDefault documents the consequence of encoding the
// threshold as a plain float: JSON cannot tell an absent field from 0, so both
// mean "use the default".
func TestZeroThresholdFallsBackToDefault(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	document := doc(baseCluster())
	document["thresholds"] = map[string]any{"memory": 0}

	cfg, err := Load(writeConfig(t, document))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Thresholds.Memory != DefaultMemoryThreshold {
		t.Errorf("thresholds.memory = %v, want %v", cfg.Thresholds.Memory, DefaultMemoryThreshold)
	}
}

// TestLoadedConfigNeverLeaksTheSecret is the most important test of the
// package: no textual rendering of the configuration, at any level, may show
// the token secret.
func TestLoadedConfigNeverLeaksTheSecret(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	cl := baseCluster()
	cl["color"] = "#378ADD"
	cl["timeout"] = "7s"

	cfg, err := Load(writeConfig(t, doc(cl)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Clusters[0].Secret.Reveal() != sentinel {
		t.Fatalf("Reveal() = %q, want the value read from the environment", cfg.Clusters[0].Secret.Reveal())
	}

	subjects := map[string]any{
		"*Config":   cfg,
		"Config":    *cfg,
		"Cluster":   cfg.Clusters[0],
		"*Cluster":  &cfg.Clusters[0],
		"[]Cluster": cfg.Clusters,
	}
	for name, subject := range subjects {
		for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
			out := fmt.Sprintf(format, subject)
			if strings.Contains(out, sentinel) {
				t.Errorf("Sprintf(%q, %s) leaked the secret: %s", format, name, out)
			}
		}
		raw, err := json.Marshal(subject)
		if err != nil {
			t.Fatalf("json.Marshal(%s) error = %v", name, err)
		}
		if strings.Contains(string(raw), sentinel) {
			t.Errorf("json.Marshal(%s) leaked the secret: %s", name, raw)
		}
	}
}
