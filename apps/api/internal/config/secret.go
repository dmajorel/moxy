package config

// redacted is the placeholder printed in place of any sensitive value.
const redacted = "***"

// Secret wraps a sensitive string — a Proxmox API token secret — so that the
// usual ways of turning a value into text cannot leak it. It implements
// fmt.Stringer, fmt.GoStringer, json.Marshaler and encoding.TextMarshaler, all
// of which yield the redacted placeholder.
//
// The methods are defined on the value type so that they apply equally to a
// Secret and to a *Secret. Beware that fmt only calls them on fields it can
// reach through reflection: a Secret stored in an *unexported* struct field is
// printed field by field, which would reveal the value. Every struct carrying a
// Secret must therefore export the field — see Cluster.Secret.
type Secret struct {
	value string
}

// NewSecret builds a Secret holding value.
func NewSecret(value string) Secret {
	return Secret{value: value}
}

// String implements fmt.Stringer and covers the %v, %s and %q verbs.
func (s Secret) String() string {
	return redacted
}

// GoString implements fmt.GoStringer and covers the %#v verb.
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

// IsEmpty reports whether the secret holds no value.
func (s Secret) IsEmpty() bool {
	return s.value == ""
}

// Reveal returns the actual secret. Its only legitimate caller is the
// authentication transport of the proxmox package, which assembles the
// Authorization header. Anything else — logging, error messages, API
// responses, request dumps — must use the redacted forms above.
func (s Secret) Reveal() string {
	return s.value
}
