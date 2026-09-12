package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmajorel/moxy/apps/api/internal/detail"
)

// detailCall records one call made to the fake source, so that a test can check
// what the HTTP layer decoded out of the path and the query string.
type detailCall struct {
	method    string
	cluster   string
	node      string
	vmid      int
	timeframe string
	limit     int
}

// fakeDetail stands in for detail.Service: these tests cover the routing, the
// validation and the error mapping, and must reach no network.
type fakeDetail struct {
	calls []detailCall

	node   *detail.Node
	guest  *detail.Guest
	series *detail.Series
	tasks  *detail.Tasks
	plan   *detail.MaintenancePlan
	err    error
}

func newFakeDetail() *fakeDetail {
	return &fakeDetail{
		node:   &detail.Node{Cluster: "prod", Name: "pve-01"},
		guest:  &detail.Guest{Cluster: "prod", VMID: 101},
		series: &detail.Series{Cluster: "prod", Timeframe: "hour"},
		tasks:  &detail.Tasks{Cluster: "prod"},
		plan:   &detail.MaintenancePlan{Cluster: "prod", Node: "pve-01"},
	}
}

func (f *fakeDetail) MaintenancePlan(_ context.Context, cluster, node string) (*detail.MaintenancePlan, error) {
	f.calls = append(f.calls, detailCall{method: "MaintenancePlan", cluster: cluster, node: node})
	if f.err != nil {
		return nil, f.err
	}
	return f.plan, nil
}

func (f *fakeDetail) Node(_ context.Context, cluster, node string) (*detail.Node, error) {
	f.calls = append(f.calls, detailCall{method: "Node", cluster: cluster, node: node})
	return f.node, f.err
}

func (f *fakeDetail) Guest(_ context.Context, cluster string, vmid int) (*detail.Guest, error) {
	f.calls = append(f.calls, detailCall{method: "Guest", cluster: cluster, vmid: vmid})
	return f.guest, f.err
}

func (f *fakeDetail) NodeSeries(_ context.Context, cluster, node, timeframe string) (*detail.Series, error) {
	f.calls = append(f.calls, detailCall{method: "NodeSeries", cluster: cluster, node: node, timeframe: timeframe})
	return f.series, f.err
}

func (f *fakeDetail) GuestSeries(_ context.Context, cluster string, vmid int, timeframe string) (*detail.Series, error) {
	f.calls = append(f.calls, detailCall{method: "GuestSeries", cluster: cluster, vmid: vmid, timeframe: timeframe})
	return f.series, f.err
}

func (f *fakeDetail) Tasks(_ context.Context, cluster string, limit int) (*detail.Tasks, error) {
	f.calls = append(f.calls, detailCall{method: "Tasks", cluster: cluster, limit: limit})
	return f.tasks, f.err
}

// only returns the single call the request under test should have produced.
func (f *fakeDetail) only(t *testing.T) detailCall {
	t.Helper()
	if len(f.calls) != 1 {
		t.Fatalf("calls = %+v, want exactly one", f.calls)
	}
	return f.calls[0]
}

