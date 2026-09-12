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
	// Overview supplies the aggregated cluster view. When nil, /api/overview
	// answers 503 rather than panicking.
	Overview OverviewSource
}

// New builds the moxyd HTTP server with explicit timeouts: the backend talks to
// potentially slow Proxmox nodes, and a hanging connection must not tie up a
// file descriptor indefinitely.
func New(opts Options) *http.Server {
	return &http.Server{
		Addr:              opts.Addr,
		Handler:           newRouter(opts.Overview),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func newRouter(src OverviewSource) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.Handle("/api/overview", handleOverview(src))
	return mux
}

type health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, health{Status: "ok", Version: Version})
}

// errorBody is the single error shape served by the API. The message stays in
// English: translating it is the frontend's job.
type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// Once the status is written the client can no longer be told about an
	// encoding failure, so the error is deliberately dropped.
	_ = json.NewEncoder(w).Encode(payload)
}
