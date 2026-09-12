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
	mock := flag.Bool("mock", false, "serve fixed sample data instead of polling real clusters")
	flag.Parse()

	if err := run(*addr, *configPath, *mock); err != nil {
		log.Fatalf("moxyd: %v", err)
	}
}

func run(addr, configPath string, mock bool) error {
	// SIGINT and SIGTERM cancel this context, which stops the pollers and then
	// drains the HTTP server.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	source, err := newSource(ctx, configPath, mock)
	if err != nil {
		return err
	}

	srv := server.New(server.Options{Addr: addr, Overview: source})

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
