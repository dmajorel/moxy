package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	indexHTML = `<!doctype html><html><body><div id="root"></div></body></html>`
	assetJS   = `console.log("moxy");`
)

// writeBundle lays out the shape of a Vite build: index.html at the root and
// content-hashed files under assets/, plus one unhashed file at the root.
func writeBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"index.html":               indexHTML,
		"assets/index-abc123.js":   assetJS,
		"assets/index-abc123.css":  `#root{color:red}`,
		"assets/font-abc123.woff2": "wOF2",
		"favicon.svg":              `<svg xmlns="http://www.w3.org/2000/svg"/>`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func newWebRouter(t *testing.T, src OverviewSource) http.Handler {
	t.Helper()
	web, err := NewWebHandler(writeBundle(t))
	if err != nil {
		t.Fatalf("NewWebHandler: %v", err)
	}
	return newHandler(Options{Overview: src, Web: web})
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func assertNosniff(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want \"nosniff\"", got)
	}
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("unreadable body: %v", err)
	}
	return body
}

func TestNewWebHandlerRequiresIndex(t *testing.T) {
	_, err := NewWebHandler(t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a directory without index.html")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error = %v, want os.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), "index.html") {
		t.Errorf("error = %q, want it to name index.html", err)
	}
}

func TestNewWebHandlerRejectsIndexDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "index.html"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := NewWebHandler(dir)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error = %v, want \"not a regular file\"", err)
	}
}

func TestNewWebHandlerRejectsEmptyDir(t *testing.T) {
	if _, err := NewWebHandler(""); err == nil {
		t.Fatal("expected an error for an empty directory name")
	}
}

func TestWebServesIndexAtRoot(t *testing.T) {
	rec := get(newWebRouter(t, nil), http.MethodGet, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want \"no-cache\"", got)
	}
	assertNosniff(t, rec)
	if !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Errorf("body = %q, want index.html", rec.Body.String())
	}
}

// FileServer would answer /index.html with a redirect to /; the bundle must be
// reachable under its real name as well.
func TestWebServesIndexHTMLWithoutRedirect(t *testing.T) {
	rec := get(newWebRouter(t, nil), http.MethodGet, "/index.html")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want \"no-cache\"", got)
	}
}

func TestWebServesHashedAssetsAsImmutable(t *testing.T) {
	rec := get(newWebRouter(t, nil), http.MethodGet, "/assets/index-abc123.js")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", got)
	}
	if rec.Header().Get("Last-Modified") == "" {
		t.Error("missing Last-Modified")
	}
	assertNosniff(t, rec)
	if rec.Body.String() != assetJS {
		t.Errorf("body = %q, want %q", rec.Body.String(), assetJS)
	}
}

func TestWebContentTypes(t *testing.T) {
	h := newWebRouter(t, nil)
	cases := []struct{ path, want string }{
		{"/assets/index-abc123.css", "text/css; charset=utf-8"},
		{"/assets/font-abc123.woff2", "font/woff2"},
		{"/favicon.svg", "image/svg+xml"},
	}
	for _, tc := range cases {
		rec := get(h, http.MethodGet, tc.path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want %d", tc.path, rec.Code, http.StatusOK)
			continue
		}
		if got := rec.Header().Get("Content-Type"); got != tc.want {
			t.Errorf("%s: Content-Type = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestWebServesUnhashedFilesWithNoCache(t *testing.T) {
	rec := get(newWebRouter(t, nil), http.MethodGet, "/favicon.svg")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want \"no-cache\"", got)
	}
}

func TestWebFallsBackToIndexForDeepLinks(t *testing.T) {
	rec := get(newWebRouter(t, nil), http.MethodGet, "/clusters/production")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want \"no-cache\"", got)
	}
	if !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Errorf("body = %q, want index.html", rec.Body.String())
	}
}

// A stale hashed asset must surface as a 404, not as index.html with a 200:
// the latter would turn a deployment problem into a JavaScript parse error.
func TestWebReturnsNotFoundForMissingAssets(t *testing.T) {
	rec := get(newWebRouter(t, nil), http.MethodGet, "/assets/index-old.js")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	assertNosniff(t, rec)
	if body := decodeError(t, rec); body.Error != "not found" {
		t.Errorf("error = %q", body.Error)
	}
}