// serveDetail runs one request through the whole router, so that the detail
// subtree is exercised next to the routes it must not disturb.
func serveDetail(src DetailSource, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	newHandler(Options{Detail: src}).ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestDetailRoutesServeJSON(t *testing.T) {
	cases := []struct {
		name   string
		target string
		want   detailCall
	}{
		{
			name:   "node",
			target: "/api/clusters/prod/nodes/pve-01",
			want:   detailCall{method: "Node", cluster: "prod", node: "pve-01"},
		},
		{
			name:   "node rrd",
			target: "/api/clusters/prod/nodes/pve-01/rrd?timeframe=day",
			want:   detailCall{method: "NodeSeries", cluster: "prod", node: "pve-01", timeframe: "day"},
		},
		{
			name:   "guest",
			target: "/api/clusters/prod/guests/101",
			want:   detailCall{method: "Guest", cluster: "prod", vmid: 101},
		},
		{
			name:   "guest rrd",
			target: "/api/clusters/prod/guests/101/rrd?timeframe=week",
			want:   detailCall{method: "GuestSeries", cluster: "prod", vmid: 101, timeframe: "week"},
		},
		{
			name:   "tasks",
			target: "/api/clusters/prod/tasks?limit=10",
			want:   detailCall{method: "Tasks", cluster: "prod", limit: 10},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newFakeDetail()
			rec := serveDetail(src, http.MethodGet, tc.target)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			// These views describe live state; a cached copy would show a guest
			// that has since migrated.
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want \"no-store\"", got)
			}
			var body map[string]interface{}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("unreadable body: %v", err)
			}
			if body["cluster"] != "prod" {
				t.Errorf("cluster = %v, want \"prod\"", body["cluster"])
			}
			if got := src.only(t); got != tc.want {
				t.Errorf("call = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDetailRejectsOtherMethods(t *testing.T) {
	targets := []string{
		"/api/clusters/prod/nodes/pve-01",
		"/api/clusters/prod/nodes/pve-01/rrd",
		"/api/clusters/prod/guests/101",
		"/api/clusters/prod/guests/101/rrd",
		"/api/clusters/prod/tasks",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			src := newFakeDetail()
			rec := serveDetail(src, http.MethodPost, target)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
			}
			if got := rec.Header().Get("Allow"); got != http.MethodGet {
				t.Errorf("Allow = %q, want %q", got, http.MethodGet)
			}
			if got := decodeError(t, rec).Error; got != "method not allowed" {
				t.Errorf("error = %q", got)
			}
			if len(src.calls) != 0 {
				t.Errorf("calls = %+v, want none", src.calls)
			}
		})
	}
}

// A path that matches no route is a 404 whatever the method: the shape is
// decided before the method, so a POST to a misspelled path reports the
// misspelling rather than a method that would never have worked anyway.
func TestDetailUnknownShapeIsNotFoundForAnyMethod(t *testing.T) {
	src := newFakeDetail()
	rec := serveDetail(src, http.MethodPost, "/api/clusters/prod/storages/local")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := decodeError(t, rec).Error; got != "not found" {
		t.Errorf("error = %q", got)
	}
}

func TestDetailMapsNotFound(t *testing.T) {
	src := newFakeDetail()
	src.err = fmt.Errorf("cluster %q: %w", "prod", detail.ErrNotFound)
	rec := serveDetail(src, http.MethodGet, "/api/clusters/prod/nodes/pve-01")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := decodeError(t, rec).Error; got != "not found" {
		t.Errorf("error = %q", got)
	}
}

// An upstream failure may name an internal host or quote a hypervisor reply.
// The caller gets a fixed message; the detail belongs in the server log.
func TestDetailDoesNotEchoSourceError(t *testing.T) {
	const sentinel = "pve-mgmt-07.internal.example"

	targets := []string{
		"/api/clusters/prod/nodes/pve-01",
		"/api/clusters/prod/nodes/pve-01/rrd",
		"/api/clusters/prod/guests/101",
		"/api/clusters/prod/guests/101/rrd",
		"/api/clusters/prod/tasks",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			src := newFakeDetail()
			src.err = errors.New("dial tcp " + sentinel + ":8006: connection refused")
			rec := serveDetail(src, http.MethodGet, target)

			if rec.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
			}
			if strings.Contains(rec.Body.String(), sentinel) {
				t.Fatalf("response body leaked the source error: %s", rec.Body.String())
			}
			if got := decodeError(t, rec).Error; got != "upstream unavailable" {
				t.Errorf("error = %q", got)
			}
		})
	}
}

