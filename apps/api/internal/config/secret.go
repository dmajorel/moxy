package config

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"io"
)

// redacted is the placeholder printed in place of any sensitive value.
const redacted = "***"

// Secret wraps a sensitive string — a Proxmox API token secret — so that the
// usual ways of turning a value into text cannot leak it. It implements
// fmt.Formatter, fmt.Stringer, fmt.GoStringer, json.Marshaler and
// encoding.TextMarshaler, all of which yield the redacted placeholder.
//
// Two things make that promise true, and both are needed.
//
// fmt.Formatter is the first. A Stringer is only consulted for %v, %s, %q, %x
// and %X; give a Secret any other verb — %d, %t, %f, %c, %U, %b, %o, %e, %g —
// and fmt falls back to printing the struct field by field, unexported field
// included, which is the value itself. A Formatter takes precedence for every
// verb fmt routes through its method check.
//
// Holding the value behind a pointer is the second. Two verbs never reach that
// check: %T, which prints a type and so cannot leak, and %p, which fmt answers
// before it. Given a value rather than a pointer, %p falls into fmt's bad-verb
// path, and that path prints the argument by reflection with method calls
// disabled — no Formatter, no Stringer, just the fields. Reflection prints a
// pointer as an address, so there is nothing to read there. The same reasoning
// covers a Secret reached through a field fmt cannot Interface(): an unexported
// field is printed by reflection too. Exporting such fields — see
// Cluster.Secret — is still the clearer habit, but it is no longer what stands
// between a format string and the token.
type Secret struct {
	// value is a pointer so that the plaintext is never reachable by
	// reflection. See the type comment: this is load-bearing, not an
	// optimisation. A nil pointer is the empty secret.
	value *string
}

// NewSecret builds a Secret holding value.
func NewSecret(value string) Secret {
	return Secret{value: &value}
}

// Format implements fmt.Formatter, which fmt consults before anything else and
// for every verb. It ignores the verb and the flags on purpose: there is no
// width or precision worth honouring on a placeholder, and honouring them
// would mean branching on a verb, which is how the gap this closes appeared in
// the first place.
func (s Secret) Format(f fmt.State, verb rune) {
	_, _ = io.WriteString(f, redacted)
}

// String implements fmt.Stringer, for the callers that ask for text without
// going through fmt.
func (s Secret) String() string {
	return redacted
}

// GoString implements fmt.GoStringer. Format already covers %#v; this stays
// for anything that calls GoString directly.
func (s Secret) GoString() string {
	return redacted
}

// MarshalJSON implements json.Marshaler: the secret is serialised as the
// redacted placeholder, never as its value.
func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redacted + `"`), nil
}

// MarshalText implements encoding.TextMarshaler, which json also uses for map
// keys and which several standard encoders prefer over String.
func (s Secret) MarshalText() ([]byte, error) {
	return []byte(redacted), nil
}

// IsEmpty reports whether the secret holds no value. The zero Secret is empty.
func (s Secret) IsEmpty() bool {
	return s.value == nil || *s.value == ""
}

// Reveal returns the actual secret. Its only legitimate caller is the
// authentication transport of the proxmox package, which assembles the
// Authorization header. Anything else — logging, error messages, API
// responses, request dumps — must use the redacted forms above.
func (s Secret) Reveal() string {
	return s.plaintext()
}

// plaintext is the in-package way to the value, so that code inside this
// package — the token policy validating what it was handed, the comparison
// below — does not have to call Reveal and turn a method with one legitimate
// caller into a method with several.
func (s Secret) plaintext() string {
	if s.value == nil {
		return ""
	}
	return *s.value
}

// ConstantTimeEqual reports whether presented is the secret.
//
// It exists so that comparing a secret does not mean getting it out of the
// Secret first: the value never leaves the type, and there is one obvious way
// to check a token that is not ==.
//
// The comparison is over SHA-256 digests rather than the strings themselves,
// which does two things. subtle.ConstantTimeCompare returns 0 immediately —
// and therefore faster — when the lengths differ, so comparing raw values
// leaks the length of the configured token, one probe at a time; digests are
// always 32 bytes. And the digests of two different values differ in about
// half their bits wherever the originals happened to agree, so no prefix of a
// guess is ever worth more than another.
func (s Secret) ConstantTimeEqual(presented string) bool {
	// An empty secret matches nothing, not even an empty presented value: a
	// policy with no token configured must refuse everyone rather than let
	// everyone in, which is what a plain digest comparison would do here.
	if s.IsEmpty() || presented == "" {
		return false
	}
	want := sha256.Sum256([]byte(s.plaintext()))
	got := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}
