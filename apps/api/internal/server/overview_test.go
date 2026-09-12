package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
)

// fakeSource stands in for the poller and the mock alike, so these tests cover
// the HTTP layer alone.
type fakeSource struct {
	overview *aggregate.Overview
	err      error
}

func (f fakeSource) Overview(context.Context) (*aggregate.Overview, error) {
	return f.overview, f.err
}

func sampleOverview() *aggregate.Overview {
	return &aggregate.Overview{
		GeneratedAt: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC),
		Thresholds:  aggregate.Thresholds{Memory: 0.8},
		Totals:      aggregate.Totals{Clusters: 1, Nodes: 3, NodesOnline: 3, VMs: 13, Alerts: 0},
		Clusters: []aggregate.ClusterOverview{
			{ID: "qualification", Name: "Qualification", Status: aggregate.StatusHealthy},
		},
	}
}

func TestOverviewServesSnapshot(t *testing.T) {
	rec := httptest.NewRecorder()
	src := fakeSource{overview: sampleOverview()}
	newRouter(src, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/overview", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want \"no-store\"", got)
	}

	var body aggregate.Overview
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("unreadable body: %v", err)
	}
	if len(body.Clusters) != 1 || body.Clusters[0].ID != "qualification" {
		t.Errorf("clusters = %+v", body.Clusters)
	}
	if body.Totals.VMs != 13 {
		t.Errorf("totals.vms = %d, want 13", body.Totals.VMs)
	}
}

func TestOverviewRejectsOtherMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	src := fakeSource{overview: sampleOverview()}
	newRouter(src, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/overview", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("Allow = %q, want %q", got, http.MethodGet)
	}
}

func TestOverviewWithoutSource(t *testing.T) {
	rec := httptest.NewRecorder()
	newRouter(nil, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/overview", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// A failing source must not leak the underlying error text to the caller: it may
// name hosts or carry detail that belongs in the log, not in a browser.
func TestOverviewDoesNotEchoSourceError(t *testing.T) {
	const sentinel = "0f3c1c1e-secret-uuid"

	rec := httptest.NewRecorder()
	src := fakeSource{err: errors.New("dial tcp: " + sentinel)}
	newRouter(src, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/overview", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if strings.Contains(rec.Body.String(), sentinel) {
		t.Fatalf("response body leaked the source error: %s", rec.Body.String())
	}

	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("unreadable body: %v", err)
	}
	if body.Error != "overview unavailable" {
		t.Errorf("error = %q", body.Error)
	}
}