// A source that reports neither an error nor an object must not be served as a
// 200 carrying the JSON literal null.
func TestDetailNilPayloadIsNotFound(t *testing.T) {
	src := newFakeDetail()
	src.node = nil
	rec := serveDetail(src, http.MethodGet, "/api/clusters/prod/nodes/pve-01")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestDetailRejectsBadVMID(t *testing.T) {
	for _, vmid := range []string{"abc", "-1", "0", "1.5", "١٠١", "101abc", "%20101"} {
		t.Run(vmid, func(t *testing.T) {
			src := newFakeDetail()
			rec := serveDetail(src, http.MethodGet, "/api/clusters/prod/guests/"+vmid)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if got := decodeError(t, rec).Error; got != "vmid must be a positive integer" {
				t.Errorf("error = %q", got)
			}
			if len(src.calls) != 0 {
				t.Errorf("calls = %+v, want none", src.calls)
			}
		})
	}
}

func TestDetailTimeframeDefaultsToHour(t *testing.T) {
	for _, target := range []string{
		"/api/clusters/prod/nodes/pve-01/rrd",
		"/api/clusters/prod/nodes/pve-01/rrd?timeframe=",
	} {
		t.Run(target, func(t *testing.T) {
			src := newFakeDetail()
			rec := serveDetail(src, http.MethodGet, target)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got := src.only(t).timeframe; got != "hour" {
				t.Errorf("timeframe = %q, want \"hour\"", got)
			}
		})
	}
}

func TestDetailAcceptsEveryKnownTimeframe(t *testing.T) {
	for _, timeframe := range []string{"hour", "day", "week", "month", "year"} {
		t.Run(timeframe, func(t *testing.T) {
			src := newFakeDetail()
			rec := serveDetail(src, http.MethodGet, "/api/clusters/prod/guests/101/rrd?timeframe="+timeframe)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got := src.only(t).timeframe; got != timeframe {
				t.Errorf("timeframe = %q, want %q", got, timeframe)
			}
		})
	}
}

// An unknown timeframe is refused here and never forwarded: the source must not
// be called at all, so no unvalidated string can reach the hypervisor.
func TestDetailRejectsUnknownTimeframe(t *testing.T) {
	for _, timeframe := range []string{"decade", "HOUR", "hour%20", "hourly", "0"} {
		t.Run(timeframe, func(t *testing.T) {
			src := newFakeDetail()
			rec := serveDetail(src, http.MethodGet, "/api/clusters/prod/nodes/pve-01/rrd?timeframe="+timeframe)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if got := decodeError(t, rec).Error; got != "timeframe must be one of hour, day, week, month, year" {
				t.Errorf("error = %q", got)
			}
			if len(src.calls) != 0 {
				t.Errorf("calls = %+v, want none", src.calls)
			}
		})
	}
}

func TestDetailTaskLimit(t *testing.T) {
	cases := []struct {
		name   string
		target string
		status int
		want   int
	}{
		{name: "default", target: "/api/clusters/prod/tasks", status: http.StatusOK, want: defaultTaskLimit},
		{name: "empty", target: "/api/clusters/prod/tasks?limit=", status: http.StatusOK, want: defaultTaskLimit},
		{name: "explicit", target: "/api/clusters/prod/tasks?limit=7", status: http.StatusOK, want: 7},
		{name: "capped", target: "/api/clusters/prod/tasks?limit=1000000", status: http.StatusOK, want: maxTaskLimit},
		{name: "text", target: "/api/clusters/prod/tasks?limit=many", status: http.StatusBadRequest},
		{name: "zero", target: "/api/clusters/prod/tasks?limit=0", status: http.StatusBadRequest},
		{name: "negative", target: "/api/clusters/prod/tasks?limit=-5", status: http.StatusBadRequest},
		{name: "fractional", target: "/api/clusters/prod/tasks?limit=2.5", status: http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newFakeDetail()
			rec := serveDetail(src, http.MethodGet, tc.target)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if tc.status != http.StatusOK {
				if got := decodeError(t, rec).Error; got != "limit must be a positive integer" {
					t.Errorf("error = %q", got)
				}
				if len(src.calls) != 0 {
					t.Errorf("calls = %+v, want none", src.calls)
				}
				return
			}
			if got := src.only(t).limit; got != tc.want {
				t.Errorf("limit = %d, want %d", got, tc.want)
			}
		})
	}
}

// A node name is not restricted to what is safe in a URL, so the segments are
// percent-decoded before use — and decoded one by one, so an encoded separator
// cannot smuggle in an extra segment.
func TestDetailDecodesPathSegments(t *testing.T) {
	src := newFakeDetail()
	rec := serveDetail(src, http.MethodGet, "/api/clusters/pr%C3%A9prod/nodes/pve%2001")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	call := src.only(t)
	if call.cluster != "préprod" {
		t.Errorf("cluster = %q, want %q", call.cluster, "préprod")
	}
	if call.node != "pve 01" {
		t.Errorf("node = %q, want %q", call.node, "pve 01")
	}
}

