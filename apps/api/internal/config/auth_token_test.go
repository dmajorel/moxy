package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

const (
	uiTokenEnv = "MOXY_UI_TOKEN"
	uiToken    = "6f1c0b9d4a2e8f37b5c1d0e9a7f26384"
)

// tokenDoc is a valid configuration whose auth block is in token mode.
func tokenDoc(auth map[string]any) map[string]any {
	document := doc(baseCluster())
	document["auth"] = auth
	return document
}

// TestLoadReadsTheTokenFromTheEnvironment: the shared token travels the same
// road as a cluster secret -- out of the environment, into a Secret, and the
// variable dropped behind it.
func TestLoadReadsTheTokenFromTheEnvironment(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	// A trailing newline is what a secret pasted through a shell carries.
	t.Setenv(uiTokenEnv, uiToken+"\n")

	cfg, err := Load(writeConfig(t, tokenDoc(map[string]any{"mode": "token", "tokenEnv": uiTokenEnv})))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Auth.Mode != AuthToken {
		t.Fatalf("mode = %q, want %q", cfg.Auth.Mode, AuthToken)
	}
	if !cfg.Auth.Enabled() {
		t.Error("Enabled() is false in token mode: the whole estate would be served to anyone")
	}
	if !cfg.Auth.Token.ConstantTimeEqual(uiToken) {
		t.Error("the token was not read from the environment, or not trimmed")
	}
	// Same reasoning as a cluster secret: once it is in a Secret, leaving the
	// variable set only hands it to whatever this process may exec.
	if _, ok := os.LookupEnv(uiTokenEnv); ok {
		t.Errorf("%s is still set after Load", uiTokenEnv)
	}
}

// TestLoadValidatesTheTokenMode: every way of writing the block that would not
// protect what it claims to protect.
func TestLoadValidatesTheTokenMode(t *testing.T) {
	cases := map[string]struct {
		auth  map[string]any
		token string // "" leaves the variable unset
		ok    bool
		want  string
	}{
		"token mode":       {map[string]any{"mode": "token", "tokenEnv": uiTokenEnv}, uiToken, true, ""},
		"without tokenEnv": {map[string]any{"mode": "token"}, uiToken, false, "tokenEnv"},
		"tokenEnv that is not a variable name": {
			map[string]any{"mode": "token", "tokenEnv": "not a name"}, uiToken, false, "valid environment variable name"},
		"variable not set":   {map[string]any{"mode": "token", "tokenEnv": uiTokenEnv}, "", false, "is not set"},
		"empty variable":     {map[string]any{"mode": "token", "tokenEnv": uiTokenEnv}, "   ", false, "empty"},
		"token too short":    {map[string]any{"mode": "token", "tokenEnv": uiTokenEnv}, "short-but-not-short-enough", false, "shorter"},
		"token with a space": {map[string]any{"mode": "token", "tokenEnv": uiTokenEnv}, strings.Repeat("a", 20) + " " + strings.Repeat("b", 20), false, "cookie cannot carry"},
		"token with a semicolon": {map[string]any{"mode": "token", "tokenEnv": uiTokenEnv},
			strings.Repeat("a", 20) + ";" + strings.Repeat("b", 20), false, "cookie cannot carry"},
		// Settings that do nothing in the chosen mode make a file read as
		// though it protected something it does not.
		"trusted proxies in token mode": {map[string]any{
			"mode": "token", "tokenEnv": uiTokenEnv, "trustedProxies": []any{"127.0.0.1/32"},
		}, uiToken, false, "only used"},
		"tokenEnv in proxy-header mode": {map[string]any{
			"mode": "proxy-header", "trustedProxies": []any{"127.0.0.1/32"}, "tokenEnv": uiTokenEnv,
		}, uiToken, false, "only used"},
		"tokenEnv without a mode": {map[string]any{"tokenEnv": uiTokenEnv}, uiToken, false, "only used"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(secretEnv, sentinel)
			if tc.token != "" {
				t.Setenv(uiTokenEnv, tc.token)
			} else {
				// t.Setenv registers the restore; Unsetenv alone would leak
				// into the next test if the variable existed outside.
				t.Setenv(uiTokenEnv, "")
				if err := os.Unsetenv(uiTokenEnv); err != nil {
					t.Fatalf("Unsetenv: %v", err)
				}
			}

			_, err := Load(writeConfig(t, tokenDoc(tc.auth)))
			if tc.ok {
				if err != nil {
					t.Fatalf("Load() error = %v, want none", err)
				}
				return
			}
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
			// Whatever is wrong with the token, saying so must not spell it
			// out: this message goes straight to the startup log.
			if tc.token != "" && strings.TrimSpace(tc.token) != "" &&
				strings.Contains(err.Error(), strings.TrimSpace(tc.token)) {
				t.Errorf("the validation error quotes the token: %v", err)
			}
		})
	}
}

