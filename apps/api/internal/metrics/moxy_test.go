package metrics

import (
	"strings"
	"testing"
)

// TestClassifyPath: the label names a FAMILY of endpoint, never one object.
// A thousand guests must produce one label value, and a node name must never
// reach a document that leaves the process.
func TestClassifyPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/cluster/resources", PathResources},
		{"/cluster/status", PathStatus},
		{"/cluster/ha/status/manager_status", PathHA},
		{"/cluster/tasks", PathTasks},
		{"/nodes/prox-pprd-2301-cit/tasks?limit=25&source=all&vmid=102", PathTasks},
		{"/nodes/prox-pprd-2301-cit/apt/update", PathApt},
		{"/nodes/prox-pprd-2301-cit/status", PathNodeStatus},
		{"/nodes/prox-pprd-2301-cit/rrddata?cf=AVERAGE&timeframe=hour", PathRRD},
		{"/nodes/prox-pprd-2301-cit/qemu/102/status/current", PathOther},
		{"/nodes/prox-pprd-2301-cit/qemu/102/status", PathGuestStatus},
		{"/nodes/prox-pprd-2301-cit/lxc/204/status", PathGuestStatus},
		{"/nodes/prox-pprd-2301-cit/qemu/102/config", PathGuestConfig},
		{"/nodes/prox-pprd-2301-cit/qemu/102/rrddata?cf=AVERAGE&timeframe=day", PathRRD},
		{"/nodes/prox-pprd-2301-cit/qemu/102/agent/network-get-interfaces", PathAgent},
		// A route added without a case here still counts, under a label that
		// says it was not classified rather than silently disappearing.
		{"/cluster/firewall/rules", PathOther},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.path, func(t *testing.T) {
			if got := ClassifyPath(tt.path); got != tt.want {
				t.Errorf("ClassifyPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestClassifyPathCarriesNothingIdentifying is the rule this whole
// classification exists for: whatever the path names, the label is one of a
// closed set of constants. A node name or a vmid in a scraped document is a
// host name leaving the process, and an unbounded time series besides.
func TestClassifyPathCarriesNothingIdentifying(t *testing.T) {
	known := map[string]bool{
		PathResources: true, PathStatus: true, PathHA: true, PathApt: true,
		PathNodeStatus: true, PathGuestStatus: true, PathGuestConfig: true,
		PathRRD: true, PathTasks: true, PathAgent: true, PathOther: true,
	}
	paths := []string{
		"/nodes/prox-pprd-2301-cit/status",
		"/nodes/prox-pprd-2301-cit/qemu/102/config",
		"/nodes/secret-host.internal.example/rrddata?timeframe=hour",
		"/nodes/prox-pprd-2301-cit/lxc/204/agent/network-get-interfaces",
		"/cluster/anything/at/all",
	}
	for _, path := range paths {
		got := ClassifyPath(path)
		if !known[got] {
			t.Errorf("ClassifyPath(%q) = %q, which is not one of the declared kinds", path, got)
		}
		for _, leak := range []string{"prox-pprd-2301-cit", "secret-host", "102", "204"} {
			if strings.Contains(got, leak) {
				t.Errorf("ClassifyPath(%q) = %q, which carries %q", path, got, leak)
			}
		}
	}
}

// TestDeclaredSetIsExposable walks the metric set of the daemon: every family
// declared in moxy.go must render, and render under the name and labels a
// dashboard will be written against.
func TestDeclaredSetIsExposable(t *testing.T) {
	PVERequests.Inc("metrics-selftest", PathResources, OutcomeOK)
	PVERequestSeconds.Observe(0.2, "metrics-selftest", PathResources)
	PollLastSuccess.Set(1, "metrics-selftest")
	ClusterStatus.Set(1, "metrics-selftest", "healthy")
	DetailCacheEvents.Inc("view", EventHit)
	BuildInfo.Set(1, "v0.0.0-selftest")

	got := Default.Text()
	for _, want := range []string{
		`moxy_pve_requests_total{cluster="metrics-selftest",path_kind="resources",outcome="ok"}`,
		`moxy_pve_request_seconds_bucket{cluster="metrics-selftest",path_kind="resources",le="0.25"}`,
		`moxy_pve_request_seconds_count{cluster="metrics-selftest",path_kind="resources"}`,
		`moxy_poll_last_success_timestamp_seconds{cluster="metrics-selftest"}`,
		`moxy_cluster_status{cluster="metrics-selftest",status="healthy"}`,
		`moxy_detail_cache_events_total{cache="view",event="hit"}`,
		`moxy_build_info{version="v0.0.0-selftest"}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("exposition is missing %q", want)
		}
	}
}
