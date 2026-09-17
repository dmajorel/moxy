package maintenance

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestKindsAreTheFrozenVocabulary pins the wire values.
//
// These strings are a contract: the HTTP layer maps them to status codes, the
// frontend translates them into French, and /metrics counts outcomes under
// them. Renaming one is a breaking change in three places at once, and nothing
// else in the repository would notice.
func TestKindsAreTheFrozenVocabulary(t *testing.T) {
	want := map[Kind]string{
		KindForbidden:            "maintenance_forbidden",
		KindNoQuorum:             "no_quorum",
		KindNoHAManager:          "no_ha_manager",
		KindNoOtherNode:          "no_other_node",
		KindAlreadyRunning:       "already_running",
		KindKeySourceUnavailable: "keysource_unavailable",
		KindKeySourceDenied:      "keysource_denied",
		KindUnreachable:          "ssh_unreachable",
		KindHostKeyMismatch:      "ssh_host_key_mismatch",
		KindAuthFailed:           "ssh_auth_failed",
		KindTimeout:              "ssh_timeout",
		KindCommandRefused:       "command_refused",
		KindCommandFailed:        "command_failed",
	}
	if len(want) != 13 {
		t.Fatalf("the vocabulary has %d kinds, the contract freezes 13", len(want))
	}
	for kind, value := range want {
		if string(kind) != value {
			t.Errorf("kind = %q, want %q", string(kind), value)
		}
	}
}

// TestKindRetryable covers every kind of the vocabulary, in both directions:
// the four transport failures let the next node be tried, and NOTHING ELSE
// does. This is the rule the whole package is built around.
func TestKindRetryable(t *testing.T) {
	tests := []struct {
		kind Kind
		want bool
	}{
		// Transport: the command never reached ha-manager.
		{KindUnreachable, true},
		{KindHostKeyMismatch, true},
		{KindAuthFailed, true},
		{KindTimeout, true},
		// Applicative: the node side answered, the command may have taken
		// effect, no other node is tried.
		{KindCommandRefused, false},
		{KindCommandFailed, false},
		// Refusals that happen before any session exists.
		{KindForbidden, false},
		{KindNoQuorum, false},
		{KindNoHAManager, false},
		{KindNoOtherNode, false},
		{KindAlreadyRunning, false},
		{KindKeySourceUnavailable, false},
		{KindKeySourceDenied, false},
		// An unset kind is not a licence to retry.
		{Kind(""), false},
		{Kind("something_new"), false},
	}
	for _, tc := range tests {
		t.Run(string(tc.kind), func(t *testing.T) {
			if got := tc.kind.Retryable(); got != tc.want {
				t.Errorf("Retryable() = %v, want %v", got, tc.want)
			}
			err := &Error{Kind: tc.kind}
			if got := err.Retryable(); got != tc.want {
				t.Errorf("(*Error).Retryable() = %v, want %v", got, tc.want)
			}
			if got := Retryable(err); got != tc.want {
				t.Errorf("Retryable(err) = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRetryableOnAnUnclassifiedError is the safe reading, and the one a
// refactoring would quietly invert: an error nothing classified is not proof
// that ha-manager was never reached.
func TestRetryableOnAnUnclassifiedError(t *testing.T) {
	if Retryable(errors.New("boom")) {
		t.Error("an unclassified error must not be retryable")
	}
	if Retryable(nil) {
		t.Error("a nil error must not be retryable")
	}
}

func TestErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "full",
			err:  &Error{Cluster: "qualification", Node: "prox-2", Kind: KindCommandFailed, ExitCode: 2, Err: errors.New("crm said no")},
			want: "cluster qualification: node prox-2: command_failed: exit 2: crm said no",
		},
		{
			name: "precondition",
			err:  &Error{Cluster: "qualification", Node: "prox-1", Kind: KindNoQuorum},
			want: "cluster qualification: node prox-1: no_quorum",
		},
		{
			name: "key source, which belongs to no node",
			err:  &Error{Cluster: "qualification", Kind: KindKeySourceUnavailable},
			want: "cluster qualification: keysource_unavailable",
		},
		{
			name: "empty",
			err:  &Error{},
			want: "maintenance: unknown error",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestErrorUnwrapAndKindOf(t *testing.T) {
	cause := errors.New("connection refused")
	err := fmt.Errorf("wrapped: %w", newError("qualification", "prox-2", KindUnreachable, cause))

	if !errors.Is(err, cause) {
		t.Error("the cause must stay reachable through errors.Is")
	}
	kind, ok := KindOf(err)
	if !ok || kind != KindUnreachable {
		t.Errorf("KindOf() = %q, %v, want %q, true", kind, ok, KindUnreachable)
	}
	if _, ok := KindOf(errors.New("plain")); ok {
		t.Error("KindOf() must report false for an error of another package")
	}
}

// TestErrorCarriesNoSecret is the security rule of this type, checked on the
// message rather than trusted: an *Error is logged, counted and, through its
// kind, reaches the browser.
func TestErrorCarriesNoSecret(t *testing.T) {
	err := newError("qualification", "prox-2", KindAuthFailed, errors.New("handshake failed"))
	message := err.Error()
	for _, forbidden := range []string{"PRIVATE KEY", CommandName} {
		if strings.Contains(message, forbidden) {
			t.Errorf("error message %q carries %q", message, forbidden)
		}
	}
}