// TestNewTokenAuthRefusesWhatLoadWouldRefuse: the constructor the tests of
// other packages use must not be a way around the loader's rules.
func TestNewTokenAuthRefusesWhatLoadWouldRefuse(t *testing.T) {
	for _, token := range []string{"", "too-short", strings.Repeat("a", MinTokenLength-1), strings.Repeat("a", 40) + ";"} {
		if _, err := NewTokenAuth(token); err == nil {
			t.Errorf("NewTokenAuth(%d characters) accepted a token Load would refuse", len(token))
		}
	}
	auth, err := NewTokenAuth(uiToken)
	if err != nil {
		t.Fatalf("NewTokenAuth: %v", err)
	}
	if !auth.Enabled() || auth.Mode != AuthToken {
		t.Errorf("mode = %q, enabled = %v", auth.Mode, auth.Enabled())
	}
}

// TestTokenAuthNeverPrintsTheToken is the non-regression test on redaction:
// the shared token is a Secret, and a Secret has to survive every way a Go
// program turns a value into text -- including the ones that skip Stringer.
func TestTokenAuthNeverPrintsTheToken(t *testing.T) {
	auth, err := NewTokenAuth(uiToken)
	if err != nil {
		t.Fatalf("NewTokenAuth: %v", err)
	}

	printed := []string{
		fmt.Sprint(auth),
		fmt.Sprintf("%v", auth),
		fmt.Sprintf("%+v", auth),
		fmt.Sprintf("%#v", auth),
		fmt.Sprintf("%s", auth),
		fmt.Sprintf("%q", auth),
		fmt.Sprint(auth.Token),
		// The verbs fmt would answer without ever consulting a Stringer.
		fmt.Sprintf("%d", auth.Token),
		fmt.Sprintf("%x", auth.Token),
		fmt.Sprintf("%#v", auth.Token),
		auth.Token.String(),
		auth.Token.GoString(),
	}
	raw, err := json.Marshal(auth)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	printed = append(printed, string(raw))
	text, err := auth.Token.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	printed = append(printed, string(text))

	for i, s := range printed {
		if strings.Contains(s, uiToken) {
			t.Errorf("rendering %d spells the token out: %s", i, s)
		}
	}
}

// TestConstantTimeEqual: the only way this package offers to check a token,
// and the reason there is no == anywhere near one.
func TestConstantTimeEqual(t *testing.T) {
	secret := NewSecret(sentinel)

	if !secret.ConstantTimeEqual(sentinel) {
		t.Error("the secret does not match itself")
	}
	for _, presented := range []string{"", sentinel + "x", sentinel[:len(sentinel)-1], strings.ToUpper(sentinel), "other"} {
		if secret.ConstantTimeEqual(presented) {
			t.Errorf("%q matched the secret", presented)
		}
	}
	// An empty secret matches nothing, not even an empty presented value: a
	// policy with no token must refuse everyone rather than let everyone in.
	empty := Secret{}
	for _, presented := range []string{"", sentinel} {
		if empty.ConstantTimeEqual(presented) {
			t.Errorf("the empty secret matched %q", presented)
		}
	}
}
