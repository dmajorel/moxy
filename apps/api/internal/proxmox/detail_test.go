package proxmox

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

// countingServer answers every request with an empty data envelope and counts
// what it received. The tests that assert a call is rejected BEFORE the
// network use it: an argument PVE would reject anyway must still not cost a
// request, and the counter is the only way to prove it.
func countingServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// recordingServer answers with body and records the escaped path and the query
// of the last request.
func recordingServer(t *testing.T, body string) (*httptest.Server, *string, *url.Values) {
	t.Helper()
	var (
		path  string
		query url.Values
	)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		query = r.URL.Query()
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &path, &query
}

func TestClientDecodesNodeStatus(t *testing.T) {
	srv := fixtureServer(t, map[string]string{"/nodes/prox-pprd-2301-cit/status": "node_status.json"})
	c := newTestClient(t, srv.URL)

	status, err := c.NodeStatus(context.Background(), "prox-pprd-2301-cit")
	if err != nil {
		t.Fatalf("NodeStatus: %v", err)
	}
	if status == nil {
		t.Fatal("NodeStatus returned nil without an error")
	}

	if got := status.Uptime.Int(); got != 3542400 {
		t.Errorf("uptime = %d seconds, want 3542400", got)
	}
	// A fraction, not a percentage.
	if got := status.CPU.Float(); got != 0.42 {
		t.Errorf("cpu = %v, want 0.42 (a fraction)", got)
	}
	if got := status.CPUInfo.CPUs.Int(); got != 32 {
		t.Errorf("cpuinfo.cpus = %d, want 32", got)
	}
	if got := status.CPUInfo.MHz.Float(); got != 2900 {
		t.Errorf(`cpuinfo.mhz = %v, want 2900 (serialised as "2900.000")`, got)
	}
	// Bytes, never converted by this package.
	if got := status.Memory.Total.Int(); got != 103079215104 {
		t.Errorf("memory.total = %d bytes, want 103079215104", got)
	}
	if got := status.Memory.Used.Int(); got != 76000000000 {
		t.Errorf("memory.used = %d bytes, want 76000000000", got)
	}
	if got := status.Swap.Total.Int(); got != 8589934592 {
		t.Errorf(`swap.total = %d bytes, want 8589934592 (serialised as a string)`, got)
	}
	if got := status.RootFS.Used.Int(); got != 12884901888 {
		t.Errorf("rootfs.used = %d bytes, want 12884901888", got)
	}

	// The trap of this endpoint: loadavg is an array of THREE STRINGS.
	if got := status.LoadAvg.One(); got != 0.53 {
		t.Errorf(`loadavg[0] = %v, want 0.53 (serialised as "0.53")`, got)
	}
	if got := status.LoadAvg.Five(); got != 0.61 {
		t.Errorf("loadavg[1] = %v, want 0.61", got)
	}
	if got := status.LoadAvg.Fifteen(); got != 0.58 {
		t.Errorf("loadavg[2] = %v, want 0.58", got)
	}

	if status.PVEVersion != "pve-manager/9.2.9/ec4c0cbd8a1d5b3a" {
		t.Errorf("pveversion = %q", status.PVEVersion)
	}
	if status.KVersion == "" {
		t.Error("kversion should not be empty")
	}
}

func TestNodeStatusRequiresANodeName(t *testing.T) {
	srv, hits := countingServer(t)
	c := newTestClient(t, srv.URL)

	if _, err := c.NodeStatus(context.Background(), ""); err == nil {
		t.Error("an empty node name should be rejected")
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Errorf("hits = %d, want 0", got)
	}
}

