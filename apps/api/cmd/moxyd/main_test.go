package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/dmajorel/moxy/apps/api/internal/server"
)

func TestBanner(t *testing.T) {
	tests := []struct {
		name                             string
		version, goVersion, goos, goarch string
		want                             string
	}{
		{
			name:      "released build",
			version:   "v0.3.1",
			goVersion: "go1.27.0",
			goos:      "linux",
			goarch:    "amd64",
			want:      "moxyd v0.3.1 starting (go1.27.0, linux/amd64)",
		},
		{
			// scripts/build.sh falls back to "dev" when git describes nothing.
			name:      "unversioned build",
			version:   "dev",
			goVersion: "go1.19.8",
			goos:      "darwin",
			goarch:    "arm64",
			want:      "moxyd dev starting (go1.19.8, darwin/arm64)",
		},
		{
			// A toolchain built from source names itself this way. It goes in
			// verbatim: normalising it would drop the only identity there is.
			name:      "toolchain built from source",
			version:   "v0.3.1-2-gdeadbee-dirty",
			goVersion: "devel +c1b2d3e4f5 Fri Sep 12 09:00:00 2026 +0000",
			goos:      "linux",
			goarch:    "amd64",
			want:      "moxyd v0.3.1-2-gdeadbee-dirty starting (devel +c1b2d3e4f5 Fri Sep 12 09:00:00 2026 +0000, linux/amd64)",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := banner(tt.version, tt.goVersion, tt.goos, tt.goarch); got != tt.want {
				t.Errorf("banner() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The line is only worth logging if it reports the running binary rather than
// constants of its own, so check it reads the build identity it claims to.
func TestStartupBannerReportsBuildIdentity(t *testing.T) {
	saved := server.Version
	defer func() { server.Version = saved }()
	server.Version = "v9.9.9-test"

	got := startupBanner()
	if want := banner("v9.9.9-test", runtime.Version(), runtime.GOOS, runtime.GOARCH); got != want {
		t.Errorf("startupBanner() = %q, want %q", got, want)
	}
	for _, part := range []string{"v9.9.9-test", runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH} {
		if !strings.Contains(got, part) {
			t.Errorf("startupBanner() = %q, missing %q", got, part)
		}
	}
}

// lookupFrom turns a map into the os.LookupEnv shape, so that the test says
// what the environment holds without touching the environment of the process.
func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := env[name]
		return value, ok
	}
}

func TestProxyEnvVars(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{
			name: "empty environment",
			env:  nil,
		},
		{
			name: "https_proxy set",
			env:  map[string]string{"https_proxy": "http://proxy.invalid:3128"},
			want: []string{"https_proxy"},
		},
		{
			name: "several variables set",
			env: map[string]string{
				"HTTPS_PROXY": "http://proxy.invalid:3128",
				"ALL_PROXY":   "socks5://proxy.invalid:1080",
			},
			want: []string{"HTTPS_PROXY", "ALL_PROXY"},
		},
		{
			// An unset variable and one set to the empty string mean the same
			// thing to net/http, and must mean the same thing here.
			name: "set but empty",
			env:  map[string]string{"HTTPS_PROXY": "   "},
		},
		{
			// HTTP_PROXY would never have applied: node urls are https only.
			name: "http_proxy is not one of them",
			env:  map[string]string{"HTTP_PROXY": "http://proxy.invalid:3128"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := proxyEnvVars(lookupFrom(tt.env))
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("proxyEnvVars() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestHealthcheckProbesTheLocalAddress: the image has no shell and no curl, so
// the HEALTHCHECK runs the binary itself. The probe has to reach the daemon on
// its own address and report the answer as an exit status.
func TestHealthcheck(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")

	if err := healthcheck(addr); err != nil {
		t.Fatalf("healthcheck: %v", err)
	}
	if asked != "/healthz" {
		t.Errorf("probed %q, want /healthz", asked)
	}
}

func TestHealthcheckFailsOnANonOKAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	if err := healthcheck(strings.TrimPrefix(srv.URL, "http://")); err == nil {
		t.Fatal("want an error on a 503")
	}
}

// TestHealthcheckRewritesAWildcardAddress: MOXY_ADDR is 0.0.0.0:8080 in the
// image, which is a listen address and not a destination. The probe must talk
// to loopback, which also keeps the request inside the container.
func TestHealthcheckRewritesAWildcardAddress(t *testing.T) {
	// Nothing is listening: the point is the address it tried, which the error
	// carries, not whether the probe succeeded.
	err := healthcheck("0.0.0.0:1")
	if err == nil {
		t.Fatal("want an error: nothing is listening on port 1")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("error = %v, want it to name the loopback address", err)
	}
}