// Paths that match no route. They are answered without ever indexing past the
// end of the split, and without reaching the source.
func TestDetailRejectsMalformedPaths(t *testing.T) {
	targets := []string{
		"/api/clusters/",
		"/api/clusters/prod",
		"/api/clusters/prod/nodes",
		"/api/clusters/prod/guests",
		"/api/clusters/prod/tasks/extra",
		"/api/clusters/prod/nodes/pve-01/rrd/extra",
		"/api/clusters/prod/nodes/pve-01/rrd/extra/more",
		"/api/clusters/prod/nodes/pve-01/status",
		"/api/clusters/prod/guests/101/console",
		"/api/clusters/prod/storages/local",
		"/api/clusters/prod/nodes/pve-01/",
		"/api/clusters/prod/tasks/",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			src := newFakeDetail()
			rec := serveDetail(src, http.MethodGet, target)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusNotFound, rec.Body.String())
			}
			if got := decodeError(t, rec).Error; got != "not found" {
				t.Errorf("error = %q", got)
			}
			if len(src.calls) != 0 {
				t.Errorf("calls = %+v, want none", src.calls)
			}
		})
	}
}

// Empty and dotted segments are checked against the handler directly.
//
// ServeMux rewrites "//" and ".." out of a path and answers a redirect before
// any handler runs, so these shapes cannot reach handleDetail through the
// router. The handler must not depend on that: it is mounted by name here, the
// way any future router would reach it.
func TestDetailHandlerRejectsEmptyAndDottedSegments(t *testing.T) {
	targets := []string{
		"/api/clusters//nodes/pve-01",
		"/api/clusters/prod//pve-01",
		"/api/clusters/prod/nodes/",
		"/api/clusters/prod/nodes/%2e",
		"/api/clusters/prod/nodes/%2e%2e",
		"/api/clusters/%2e%2e/nodes/pve-01",
		"/api/clusters/prod/nodes/x%2F..%2Fy",
		"/api/clusters/prod/nodes/%2e%2e%2f%2e%2e%2faccess%2fusers",
		"/api/clusters/prod/nodes/pve%00-01",
		"/api/clusters/prod/nodes/pve%0a-01",
		"/somewhere/else/entirely",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			src := newFakeDetail()
			rec := httptest.NewRecorder()
			handleDetail(src).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusNotFound, rec.Body.String())
			}
			if len(src.calls) != 0 {
				t.Errorf("calls = %+v, want none", src.calls)
			}
		})
	}
}

// Whatever ServeMux decides to do with a path it considers unclean, it must
// never end up serving one.
func TestDetailRouterNeverServesUncleanPaths(t *testing.T) {
	targets := []string{
		"/api/clusters//nodes/pve-01",
		"/api/clusters/prod//pve-01",
		"/api/clusters/prod/nodes/../../access/users",
		"/api/clusters/prod/nodes/%2e%2e",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			src := newFakeDetail()
			rec := serveDetail(src, http.MethodGet, target)

			if rec.Code == http.StatusOK {
				t.Fatalf("status = %d, want anything but 200", rec.Code)
			}
			if len(src.calls) != 0 {
				t.Errorf("calls = %+v, want none", src.calls)
			}
		})
	}
}

// Without a source — mock mode — the routes answer 501: they exist, but nothing
// behind them can be reached. A 404 would claim the object does not exist.
func TestDetailWithoutSource(t *testing.T) {
	rec := serveDetail(nil, http.MethodGet, "/api/clusters/prod/nodes/pve-01")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
	if got := decodeError(t, rec).Error; got == "" {
		t.Error("empty error message")
	}
}

// A malformed request is still reported as malformed when no source is
// configured: the validation does not depend on one.
func TestDetailWithoutSourceStillValidates(t *testing.T) {
	rec := serveDetail(nil, http.MethodGet, "/api/clusters/prod/guests/abc")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// The detail subtree must not open a hole in the guard that keeps the /api/
// namespace closed, nor shadow the routes registered beside it.
func TestDetailDoesNotDisturbTheOtherRoutes(t *testing.T) {
	handler := newHandler(Options{
		Overview: fakeSource{overview: sampleOverview()},
		Detail:   newFakeDetail(),
	})

	t.Run("unknown api path stays a json 404", func(t *testing.T) {
		for _, target := range []string{"/api/bogus", "/api/clusterz/prod", "/api/overview/extra"} {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s: status = %d, want %d", target, rec.Code, http.StatusNotFound)
			}
			if got := decodeError(t, rec).Error; got != "not found" {
				t.Errorf("%s: error = %q", target, got)
			}
		}
	})

	t.Run("overview still answers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/overview", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("healthz still answers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}