func TestClientDecodesGuestStatusQemu(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"/nodes/prox-pprd-2301-cit/qemu/102/status/current": "guest_status_qemu.json",
	})
	c := newTestClient(t, srv.URL)

	guest, err := c.GuestStatus(context.Background(), "prox-pprd-2301-cit", ResourceTypeQemu, 102)
	if err != nil {
		t.Fatalf("GuestStatus: %v", err)
	}
	if guest == nil {
		t.Fatal("GuestStatus returned nil without an error")
	}

	if guest.Status != StatusRunning {
		t.Errorf("status = %q, want %q", guest.Status, StatusRunning)
	}
	if got := guest.VMID.Int(); got != 102 {
		t.Errorf("vmid = %d, want 102", got)
	}
	if got := guest.CPUs.Int(); got != 8 {
		t.Errorf(`cpus = %d, want 8 (serialised as "8")`, got)
	}
	if got := guest.CPU.Float(); got != 0.17 {
		t.Errorf("cpu = %v, want 0.17 (a fraction)", got)
	}
	if got := guest.MaxMem.Int(); got != 34359738368 {
		t.Errorf(`maxmem = %d bytes, want 34359738368 (serialised as a string)`, got)
	}
	if got := guest.Balloon.Int(); got != 34359738368 {
		t.Errorf("balloon = %d bytes, want 34359738368", got)
	}

	// The trap of this endpoint: maxdisk is the SIZE of the disk, and disk
	// is 0 because PVE does not know what the guest consumes inside it.
	if got := guest.MaxDisk.Int(); got != 214748364800 {
		t.Errorf("maxdisk = %d bytes, want 214748364800", got)
	}
	if got := guest.Disk.Int(); got != 0 {
		t.Errorf("disk = %d, want 0: a qemu guest does not report its usage", got)
	}

	if !guest.HA.Managed.Bool() {
		t.Error("ha.managed should be true (serialised as 1)")
	}
	if guest.Template.Bool() {
		t.Error("the guest is not a template")
	}
	if got := guest.TagList(); len(got) != 2 || got[0] != "pprd" || got[1] != "db" {
		t.Errorf("tags = %v, want [pprd db]", got)
	}
	if got := guest.NetOut.Int(); got != 9876543210 {
		t.Errorf(`netout = %d, want 9876543210 (serialised as a string)`, got)
	}
	if guest.QMPStatus != "running" {
		t.Errorf("qmpstatus = %q, want %q", guest.QMPStatus, "running")
	}
	// A pointer, so that an absent field stays distinct from an explicit 0.
	if guest.Agent == nil {
		t.Error("agent is nil, want the value the fixture carries")
	} else if !guest.Agent.Bool() {
		t.Error(`agent should be true (serialised as "1")`)
	}
	if !guest.AgentConfigured() {
		t.Error("AgentConfigured should be true when the agent is declared")
	}
}

func TestClientDecodesGuestStatusLXC(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"/nodes/prox-pprd-2302-cit/lxc/204/status/current": "guest_status_lxc.json",
	})
	c := newTestClient(t, srv.URL)

	guest, err := c.GuestStatus(context.Background(), "prox-pprd-2302-cit", ResourceTypeLXC, 204)
	if err != nil {
		t.Fatalf("GuestStatus: %v", err)
	}
	if guest == nil {
		t.Fatal("GuestStatus returned nil without an error")
	}

	if got := guest.VMID.Int(); got != 204 {
		t.Errorf(`vmid = %d, want 204 (serialised as "204")`, got)
	}
	if got := guest.Uptime.Int(); got != 345600 {
		t.Errorf("uptime = %d, want 345600", got)
	}
	if got := guest.CPU.Float(); got != 0.021 {
		t.Errorf("cpu = %v, want 0.021", got)
	}
	// Unlike a VM, a container does report what it uses.
	if got := guest.Disk.Int(); got != 5368709120 {
		t.Errorf("disk = %d bytes, want 5368709120", got)
	}
	if got := guest.MaxDisk.Int(); got != 34359738368 {
		t.Errorf("maxdisk = %d bytes, want 34359738368", got)
	}
	if guest.HA.Managed.Bool() {
		t.Error(`ha.managed should be false (serialised as "0")`)
	}
	if guest.QMPStatus != "" {
		t.Errorf("qmpstatus = %q, want empty on lxc", guest.QMPStatus)
	}
}

// TestGuestCallsRejectUnknownKind: an unknown kind is a caller mistake, and it
// must cost nothing — no socket, no failed authentication on the cluster.
func TestGuestCallsRejectUnknownKind(t *testing.T) {
	srv, hits := countingServer(t)
	c := newTestClient(t, srv.URL)
	ctx := context.Background()

	for _, kind := range []string{"", "node", "storage", "QEMU", "container"} {
		if _, err := c.GuestStatus(ctx, "prox-pprd-2301-cit", kind, 102); err == nil {
			t.Errorf("GuestStatus with kind %q should be rejected", kind)
		}
		if _, err := c.GuestRRD(ctx, "prox-pprd-2301-cit", kind, 102, TimeframeHour); err == nil {
			t.Errorf("GuestRRD with kind %q should be rejected", kind)
		}
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Errorf("hits = %d, want 0: an unknown kind must not reach the network", got)
	}

	// A vmid is a positive integer; zero is what an unparsed one looks like.
	if _, err := c.GuestStatus(ctx, "prox-pprd-2301-cit", ResourceTypeQemu, 0); err == nil {
		t.Error("vmid 0 should be rejected")
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Errorf("hits = %d, want 0: an invalid vmid must not reach the network", got)
	}
}

