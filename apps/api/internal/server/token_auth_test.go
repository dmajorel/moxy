package server

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmajorel/moxy/apps/api/internal/config"
)

// uiToken is the shared token these tests configure. It is long enough and
// cookie-safe, like the one an operator is told to generate.
const uiToken = "6f1c0b9d4a2e8f37b5c1d0e9a7f26384"

func tokenAuth(t *testing.T) config.Auth {
	t.Helper()
	// Built through the constructor the loader uses, so the test cannot drift
	// from what a real configuration produces.
	auth, err := config.NewTokenAuth(uiToken)
	if err != nil {
		t.Fatalf("NewTokenAuth: %v", err)
	}
	return auth
}

// login posts a token to the login route and returns the recorder.
func login(t *testing.T, handler http.Handler, token string, overTLS bool) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, LoginPath, strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	if overTLS {
		r.TLS = &tls.ConnectionState{}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	return rec
}

// cookieOf returns the token cookie a response set, or nil.
func cookieOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == TokenCookie {
			return cookie
		}
	}
	return nil
}

// TestTokenAuthRefusesWithoutTheToken is the whole point of the mode: a
// request that carries nothing reads nothing, and a request carrying the wrong
// thing fares no better.
func TestTokenAuthRefusesWithoutTheToken(t *testing.T) {
	handler := newHandler(Options{Auth: tokenAuth(t)})

	tests := []struct {
		name   string
		set    func(*http.Request)
		wantOK bool
	}{
		{"nothing at all", func(*http.Request) {}, false},
		{"a wrong cookie", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: TokenCookie, Value: "not-the-token-but-just-as-long"})
		}, false},
		{"a wrong bearer", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer not-the-token-but-just-as-long")
		}, false},
		// A prefix of the real token: the comparison is over digests, so the
		// first characters being right buys nothing.
		{"a prefix of the token", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: TokenCookie, Value: uiToken[:len(uiToken)-1]})
		}, false},
		{"an empty cookie", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: TokenCookie, Value: ""})
		}, false},
		// The proxy-header mode's own credential means nothing here.
		{"an asserted user name", func(r *http.Request) {
			r.Header.Set(config.DefaultAuthHeader, "roman")
		}, false},
		{"the token in the cookie", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: TokenCookie, Value: uiToken})
		}, true},
		{"the token as a bearer", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+uiToken)
		}, true},
		{"a bearer whose scheme is shouted", func(r *http.Request) {
			r.Header.Set("Authorization", "BEARER "+uiToken)
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"/api/overview", "/metrics"} {
				r := httptest.NewRequest(http.MethodGet, path, nil)
				tc.set(r)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, r)
				// 503 is what an unconfigured overview source answers;
				// anything other than 401 means the request got through.
				if tc.wantOK && rec.Code == http.StatusUnauthorized {
					t.Fatalf("%s: status = %d, want the request to be let through", path, rec.Code)
				}
				if !tc.wantOK && rec.Code != http.StatusUnauthorized {
					t.Fatalf("%s: status = %d, want %d", path, rec.Code, http.StatusUnauthorized)
				}
			}
		})
	}
}

// TestLoginSetsTheCookie: the right token is answered with a cookie the
// browser will keep to itself and JavaScript cannot read.
func TestLoginSetsTheCookie(t *testing.T) {
	handler := newHandler(Options{Auth: tokenAuth(t)})

	rec := login(t, handler, uiToken, false)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	cookie := cookieOf(rec)
	if cookie == nil {
		t.Fatal("no token cookie was set")
	}
	if cookie.Value != uiToken {
		t.Error("the cookie does not carry the token that was presented")
	}
	if !cookie.HttpOnly {
		t.Error("the cookie is not HttpOnly: a script on the page could read the token")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, want %v", cookie.SameSite, http.SameSiteStrictMode)
	}
	if cookie.Path != "/" {
		t.Errorf("path = %q, want /", cookie.Path)
	}
	// A session cookie: nothing left on the workstation once the browser is
	// closed.
	if !cookie.Expires.IsZero() || cookie.MaxAge != 0 {
		t.Error("the cookie outlives the browser session")
	}
	if cookie.Secure {
		t.Error("Secure is set on a plain http answer, where the browser would refuse the cookie")
	}
	if store := rec.Header().Get("Cache-Control"); !strings.Contains(store, "no-store") {
		t.Errorf("Cache-Control = %q, want it to forbid storing the answer", store)
	}

	// And the cookie it just handed out opens the API.
	r := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	r.AddCookie(cookie)
	after := httptest.NewRecorder()
	handler.ServeHTTP(after, r)
	if after.Code == http.StatusUnauthorized {
		t.Fatalf("the cookie from the login is refused: status = %d", after.Code)
	}
}

// TestLoginMarksTheCookieSecureUnderTLS: the flag follows the connection, and
// only the connection -- there is no proxy in this mode whose word about the
// scheme could be taken.
func TestLoginMarksTheCookieSecureUnderTLS(t *testing.T) {
	handler := newHandler(Options{Auth: tokenAuth(t)})

	rec := login(t, handler, uiToken, true)
	cookie := cookieOf(rec)
	if cookie == nil {
		t.Fatal("no token cookie was set")
	}
	if !cookie.Secure {
		t.Error("Secure is not set on a TLS request")
	}
}

