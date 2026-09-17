package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// silenceLog muffles the package logger for the duration of a test. newSources
// and newWeb both report what they have decided, which is the right behaviour
// for a daemon and pure noise in a test run.
func silenceLog(t *testing.T) {
	t.Helper()
	saved := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(saved) })
}

// TestNewSourcesMockNeedsNothing: -mock is the setup a frontend developer and
// the end-to-end checks run in, and its whole promise is that it reads no
// configuration and contacts no cluster. It must therefore build both sources
// with an empty config path, and it must hand back a nil readiness channel --
// a nil channel is what makes /readyz answer at once, and a non-nil one that
// nobody ever closes would leave the mock permanently not ready.
func TestNewSourcesMockNeedsNothing(t *testing.T) {
	silenceLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src, err := newSources(ctx, "", true)
	if err != nil {
		t.Fatalf("newSources: %v", err)
	}
	overview := src.overview
	if overview == nil {
		t.Error("no overview source in mock mode")
	}
	if src.detail == nil {
		t.Error("no detail source in mock mode")
	}
	if src.ready != nil {
		t.Error("mock mode returned a readiness channel: there is nothing to warm up")
	}
	if src.auth.Enabled() {
		t.Error("mock mode must not ask for an identity: it serves sample data")
	}
	if src.maintenanceUsers != nil {
		t.Error("mock mode named users allowed to drain a node: it reads no configuration")
	}

	// Not just non-nil: the overview must actually answer, since this is the
	// document the whole frontend is developed against.
	snapshot, err := overview.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if len(snapshot.Clusters) == 0 {
		t.Error("the sample overview carries no cluster")
	}
}

// TestNewSourcesReportsAMissingConfigurationInPlainWords: the first run of
// anyone who did not read the README lands here, and "open
// /etc/moxy/config.json: no such file or directory" tells them nothing about
// what to do next.
func TestNewSourcesReportsAMissingConfigurationInPlainWords(t *testing.T) {
	silenceLog(t)
	path := filepath.Join(t.TempDir(), "absent.json")

	_, err := newSources(context.Background(), path, false)
	if err == nil {
		t.Fatal("newSources with no configuration file returned no error")
	}
	for _, want := range []string{path, "config.example.json", "-mock"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}
}

// TestNewWebWithoutDirectoryIsAPIOnly: no -web is the development setup, where
// Vite serves the frontend on its own port. It is not a failure, and it must
// not be turned into an empty handler either: server.Options.Web being nil is
// what makes an unknown path answer 404 instead of a page.
func TestNewWebWithoutDirectoryIsAPIOnly(t *testing.T) {
	silenceLog(t)
	web, err := newWeb("")
	if err != nil {
		t.Fatalf("newWeb(\"\") error = %v, want nil", err)
	}
	if web != nil {
		t.Errorf("newWeb(\"\") = %v, want nil", web)
	}
}

// TestNewWebWithoutBundleExplainsItself: pointing -web at a directory with no
// index.html is the mistake of someone who has not run the frontend build, or
// who aimed at apps/web instead of apps/web/dist. os.ErrNotExist is
// deliberately NOT propagated here: it is replaced by the sentence that says
// which script to run, because nothing upstream matches on it -- unlike the
// configuration path, where main.go does.
func TestNewWebWithoutBundleExplainsItself(t *testing.T) {
	silenceLog(t)
	dir := t.TempDir()

	web, err := newWeb(dir)
	if err == nil {
		t.Fatal("newWeb() on a directory without index.html returned no error")
	}
	if web != nil {
		t.Errorf("newWeb() = %v on error, want nil", web)
	}
	for _, want := range []string{dir, "build-web.sh", "-web"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}
}

// TestNewWebServesTheBundle is the other half: a real bundle is accepted and
// the handler returned actually serves it.
func TestNewWebServesTheBundle(t *testing.T) {
	silenceLog(t)
	dir := t.TempDir()
	const index = `<!doctype html><html><body><div id="root"></div></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}

	web, err := newWeb(dir)
	if err != nil {
		t.Fatalf("newWeb: %v", err)
	}
	if web == nil {
		t.Fatal("newWeb returned no handler and no error")
	}
	rec := httptest.NewRecorder()
	web.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Errorf("body = %q, want index.html", rec.Body.String())
	}
}

// TestNewSourcesFromAConfiguration walks the real path: a configuration file
// is read, a poller and a detail service are built on top of it, and the
// readiness channel is the poller's own -- which is what keeps /readyz from
// answering yes before a single cluster has been read.
//
// No cluster is reachable, and none needs to be: newSources opens no
// connection of its own, and the poll round it starts is cancelled with the
// context before it can finish one.
func TestNewSourcesFromAConfiguration(t *testing.T) {
	silenceLog(t)
	t.Setenv("MOXY_TEST_SECRET", "0f3c1c1e-4a2b-4c6d-9e8f-1a2b3c4d5e6f")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	const document = `{
	  "clusters": [
	    {
	      "id": "qualification",
	      "name": "Qualification",
	      "urls": ["https://prox-qual-2201-cit.invalid:8006"],
	      "tokenId": "moxy@pve!ro",
	      "secretEnv": "MOXY_TEST_SECRET",
	      "tls": {"mode": "insecure"}
	    }
	  ]
	}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src, err := newSources(ctx, path, false)
	if err != nil {
		t.Fatalf("newSources: %v", err)
	}
	if src.overview == nil || src.detail == nil {
		t.Fatal("newSources returned an incomplete pair of sources")
	}
	ready := src.ready
	if ready == nil {
		t.Error("no readiness channel: /readyz would answer yes before the first poll")
	} else {
		select {
		case <-ready:
			t.Error("the daemon called itself ready before any cluster answered")
		default:
		}
	}
	// Nothing in the file asks for an identity, so the zero value stands and
	// the daemon serves everyone -- which main.go warns about separately.
	if src.auth.Enabled() {
		t.Error("auth is enabled without a proxyHeader section")
	}
}