func TestClientDecodesNodeRRD(t *testing.T) {
	srv := fixtureServer(t, map[string]string{"/nodes/prox-pprd-2301-cit/rrddata": "node_rrd.json"})
	c := newTestClient(t, srv.URL)

	points, err := c.NodeRRD(context.Background(), "prox-pprd-2301-cit", TimeframeHour)
	if err != nil {
		t.Fatalf("NodeRRD: %v", err)
	}
	if len(points) != 4 {
		t.Fatalf("len(points) = %d, want 4", len(points))
	}

	if points[0].Time != 1757664000 {
		t.Errorf("points[0].Time = %d, want 1757664000", points[0].Time)
	}
	// A sample serialised with string values decodes like any other.
	if points[2].Time != 1757664120 {
		t.Errorf(`points[2].Time = %d, want 1757664120 (serialised as a string)`, points[2].Time)
	}
	if points[2].CPU == nil || *points[2].CPU != 0.44 {
		t.Errorf(`points[2].CPU = %v, want 0.44 (serialised as "0.44")`, points[2].CPU)
	}
	if points[2].MemUsed == nil || *points[2].MemUsed != 76000000000 {
		t.Errorf("points[2].MemUsed = %v, want 76000000000 bytes", points[2].MemUsed)
	}

	if points[1].CPU == nil || *points[1].CPU != 0.41 {
		t.Errorf("points[1].CPU = %v, want 0.41", points[1].CPU)
	}
	if points[1].LoadAvg == nil || *points[1].LoadAvg != 0.52 {
		t.Errorf("points[1].LoadAvg = %v, want 0.52", points[1].LoadAvg)
	}
	if points[1].NetIn == nil || *points[1].NetIn != 10485760 {
		t.Errorf("points[1].NetIn = %v, want 10485760", points[1].NetIn)
	}
	// A node series never carries the guest columns.
	if points[1].Disk != nil {
		t.Errorf("points[1].Disk = %v, want nil on a node series", points[1].Disk)
	}
}

// TestRRDHoleIsNilNotZero is the point of the pointers in RRDPoint. The first
// sample of the fixture has no "cpu" key at all, which is RRD saying "nothing
// is known about this step" — not "the cpu was idle".
func TestRRDHoleIsNilNotZero(t *testing.T) {
	srv := fixtureServer(t, map[string]string{"/nodes/prox-pprd-2301-cit/rrddata": "node_rrd.json"})
	c := newTestClient(t, srv.URL)

	points, err := c.NodeRRD(context.Background(), "prox-pprd-2301-cit", TimeframeDay)
	if err != nil {
		t.Fatalf("NodeRRD: %v", err)
	}

	hole := points[0]
	if hole.Time != 1757664000 {
		t.Fatalf("points[0].Time = %d, want the sample with the hole", hole.Time)
	}
	if hole.CPU != nil {
		t.Errorf("points[0].CPU = %v, want nil: a missing column is a hole, not a zero", *hole.CPU)
	}
	if hole.IOWait != nil {
		t.Errorf("points[0].IOWait = %v, want nil", *hole.IOWait)
	}
	if hole.LoadAvg != nil {
		t.Errorf("points[0].LoadAvg = %v, want nil", *hole.LoadAvg)
	}
	if hole.MemUsed != nil {
		t.Errorf("points[0].MemUsed = %v, want nil", *hole.MemUsed)
	}
	if hole.NetIn != nil {
		t.Errorf("points[0].NetIn = %v, want nil", *hole.NetIn)
	}
	// The columns that ARE there must still decode, holes and values coexist
	// in the same sample.
	if hole.MaxCPU == nil || *hole.MaxCPU != 32 {
		t.Errorf("points[0].MaxCPU = %v, want 32", hole.MaxCPU)
	}
	if hole.MemTotal == nil || *hole.MemTotal != 103079215104 {
		t.Errorf("points[0].MemTotal = %v, want 103079215104", hole.MemTotal)
	}

	// And the converse: a sample whose cpu really is 0 must NOT read as a
	// hole, or an idle node would look like a gap in the graph.
	idle := points[3]
	if idle.CPU == nil {
		t.Fatal("points[3].CPU = nil, want a real zero: 0 is a measurement")
	}
	if *idle.CPU != 0 {
		t.Errorf("points[3].CPU = %v, want 0", *idle.CPU)
	}
	if idle.NetIn == nil || *idle.NetIn != 0 {
		t.Errorf("points[3].NetIn = %v, want a real 0", idle.NetIn)
	}
}

