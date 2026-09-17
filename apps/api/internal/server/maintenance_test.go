package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmajorel/moxy/apps/api/internal/config"
	"github.com/dmajorel/moxy/apps/api/internal/detail"
	"github.com/dmajorel/moxy/apps/api/internal/maintenance"
	"github.com/dmajorel/moxy/apps/api/internal/metrics"
)

// maintenanceTarget is the route under test throughout this file.
const maintenanceTarget = "/api/clusters/prod/nodes/pve-01/maintenance"

// enableBody is the only body a legitimate caller sends.
const enableBody = `{"action":"enable"}`

// drainAuth is an authentication mode that NAMES the caller, configured to
// trust the peer httptest gives a request. It takes no *testing.T so that a
// table can build its options in a closure, before the subtest runs.
func drainAuth() config.Auth {
	auth, err := config.NewProxyHeaderAuth("X-Forwarded-User", []string{"192.0.2.0/24"})
	if err != nil {
		panic(err)
	}
	return auth
}

// postMaintenance builds a well-formed request: the JSON content type the
// route demands, and no Origin, which is what a caller that is not a browser
// looks like.
func postMaintenance(target, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// serveMaintenance runs one request through the whole router, guards included.
func serveMaintenance(opts Options, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	newHandler(opts).ServeHTTP(rec, r)
	return rec
}

// openOptions is a deployment where nobody is named and no cluster restricts
// anyone: mock mode, and the shape most tests below only care about the
// routing of.
func openOptions(src DetailSource) Options {
	return Options{Detail: src}
}

// TestMaintenanceExecutes is the happy path: the body is decoded, the action
// reaches the source, and the answer is the payload as it stands.
func TestMaintenanceExecutes(t *testing.T) {
	src := newFakeDetail()
	rec := serveMaintenance(openOptions(src), postMaintenance(maintenanceTarget, enableBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	want := detailCall{method: "ExecuteMaintenance", cluster: "prod", node: "pve-01", action: "enable"}
	if got := src.only(t); got != want {
		t.Errorf("call = %+v, want %+v", got, want)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("unreadable body: %v", err)
	}
	if body["via"] != "pve-02" || body["accepted"] != true {
		t.Errorf("body = %v, want the outcome of the execution", body)
	}
	// An answer that says a node was drained must never be read from a cache.
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want \"no-store\"", got)
	}
}

// TestMaintenanceMethodIsAPropertyOfTheRoute: eight routes read and one
// writes, so the Allow header cannot be a property of the /api/clusters/
// prefix any more.
func TestMaintenanceMethodIsAPropertyOfTheRoute(t *testing.T) {
	cases := []struct {
		name   string
		method string
		target string
		allow  string
	}{
		{"execution refuses a read", http.MethodGet, maintenanceTarget, "POST"},
		// HEAD especially: net/http answers one by running the handler and
		// dropping the body, so accepting it would drain a node for a load
		// balancer's probe.
		{"execution refuses HEAD", http.MethodHead, maintenanceTarget, "POST"},
		{"execution refuses DELETE", http.MethodDelete, maintenanceTarget, "POST"},
		{"the plan refuses a write", http.MethodPost, maintenanceTarget + "/plan", "GET, HEAD"},
		{"a node refuses a write", http.MethodPost, "/api/clusters/prod/nodes/pve-01", "GET, HEAD"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newFakeDetail()
			r := httptest.NewRequest(tc.method, tc.target, nil)
			r.Header.Set("Content-Type", "application/json")
			rec := serveMaintenance(openOptions(src), r)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
			}
			if got := rec.Header().Get("Allow"); got != tc.allow {
				t.Errorf("Allow = %q, want %q", got, tc.allow)
			}
			if len(src.calls) != 0 {
				t.Errorf("calls = %+v, want none", src.calls)
			}
		})
	}
}