func TestWebDoesNotListDirectories(t *testing.T) {
	h := newWebRouter(t, nil)
	for _, target := range []string{"/assets/", "/assets"} {
		rec := get(h, http.MethodGet, target)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want %d", target, rec.Code, http.StatusOK)
		}
		body := rec.Body.String()
		if strings.Contains(body, "index-abc123.js") || strings.Contains(body, "<pre>") {
			t.Errorf("%s: directory listing leaked: %q", target, body)
		}
		if !strings.Contains(body, `id="root"`) {
			t.Errorf("%s: body = %q, want index.html", target, body)
		}
	}
}

func TestWebRejectsOtherMethods(t *testing.T) {
	h := newWebRouter(t, nil)
	for _, tc := range []struct{ method, target string }{
		{http.MethodPost, "/"},
		{http.MethodPut, "/assets/index-abc123.js"},
		{http.MethodDelete, "/clusters/production"},
	} {
		rec := get(h, tc.method, tc.target)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: status = %d, want %d", tc.method, tc.target, rec.Code, http.StatusMethodNotAllowed)
			continue
		}
		if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("%s %s: Allow = %q, want \"GET, HEAD\"", tc.method, tc.target, got)
		}
		if body := decodeError(t, rec); body.Error != "method not allowed" {
			t.Errorf("%s %s: error = %q", tc.method, tc.target, body.Error)
		}
	}
}

func TestWebHandlesHead(t *testing.T) {
	rec := get(newWebRouter(t, nil), http.MethodHead, "/assets/index-abc123.js")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD body = %q, want empty", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(assetJS)) {
		t.Errorf("Content-Length = %q, want %d", got, len(assetJS))
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", got)
	}
}

// The handler is exercised directly here, without the mux's own path cleaning,
// because the mux is a router and not a security boundary.
func TestWebRejectsTraversal(t *testing.T) {
	const sentinel = "top-secret-outside-the-bundle"
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	if err := os.Mkdir(dist, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(indexHTML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	web, err := NewWebHandler(dist)
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{"/../secret.txt", "/assets/../../secret.txt", "/a\x00b.js"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = target
		web.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%q: status = %d, want %d", target, rec.Code, http.StatusNotFound)
		}
		if strings.Contains(rec.Body.String(), sentinel) {
			t.Errorf("%q: escaped the bundle directory", target)
		}
	}
}

// no-cache only stays cheap if the real modification time is sent, so a
// conditional reload can be answered with a 304.
func TestWebReturns304WhenUnmodified(t *testing.T) {
	h := newWebRouter(t, nil)
	first := get(h, http.MethodGet, "/")
	lastModified := first.Header().Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("missing Last-Modified")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("If-Modified-Since", lastModified)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotModified)
	}
}

// Without -web the daemon is API-only: nothing outside /api and /healthz answers.
func TestRouterWithoutWebServesAPIOnly(t *testing.T) {
	h := newHandler(Options{})
	for _, target := range []string{"/", "/clusters", "/assets/index-abc123.js"} {
		if rec := get(h, http.MethodGet, target); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d", target, rec.Code, http.StatusNotFound)
		}
	}
	if rec := get(h, http.MethodGet, "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("/healthz: status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// The API namespace stays closed when the bundle is served: a misspelled
// endpoint gets a JSON 404, never index.html.
func TestWebDoesNotShadowAPI(t *testing.T) {
	h := newWebRouter(t, nil)
	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/api/unknown"},
		{http.MethodGet, "/api"},
		{http.MethodPost, "/api/unknown"},
	} {
		rec := get(h, tc.method, tc.target)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want %d", tc.method, tc.target, rec.Code, http.StatusNotFound)
			continue
		}
		if strings.Contains(rec.Body.String(), "<html") {
			t.Errorf("%s %s: served index.html instead of a JSON error", tc.method, tc.target)
		}
		if body := decodeError(t, rec); body.Error != "not found" {
			t.Errorf("%s %s: error = %q", tc.method, tc.target, body.Error)
		}
	}
}

func TestAPIRoutesUnchangedWithWeb(t *testing.T) {
	h := newWebRouter(t, fakeSource{overview: sampleOverview()})

	if rec := get(h, http.MethodGet, "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("/healthz: status = %d, want %d", rec.Code, http.StatusOK)
	}
	rec := get(h, http.MethodGet, "/api/overview")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/overview: status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("/api/overview: Cache-Control = %q, want \"no-store\"", got)
	}
}
