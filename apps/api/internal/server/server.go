// Package server wires up the moxyd HTTP routes.
package server

import (
	"encoding/json"
	"net/http"
	"path"
	"strings"
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
	// Detail supplies the per-object views. When nil, as in mock mode, the
	// detail routes answer 501 rather than panicking: they exist, but nothing
	// behind them can be reached.
	Detail DetailSource
	// Web serves the frontend bundle for every path the API does not own. When
	// nil, moxyd is API-only and unknown paths answer 404, which is the
	// development setup where Vite serves the frontend itself.
	Web http.Handler
	// AllowedHosts are the extra names a request may be addressed to, on top
	// of the loopback names and the host of Addr. A reverse proxy that passes
	// the public Host through needs the public name here. See host.go.
	AllowedHosts []string
}

// New builds the moxyd HTTP server with explicit timeouts: the backend talks to
// potentially slow Proxmox nodes, and a hanging connection must not tie up a
// file descriptor indefinitely.
func New(opts Options) *http.Server {
	return &http.Server{
		Addr:              opts.Addr,
		Handler:           newHandler(opts),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func newHandler(opts Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	// Without this entry /healthz/ falls through to the SPA, and a probe
	// written with one slash too many is answered by index.html with a 200:
	// the daemon would look alive for as long as the bundle is readable.
	mux.HandleFunc("/healthz/", handleNotFound)
	mux.Handle("/api/overview", handleOverview(opts.Overview))
	// The per-object views are a subtree rather than a list of patterns: Go
	// 1.19 has no path parameters, so handleDetail splits the rest of the path
	// itself and answers 404 for any shape it does not recognise. Being the
	// longest matching prefix, it wins over /api/ below for its own paths and
	// leaves every other /api/ path to the guard.
	mux.Handle(detailPrefix, handleDetail(opts.Detail))
	// The API namespace is closed: an unknown /api/ path is a JSON 404, never
	// index.html, otherwise a frontend calling a misspelled endpoint would get
	// HTML with a 200 and fail to parse it far from the cause. The bare /api
	// entry keeps ServeMux from redirecting it to /api/ instead.
	mux.HandleFunc("/api/", handleNotFound)
	mux.HandleFunc("/api", handleNotFound)
	if opts.Web != nil {
		mux.Handle("/", opts.Web)
	}
	// The Host check comes FIRST, before any routing: a request that is not
	// addressed to moxy must not reach a handler at all.
	return checkHost(newHostGuard(opts.Addr, opts.AllowedHosts), rejectUncleanAPIPath(mux))
}

// rejectUncleanAPIPath answers 404 for an API path that ServeMux would rewrite,
// instead of letting it reply with a redirect.
//
// ServeMux collapses "." and ".." segments and duplicate slashes, then sends a
// 301 to the cleaned path. The outcome is harmless today, because every route
// validates its own segments and the cleaned path simply misses. It stops being
// harmless the day an authorization rule is added: the rule would be evaluated
// against the cleaned path while the caller sent another one, and the two must
// never be allowed to diverge. Refusing outright keeps that door shut, and an
// API has no business redirecting anyway.
//
// Only the API namespace is guarded. The SPA fallback below it still wants the
// usual cleaning, so that /clusters/../ resolves to a page instead of a 404.
func rejectUncleanAPIPath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; strings.HasPrefix(p, "/api/") && p != cleanedLikeServeMux(p) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cleanedLikeServeMux mirrors what ServeMux does to a path before deciding
// whether to redirect, trailing slash included.
func cleanedLikeServeMux(p string) string {
	if p == "" {
		return "/"
	}
	cleaned := path.Clean(p)
	if strings.HasSuffix(p, "/") && cleaned != "/" {
		cleaned += "/"
	}
	return cleaned
}

func handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

// allowReadMethods is the Allow header of every route moxyd serves: the API is
// read-only, and HEAD comes with GET everywhere.
const allowReadMethods = "GET, HEAD"

// isReadMethod reports whether a request may be answered by a read-only route.
//
// HEAD is accepted wherever GET is because that is what load balancers send —
// HAProxy's httpchk defaults to it — and refusing it turns a health check into
// a 405 for no gain. Nothing else has to change for it: net/http answers a HEAD
// by running the handler and dropping the body.
func isReadMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

type health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	// A liveness answer read from a cache says nothing about the daemon that
	// is running now, which is the only thing the probe is asking about.
	w.Header().Set("Cache-Control", "no-store")

	if !isReadMethod(r.Method) {
		w.Header().Set("Allow", allowReadMethods)
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
	// Every answer of this server says what it is and is taken at its word,
	// the bundle's files and the API's JSON alike. One rule for the whole
	// server is easier to hold than one with an exception in it.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	// Once the status is written the client can no longer be told about an
	// encoding failure, so the error is deliberately dropped.
	_ = json.NewEncoder(w).Encode(payload)
}
