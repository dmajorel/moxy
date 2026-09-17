// Package maintenance puts a node of a Proxmox VE cluster into maintenance, and
// takes it out again, by running one predefined command over SSH on ANOTHER
// node of the same cluster.
//
// PVE exposes no REST route for this (ADR 0003, still true): the only way in is
// "ha-manager crm-command node-maintenance <action> <node>", which is a shell
// command on a node. ADR 0010 accepts that reversal and dictates the shape of
// everything here: a service account with no reachable shell, a ForceCommand
// validator, a closed grammar, and a backend that never builds a command line
// out of anything but two verbs and a node name it has already seen in the
// cluster view.
//
// WHAT IS NOT HERE. The SSH transport itself -- dial, host key verification,
// handshake, exec -- sits behind the Runner interface and is not implemented in
// this revision: the Go standard library has no SSH client, and vendoring
// golang.org/x/crypto/ssh is a separate step. Nothing in this package pretends
// to speak SSH, and nothing in it imports a crypto transport.
package maintenance

import (
	"context"
	"strings"
	"time"
)

// Actions this package will send, and the only ones. They are the two verbs the
// node-side validator accepts; anything else is refused here, before a command
// line exists at all.
const (
	ActionEnable  = "enable"
	ActionDisable = "disable"
)

// CommandName is the verb of the command sent to the node. It arrives on the
// node in $SSH_ORIGINAL_COMMAND, as data, because authorized_keys pins the
// executable with command= (or the OpenBao role does with force-command): what
// we send is a request, never something a shell runs as written.
const CommandName = "node-maintenance"

// OutputLimit caps the output carried back from one execution, stdout and
// stderr together. A talkative -- or hostile -- node must not be able to fill
// the memory of a daemon that holds the API tokens of the whole estate.
const OutputLimit = 8 << 10 // 8 KiB

// truncationNotice marks a capped output. English, like every other string the
// backend produces: the frontend renders it under a French label.
const truncationNotice = "\n[output truncated]"

// Credential is the material one session is opened with.
//
// It is deliberately NOT an ssh.Signer. Keeping the SSH library out of this
// type confines the only external dependency of the backend to the transport
// that actually needs it: the service, the key sources and their tests compile
// and run on the standard library alone, and swapping the transport -- or
// writing a test double for it -- changes no signature here.
type Credential struct {
	// PrivateKeyPEM is an ed25519 private key, PEM encoded, unencrypted.
	PrivateKeyPEM []byte
	// Certificate is an OpenSSH certificate line, and is nil in ssh-key mode:
	// there the public key is listed in authorized_keys on each node, so there
	// is nothing to present besides the key itself.
	Certificate []byte
}

// String redacts the credential, the way config.Secret redacts a token: this
// struct passes through a service, a runner and an audit path, and the first
// %v somebody adds for debugging must not print a private key. GoString covers
// %#v, which is the verb a test helper reaches for.
func (c Credential) String() string { return "maintenance.Credential{***}" }

// GoString redacts the credential under %#v.
func (c Credential) GoString() string { return c.String() }

// KeyProvider answers with the credential to open one session with.
//
// It is called PER EXECUTION, never once at start-up. The ssh-key
// implementation could perfectly well answer from a field read at load time --
// and does -- but the OpenBao one mints a short-lived certificate, and a cached
// one would be expired by the time the second maintenance of the month is
// asked for. Caching here would therefore work for weeks and fail on the night
// somebody needs to drain a node, which is the one night it must not.
//
// The interface is the same in both modes on purpose: the service holds ONE
// provider, chosen once at load time, and never learns which mode is active.
// That is what makes a mixed estate impossible to express rather than merely
// forbidden.
type KeyProvider interface {
	Credential(ctx context.Context) (*Credential, error)
}

// Target is one node a session may be opened on. It is never the node being put
// into maintenance: see Service.Execute.
type Target struct {
	Node string // PVE node name
	Host string // SSH address, from clusters[].maintenance.hosts or the node name
	Port int
	User string
}

