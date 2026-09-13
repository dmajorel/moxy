// Command moxyd is the moxy aggregating backend: it queries N Proxmox VE
// clusters and exposes a unified API to the frontend, without ever handing API
// tokens to the browser.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
	"github.com/dmajorel/moxy/apps/api/internal/config"
	"github.com/dmajorel/moxy/apps/api/internal/detail"
	"github.com/dmajorel/moxy/apps/api/internal/proxmox"
	"github.com/dmajorel/moxy/apps/api/internal/server"
)

const shutdownTimeout = 5 * time.Second

func main() {
	addr := flag.String("addr", defaultAddr(), "HTTP listen address")
	configPath := flag.String("config", defaultConfigPath(), "path to the cluster configuration file")
	webDir := flag.String("web", defaultWebDir(), "directory of the built frontend bundle to serve; empty serves the API only")
	mock := flag.Bool("mock", false, "serve fixed sample data instead of polling real clusters")
	allowedHosts := flag.String("allowed-hosts", defaultAllowedHosts(), "comma-separated Host header values to accept, on top of the loopback names and the host of -addr")
	flag.Parse()

	if err := run(*addr, *configPath, *webDir, *allowedHosts, *mock); err != nil {
		log.Fatalf("moxyd: %v", err)
	}
}

func run(addr, configPath, webDir, allowedHosts string, mock bool) error {
	// First line of the log, before any validation: a start that fails on a bad
	// -web or a missing configuration must still say which binary failed.
	log.Print(startupBanner())

	// SIGINT and SIGTERM cancel this context, which stops the pollers and then
	// drains the HTTP server.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The bundle is checked before any cluster is contacted: a typo in -web must
	// fail in milliseconds, not after the first blocking poll round.
	web, err := newWeb(webDir)
	if err != nil {
		return err
	}

	overview, details, err := newSources(ctx, configPath, mock)
	if err != nil {
		return err
	}

	hosts := splitAllowedHosts(allowedHosts)
	// A generic listen address with no declared name leaves moxy unable to
	// tell its own name from anyone else's, so the check cannot run. That is
	// the container deployment, and refusing every request there would be
	// worse than the exposure — but an operator must not believe in a
	// protection that is switched off.
	if server.HostCheckDisabled(addr, hosts) {
		log.Printf("warning: listening on %s with no -allowed-hosts, so the Host header is not checked; "+
			"set -allowed-hosts (or MOXY_ALLOWED_HOSTS) to the name moxy is reached by, or listen on a fixed address", addr)
	}

	srv := server.New(server.Options{
		Addr:         addr,
		Overview:     overview,
		Detail:       details,
		Web:          web,
		AllowedHosts: hosts,
	})

	errc := make(chan error, 1)
	go func() {
		log.Printf("moxyd listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Print("moxyd shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// startupBanner identifies the running binary: the moxy build, the Go toolchain
// that linked it, and the target platform.
//
// The toolchain is worth a line of its own because go.mod pins the language
// level, not the standard library the binary carries: the image builds with the
// supported Go series of the moment (see CLAUDE.md), so a running container has
// nothing else that says which stdlib it terminates TLS with. When an advisory
// lands on crypto/tls, crypto/x509 or net/http, this line answers "is this
// instance affected?" from the log that was already collected.
func startupBanner() string {
	return banner(server.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// banner formats the startup line. goVersion is logged verbatim: a toolchain
// built from source reports "devel +hash" rather than a tidy goX.Y.Z, and that
// is precisely the identity worth keeping.
func banner(version, goVersion, goos, goarch string) string {
	return fmt.Sprintf("moxyd %s starting (%s, %s/%s)", version, goVersion, goos, goarch)
}

// newSources builds what serves the two families of routes: the poller behind
// /api/overview, refreshed in the background, and the on-demand service behind
// the per-object views. Both read the same configuration, so it is loaded once.
func newSources(ctx context.Context, configPath string, mock bool) (server.OverviewSource, server.DetailSource, error) {
	if mock {
		// Mock mode reads no configuration and opens no connection, so the
		// frontend can be developed without a reachable cluster. The per-object
		// views are derived from the very same sample overview, so a node
		// opened from the tree carries the figures its card showed.
		log.Print("moxyd running in mock mode: serving sample data, no cluster is contacted")
		overview := aggregate.NewMock()
		return overview, detail.NewMock(overview), nil
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, errors.New("no configuration file at " + configPath +
				": copy config.example.json and adjust it, or start with -mock (see README.md)")
		}
		return nil, nil, err
	}

	// Relaxed certificate verification is a per-cluster decision, and it must be
	// visible in the log of whoever runs the daemon.
	for _, id := range cfg.InsecureClusters() {
		log.Printf("warning: cluster %q runs with TLS verification disabled", id)
	}

	// A proxy is a per-cluster decision too. The environment of the process is
	// not one, so say so rather than let an operator wonder why their intranet
	// HTTPS_PROXY has no effect.
	for _, name := range proxyEnvVars(os.LookupEnv) {
		log.Printf("warning: %s is set but ignored for PVE calls; set clusters[].proxy to use one", name)
	}

	poller, err := aggregate.NewPoller(cfg)
	if err != nil {
		return nil, nil, err
	}

	// The detail service gets clients of its own rather than sharing the
	// poller's: an on-demand fetch must never sit behind a poll round in the
	// same connection pool. A zero TTL asks for the package default.
	clients := make(map[string]*proxmox.Client, len(cfg.Clusters))
	for _, cl := range cfg.Clusters {
		client, err := proxmox.New(cl)
		if err != nil {
			return nil, nil, err
		}
		clients[cl.ID] = client
	}
	details := detail.NewService(clients, 0)

	// Block on the first round so the very first HTTP response carries real
	// data rather than an empty payload.
	poller.Start(ctx)
	return poller, details, nil
}

// proxyEnvNames are the variables net/http would have honoured, had the PVE
// transport kept http.ProxyFromEnvironment. HTTP_PROXY is not among them: node
// URLs are https only, so it would never have applied.
var proxyEnvNames = []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"}

// proxyEnvVars returns the proxy variables that are set and non-empty, in the
// order above. lookup is os.LookupEnv, taken as a parameter so that the test
// does not have to touch the environment of the process.
func proxyEnvVars(lookup func(string) (string, bool)) []string {
	var set []string
	for _, name := range proxyEnvNames {
		if value, ok := lookup(name); ok && strings.TrimSpace(value) != "" {
			set = append(set, name)
		}
	}
	return set
}

// newWeb returns nil when no directory is given: the API is then served alone
// and the frontend runs on its own development server.
func newWeb(dir string) (http.Handler, error) {
	if dir == "" {
		return nil, nil
	}
	web, err := server.NewWebHandler(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("no frontend bundle at " + dir +
				": run ./scripts/build-web.sh and point -web at apps/web/dist, or omit -web (see README.md)")
		}
		return nil, err
	}
	log.Printf("moxyd serving the frontend bundle from %s", dir)
	return web, nil
}

func defaultAddr() string {
	if addr := os.Getenv("MOXY_ADDR"); addr != "" {
		return addr
	}
	return "127.0.0.1:8080"
}

func defaultAllowedHosts() string {
	return os.Getenv("MOXY_ALLOWED_HOSTS")
}

// splitAllowedHosts turns the comma-separated flag into a list, dropping empty
// entries so that a trailing comma or an unset variable means "none" rather
// than "one nameless host".
func splitAllowedHosts(raw string) []string {
	var hosts []string
	for _, item := range strings.Split(raw, ",") {
		if h := strings.TrimSpace(item); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

func defaultConfigPath() string {
	if path := os.Getenv("MOXY_CONFIG"); path != "" {
		return path
	}
	return "config.local.json"
}

func defaultWebDir() string {
	// Empty means disabled, so the environment is the only source of a default.
	return os.Getenv("MOXY_WEB")
}
