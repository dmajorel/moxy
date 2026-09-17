package server

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
	"github.com/dmajorel/moxy/apps/api/internal/detail"
)

// updateFixtures rewrites the frontend fixtures instead of comparing against
// them: go test ./internal/server -update.
var updateFixtures = flag.Bool("update", false, "rewrite the frontend fixtures from the mock")

// fixtureDir is where the frontend keeps the payloads its integration tests
// run against. It is outside this module on purpose: these files are ONE
// artefact with two readers, and duplicating them would defeat the point.
const fixtureDir = "../../../web/src/test/fixtures"

// fixtureClock anchors the mock so that two runs produce identical bytes. The
// instant is arbitrary; only its stability matters.
var fixtureClock = time.Date(2026, time.September, 12, 12, 47, 0, 0, time.UTC)

// webFixtures maps each generated file to the route it is the answer of.
//
// One entry per route the frontend calls, plus the two node payloads its
// integration test needs: a node whose token may not ask about updates, and
// one that lists a dozen pending packages. Adding a route here is what keeps
// it from being the one nobody notices drifting.
var webFixtures = []struct {
	file string
	path string
	// body is the JSON a route that WRITES is called with, and its presence
	// is what turns the request into a POST. The eight read routes leave it
	// empty; the ninth is the only one that has a body at all.
	body string
}{
	{"overview.mock.json", "/api/overview", ""},
	{"node.mock.json", "/api/clusters/qualification/nodes/prox-qual-2201-cit", ""},
	{"node-updates.mock.json", "/api/clusters/production/nodes/prox-prod-2401-cit", ""},
	{"guest.mock.json", "/api/clusters/qualification/guests/100", ""},
	{"series.mock.json", "/api/clusters/qualification/nodes/prox-qual-2201-cit/rrd", ""},
	{"guest-series.mock.json", "/api/clusters/qualification/guests/100/rrd", ""},
	{"cluster-series.mock.json", "/api/clusters/qualification/rrd", ""},
	{"tasks.mock.json", "/api/clusters/qualification/tasks", ""},
	{"guest-tasks.mock.json", "/api/clusters/qualification/guests/100/tasks", ""},
	// Two plans, because the screen has two answers to render: a drain that
	// fits, and one that does not because a target cannot be measured.
	{"plan.mock.json", "/api/clusters/qualification/nodes/prox-qual-2201-cit/maintenance/plan", ""},
	{"plan-blocked.mock.json", "/api/clusters/lab/nodes/prox-lab-2501-cit/maintenance/plan", ""},
	// The one route that writes. It answers from the mock without opening a
	// session, and the frontend needs the shape of an accepted request as
	// much as it needs the shape of a plan.
	{"maintenance.mock.json", "/api/clusters/qualification/nodes/prox-qual-2201-cit/maintenance", `{"action":"enable"}`},
}

// mockServer is the daemon as `moxyd -mock` runs it, on a pinned clock.
func mockServer(t *testing.T) http.Handler {
	t.Helper()
	overview := aggregate.NewMockAt(fixtureClock)
	return newHandler(Options{Overview: overview, Detail: detail.NewMockAt(overview, fixtureClock)})
}

// fetchFixture asks the mock daemon for one route and returns the body,
// re-indented the way the files on disk are written.
//
// A body makes it a POST, with the JSON content type the route demands: that
// refusal is the CSRF guard, so a fixture that forgot it would be captured as
// a 415 rather than as a payload. No Origin header is set, which is what a
// caller that is not a browser looks like.
func fetchFixture(t *testing.T, handler http.Handler, path, body string) []byte {
	t.Helper()
	method, reader := http.MethodGet, io.Reader(nil)
	if body != "" {
		method, reader = http.MethodPost, strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s = %d, want 200: %s", method, path, rec.Code, rec.Body.String())
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, rec.Body.Bytes(), "", "    "); err != nil {
		t.Fatalf("%s %s returned something that is not JSON: %v", method, path, err)
	}
	// json.Indent keeps whatever trailing byte the source had; the files on
	// disk end with exactly one newline.
	return append(bytes.TrimRight(pretty.Bytes(), "\n"), '\n')
}

// TestMockMatchesWebFixtures is the contract test between apps/api and apps/web.
//
// apps/web/src/test/fixtures/*.json claim to be payloads of `moxyd -mock`, and
// the integration tests that read them claim to fail "the day model.go and
// types.ts drift apart". That was only ever true of someone remembering to
// re-capture them by hand, and they had already drifted: the overview fixture
// was three clusters old.
//
// They are now GENERATED from the mock, on a pinned clock, and this test
// regenerates and compares. Rename a field in model.go and this fails until
// the fixtures are rebuilt; rebuild them without following in types.ts and the
// frontend contract test fails in turn.
func TestMockMatchesWebFixtures(t *testing.T) {
	handler := mockServer(t)

	for _, f := range webFixtures {
		f := f
		t.Run(f.file, func(t *testing.T) {
			got := fetchFixture(t, handler, f.path, f.body)
			path := filepath.Join(fixtureDir, f.file)

			if *updateFixtures {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v (run go test ./internal/server -update)", path, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s is stale: %s\nrun: go test ./internal/server -update", f.file, firstDifference(got, want))
			}
		})
	}
}

// TestMockIsReproducible: the whole scheme rests on two runs of the pinned
// mock producing the same bytes. A stray time.Now, a map iterated without
// sorting, or a random seed would make the fixtures churn on every run and the
// clean-tree check in CI fail for no reason.
func TestMockIsReproducible(t *testing.T) {
	first, second := mockServer(t), mockServer(t)
	for _, f := range webFixtures {
		f := f
		t.Run(f.file, func(t *testing.T) {
			a := fetchFixture(t, first, f.path, f.body)
			b := fetchFixture(t, second, f.path, f.body)
			if !bytes.Equal(a, b) {
				t.Errorf("two runs of the pinned mock disagree: %s", firstDifference(a, b))
			}
		})
	}
}

// firstDifference names the line the two payloads part ways on, which is more
// use than dumping two hundred kilobytes of JSON into the test log.
func firstDifference(got, want []byte) string {
	g, w := bytes.Split(got, []byte("\n")), bytes.Split(want, []byte("\n"))
	for i := 0; i < len(g) && i < len(w); i++ {
		if !bytes.Equal(g[i], w[i]) {
			return "line " + itoa(i+1) + ":\n  got:  " + string(bytes.TrimSpace(g[i])) +
				"\n  want: " + string(bytes.TrimSpace(w[i]))
		}
	}
	if len(g) != len(w) {
		return "got " + itoa(len(g)) + " lines, want " + itoa(len(w))
	}
	return "the two differ outside their lines"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