// TestRRDRejectsUnknownTimeframe: the timeframe comes from an HTTP parameter
// two layers up. A free-form value must be rejected here, not forwarded to
// PVE, and the counter proves nothing left.
func TestRRDRejectsUnknownTimeframe(t *testing.T) {
	srv, hits := countingServer(t)
	c := newTestClient(t, srv.URL)
	ctx := context.Background()

	for _, tf := range []string{"", "decade", "HOUR", "hour ", "day; drop", "*"} {
		if _, err := c.NodeRRD(ctx, "prox-pprd-2301-cit", tf); err == nil {
			t.Errorf("NodeRRD with timeframe %q should be rejected", tf)
		}
		if _, err := c.GuestRRD(ctx, "prox-pprd-2301-cit", ResourceTypeQemu, 102, tf); err == nil {
			t.Errorf("GuestRRD with timeframe %q should be rejected", tf)
		}
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Errorf("hits = %d, want 0: an invalid timeframe must not reach the network", got)
	}

	for _, tf := range []string{TimeframeHour, TimeframeDay, TimeframeWeek, TimeframeMonth, TimeframeYear} {
		if !ValidTimeframe(tf) {
			t.Errorf("ValidTimeframe(%q) = false, want true", tf)
		}
	}
}

// TestRRDSendsAverageConsolidation: the timeframe and cf parameters go through
// url.Values, and AVERAGE is the only consolidation whose points compare
// across timeframes.
func TestRRDSendsAverageConsolidation(t *testing.T) {
	srv, path, query := recordingServer(t, `{"data":[]}`)
	c := newTestClient(t, srv.URL)

	if _, err := c.GuestRRD(context.Background(), "prox-pprd-2301-cit", ResourceTypeLXC, 204, TimeframeWeek); err != nil {
		t.Fatalf("GuestRRD: %v", err)
	}
	if want := apiPrefix + "/nodes/prox-pprd-2301-cit/lxc/204/rrddata"; *path != want {
		t.Errorf("path = %q, want %q", *path, want)
	}
	if got := query.Get("timeframe"); got != TimeframeWeek {
		t.Errorf("timeframe = %q, want %q", got, TimeframeWeek)
	}
	if got := query.Get("cf"); got != "AVERAGE" {
		t.Errorf("cf = %q, want %q", got, "AVERAGE")
	}
}

