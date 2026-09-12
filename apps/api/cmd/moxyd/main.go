// Command moxyd is the moxy aggregating backend: it queries N Proxmox VE
// clusters and exposes a unified API to the frontend, without ever handing API
// tokens to the browser.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
	"github.com/dmajorel/moxy/apps/api/internal/config"
	"github.com/dmajorel/moxy/apps/api/internal/server"
)

const shutdownTimeout = 5 * time.Second

func main() {
	addr := flag.String("addr", defaultAddr(), "HTTP listen address")
	configPath := flag.String("config", defaultConfigPath(), "path to the cluster configuration file")
	webDir := flag.String("web", defaultWebDir(), "directory of the built frontend bundle to serve; empty serves the API only")
	mock := flag.Bool("mock", false, "serve fixed sample data instead of polling real clusters")
	flag.Parse()

	if err := run(*addr, *configPath, *webDir, *mock); err != nil {
		log.Fatalf("moxyd: %v", err)
	}
}

func run(addr, configPath, webDir string, mock bool) error {
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

	source, err := newSource(ctx, configPath, mock)
	if err != nil {
		return err
	}

	srv := server.New(server.Options{Addr: addr, Overview: source, Web: web})

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

func newSource(ctx context.Context, configPath string, mock bool) (server.OverviewSource, error) {
	if mock {
		// Mock mode reads no configuration and opens no connection, so the
		// frontend can be developed without a reachable cluster.
		log.Print("moxyd running in mock mode: serving sample data, no cluster is contacted")
		return aggregate.NewMock(), nil
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("no configuration file at " + configPath +
				": copy config.example.json and adjust it, or start with -mock (see README.md)")
		}
		return nil, err
	}

	// Relaxed certificate verification is a per-cluster decision, and it must be
	// visible in the log of whoever runs the daemon.
	for _, id := range cfg.InsecureClusters() {
		log.Printf("warning: cluster %q runs with TLS verification disabled", id)
	}

	poller, err := aggregate.NewPoller(cfg)
	if err != nil {
		return nil, err
	}

	// Block on the first round so the very first HTTP response carries real
	// data rather than an empty payload.
	poller.Start(ctx)
	return poller, nil
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
