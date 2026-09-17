package metrics

import (
	"strings"
	"testing"
	"time"
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

// TestMaintenanceSetIsExposable: the families of the maintenance route must
// render under the exact names and labels a dashboard will be written
// against, the histogram included.
func TestMaintenanceSetIsExposable(t *testing.T) {
	RecordMaintenanceCommand("metrics-selftest", ActionEnable, OutcomeOK, 3*time.Second)
	RecordMaintenanceCommand("metrics-selftest", ActionDisable, OutcomeTimeout, 21*time.Second)
	RecordKeySource(KeySourceOpenBao, OutcomeKeySourceDenied)

	got := Default.Text()
	for _, want := range []string{
		`moxy_maintenance_commands_total{cluster="metrics-selftest",action="enable",outcome="ok"} 1`,
		`moxy_maintenance_commands_total{cluster="metrics-selftest",action="disable",outcome="ssh_timeout"} 1`,
		`moxy_maintenance_command_seconds_bucket{cluster="metrics-selftest",action="enable",outcome="ok",le="5"} 1`,
		// A 21s command lands above the default run budget and below the cap.
		`moxy_maintenance_command_seconds_bucket{cluster="metrics-selftest",action="disable",outcome="ssh_timeout",le="20"} 0`,
		`moxy_maintenance_command_seconds_bucket{cluster="metrics-selftest",action="disable",outcome="ssh_timeout",le="30"} 1`,
		`moxy_maintenance_command_seconds_count{cluster="metrics-selftest",action="enable",outcome="ok"} 1`,
		`moxy_maintenance_keysource_total{mode="openbao",outcome="keysource_denied"} 1`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("exposition is missing %q", want)
		}
	}
}

// TestMaintenanceOutcomeVocabulary pins the outcome label to the frozen Kind
// vocabulary of the maintenance package. The two lists are spelled out twice
// — there and here — because that package counts through this one and cannot
// be imported back; this test is what keeps them from drifting apart.
func TestMaintenanceOutcomeVocabulary(t *testing.T) {
	want := []string{
		"ok",
		"maintenance_forbidden",
		"no_quorum",
		"no_ha_manager",
		"no_other_node",
		"already_running",
		"keysource_unavailable",
		"keysource_denied",
		"ssh_unreachable",
		"ssh_host_key_mismatch",
		"ssh_auth_failed",
		"ssh_timeout",
		"command_refused",
		"command_failed",
	}
	if len(maintenanceOutcomes) != len(want) {
		t.Errorf("the closed set holds %d outcomes, want %d", len(maintenanceOutcomes), len(want))
	}
	for _, kind := range want {
		if got := MaintenanceOutcome(kind); got != kind {
			t.Errorf("MaintenanceOutcome(%q) = %q, want it passed through", kind, got)
		}
	}
}

// TestMaintenanceLabelsAreClosedSets is the rule these labels exist under: a
// value from outside the vocabulary folds to Unclassified instead of opening
// a time series of its own. A node name, an OpenBao address or an error
// message reaching a label would be both an unbounded cardinality and a host
// name in a document that leaves the process.
func TestMaintenanceLabelsAreClosedSets(t *testing.T) {
	outside := []string{
		"",
		"prox-pprd-2301-cit",
		"https://bao.internal.example:8200",
		`dial tcp 10.0.0.7:22: i/o timeout`,
		"OK",
	}
	for _, value := range outside {
		if got := MaintenanceOutcome(value); got != Unclassified {
			t.Errorf("MaintenanceOutcome(%q) = %q, want %q", value, got, Unclassified)
		}
		if got := MaintenanceAction(value); got != Unclassified {
			t.Errorf("MaintenanceAction(%q) = %q, want %q", value, got, Unclassified)
		}
		if got := KeySourceMode(value); got != Unclassified {
			t.Errorf("KeySourceMode(%q) = %q, want %q", value, got, Unclassified)
		}
	}
	if got := MaintenanceAction(ActionEnable); got != ActionEnable {
		t.Errorf("MaintenanceAction(%q) = %q", ActionEnable, got)
	}
	if got := KeySourceMode(KeySourceSSHKey); got != KeySourceSSHKey {
		t.Errorf("KeySourceMode(%q) = %q", KeySourceSSHKey, got)
	}
}

// TestMaintenanceRecordingFoldsAtWriteTime: the fold is not advice a caller
// may skip. Whatever is handed to the recording helpers, the exposition holds
// only declared values — and nothing identifying.
func TestMaintenanceRecordingFoldsAtWriteTime(t *testing.T) {
	r := New()
	saved, savedSeconds, savedKey := MaintenanceCommands, MaintenanceCommandSeconds, MaintenanceKeySource
	defer func() {
		MaintenanceCommands, MaintenanceCommandSeconds, MaintenanceKeySource = saved, savedSeconds, savedKey
	}()
	MaintenanceCommands = r.CounterVec("moxy_maintenance_commands_total",
		"Node maintenance commands run over SSH, by action and outcome.",
		"cluster", "action", "outcome")
	MaintenanceCommandSeconds = r.HistogramVec("moxy_maintenance_command_seconds",
		"Duration of a node maintenance command, credential minting included.",
		[]float64{0.5, 1}, "cluster", "action", "outcome")
	MaintenanceKeySource = r.CounterVec("moxy_maintenance_keysource_total",
		"Attempts to obtain the credential of a maintenance session, by mode and outcome.",
		"mode", "outcome")

	RecordMaintenanceCommand("prod", "drain", "ssh: handshake failed for prox-pprd-2301-cit", time.Second)
	RecordKeySource("https://bao.internal.example:8200", "connection refused")

	got := r.Text()
	for _, want := range []string{
		`moxy_maintenance_commands_total{cluster="prod",action="other",outcome="other"} 1`,
		`moxy_maintenance_command_seconds_count{cluster="prod",action="other",outcome="other"} 1`,
		`moxy_maintenance_keysource_total{mode="other",outcome="other"} 1`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("exposition is missing %q:\n%s", want, got)
		}
	}
	for _, leak := range []string{"prox-pprd-2301-cit", "bao.internal.example", "8200", "drain"} {
		if strings.Contains(got, leak) {
			t.Errorf("exposition carries %q:\n%s", leak, got)
		}
	}
}

// A label value is escaped even though the fold already keeps the reserved
// characters out: the escaping is what makes the exposition parsable the day
// one slips past rather than silently truncated.
func TestMaintenanceLabelsAreEscaped(t *testing.T) {
	r := New()
	r.CounterVec("moxy_maintenance_keysource_total", "A counter.", "mode", "outcome").
		Inc(`ssh-key"x`, "a\\b\nc")

	if got := r.Text(); !strings.Contains(got, `moxy_maintenance_keysource_total{mode="ssh-key\"x",outcome="a\\b\nc"} 1`) {
		t.Errorf("exposition did not escape the values:\n%s", got)
	}
}