func TestClientDecodesClusterTasks(t *testing.T) {
	srv := fixtureServer(t, map[string]string{"/cluster/tasks": "cluster_tasks.json"})
	c := newTestClient(t, srv.URL)

	tasks, err := c.ClusterTasks(context.Background())
	if err != nil {
		t.Fatalf("ClusterTasks: %v", err)
	}
	if len(tasks) != 5 {
		t.Fatalf("len(tasks) = %d, want 5", len(tasks))
	}

	// The trap of this endpoint: a RUNNING task has no endtime key at all,
	// and a FlexInt would have turned that into the epoch.
	running := tasks[0]
	if running.EndTime != nil {
		t.Errorf("a running task should have no end time, got %d", running.EndTime.Int())
	}
	if !running.Running() {
		t.Error("tasks[0] should be running")
	}
	if running.Succeeded() || running.Failed() {
		t.Error("a running task has neither succeeded nor failed")
	}
	if running.Status != "" {
		t.Errorf("a running task has no status, got %q", running.Status)
	}
	if running.Type != "vzdump" || running.ID != "102" || running.User != "root@pam" {
		t.Errorf("tasks[0] = %q %q %q", running.Type, running.ID, running.User)
	}
	if got := running.StartTime.Int(); got != 1757671200 {
		t.Errorf("tasks[0].StartTime = %d, want 1757671200", got)
	}

	done := tasks[1]
	if done.Running() {
		t.Error("tasks[1] should be finished")
	}
	if done.EndTime == nil || done.EndTime.Int() != 1757671005 {
		t.Errorf("tasks[1].EndTime = %v, want 1757671005", done.EndTime)
	}
	if got := done.StartTime.Int(); got != 1757670912 {
		t.Errorf(`tasks[1].StartTime = %d, want 1757670912 (serialised as a string)`, got)
	}
	if !done.Succeeded() || done.Failed() {
		t.Errorf("tasks[1] status %q should be a success", done.Status)
	}

	// A failed task carries its error message in "status", where OK would be.
	failed := tasks[2]
	if failed.Running() || failed.Succeeded() {
		t.Error("tasks[2] should be a finished failure")
	}
	if !failed.Failed() {
		t.Errorf("tasks[2] status %q should be a failure", failed.Status)
	}
	if failed.Status != "start failed: QEMU exited with code 1" {
		t.Errorf("tasks[2].Status = %q", failed.Status)
	}
	if failed.EndTime == nil || failed.EndTime.Int() != 1757670551 {
		t.Errorf(`tasks[2].EndTime = %v, want 1757670551 (serialised as a string)`, failed.EndTime)
	}

	// The third outcome, and the one that was missing from every fixture in
	// the repository: a job that RAN and warned. PVE writes the count into
	// the status string, and "anything but OK is a failure" made a nightly
	// backup that warned about one guest read as a backup that did not happen.
	warned := tasks[3]
	if warned.Running() || warned.Succeeded() {
		t.Error("tasks[3] should be a finished task that warned")
	}
	if !warned.Warned() {
		t.Errorf("tasks[3] status %q should be recognised as warnings", warned.Status)
	}
	if warned.Failed() {
		t.Error("a job that warned was classified as a failure")
	}
	if n, ok := warned.TaskWarnings(); !ok || n != 2 {
		t.Errorf("tasks[3] warnings = %d (readable: %v), want 2", n, ok)
	}
}

// TestTaskOutcomesAreExclusive: the three verdicts of a finished task are
// mutually exclusive, and a running task has none of them. Nothing else in
// the package may ever answer true twice.
func TestTaskOutcomesAreExclusive(t *testing.T) {
	tests := []struct {
		name                              string
		task                              Task
		running, succeeded, warned, faild bool
	}{
		{name: "running", task: Task{}, running: true},
		{name: "ok", task: Task{EndTime: taskEnd(1), Status: TaskStatusOK}, succeeded: true},
		{name: "warnings", task: Task{EndTime: taskEnd(1), Status: "WARNINGS: 3"}, warned: true},
		{name: "one warning", task: Task{EndTime: taskEnd(1), Status: "WARNINGS: 1"}, warned: true},
		{name: "failure", task: Task{EndTime: taskEnd(1), Status: "storage is not online"}, faild: true},
		// An empty status on a finished task is a verdict nobody stated: a
		// failure of unknown cause, never a silent success.
		{name: "no status at all", task: Task{EndTime: taskEnd(1)}, faild: true},
		// "WARNINGS" without the colon is not the prefix PVE writes, and
		// guessing at it would turn an error message into a clean bill.
		{name: "the word alone is not the prefix", task: Task{EndTime: taskEnd(1), Status: "WARNINGS emitted"}, faild: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.task.Running(); got != tt.running {
				t.Errorf("Running() = %v, want %v", got, tt.running)
			}
			if got := tt.task.Succeeded(); got != tt.succeeded {
				t.Errorf("Succeeded() = %v, want %v", got, tt.succeeded)
			}
			if got := tt.task.Warned(); got != tt.warned {
				t.Errorf("Warned() = %v, want %v", got, tt.warned)
			}
			if got := tt.task.Failed(); got != tt.faild {
				t.Errorf("Failed() = %v, want %v", got, tt.faild)
			}
			n := 0
			for _, set := range []bool{tt.task.Succeeded(), tt.task.Warned(), tt.task.Failed()} {
				if set {
					n++
				}
			}
			if want := 0; tt.running && n != want {
				t.Errorf("a running task answered %d verdicts, want none", n)
			}
			if want := 1; !tt.running && n != want {
				t.Errorf("a finished task answered %d verdicts, want exactly one", n)
			}
		})
	}
}