// TestMaintenancePathIsRevalidated: Go 1.19 has no path parameters, so every
// segment of the new route is checked by hand, exactly like the eight read
// ones. None of these may reach the source.
func TestMaintenancePathIsRevalidated(t *testing.T) {
	targets := []string{
		"/api/clusters//nodes/pve-01/maintenance",
		"/api/clusters/prod/nodes//maintenance",
		"/api/clusters/prod/nodes/./maintenance",
		"/api/clusters/prod/nodes/../maintenance",
		"/api/clusters/%2e%2e/nodes/pve-01/maintenance",
		"/api/clusters/prod/nodes/%2e%2e/maintenance",
		// Percent-encoded traversal: %2F%2E%2E%2F decodes to "/../", which
		// must never be concatenated into a PVE request path.
		"/api/clusters/prod/nodes/x%2F..%2Fy/maintenance",
		"/api/clusters/prod/nodes/pve%00-01/maintenance",
		"/api/clusters/prod/nodes/pve%0a-01/maintenance",
		// A segment too many, and one too few.
		"/api/clusters/prod/nodes/pve-01/maintenance/execute",
		"/api/clusters/prod/nodes/maintenance",
		// The collection has to be the right one.
		"/api/clusters/prod/guests/101/maintenance",
		"/api/clusters/prod/nodes/pve-01/maintenance/",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			src := newFakeDetail()
			rec := serveMaintenance(openOptions(src), postMaintenance(target, enableBody))

			if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 404 or 405: %s", rec.Code, rec.Body.String())
			}
			if len(src.calls) != 0 {
				t.Errorf("calls = %+v, want none", src.calls)
			}
		})
	}
}

// TestMaintenanceAcceptsAnOversizedNodeNameNowhere: a node name is a hostname,
// so a segment longer than one cannot name anything.
func TestMaintenanceRejectsAnOversizedNodeName(t *testing.T) {
	src := newFakeDetail()
	target := "/api/clusters/prod/nodes/" + strings.Repeat("a", 254) + "/maintenance"
	rec := serveMaintenance(openOptions(src), postMaintenance(target, enableBody))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if len(src.calls) != 0 {
		t.Errorf("calls = %+v, want none", src.calls)
	}
}

// TestMaintenanceDemandsJSON is half of the CSRF answer. An HTML form can post
// three content types from another site with no preflight, and none of them is
// application/json.
func TestMaintenanceDemandsJSON(t *testing.T) {
	for _, contentType := range []string{
		"application/x-www-form-urlencoded",
		"multipart/form-data; boundary=x",
		"text/plain",
		"",
	} {
		t.Run(contentType, func(t *testing.T) {
			src := newFakeDetail()
			r := httptest.NewRequest(http.MethodPost, maintenanceTarget, strings.NewReader(enableBody))
			if contentType != "" {
				r.Header.Set("Content-Type", contentType)
			}
			rec := serveMaintenance(openOptions(src), r)

			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
			}
			if len(src.calls) != 0 {
				t.Errorf("calls = %+v, want none", src.calls)
			}
		})
	}
}

// TestMaintenanceRefusesAForeignOrigin is the other half. Token mode sets its
// cookie SameSite=Strict and is covered; proxy-header mode rides on a cookie
// of the proxy, which moxy does not configure and cannot make strict.
func TestMaintenanceRefusesAForeignOrigin(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		want   int
	}{
		{"no origin at all, which is what curl looks like", "", http.StatusOK},
		{"the service itself", "http://example.com", http.StatusOK},
		{"the service itself over TLS", "https://example.com", http.StatusOK},
		{"another site", "https://evil.example", http.StatusForbidden},
		{"a sandboxed frame", "null", http.StatusForbidden},
		{"something that is not a url", "not a url at all", http.StatusForbidden},
		{"a scheme with no host", "file://", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newFakeDetail()
			r := postMaintenance(maintenanceTarget, enableBody)
			// The Host the request is addressed to, which checkHost has
			// already vetted by the time the origin is compared to it.
			r.Host = "example.com"
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			rec := serveMaintenance(Options{Detail: src, AllowedHosts: []string{"example.com"}}, r)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want != http.StatusOK && len(src.calls) != 0 {
				t.Errorf("calls = %+v, want none", src.calls)
			}
		})
	}
}

