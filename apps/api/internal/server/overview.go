package server

import (
	"context"
	"log"
	"net/http"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
)

// OverviewSource supplies the aggregated view of every configured cluster.
//
// It is implemented both by the live poller and by the mock, which is what lets
// the frontend be developed without a reachable Proxmox cluster.
type OverviewSource interface {
	Overview(ctx context.Context) (*aggregate.Overview, error)
}

func handleOverview(src OverviewSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if src == nil {
			writeError(w, http.StatusServiceUnavailable, "no overview source configured")
			return
		}

		overview, err := src.Overview(r.Context())
		if err != nil {
			// The error text comes from the aggregator, which is built never to
			// carry credentials. It is still not echoed verbatim: the caller
			// gets a fixed message and the detail stays in the server log.
			log.Printf("overview unavailable: %v", err)
			writeError(w, http.StatusServiceUnavailable, "overview unavailable")
			return
		}

		// The payload describes live infrastructure; a cached copy would be
		// misleading the moment a node changes state.
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, overview)
	}
}