// /cluster/tasks accepts no parameter, and rejects the ones it does not know
// instead of ignoring them: a "limit" costs a 400 on a real cluster. The
// request must therefore carry a bare path.
func TestClusterTasksSendsNoParameter(t *testing.T) {
	srv, path, query := recordingServer(t, `{"data":[]}`)
	c := newTestClient(t, srv.URL)

	if _, err := c.ClusterTasks(context.Background()); err != nil {
		t.Fatalf("ClusterTasks: %v", err)
	}
	if want := apiPrefix + "/cluster/tasks"; *path != want {
		t.Errorf("path = %q, want %q", *path, want)
	}
	if len(*query) != 0 {
		t.Errorf("query = %v, want it empty", query.Encode())
	}
}

// The per-node route is the one that takes a filter, and the three parameters
// it is given all matter: vmid is what narrows the log to one guest, limit is
// what keeps a browser tab from asking a node for its whole history, and
// source is the trap — its default holds finished tasks ONLY, so without it a
// running backup would be missing from the very list meant to show it.
func TestNodeTasksFiltersOnVmidLimitAndSource(t *testing.T) {
	srv, path, query := recordingServer(t, `{"data":[]}`)
	c := newTestClient(t, srv.URL)

	if _, err := c.NodeTasks(context.Background(), "prox-pprd-2301-cit", 102, 25); err != nil {
		t.Fatalf("NodeTasks: %v", err)
	}
	if want := apiPrefix + "/nodes/prox-pprd-2301-cit/tasks"; *path != want {
		t.Errorf("path = %q, want %q", *path, want)
	}
	if got := query.Get("vmid"); got != "102" {
		t.Errorf("vmid = %q, want %q", got, "102")
	}
	if got := query.Get("limit"); got != "25" {
		t.Errorf("limit = %q, want %q", got, "25")
	}
	if got := query.Get("source"); got != TaskSourceAll {
		t.Errorf("source = %q, want %q — the default would hide a running task", got, TaskSourceAll)
	}
}

// The same decoding as the cluster log, since it is the same type: a running
// task has no end time, and must not be read as one that ended in 1970.
func TestNodeTasksDecodesARunningTask(t *testing.T) {
	const body = `{"data":[
		{"upid":"UPID:pve-1:0000A1:00B2:68C0:vzdump:102:root@pam:","node":"pve-1","type":"vzdump","id":"102","user":"root@pam","starttime":1757671200},
		{"upid":"UPID:pve-1:0000A0:00B1:68BF:qmstart:102:root@pam:","node":"pve-1","type":"qmstart","id":"102","user":"root@pam","starttime":"1757670912","endtime":1757671005,"status":"OK"}]}`
	srv, _, _ := recordingServer(t, body)
	c := newTestClient(t, srv.URL)

	tasks, err := c.NodeTasks(context.Background(), "pve-1", 102, 25)
	if err != nil {
		t.Fatalf("NodeTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(tasks))
	}
	if !tasks[0].Running() || tasks[0].EndTime != nil {
		t.Errorf("tasks[0] = %+v, want it still running", tasks[0])
	}
	if !tasks[1].Succeeded() {
		t.Errorf("tasks[1] status %q should be a success", tasks[1].Status)
	}
}

// An argument PVE would refuse anyway must not cost a request: the 400 it
// answers carries a body this package drops, leaving an operator with an
// unexplained protocol error instead of a programming mistake.
func TestNodeTasksRejectsBadArgumentsWithoutARequest(t *testing.T) {
	cases := []struct {
		name  string
		node  string
		vmid  int
		limit int
	}{
		{name: "no node", node: "", vmid: 102, limit: 25},
		{name: "zero vmid", node: "pve-1", vmid: 0, limit: 25},
		{name: "negative vmid", node: "pve-1", vmid: -1, limit: 25},
		{name: "zero limit", node: "pve-1", vmid: 102, limit: 0},
		{name: "negative limit", node: "pve-1", vmid: 102, limit: -5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, hits := countingServer(t)
			c := newTestClient(t, srv.URL)

			if _, err := c.NodeTasks(context.Background(), tc.node, tc.vmid, tc.limit); err == nil {
				t.Fatal("NodeTasks accepted an argument it should refuse")
			}
			if got := atomic.LoadInt32(hits); got != 0 {
				t.Errorf("requests = %d, want none", got)
			}
		})
	}
}

