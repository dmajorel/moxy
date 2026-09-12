package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// assetsPrefix is where Vite writes its content-hashed files: a URL under it
// never changes meaning, which is what makes the immutable cache policy safe.
const assetsPrefix = "/assets/"

// contentTypes pins the media types moxyd cares about. Go's built-in table
// lacks fonts, .ico and .map and otherwise consults /etc/mime.types, which is
// absent from the distroless image and differs between hosts. A fixed table
// keeps responses identical wherever the daemon runs.
var contentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".svg":         "image/svg+xml",
	".json":        "application/json",
	".map":         "application/json",
	".webmanifest": "application/manifest+json",
	".png":         "image/png",
	".ico":         "image/x-icon",
	".txt":         "text/plain; charset=utf-8",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".ttf":         "font/ttf",
}

// NewWebHandler serves the built frontend bundle found in dir.
//
// It fails at construction when dir holds no readable index.html, so a
// mis-pointed -web is reported at startup rather than on the first browser
// request. Symlinks inside dir are followed: the bundle is a build artefact
// under the operator's control, not untrusted input.
func NewWebHandler(dir string) (http.Handler, error) {
	if dir == "" {
		// http.Dir("") would silently mean the current directory.
		return nil, errors.New("web: empty directory")
	}
	index := filepath.Join(dir, "index.html")
	// Open rather than Stat: the daemon runs as a service user, and a bundle
	// readable by root only must be reported now, not as a 500 later.
	f, err := os.Open(index)
	if err != nil {
		return nil, fmt.Errorf("web: %w", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("web: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("web: %s: not a regular file", index)
	}
	return &webHandler{root: http.Dir(dir)}, nil
}

// webHandler serves a single-page application: real files as they are, and
// index.html for everything that looks like a client-side route.
type webHandler struct {
	// root cleans every path and rejects NUL bytes, so ".." cannot escape it.
	root http.Dir
}

func (h *webHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set on every branch, errors included, before anything is written.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// ServeMux already redirects non-canonical paths, but the handler must not
	// depend on the mux to be safe: Clean("/"+p) is rooted, so no ".." remains.
	p := path.Clean("/" + r.URL.Path)
	if p == "/" {
		h.serveIndex(w, r)
		return
	}

	if f, fi, ok := h.openFile(p); ok {
		defer f.Close()
		if strings.HasPrefix(p, assetsPrefix) {
			// Vite hashes every file under assets/: a URL never changes content,
			// so the browser may keep it for as long as it likes.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// Unhashed files must be revalidated on each load so a new deployment
			// is picked up; the Last-Modified below turns that into a cheap 304.
			w.Header().Set("Cache-Control", "no-cache")
		}
		setContentType(w, p)
		http.ServeContent(w, r, p, fi.ModTime(), f)
		return
	}

	// A path with an extension names a file that should exist, typically a
	// hashed asset from a previous build still referenced by a cached page.
	// Answering index.html with a 200 there would hide the deployment problem
	// behind a JavaScript parse error far from the cause.
	if path.Ext(p) != "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	// Extensionless unknown paths are client-side routes: the page owns them.
	h.serveIndex(w, r)
}

func (h *webHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	f, fi, ok := h.openFile("/index.html")
	if !ok {
		// Verified at startup; only a deletion after start reaches this branch.
		writeError(w, http.StatusInternalServerError, "web bundle unavailable")
		return
	}
	defer f.Close()
	w.Header().Set("Cache-Control", "no-cache")
	setContentType(w, "/index.html")
	http.ServeContent(w, r, "index.html", fi.ModTime(), f)
}

// openFile returns the regular file at p, or ok=false for anything else:
// missing, unreadable, or a directory. A directory is treated exactly like a
// missing file, so no listing can ever be produced.
func (h *webHandler) openFile(p string) (http.File, fs.FileInfo, bool) {
	f, err := h.root.Open(p)
	if err != nil {
		return nil, nil, false
	}
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		f.Close()
		return nil, nil, false
	}
	return f, fi, true
}

func setContentType(w http.ResponseWriter, p string) {
	if ct, ok := contentTypes[strings.ToLower(path.Ext(p))]; ok {
		w.Header().Set("Content-Type", ct)
	}
	// Otherwise ServeContent falls back to the mime package, then to sniffing.
}