// TestMaintenanceAcceptsADeclaredOrigin: a reverse proxy may rewrite Host to
// an internal name and pass the public one through, and -allowed-hosts is
// where the operator declares that public name.
func TestMaintenanceAcceptsADeclaredOrigin(t *testing.T) {
	src := newFakeDetail()
	r := postMaintenance(maintenanceTarget, enableBody)
	r.Host = "moxy.internal"
	r.Header.Set("Origin", "https://moxy.example")
	rec := serveMaintenance(Options{
		Detail:       src,
		AllowedHosts: []string{"moxy.internal", "moxy.example"},
	}, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// TestMaintenanceRefusesAnUnreadableBody: the decoder's own message quotes
// what it choked on, and what it choked on is a string the caller chose.
func TestMaintenanceRefusesAnUnreadableBody(t *testing.T) {
	const secret = "s3cr3t-value-the-caller-chose"
	src := newFakeDetail()
	rec := serveMaintenance(openOptions(src), postMaintenance(maintenanceTarget, `{"action": "`+secret))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if got := decodeError(t, rec).Error; got != "malformed body" {
		t.Errorf("error = %q, want \"malformed body\"", got)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Error("the answer echoes the body back")
	}
	if len(src.calls) != 0 {
		t.Errorf("calls = %+v, want none", src.calls)
	}
}

// TestMaintenanceRefusesAnOversizedBody: the reader is bounded before the
// decoder sees it. A route that reads whatever it is given is a memory tap.
func TestMaintenanceRefusesAnOversizedBody(t *testing.T) {
	src := newFakeDetail()
	body := `{"action":"` + strings.Repeat("a", maxMaintenanceBody*2) + `"}`
	rec := serveMaintenance(openOptions(src), postMaintenance(maintenanceTarget, body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(src.calls) != 0 {
		t.Errorf("calls = %+v, want none", src.calls)
	}
}

// TestMaintenanceErrorTable walks every row of §6 of the issue: one status and
// one kind per failure, so that the frontend translates by a stable word and
// never by matching English prose.
func TestMaintenanceErrorTable(t *testing.T) {
	cases := []struct {
		err      error
		status   int
		kind     string
		recorded bool
	}{
		{&maintenance.Error{Kind: maintenance.KindForbidden}, http.StatusForbidden, "maintenance_forbidden", true},
		{&maintenance.Error{Kind: maintenance.KindNoQuorum}, http.StatusConflict, "no_quorum", true},
		{&maintenance.Error{Kind: maintenance.KindNoHAManager}, http.StatusConflict, "no_ha_manager", true},
		{&maintenance.Error{Kind: maintenance.KindNoOtherNode}, http.StatusConflict, "no_other_node", true},
		{&maintenance.Error{Kind: maintenance.KindAlreadyRunning}, http.StatusConflict, "already_running", true},
		{&maintenance.Error{Kind: maintenance.KindKeySourceUnavailable}, http.StatusBadGateway, "keysource_unavailable", true},
		{&maintenance.Error{Kind: maintenance.KindKeySourceDenied}, http.StatusBadGateway, "keysource_denied", true},
		{&maintenance.Error{Kind: maintenance.KindUnreachable}, http.StatusBadGateway, "ssh_unreachable", true},
		{&maintenance.Error{Kind: maintenance.KindHostKeyMismatch}, http.StatusBadGateway, "ssh_host_key_mismatch", true},
		{&maintenance.Error{Kind: maintenance.KindAuthFailed}, http.StatusBadGateway, "ssh_auth_failed", true},
		{&maintenance.Error{Kind: maintenance.KindTimeout}, http.StatusGatewayTimeout, "ssh_timeout", true},
		{&maintenance.Error{Kind: maintenance.KindCommandRefused, ExitCode: 64}, http.StatusBadGateway, "command_refused", true},
		{&maintenance.Error{Kind: maintenance.KindCommandFailed, ExitCode: 1}, http.StatusBadGateway, "command_failed", true},
		// The two that carry no kind: nothing for the frontend to translate,
		// and nothing for a metric to count under an outcome.
		{maintenance.ErrNotFound, http.StatusNotFound, "", false},
		{detail.ErrNotFound, http.StatusNotFound, "", false},
		{maintenance.ErrInvalidAction, http.StatusBadRequest, "", false},
		{detail.ErrInvalidArgument, http.StatusBadRequest, "", false},
		// Anything else is an upstream failure, said the way a read says it.
		{errors.New("something nobody classified"), http.StatusBadGateway, "", true},
	}

	for _, tc := range cases {
		name := tc.kind
		if name == "" {
			name = tc.err.Error()
		}
		t.Run(name, func(t *testing.T) {
			src := newFakeDetail()
			src.execErr = tc.err
			rec := serveMaintenance(openOptions(src), postMaintenance(maintenanceTarget, enableBody))

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			body := decodeError(t, rec)
			if body.Kind != tc.kind {
				t.Errorf("kind = %q, want %q", body.Kind, tc.kind)
			}
			if body.Error == "" {
				t.Error("no English message alongside the kind")
			}
		})
	}
}

// TestMaintenanceMessagesSayNothingAboutTheFarEnd: the sentences served are
// fixed strings. What a node or a key source replied stays in the log, or it
// is an answer an attacker chooses the wording of.
func TestMaintenanceMessagesSayNothingAboutTheFarEnd(t *testing.T) {
	const leak = "root@prox-secret-host.internal: Permission denied (publickey)"
	src := newFakeDetail()
	src.execErr = &maintenance.Error{
		Kind: maintenance.KindAuthFailed,
		Node: "prox-secret-host",
		Err:  errors.New(leak),
	}
	rec := serveMaintenance(openOptions(src), postMaintenance(maintenanceTarget, enableBody))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	for _, forbidden := range []string{leak, "prox-secret-host", "publickey"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Errorf("the answer carries %q", forbidden)
		}
	}
}

// TestMaintenanceKeySourceIsNotTheNode: the two keysource kinds exist so that
// an operator is not sent probing port 22 on a node that is perfectly fine.
func TestMaintenanceKeySourceIsNotTheNode(t *testing.T) {
	src := newFakeDetail()
	src.execErr = &maintenance.Error{Kind: maintenance.KindKeySourceUnavailable}
	rec := serveMaintenance(openOptions(src), postMaintenance(maintenanceTarget, enableBody))

	message := decodeError(t, rec).Error
	if !strings.Contains(message, "key source") {
		t.Errorf("message = %q, want it to name the key source", message)
	}
	if strings.Contains(message, "node") {
		t.Errorf("message = %q, want it to blame the key source and not a node", message)
	}
}

// TestMaintenanceAnswersCarryNoSecret: the shared token travels in a cookie
// and in an Authorization header on this very request, and a refusal must not
// hand either of them back -- nor the body the caller chose the contents of.
func TestMaintenanceAnswersCarryNoSecret(t *testing.T) {
	src := newFakeDetail()
	src.execErr = &maintenance.Error{Kind: maintenance.KindCommandFailed, ExitCode: 1}
	r := postMaintenance(maintenanceTarget, `{"action":"enable","note":"do-not-echo-me"}`)
	r.AddCookie(&http.Cookie{Name: TokenCookie, Value: uiToken})
	r.Header.Set("Authorization", "Bearer "+uiToken)
	rec := serveMaintenance(Options{Detail: src, Auth: tokenAuth(t)}, r)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	for _, forbidden := range []string{uiToken, "do-not-echo-me", "Bearer"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Errorf("the answer carries %q", forbidden)
		}
	}
}

/* --------------------------------------------------------- authorization --- */

// TestMaintenanceAuthorizesANamedCaller: in proxy-header mode the list is what
// authorizes, and it is checked HERE -- the repository rule on destructive
// actions is that the backend verifies, never only the UI.
func TestMaintenanceAuthorizesANamedCaller(t *testing.T) {
	cases := []struct {
		name  string
		user  string
		users map[string][]string
		want  int
	}{
		{"a listed caller", "alice", map[string][]string{"prod": {"alice", "bob"}}, http.StatusOK},
		{"the other listed caller", "bob", map[string][]string{"prod": {"alice", "bob"}}, http.StatusOK},
		{"someone who is not on the list", "mallory", map[string][]string{"prod": {"alice"}}, http.StatusForbidden},
		// An empty list names nobody. It is the fail-closed reading, and the
		// loader refuses the configuration that would produce it.
		{"an empty list", "alice", map[string][]string{"prod": {}}, http.StatusForbidden},
		// A cluster nobody configured refuses a named caller rather than
		// opening to them, which is what makes forgetting the map safe.
		{"a cluster that is not in the map", "alice", map[string][]string{"qualification": {"alice"}}, http.StatusForbidden},
		{"no map at all", "alice", nil, http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newFakeDetail()
			r := postMaintenance(maintenanceTarget, enableBody)
			r.Header.Set("X-Forwarded-User", tc.user)
			rec := serveMaintenance(Options{
				Detail:           src,
				Auth:             drainAuth(),
				MaintenanceUsers: tc.users,
			}, r)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusForbidden {
				if got := decodeError(t, rec).Kind; got != string(maintenance.KindForbidden) {
					t.Errorf("kind = %q, want %q", got, maintenance.KindForbidden)
				}
				if len(src.calls) != 0 {
					t.Errorf("calls = %+v, want none: nothing must be attempted", src.calls)
				}
			}
		})
	}
}

// TestMaintenanceAuthorizesAnAnonymousCaller: token mode authorizes with one
// shared secret and identifies nobody, so there is no name to compare. The
// loader refuses a list of names in that mode; a list that reached here all
// the same is honoured the only safe way, by refusing.
func TestMaintenanceAuthorizesAnAnonymousCaller(t *testing.T) {
	cases := []struct {
		name  string
		users map[string][]string
		want  int
	}{
		{"no names configured, which is what the loader allows", map[string][]string{"prod": nil}, http.StatusOK},
		{"no map at all", nil, http.StatusOK},
		{"names the mode could never check", map[string][]string{"prod": {"alice"}}, http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newFakeDetail()
			r := postMaintenance(maintenanceTarget, enableBody)
			r.AddCookie(&http.Cookie{Name: TokenCookie, Value: uiToken})
			rec := serveMaintenance(Options{
				Detail:           src,
				Auth:             tokenAuth(t),
				MaintenanceUsers: tc.users,
			}, r)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestMaintenanceRefusesAnUnauthenticatedCaller: the guard in front answers
// before the route does, and it answers 401 -- there is nobody to forbid yet.
func TestMaintenanceRefusesAnUnauthenticatedCaller(t *testing.T) {
	src := newFakeDetail()
	rec := serveMaintenance(Options{
		Detail:           src,
		Auth:             drainAuth(),
		MaintenanceUsers: map[string][]string{"prod": {"alice"}},
	}, postMaintenance(maintenanceTarget, enableBody))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
	if len(src.calls) != 0 {
		t.Errorf("calls = %+v, want none", src.calls)
	}
}

/* ----------------------------------------------------------------- audit --- */

// captureLog collects what the package logger writes during a test.
func captureLog(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	flags, writer := log.Flags(), log.Writer()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
	return &buf
}

// TestMaintenanceAuditsEveryAttempt: one line per attempt, succeeded or not,
// naming who asked. It is the only record of a destructive action, and a mode
// that cannot name anyone says so in a word rather than leaving a blank.
func TestMaintenanceAuditsEveryAttempt(t *testing.T) {
	cases := []struct {
		name string
		opts func(DetailSource) Options
		user string
		fail error
		want []string
	}{
		{
			name: "a named caller who succeeds",
			opts: func(src DetailSource) Options {
				return Options{Detail: src, Auth: drainAuth(), MaintenanceUsers: map[string][]string{"prod": {"alice"}}}
			},
			user: "alice",
			want: []string{`user="alice"`, `cluster="prod"`, `node="pve-01"`, `action="enable"`, `via="pve-02"`, `outcome="ok"`},
		},
		{
			name: "a named caller who is refused",
			opts: func(src DetailSource) Options {
				return Options{Detail: src, Auth: drainAuth(), MaintenanceUsers: map[string][]string{"prod": {"alice"}}}
			},
			user: "mallory",
			want: []string{`user="mallory"`, `outcome="maintenance_forbidden"`},
		},
		{
			name: "a caller nothing can name",
			opts: func(src DetailSource) Options { return Options{Detail: src} },
			want: []string{`user="anonymous"`, `outcome="ok"`},
		},
		{
			name: "a command that failed on the node",
			opts: func(src DetailSource) Options { return Options{Detail: src} },
			fail: &maintenance.Error{Kind: maintenance.KindCommandFailed, ExitCode: 1},
			want: []string{`outcome="command_failed"`, "exit=1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logged := captureLog(t)
			src := newFakeDetail()
			src.execErr = tc.fail
			r := postMaintenance(maintenanceTarget, enableBody)
			if tc.user != "" {
				r.Header.Set("X-Forwarded-User", tc.user)
			}
			serveMaintenance(tc.opts(src), r)

			line := logged.String()
			if !strings.Contains(line, "maintenance audit:") {
				t.Fatalf("no audit line was written: %q", line)
			}
			for _, want := range tc.want {
				if !strings.Contains(line, want) {
					t.Errorf("audit = %q, want it to carry %s", line, want)
				}
			}
			// A timestamp of its own, so that the line stays an audit line
			// whatever log.SetFlags was called with -- as it just was.
			if !strings.Contains(line, "at=20") {
				t.Errorf("audit = %q, want a timestamp of its own", line)
			}
		})
	}
}

// TestMaintenanceAuditQuotesWhatCameFromAHeader: the caller's name is asserted
// by a proxy, and an unquoted newline in one would let it forge a line of its
// own in the very trail meant to record it.
func TestMaintenanceAuditQuotesWhatCameFromAHeader(t *testing.T) {
	logged := captureLog(t)
	src := newFakeDetail()
	r := postMaintenance(maintenanceTarget, enableBody)
	// A header value net/http accepts, carrying what would end a line and
	// start a second one saying somebody else asked.
	r.Header.Set("X-Forwarded-User", "alice\\nmaintenance audit: user=\"root\"")
	serveMaintenance(Options{
		Detail:           src,
		Auth:             drainAuth(),
		MaintenanceUsers: map[string][]string{"prod": {"alice"}},
	}, r)

	written := strings.TrimSuffix(logged.String(), "\n")
	if strings.Contains(written, "\n") {
		t.Errorf("audit = %q, want exactly one line", logged.String())
	}
	// The quotes of the forged fragment come back escaped, which is what
	// proves it was written as a value and not as syntax.
	if !strings.Contains(written, `\"root\"`) {
		t.Errorf("audit = %q, want the injected quotes escaped", written)
	}
	// And the name was still refused: it is not "alice".
	if !strings.Contains(written, `outcome="maintenance_forbidden"`) {
		t.Errorf("audit = %q, want the forged name refused", written)
	}
}

/* --------------------------------------------------------------- metrics --- */

// TestMaintenanceCountsWhatItServes wires the counters that already exist, and
// checks the one label rule that matters: a cluster id nobody configured must
// not be able to create a time series by being spelled into a URL.
func TestMaintenanceCountsWhatItServes(t *testing.T) {
	before := maintenanceCount(t)

	src := newFakeDetail()
	serveMaintenance(openOptions(src), postMaintenance(maintenanceTarget, enableBody))
	if got := maintenanceCount(t) - before; got != 1 {
		t.Errorf("a successful execution counted %d times, want 1", got)
	}

	// A cluster this deployment never configured, refused before anything is
	// attempted: no line, and no label.
	refused := newFakeDetail()
	r := postMaintenance("/api/clusters/unconfigured/nodes/pve-01/maintenance", enableBody)
	r.Header.Set("X-Forwarded-User", "alice")
	rec := serveMaintenance(Options{
		Detail:           refused,
		Auth:             drainAuth(),
		MaintenanceUsers: map[string][]string{"prod": {"alice"}},
	}, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if strings.Contains(exposition(t), `cluster="unconfigured"`) {
		t.Error("an unconfigured cluster id became a metric label")
	}
}

// maintenanceCount is how many maintenance commands the default registry has
// counted, all labels together.
func maintenanceCount(t *testing.T) int {
	t.Helper()
	total := 0
	for _, line := range strings.Split(exposition(t), "\n") {
		if !strings.HasPrefix(line, "moxy_maintenance_commands_total{") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		total += n
	}
	return total
}

// exposition renders the default registry the way /metrics serves it.
func exposition(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	metrics.Default.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Body.String()
}