// agentInterfaces is what the guest agent answers through PVE: its own payload
// under "result", inside the usual data envelope. Loopback first, then IPv6,
// then a link-local address a failed DHCP left behind — none of which is the
// answer — and finally the real one.
const agentInterfaces = `{"data":{"result":[
	{"name":"lo","hardware-address":"00:00:00:00:00:00","ip-addresses":[
		{"ip-address-type":"ipv4","ip-address":"127.0.0.1","prefix":8},
		{"ip-address-type":"ipv6","ip-address":"::1","prefix":128}]},
	{"name":"eth0","hardware-address":"aa:bb:cc:dd:ee:01","ip-addresses":[
		{"ip-address-type":"ipv6","ip-address":"fe80::a8bb:ccff:fedd:ee01","prefix":64},
		{"ip-address-type":"ipv4","ip-address":"169.254.12.34","prefix":16},
		{"ip-address-type":"ipv4","ip-address":"10.20.31.42","prefix":"24"}]}]}}`

func TestGuestIPv4(t *testing.T) {
	srv, path, _ := recordingServer(t, agentInterfaces)
	c := newTestClient(t, srv.URL)

	ip, err := c.GuestIPv4(context.Background(), "prox-pprd-2301-cit", 102)
	if err != nil {
		t.Fatalf("GuestIPv4: %v", err)
	}
	if ip != "10.20.31.42" {
		t.Errorf("ip = %q, want %q: loopback, ipv6 and link-local are not answers", ip, "10.20.31.42")
	}
	want := apiPrefix + "/nodes/prox-pprd-2301-cit/qemu/102/agent/network-get-interfaces"
	if *path != want {
		t.Errorf("path = %q, want %q", *path, want)
	}
}

// TestGuestIPv4WithoutAUsableAddress: the agent answered, it just has nothing
// reachable to report. That is "unknown", a sentinel, not a cluster failure.
func TestGuestIPv4WithoutAUsableAddress(t *testing.T) {
	const loopbackOnly = `{"data":{"result":[{"name":"lo","ip-addresses":[
		{"ip-address-type":"ipv4","ip-address":"127.0.0.1","prefix":8}]}]}}`
	srv, _, _ := recordingServer(t, loopbackOnly)
	c := newTestClient(t, srv.URL)

	ip, err := c.GuestIPv4(context.Background(), "prox-pprd-2301-cit", 102)
	if !errors.Is(err, ErrNoGuestIPv4) {
		t.Fatalf("err = %v, want ErrNoGuestIPv4", err)
	}
	if ip != "" {
		t.Errorf("ip = %q, want empty", ip)
	}

	// An agent tree that answers with an empty payload, same reading.
	empty, _, _ := recordingServer(t, `{"data":null}`)
	c2 := newTestClient(t, empty.URL)
	if _, err := c2.GuestIPv4(context.Background(), "prox-pprd-2301-cit", 102); !errors.Is(err, ErrNoGuestIPv4) {
		t.Errorf("err = %v, want ErrNoGuestIPv4", err)
	}
}

// TestGuestIPv4WithoutAnAgent: a VM with no guest agent makes PVE answer 500
// or 501. It is the ordinary case, so it must come back as a classified error
// the caller can read — not a panic, and not an empty success.
func TestGuestIPv4WithoutAnAgent(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusNotImplemented} {
		srv, hits := statusServer(t, status)
		c := newTestClient(t, srv.URL)

		ip, err := c.GuestIPv4(context.Background(), "prox-pprd-2301-cit", 102)
		if got := kindOf(t, err); got != KindProtocol {
			t.Errorf("status %d: kind = %q, want %q", status, got, KindProtocol)
		}
		if ip != "" {
			t.Errorf("status %d: ip = %q, want empty", status, ip)
		}
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("status %d: want a *proxmox.Error", status)
		}
		if e.Path != "/nodes/prox-pprd-2301-cit/qemu/102/agent/network-get-interfaces" {
			t.Errorf("status %d: path = %q", status, e.Path)
		}
		if atomic.LoadInt32(hits) == 0 {
			t.Errorf("status %d: the server received nothing", status)
		}
	}
}

