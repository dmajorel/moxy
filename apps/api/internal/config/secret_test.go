package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const sentinel = "0f3c1c1e-sentinel-uuid"

func TestSecretRedactsEveryTextualForm(t *testing.T) {
	s := NewSecret(sentinel)

	if got := s.String(); got != redacted {
		t.Errorf("String() = %q, want %q", got, redacted)
	}
	if got := s.GoString(); got != redacted {
		t.Errorf("GoString() = %q, want %q", got, redacted)
	}
	text, err := s.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	if string(text) != redacted {
		t.Errorf("MarshalText() = %q, want %q", text, redacted)
	}
	raw, err := s.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	if string(raw) != `"`+redacted+`"` {
		t.Errorf("MarshalJSON() = %s, want %q", raw, redacted)
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("MarshalJSON() produced invalid JSON: %v", err)
	}
}

func TestSecretReveal(t *testing.T) {
	if got := NewSecret(sentinel).Reveal(); got != sentinel {
		t.Errorf("Reveal() = %q, want %q", got, sentinel)
	}
	if !NewSecret("").IsEmpty() {
		t.Error("IsEmpty() = false for an empty secret")
	}
	if NewSecret(sentinel).IsEmpty() {
		t.Error("IsEmpty() = true for a non-empty secret")
	}
}

// everyVerb is every formatting verb fmt knows, minus %T, which prints a type
// and not a value.
//
// The list is the point of the test. Before Secret implemented fmt.Formatter,
// only the verbs a Stringer covers were safe, and %d on a Cluster printed the
// unexported field as {%!d(string=...)} — the secret itself, spelled out.
var everyVerb = []string{
	"%v", "%+v", "%#v", "%s", "%q", "%x", "%X",
	"%d", "%t", "%f", "%e", "%g", "%c", "%U", "%b", "%o", "%p",
	"%8v", "%-8s", "%.3s",
}

// TestSecretRedactedThroughFmtAndJSON checks every verb on the Secret itself,
// on a value and on a pointer: the methods are defined on the value type so
// that both forms are covered.
func TestSecretRedactedThroughFmtAndJSON(t *testing.T) {
	s := NewSecret(sentinel)
	for _, format := range everyVerb {
		if out := fmt.Sprintf(format, s); strings.Contains(out, sentinel) {
			t.Errorf("Sprintf(%q, value) leaked the secret: %s", format, out)
		}
		if out := fmt.Sprintf(format, &s); strings.Contains(out, sentinel) {
			t.Errorf("Sprintf(%q, pointer) leaked the secret: %s", format, out)
		}
	}
	raw, err := json.Marshal(struct {
		Secret Secret `json:"secret"`
	}{Secret: s})
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}
	if strings.Contains(string(raw), sentinel) {
		t.Errorf("json.Marshal leaked the secret: %s", raw)
	}
}

// TestClusterRedactedThroughEveryVerb is the shape a caller actually prints:
// nobody writes %d on a Secret, they write it on the struct that holds one.
func TestClusterRedactedThroughEveryVerb(t *testing.T) {
	cluster := Cluster{
		ID:        "preproduction",
		Name:      "Preproduction",
		TokenID:   "moxy@pve!ro",
		SecretEnv: "MOXY_PREPROD_SECRET",
		Secret:    NewSecret(sentinel),
	}
	for _, format := range everyVerb {
		if out := fmt.Sprintf(format, cluster); strings.Contains(out, sentinel) {
			t.Errorf("Sprintf(%q, cluster) leaked the secret: %s", format, out)
		}
		if out := fmt.Sprintf(format, &cluster); strings.Contains(out, sentinel) {
			t.Errorf("Sprintf(%q, *cluster) leaked the secret: %s", format, out)
		}
	}
}

// TestSecretFieldsAreExported guards the trap found in review: fmt prints an
// unexported field by reflection, bypassing String, GoString and MarshalText.
// Any struct in this package holding a Secret must export that field.
func TestSecretFieldsAreExported(t *testing.T) {
	secretType := reflect.TypeOf(Secret{})
	for _, holder := range []reflect.Type{reflect.TypeOf(Cluster{}), reflect.TypeOf(Config{})} {
		for i := 0; i < holder.NumField(); i++ {
			f := holder.Field(i)
			if f.Type == secretType && f.PkgPath != "" {
				t.Errorf("%s.%s holds a Secret in an unexported field: fmt would print its value", holder.Name(), f.Name)
			}
		}
	}
}

// TestUnexportedSecretFieldWouldLeak documents why the rule above exists: it
// is not a hypothesis about fmt, it is its behaviour.
func TestUnexportedSecretFieldWouldLeak(t *testing.T) {
	type holder struct {
		secret Secret
	}
	out := fmt.Sprintf("%+v", holder{secret: NewSecret(sentinel)})
	if !strings.Contains(out, sentinel) {
		t.Skipf("fmt no longer prints unexported fields by reflection (%s); the exported-field rule may be relaxed", out)
	}
}