// Result is what one command ended with.
type Result struct {
	ExitCode int
	// Output is stdout and stderr together, capped at OutputLimit.
	Output string
}

// Runner opens one session on Target and runs command.
//
// The real SSH implementation is not in this revision; what is wired today
// answers from the mock. An implementation MUST classify its failures with the
// Kind vocabulary of errors.go -- the retry rule of this package is read off
// those kinds, and an error it cannot classify is treated as APPLICATIVE, which
// is to say no other node is tried.
type Runner interface {
	Run(ctx context.Context, target Target, cred *Credential, command string) (*Result, error)
}

// SSHOptions are the process-wide session settings. The mode, the key source
// and these settings are properties of the process, not of a cluster and not of
// a node: one known_hosts file is made to carry a whole estate, and one file
// per cluster multiplies the places a newly added node is forgotten.
//
// They are a plain struct rather than a config type so that this package does
// not depend on internal/config: the wiring converts, and the tests here need
// no configuration file at all.
type SSHOptions struct {
	User        string
	Port        int
	DialTimeout time.Duration
	RunTimeout  time.Duration
}

// ClusterOptions is what one cluster says about maintenance. It carries NO
// mode: the estate-wide exclusivity of the two key sources is structural, and a
// per-cluster mode is not something this type can express.
type ClusterOptions struct {
	// Enabled false, or a cluster absent from Options.Clusters, means the
	// route does not exist for it: the answer is 404 and the UI shows no
	// button at all, never a disabled one.
	Enabled bool
	// Hosts maps a PVE node name to the address a session is opened on. It is
	// an override table: a node with no entry is reached at its own name,
	// because the configured cluster urls are an unlabelled list and nothing
	// says their first FQDN label is a node name.
	Hosts map[string]string
}

// Options are everything the service is built with.
type Options struct {
	SSH SSHOptions
	// Clusters is keyed by cluster id.
	Clusters map[string]ClusterOptions
	// Now is the clock, injectable so that a test can assert on RequestedAt.
	Now func() time.Time
}

// Defaults of a session, applied to whatever the operator left out.
const (
	defaultUser        = "moxy"
	defaultPort        = 22
	defaultDialTimeout = 2 * time.Second
	defaultRunTimeout  = 20 * time.Second
)

// withDefaults returns o with every zero value filled in.
func (o SSHOptions) withDefaults() SSHOptions {
	if o.User == "" {
		o.User = defaultUser
	}
	if o.Port == 0 {
		o.Port = defaultPort
	}
	if o.DialTimeout <= 0 {
		o.DialTimeout = defaultDialTimeout
	}
	if o.RunTimeout <= 0 {
		o.RunTimeout = defaultRunTimeout
	}
	return o
}

// buildCommand builds the request sent to the node.
//
// It is three words joined by single spaces, and it is built HERE, from an
// action this package validated and a node name the cluster view confirmed --
// never from a string that came in with the request. The node side splits it
// again on whitespace and refuses anything that is not exactly three words, so
// both ends hold the same closed grammar.
func buildCommand(action, node string) string {
	return CommandName + " " + action + " " + node
}

// capOutput trims output to OutputLimit bytes and says that it did.
//
// The cut is moved back to a rune boundary so the result is still valid UTF-8:
// a half-encoded rune would reach encoding/json, which replaces it silently,
// and the operator would read a corrupt last line with nothing saying why.
func capOutput(s string) string {
	if len(s) <= OutputLimit {
		return s
	}
	cut := OutputLimit
	for cut > 0 && !startsRune(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], "\n") + truncationNotice
}

// startsRune reports whether b starts a rune, that is, whether it is not a
// continuation byte.
func startsRune(b byte) bool { return b&0xC0 != 0x80 }

// validNodeName reports whether name is one PVE could have produced, using the
// same grammar as the node-side validator: letters, digits, dots and dashes.
//
// The names this package handles come from the cluster view, so this can only
// fire on a bug -- which is the point. It is the last place a name is checked
// before it becomes a word of a command line, and the cheapest one to keep.
func validNodeName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '-':
		default:
			return false
		}
	}
	return true
}
