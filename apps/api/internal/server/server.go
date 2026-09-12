// Package server wires up the moxyd HTTP routes.
package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// Version is the build identifier, overridden at link time via
// -ldflags "-X .../internal/server.Version=...".
var Version = "dev"

// Options configures the HTTP server.
type Options struct {
	// Addr is the listen address, in the form accepted by net.Listen.
	Addr string
}

// New builds the moxyd HTTP server with explicit timeouts: the backend talks to
// potentially slow Proxmox nodes, and a hanging connection must not tie up a
// file descriptor indefinitely.
func New(opts Options) *http.Server {
	return &http.Server{
		Addr:              opts.Addr,
		Handler:           newRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func newRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	return mux
}

type health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, health{Status: "ok", Version: Version})
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// Once the status is written the client can no longer be told about an
	// encoding failure, so the error is deliberately dropped.
	_ = json.NewEncoder(w).Encode(payload)
}