// TestGuestIPv4RejectsLXC: containers have no agent tree at all, so the call
// is a caller mistake and must not become a request.
func TestGuestIPv4RejectsLXC(t *testing.T) {
	srv, hits := countingServer(t)
	c := newTestClient(t, srv.URL)

	// The exported call takes no kind, since the answer only ever comes from
	// QEMU; the rule it hard-codes is exercised through its body.
	if GuestKindSupportsAgent(ResourceTypeLXC) {
		t.Error("an lxc container has no guest agent endpoint")
	}
	if !GuestKindSupportsAgent(ResourceTypeQemu) {
		t.Error("a qemu guest can have an agent")
	}
	for _, kind := range []string{ResourceTypeLXC, ResourceTypeNode, ""} {
		if _, err := c.guestIPv4(context.Background(), "prox-pprd-2301-cit", kind, 204); err == nil {
			t.Errorf("the agent endpoint should be rejected for kind %q", kind)
		}
	}
	if _, err := c.GuestIPv4(context.Background(), "prox-pprd-2301-cit", 0); err == nil {
		t.Error("an invalid vmid should be rejected")
	}
	if _, err := c.GuestIPv4(context.Background(), "", 204); err == nil {
		t.Error("an empty node name should be rejected")
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Errorf("hits = %d, want 0", got)
	}
}

// TestDetailPathsAreEscaped: a node name is operator data that lands in a URL
// path. Without escaping, a name carrying a slash would reach a different
// endpoint entirely.
func TestDetailPathsAreEscaped(t *testing.T) {
	const node = "weird/node name"
	ctx := context.Background()

	tests := []struct {
		name string
		body string
		call func(*Client) error
		want string
	}{
		{
			"node status",
			`{"data":{}}`,
			func(c *Client) error { _, err := c.NodeStatus(ctx, node); return err },
			"/nodes/weird%2Fnode%20name/status",
		},
		{
			"guest status",
			`{"data":{}}`,
			func(c *Client) error { _, err := c.GuestStatus(ctx, node, ResourceTypeQemu, 102); return err },
			"/nodes/weird%2Fnode%20name/qemu/102/status/current",
		},
		{
			"node rrd",
			`{"data":[]}`,
			func(c *Client) error { _, err := c.NodeRRD(ctx, node, TimeframeMonth); return err },
			"/nodes/weird%2Fnode%20name/rrddata",
		},
		{
			"guest rrd",
			`{"data":[]}`,
			func(c *Client) error {
				_, err := c.GuestRRD(ctx, node, ResourceTypeLXC, 204, TimeframeYear)
				return err
			},
			"/nodes/weird%2Fnode%20name/lxc/204/rrddata",
		},
		{
			"node tasks",
			`{"data":[]}`,
			func(c *Client) error { _, err := c.NodeTasks(ctx, node, 102, 25); return err },
			"/nodes/weird%2Fnode%20name/tasks",
		},
		{
			"guest agent",
			`{"data":{"result":[]}}`,
			// An agent with no interface to report answers the call
			// successfully and still has no address: that sentinel is not
			// a request failure.
			func(c *Client) error {
				_, err := c.GuestIPv4(ctx, node, 102)
				if errors.Is(err, ErrNoGuestIPv4) {
					return nil
				}
				return err
			},
			"/nodes/weird%2Fnode%20name/qemu/102/agent/network-get-interfaces",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			srv, path, _ := recordingServer(t, tt.body)
			if err := tt.call(newTestClient(t, srv.URL)); err != nil {
				t.Fatalf("call: %v", err)
			}
			if want := apiPrefix + tt.want; *path != want {
				t.Errorf("path = %q, want %q", *path, want)
			}
		})
	}
}

// TestSingleObjectGettersNeverReturnNil: a 200 whose data member is null must
// come back as a classified error, not as a nil pointer a caller would
// dereference on the strength of a nil error.
func TestSingleObjectGettersNeverReturnNil(t *testing.T) {
	srv, _, _ := recordingServer(t, `{"data":null}`)
	c := newTestClient(t, srv.URL)
	ctx := context.Background()

	status, err := c.NodeStatus(ctx, "prox-pprd-2301-cit")
	if status != nil {
		t.Errorf("NodeStatus = %v, want nil alongside an error", status)
	}
	if got := kindOf(t, err); got != KindProtocol {
		t.Errorf("NodeStatus kind = %q, want %q", got, KindProtocol)
	}

	guest, err := c.GuestStatus(ctx, "prox-pprd-2301-cit", ResourceTypeQemu, 102)
	if guest != nil {
		t.Errorf("GuestStatus = %v, want nil alongside an error", guest)
	}
	if got := kindOf(t, err); got != KindProtocol {
		t.Errorf("GuestStatus kind = %q, want %q", got, KindProtocol)
	}
}

// taskEnd is an end time, the pointer that tells a finished task from a
// running one. See the trap documented on Task.
func taskEnd(v int64) *FlexInt {
	f := FlexInt(v)
	return &f
}