// TestLoginRefusesAWrongToken: no cookie, no hint, and the same shape of
// answer as the guard itself gives.
func TestLoginRefusesAWrongToken(t *testing.T) {
	handler := newHandler(Options{Auth: tokenAuth(t)})

	for _, token := range []string{"", "wrong", uiToken + "x", uiToken[:len(uiToken)-1], strings.ToUpper(uiToken)} {
		rec := login(t, handler, token, false)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("token %d characters long: status = %d, want %d", len(token), rec.Code, http.StatusUnauthorized)
		}
		if cookieOf(rec) != nil {
			t.Error("a cookie was set for a token that does not match")
		}
	}
}

// TestLoginRefusesAnythingButAPostOfJSON.
//
// The method check is routine. The content type is not: a form content type is
// what a page on another site can post without a preflight, and accepting one
// would let such a page log a browser in to a token of its choosing.
func TestLoginRefusesAnythingButAPostOfJSON(t *testing.T) {
	handler := newHandler(Options{Auth: tokenAuth(t)})

	tests := []struct {
		name        string
		method      string
		contentType string
		body        string
		want        int
	}{
		{"a GET", http.MethodGet, "application/json", "", http.StatusMethodNotAllowed},
		{"a form post", http.MethodPost, "application/x-www-form-urlencoded", "token=" + uiToken, http.StatusUnsupportedMediaType},
		{"a multipart post", http.MethodPost, "multipart/form-data; boundary=x", "", http.StatusUnsupportedMediaType},
		{"no content type", http.MethodPost, "", `{"token":"x"}`, http.StatusUnsupportedMediaType},
		{"a body that is not JSON", http.MethodPost, "application/json", "token=" + uiToken, http.StatusBadRequest},
		{"a body that is too large", http.MethodPost, "application/json",
			`{"token":"` + strings.Repeat("a", maxLoginBody) + `"}`, http.StatusBadRequest},
		{"JSON with a charset", http.MethodPost, "application/json; charset=utf-8", `{"token":"` + uiToken + `"}`, http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, LoginPath, strings.NewReader(tc.body))
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want != http.StatusNoContent && cookieOf(rec) != nil {
				t.Error("a cookie was set by a request that was refused")
			}
		})
	}
}

// TestLoginExistsOnlyInTokenMode: the route is served by the guard, so in any
// other mode it is not a route at all.
func TestLoginExistsOnlyInTokenMode(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		rec := login(t, newHandler(Options{}), uiToken, false)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
	t.Run("proxy header", func(t *testing.T) {
		rec := login(t, newHandler(Options{Auth: proxyAuth(t)}), uiToken, false)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
		if cookieOf(rec) != nil {
			t.Error("a token cookie was set in proxy-header mode")
		}
	})
}

// TestTokenAuthNeverEchoesTheToken: not in a refusal, not in an acceptance,
// not in a header. The cookie of a successful login is the only place the
// token is ever written, and it goes to the caller who already knew it.
func TestTokenAuthNeverEchoesTheToken(t *testing.T) {
	handler := newHandler(Options{Auth: tokenAuth(t)})

	answers := []*httptest.ResponseRecorder{
		login(t, handler, "totally-wrong-but-long-enough-token", false),
		login(t, handler, uiToken, false),
	}
	{
		r := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
		r.Header.Set("Authorization", "Bearer wrong-token-presented-by-a-caller")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		answers = append(answers, rec)
	}
	for i, rec := range answers {
		if strings.Contains(rec.Body.String(), uiToken) {
			t.Errorf("answer %d spells the configured token out in its body: %s", i, rec.Body.String())
		}
		for name, values := range rec.Header() {
			if name == "Set-Cookie" {
				continue
			}
			for _, value := range values {
				if strings.Contains(value, uiToken) {
					t.Errorf("answer %d spells the configured token out in %s", i, name)
				}
			}
		}
		// Nor the wrong value that was presented: echoing it back is how a
		// refusal becomes a reflection gadget.
		if strings.Contains(rec.Body.String(), "wrong") {
			t.Errorf("answer %d echoes what was presented: %s", i, rec.Body.String())
		}
	}
}

// TestTokenAuthLeavesTheProbesAlone: the split between the probes and
// everything else is the mode's to respect, not to redefine.
func TestTokenAuthLeavesTheProbesAlone(t *testing.T) {
	handler := newHandler(Options{Auth: tokenAuth(t)})

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

// TestTokenAuthServesTheBundleSoTheTokenCanBeTyped.
//
// The one concession of the mode, and it is bounded: the static bundle is
// served so that the login screen can exist, while everything carrying a
// reading about the estate stays behind the token. In proxy-header mode the
// bundle stays guarded, because something in front has already authenticated.
func TestTokenAuthServesTheBundleSoTheTokenCanBeTyped(t *testing.T) {
	web, err := NewWebHandler(writeBundle(t))
	if err != nil {
		t.Fatalf("NewWebHandler: %v", err)
	}
	handler := newHandler(Options{Auth: tokenAuth(t), Web: web})

	for _, path := range []string{"/", "/clusters/production"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want the login screen to be reachable", path, rec.Code)
		}
	}
	// And nothing else is: the data routes answer 401 even though the bundle
	// does not.
	for _, path := range []string{"/api/overview", "/api/clusters/production/nodes/n1", "/metrics"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want %d", path, rec.Code, http.StatusUnauthorized)
		}
	}
	// A write to the bundle's namespace is not a bundle read.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
