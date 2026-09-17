package config

import (
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// How moxy gets hold of the credential a maintenance session is opened with.
//
// The mode is a property of the process, not of a cluster and not of a node:
// there is no mixed estate, no automatic fallback from one to the other, and
// nothing downstream ever branches on it. It is written by hand and has no
// default -- tls.mode can afford to default to "system" because that is the
// safe choice, whereas these two are not comparable, and whoever decides where
// the key that opens a privileged session lives has to say so.
const (
	// MaintenanceModeSSHKey opens every session with one local ed25519 key,
	// read once at load time. It is the mode one starts with.
	MaintenanceModeSSHKey = "ssh-key"
	// MaintenanceModeOpenBao mints an ephemeral key per execution and has
	// OpenBao sign it, so that nothing long-lived sits on a disk and the
	// constraints live in a role instead of in an authorized_keys file on
	// every node. It is the mode one hardens into.
	MaintenanceModeOpenBao = "openbao"
)

// Defaults and ceilings of the maintenance block.
const (
	// DefaultMaintenanceUser is the service account deploy/ creates on each
	// node: no reachable shell, one forced command.
	DefaultMaintenanceUser = "moxy"
	// DefaultMaintenancePort is the usual sshd port.
	DefaultMaintenancePort = 22
	// DefaultMaintenanceConnectTimeout bounds getting the connection up, as
	// DefaultConnectTimeout does for a Proxmox call: a node that is switched
	// off, or behind a firewall that drops rather than refuses, costs exactly
	// this much before the next node is tried.
	DefaultMaintenanceConnectTimeout = 2 * time.Second
	// DefaultMaintenanceTimeout is the budget for the whole session, dial
	// included. `ha-manager crm-command` answers in well under a second; the
	// margin is for a node that is busy, not for one that is gone.
	DefaultMaintenanceTimeout = 20 * time.Second
	// DefaultOpenBaoMountPath is where the SSH secrets engine sits in a stock
	// OpenBao.
	DefaultOpenBaoMountPath = "ssh-client-signer"
	// DefaultOpenBaoTimeout is the budget for one call to OpenBao. Two round
	// trips happen per execution -- an AppRole login and a signature -- and
	// neither is a long operation.
	DefaultOpenBaoTimeout = 5 * time.Second
)

var (
	// unixUserPattern is the portable shape of a login name. The value is
	// handed to sshd as the account to open the session under, so it stays
	// within what a POSIX system accepts rather than within what Go can
	// encode.
	unixUserPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	// openBaoPathPattern is one or more path segments, since a mount may be
	// nested. It is interpolated into the url of the request that returns a
	// signed certificate, so no percent escape, no dot segment and no empty
	// segment gets through: this is not the place to find out that a path
	// traversal reaches another engine.
	openBaoPathPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*$`)
	// openBaoRolePattern is a single segment, for the same reason.
	openBaoRolePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// Maintenance is the process-wide half of node maintenance: which credential
// opens a session, and how a session is opened. A cluster only says whether it
// takes part -- see ClusterMaintenance.
//
// The split is not cosmetic. The mode names where the key lives, and a file
// that could name it twice is a file that could describe a mixed estate; the
// ssh block holds known_hosts, whose format was made to carry a whole estate,
// and one file per cluster would only multiply the places where the node added
// last month is missing.
type Maintenance struct {
	// Mode is "ssh-key" or "openbao". It is required and has no default.
	Mode string `json:"mode"`
	// SSHKey is the local key, in "ssh-key" mode only.
	SSHKey *SSHKeySource `json:"sshKey,omitempty"`
	// OpenBao is the signing authority, in "openbao" mode only.
	OpenBao *OpenBaoSource `json:"openbao,omitempty"`
	// SSH is how a session is opened, in both modes.
	SSH MaintenanceSSH `json:"ssh"`
}

// SSHKeySource is the local ed25519 key of "ssh-key" mode.
//
// The key is a file rather than an environment variable, unlike every other
// secret here: it is multi-line data, orchestrators mount that kind of thing as
// a file, and tls.caFile already set the precedent. It is read once, at load
// time -- it does not change, and an implementation whose credential is always
// the same is exactly right for it.
type SSHKeySource struct {
	// KeyFile is the private key in PEM form, unencrypted, 0600 and owned by
	// the uid this process runs as. A relative path is resolved against the
	// configuration file, like tls.caFile.
	KeyFile string `json:"keyFile"`
	// Key is KeyFile read at load time, wrapped so that no format verb, no
	// encoder and no log line can spell it out.
	Key Secret `json:"-"`
}

// OpenBaoSource is the signing authority of "openbao" mode.
//
// No SDK and no dependency: the OpenBao API is HTTP and JSON, which net/http
// and encoding/json cover. Nothing is kept between two executions either --
// one AppRole login and one signature each time -- so there is no token to
// renew, no TTL machinery to test, and the role alone decides how long a
// certificate lives.
type OpenBaoSource struct {
	// Address is the https endpoint of OpenBao, without a path.
	Address string `json:"address"`
	// TLS is the certificate policy, "system" or "pinned". There is
	// deliberately no "insecure": this is not the reading of a measurement,
	// it is the delivery of what opens a privileged session.
	TLS TLS `json:"tls"`
	// RoleID is the non-sensitive half of the AppRole credentials.
	RoleID string `json:"roleId"`
	// SecretIDFile holds the sensitive half. Wrapped, the file holds a
	// single-use response-wrapping token that has to be regenerated at every
	// restart of moxy, which is an operating constraint to state rather than
	// to discover.
	SecretIDFile string `json:"secretIdFile"`
	// Wrapped tells whether SecretIDFile holds a response-wrapping token to
	// redeem at start-up rather than the secret_id itself.
	Wrapped bool `json:"wrapped,omitempty"`
	// MountPath is where the SSH secrets engine is mounted. Defaults to
	// DefaultOpenBaoMountPath.
	MountPath string `json:"mountPath,omitempty"`
	// SSHRole is the role that signs, and that carries every constraint the
	// certificate will bear.
	SSHRole string `json:"sshRole"`
	// Timeout is the per-call budget as written in the file. Use
	// RequestTimeout, its parsed form, at run time.
	Timeout string `json:"timeout,omitempty"`
	// RequestTimeout is Timeout parsed, defaulted to DefaultOpenBaoTimeout.
	RequestTimeout time.Duration `json:"-"`
	// SecretID is SecretIDFile read at load time, wrapped like every other
	// secret. Whether OpenBao answers is NOT checked here: refusing to start
	// an overview because a third-party service is sealed loses the view that
	// does work along with the maintenance that does not.
	SecretID Secret `json:"-"`
}

// MaintenanceSSH is how a session is opened, in both modes.
type MaintenanceSSH struct {
	// User is the service account on the nodes. Defaults to
	// DefaultMaintenanceUser.
	User string `json:"user,omitempty"`
	// Port is the sshd port of the nodes. Defaults to
	// DefaultMaintenancePort.
	Port int `json:"port,omitempty"`
	// KnownHostsFile is the host keys collected out of band, in the usual
	// format. It is required in both modes, and there is no way to turn the
	// check off: tls.mode "insecure" loosens the reading of a measurement,
	// whereas an unverified host key runs a privileged command on whichever
	// machine answered in the node's place.
	KnownHostsFile string `json:"knownHostsFile"`
	// ConnectTimeout is the budget for getting the connection up, as written
	// in the file. Use DialTimeout, its parsed form, at run time.
	ConnectTimeout string `json:"connectTimeout,omitempty"`
	// Timeout is the budget for the whole session. Use RunTimeout, its parsed
	// form, at run time.
	Timeout string `json:"timeout,omitempty"`
	// DialTimeout is ConnectTimeout parsed, defaulted to
	// DefaultMaintenanceConnectTimeout and never above RunTimeout.
	DialTimeout time.Duration `json:"-"`
	// RunTimeout is Timeout parsed, defaulted to DefaultMaintenanceTimeout
	// and capped at MaxTimeout.
	RunTimeout time.Duration `json:"-"`
}

// ClusterMaintenance is everything a cluster may say about maintenance, and it
// carries no mode: the mixed estate is not forbidden by a validation rule, it
// is impossible to write down. A rule no configuration can break needs no
// check.
type ClusterMaintenance struct {
	// Enabled opens maintenance on this cluster. Opening it on qualification
	// without opening it on production is the normal case. It needs the
	// process-wide block, which is what says how a session is opened.
	Enabled bool `json:"enabled"`
	// AllowedUsers are the authenticated names allowed to drain a node of
	// this cluster. Who may drain production is not who may drain
	// qualification, so the list is per cluster.
	//
	// AN EMPTY LIST NAMES NOBODY, and never "everybody": draining a node is
	// a destructive action, so the fail-closed reading is the only one. Which
	// is why the two authentication modes have opposite rules about this
	// field, both checked at load time -- see checkAllowedUsersAgainstAuth.
	AllowedUsers []string `json:"allowedUsers,omitempty"`
	// Hosts maps a PVE node name to the address a session is opened to. It
	// exists because urls is an unlabelled list: nothing guarantees that the
	// first label of an endpoint's FQDN is the name of the node behind it.
	Hosts map[string]string `json:"hosts,omitempty"`
}

// NewSSHKeyMaintenance builds a validated "ssh-key" policy. It exists for the
// same reason NewTokenAuth does: a caller outside this package -- the server
// and maintenance tests, chiefly -- must not be able to assemble a policy the
// loader would have refused, and every rule below is a rule about a file on
// disk that a hand-built struct would silently skip.
//
// Paths are taken as written, so a relative one resolves against the working
// directory rather than against a configuration file there is none of.
func NewSSHKeyMaintenance(keyFile string, ssh MaintenanceSSH) (*Maintenance, error) {
	m := &Maintenance{
		Mode:   MaintenanceModeSSHKey,
		SSHKey: &SSHKeySource{KeyFile: keyFile},
		SSH:    ssh,
	}
	if errs := m.resolve(""); len(errs) > 0 {
		return nil, ValidationErrors(errs)
	}
	return m, nil
}

// NewOpenBaoMaintenance builds a validated "openbao" policy, for the same
// reason NewSSHKeyMaintenance exists.
func NewOpenBaoMaintenance(source OpenBaoSource, ssh MaintenanceSSH) (*Maintenance, error) {
	m := &Maintenance{
		Mode:    MaintenanceModeOpenBao,
		OpenBao: &source,
		SSH:     ssh,
	}
	if errs := m.resolve(""); len(errs) > 0 {
		return nil, ValidationErrors(errs)
	}
	return m, nil
}

// MaintenanceEnabled reports whether maintenance may be executed on this
// cluster. A cluster that says nothing does not take part.
func (cl *Cluster) MaintenanceEnabled() bool {
	return cl.Maintenance != nil && cl.Maintenance.Enabled
}

// resolveMaintenance validates the process-wide block and every cluster that
// refers to it. It lives here rather than in Config.resolve because two of its
// rules are about two blocks at once -- maintenance against auth, a cluster
// against the process -- and neither has an obvious home in either of them.
func (c *Config) resolveMaintenance(baseDir string) []error {
	var errs []error

	if c.Maintenance != nil {
		errs = append(errs, c.Maintenance.resolve(baseDir)...)
		// Running a command on a hypervisor for whoever reaches the port is
		// not a degraded mode, it is a remote shell. Auth.resolve has already
		// defaulted an empty mode to "none", so this reads a resolved value.
		if !c.Auth.Enabled() {
			errs = append(errs, fmt.Errorf("maintenance: auth.mode %q would let anyone who reaches the port drain a node, "+
				"set auth to %q or %q first", c.Auth.Mode, AuthProxyHeader, AuthToken))
		}
	}

	for i := range c.Clusters {
		cl := &c.Clusters[i]
		if cl.Maintenance == nil {
			continue
		}
		where := clusterWhere(i, cl)
		if cl.Maintenance.Enabled && c.Maintenance == nil {
			errs = append(errs, fmt.Errorf("%s: maintenance.enabled needs the process-wide maintenance block, "+
				"which is what says how a session is opened", where))
		}
		errs = append(errs, cl.Maintenance.resolve(where)...)
		errs = append(errs, cl.Maintenance.checkAllowedUsersAgainstAuth(where, c.Auth.Mode)...)
	}

	return errs
}

// checkAllowedUsersAgainstAuth makes allowedUsers mean the same thing as the
// authentication mode can prove, and refuses the two ways of writing a setting
// that does nothing.
//
// The two rules are opposites because the two modes are:
//
//   - "proxy-header" NAMES the caller, so the list is what authorizes. An
//     empty list names nobody -- draining is destructive, so the fail-closed
//     reading is the only one -- and a cluster with maintenance.enabled and an
//     empty list is therefore a switch that is on and wired to nothing. That
//     is the rule this repository already applies to tokenEnv in the wrong
//     mode: a setting that does nothing makes a file read as though it applied
//     something it does not.
//   - "token" authorizes with one shared secret and identifies NOBODY
//     (auth.go: everyone who holds it is the same caller), so a list of names
//     here would be a list nothing can be compared against. It must be empty,
//     the token alone authorizes, and the audit line says "anonymous".
//
// Under auth "none" neither rule applies: resolveMaintenance has already
// refused the whole block above, and a second message about the list would
// only bury the one that matters.
func (cm *ClusterMaintenance) checkAllowedUsersAgainstAuth(where, authMode string) []error {
	if !cm.Enabled {
		// A cluster that does not take part authorizes nobody either way.
		return nil
	}
	switch authMode {
	case AuthProxyHeader:
		if len(cm.AllowedUsers) == 0 {
			return []error{fmt.Errorf("%s: maintenance.enabled needs a non-empty maintenance.allowedUsers in auth mode %q, "+
				"because an empty list names nobody and would open maintenance to no one", where, AuthProxyHeader)}
		}
	case AuthToken:
		if len(cm.AllowedUsers) > 0 {
			return []error{fmt.Errorf("%s: maintenance.allowedUsers is only used in auth mode %q: "+
				"mode %q authorizes with one shared secret and identifies nobody, so these names could never be checked",
				where, AuthProxyHeader, AuthToken)}
		}
	}
	return nil
}

// resolve applies the defaults of the process-wide block and returns its
// problems, one per rule broken rather than one per file.
func (m *Maintenance) resolve(baseDir string) []error {
	var errs []error

	switch m.Mode {
	case MaintenanceModeSSHKey:
		// A block that does nothing in the chosen mode makes a file read as
		// though it applied something it does not -- the rule auth.go already
		// spells out for tokenEnv, and here the rule that keeps an estate
		// from being described half keyed and half signed.
		if m.OpenBao != nil {
			errs = append(errs, fmt.Errorf("maintenance: openbao is only used in %q mode", MaintenanceModeOpenBao))
		}
		if m.SSHKey == nil {
			errs = append(errs, fmt.Errorf("maintenance: %q mode needs the sshKey block", MaintenanceModeSSHKey))
		} else {
			errs = append(errs, m.SSHKey.resolve(baseDir)...)
		}
	case MaintenanceModeOpenBao:
		if m.SSHKey != nil {
			errs = append(errs, fmt.Errorf("maintenance: sshKey is only used in %q mode", MaintenanceModeSSHKey))
		}
		if m.OpenBao == nil {
			errs = append(errs, fmt.Errorf("maintenance: %q mode needs the openbao block", MaintenanceModeOpenBao))
		} else {
			errs = append(errs, m.OpenBao.resolve(baseDir)...)
		}
	case "":
		errs = append(errs, fmt.Errorf("maintenance: mode is required and has no default, want %q or %q: "+
			"where the key that opens a privileged session lives is written by hand",
			MaintenanceModeSSHKey, MaintenanceModeOpenBao))
	default:
		errs = append(errs, fmt.Errorf("maintenance: mode %q is unknown, want %q or %q",
			m.Mode, MaintenanceModeSSHKey, MaintenanceModeOpenBao))
	}

	// The ssh block is checked whatever the mode says, a wrong mode included:
	// an operator fixing the mode should not then discover the port.
	errs = append(errs, m.SSH.resolve(baseDir)...)

	return errs
}

// resolve reads the private key and checks what is only cheap to check now.
func (s *SSHKeySource) resolve(baseDir string) []error {
	if s.KeyFile == "" {
		return []error{fmt.Errorf("maintenance.sshKey: keyFile is required in %q mode", MaintenanceModeSSHKey)}
	}
	s.KeyFile = resolvePath(s.KeyFile, baseDir)

	info, err := os.Stat(s.KeyFile)
	if err != nil {
		return []error{fmt.Errorf("maintenance.sshKey.keyFile: %w", err)}
	}
	if !info.Mode().IsRegular() {
		return []error{fmt.Errorf("maintenance.sshKey.keyFile %s is not a regular file", s.KeyFile)}
	}

	var errs []error
	// A key the group or the others can read is a key the deployment believes
	// is private and is not. Start-up is the only moment where saying so is
	// cheap; the alternative is a key that has been readable for six weeks by
	// the time anybody looks.
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		errs = append(errs, fmt.Errorf("maintenance.sshKey.keyFile %s is mode %04o, reachable beyond its owner: chmod 0600 it",
			s.KeyFile, perm))
	}
	// Sys() carries a *syscall.Stat_t on every platform this daemon is built
	// for; where it does not there is no uid to compare, and the check is
	// skipped rather than guessed at. The comparison is against the uid of
	// the process -- 65532 in the image -- and never a hardcoded one.
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if uid := os.Getuid(); int(st.Uid) != uid {
			errs = append(errs, fmt.Errorf("maintenance.sshKey.keyFile %s is owned by uid %d, not by uid %d which this process runs as",
				s.KeyFile, st.Uid, uid))
		}
	}

	pemData, err := os.ReadFile(s.KeyFile)
	if err != nil {
		return append(errs, fmt.Errorf("maintenance.sshKey.keyFile: %w", err))
	}
	if err := checkUnencryptedPrivateKey(pemData); err != nil {
		return append(errs, fmt.Errorf("maintenance.sshKey.keyFile %s %w", s.KeyFile, err))
	}
	// The file is read once and never again: it does not change, and holding
	// it in a Secret is what keeps it out of a format verb and an encoder.
	s.Key = NewSecret(string(pemData))
	return errs
}

// checkUnencryptedPrivateKey refuses anything that is not a private key moxy
// can use unattended.
//
// The key is not parsed: the standard library cannot read an OpenSSH private
// key, and the transport that can is not in this revision. What is checked is
// exactly what would otherwise fail on the first click, six weeks later --
// there being no way for a daemon to be asked for a passphrase.
func checkUnencryptedPrivateKey(pemData []byte) error {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return fmt.Errorf("holds no PEM block, want an unencrypted ed25519 private key")
	}
	if !strings.Contains(block.Type, "PRIVATE KEY") {
		return fmt.Errorf("holds a %q block, want a private key -- the public half is what goes on the nodes", block.Type)
	}
	// The pre-OpenSSH format announces itself in a PEM header.
	if procType := block.Headers["Proc-Type"]; strings.Contains(strings.ToUpper(procType), "ENCRYPTED") {
		return fmt.Errorf("is an encrypted private key (Proc-Type: %s): moxy cannot be asked for a passphrase, "+
			"use a dedicated key without one", procType)
	}
	if block.Type == "OPENSSH PRIVATE KEY" {
		cipher, err := opensshCipher(block.Bytes)
		if err != nil {
			return err
		}
		if cipher != "none" {
			return fmt.Errorf("is an encrypted private key (cipher %s): moxy cannot be asked for a passphrase, "+
				"use a dedicated key without one", cipher)
		}
	}
	return nil
}

// opensshMagic opens the body of an OPENSSH PRIVATE KEY block.
const opensshMagic = "openssh-key-v1\x00"

// opensshCipher reads the cipher name out of an OpenSSH private key. It is the
// first string of the body and the only field needed to tell an encrypted key
// from a usable one; reading any further would mean implementing the format.
func opensshCipher(body []byte) (string, error) {
	if len(body) < len(opensshMagic) || string(body[:len(opensshMagic)]) != opensshMagic {
		return "", fmt.Errorf("is not an OpenSSH private key despite its PEM header")
	}
	rest := body[len(opensshMagic):]
	if len(rest) < 4 {
		return "", fmt.Errorf("is truncated: it carries no cipher name")
	}
	n := binary.BigEndian.Uint32(rest[:4])
	if uint64(n) > uint64(len(rest)-4) {
		return "", fmt.Errorf("is truncated: its cipher name runs past the end of the key")
	}
	return string(rest[4 : 4+n]), nil
}

// resolve applies the defaults of the OpenBao block and reads the secret_id.
// What it does not do is call OpenBao: the shape is validated at start-up, the
// reachability is not, so a sealed vault costs the maintenance and not the
// overview.
func (o *OpenBaoSource) resolve(baseDir string) []error {
	var errs []error

	if o.Address == "" {
		errs = append(errs, fmt.Errorf("maintenance.openbao: address is required"))
	} else if err := checkOpenBaoAddress(o.Address); err != nil {
		errs = append(errs, fmt.Errorf("maintenance.openbao: address: %w", err))
	}

	// The TLS policy of a cluster, minus its insecure mode: what comes back
	// from this address is not a measurement, it is a signature.
	if err := o.TLS.resolve("maintenance.openbao", baseDir, false); err != nil {
		errs = append(errs, err)
	}

	if o.RoleID == "" {
		errs = append(errs, fmt.Errorf("maintenance.openbao: roleId is required"))
	}

	if o.MountPath == "" {
		o.MountPath = DefaultOpenBaoMountPath
	}
	o.MountPath = strings.Trim(o.MountPath, "/")
	if !openBaoPathPattern.MatchString(o.MountPath) {
		errs = append(errs, fmt.Errorf("maintenance.openbao: mountPath %q must match %s", o.MountPath, openBaoPathPattern))
	}

	switch {
	case o.SSHRole == "":
		errs = append(errs, fmt.Errorf("maintenance.openbao: sshRole is required, it is the role that carries every constraint of the certificate"))
	case !openBaoRolePattern.MatchString(o.SSHRole):
		errs = append(errs, fmt.Errorf("maintenance.openbao: sshRole %q must match %s", o.SSHRole, openBaoRolePattern))
	}

	if err := o.resolveSecretID(baseDir); err != nil {
		errs = append(errs, err)
	}

	o.RequestTimeout = DefaultOpenBaoTimeout
	if o.Timeout != "" {
		d, err := time.ParseDuration(o.Timeout)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("maintenance.openbao: timeout %q is malformed: %w", o.Timeout, err))
		case d <= 0:
			errs = append(errs, fmt.Errorf("maintenance.openbao: timeout %q must be positive", o.Timeout))
		case d > MaxTimeout:
			errs = append(errs, fmt.Errorf("maintenance.openbao: timeout %q is above the %s maximum", o.Timeout, MaxTimeout))
		default:
			o.RequestTimeout = d
		}
	}

	return errs
}

// resolveSecretID reads the AppRole secret_id. Whether the file holds the
// secret itself or a single-use wrapping token to redeem at start-up is the
// operator's call; either way the value never leaves the Secret, and no error
// here ever quotes it.
func (o *OpenBaoSource) resolveSecretID(baseDir string) error {
	if o.SecretIDFile == "" {
		return fmt.Errorf("maintenance.openbao: secretIdFile is required")
	}
	o.SecretIDFile = resolvePath(o.SecretIDFile, baseDir)
	data, err := os.ReadFile(o.SecretIDFile)
	if err != nil {
		return fmt.Errorf("maintenance.openbao: secretIdFile: %w", err)
	}
	// A secret written by a shell redirection carries a trailing newline, the
	// same way one pasted into an environment variable does.
	value := strings.TrimSpace(string(data))
	if value == "" {
		return fmt.Errorf("maintenance.openbao: secretIdFile %s is empty", o.SecretIDFile)
	}
	o.SecretID = NewSecret(value)
	return nil
}

// resolve applies the defaults of the ssh block and checks its budgets.
func (s *MaintenanceSSH) resolve(baseDir string) []error {
	var errs []error

	if s.User == "" {
		s.User = DefaultMaintenanceUser
	}
	if !unixUserPattern.MatchString(s.User) {
		errs = append(errs, fmt.Errorf("maintenance.ssh: user %q must match %s", s.User, unixUserPattern))
	}

	if s.Port == 0 {
		s.Port = DefaultMaintenancePort
	}
	if s.Port < 1 || s.Port > 65535 {
		errs = append(errs, fmt.Errorf("maintenance.ssh: port %d is out of range, want 1..65535", s.Port))
	}

	errs = append(errs, s.resolveKnownHosts(baseDir)...)
	errs = append(errs, s.resolveTimeouts()...)

	return errs
}

// resolveKnownHosts requires the host keys and makes sure the file is there
// while the daemon is still starting.
//
// There is no insecure mode here and there will not be one. tls.mode
// "insecure" loosens the reading of measurements from a cluster; an unverified
// host key makes a privileged command run on whichever machine answered in the
// node's place, which is the one thing SSH was brought in to prevent.
func (s *MaintenanceSSH) resolveKnownHosts(baseDir string) []error {
	if s.KnownHostsFile == "" {
		return []error{fmt.Errorf("maintenance.ssh: knownHostsFile is required, and host key checking cannot be turned off: " +
			"collect the node keys out of band")}
	}
	s.KnownHostsFile = resolvePath(s.KnownHostsFile, baseDir)
	info, err := os.Stat(s.KnownHostsFile)
	if err != nil {
		return []error{fmt.Errorf("maintenance.ssh: knownHostsFile: %w", err)}
	}
	// The contents are the transport's business: it is the one that knows
	// which host key algorithms it accepts. What is worth saying now is that
	// the path names something, the alternative being a first maintenance
	// that fails on every node at once.
	if !info.Mode().IsRegular() {
		return []error{fmt.Errorf("maintenance.ssh: knownHostsFile %s is not a regular file", s.KnownHostsFile)}
	}
	return nil
}

// resolveTimeouts parses both budgets and caps them.
func (s *MaintenanceSSH) resolveTimeouts() []error {
	var errs []error

	s.RunTimeout = DefaultMaintenanceTimeout
	if s.Timeout != "" {
		d, err := time.ParseDuration(s.Timeout)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("maintenance.ssh: timeout %q is malformed: %w", s.Timeout, err))
		case d <= 0:
			errs = append(errs, fmt.Errorf("maintenance.ssh: timeout %q must be positive", s.Timeout))
		case d > MaxTimeout:
			// The ceiling of a Proxmox call, for the same reason: past it a
			// budget stops being a tuning knob and becomes a way to hang a
			// request on a node that will never answer.
			errs = append(errs, fmt.Errorf("maintenance.ssh: timeout %q is above the %s maximum", s.Timeout, MaxTimeout))
		default:
			s.RunTimeout = d
		}
	}

	s.DialTimeout = DefaultMaintenanceConnectTimeout
	if s.ConnectTimeout != "" {
		d, err := time.ParseDuration(s.ConnectTimeout)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("maintenance.ssh: connectTimeout %q is malformed: %w", s.ConnectTimeout, err))
		case d <= 0:
			errs = append(errs, fmt.Errorf("maintenance.ssh: connectTimeout %q must be positive", s.ConnectTimeout))
		default:
			s.DialTimeout = d
		}
	}
	// Connecting is part of running, so a connect budget above the session
	// budget can never be reached and only misleads whoever reads the file --
	// the reasoning of a cluster's connectTimeout, applied here.
	if s.DialTimeout > s.RunTimeout {
		errs = append(errs, fmt.Errorf("maintenance.ssh: connectTimeout %s is above timeout %s", s.DialTimeout, s.RunTimeout))
	}

	return errs
}

// resolve checks what one cluster says about maintenance. There is nothing to
// default here: an absent block means the cluster does not take part, and an
// empty allowedUsers means every authenticated caller, which is a decision and
// not an omission.
func (cm *ClusterMaintenance) resolve(where string) []error {
	var errs []error

	for _, user := range cm.AllowedUsers {
		if strings.TrimSpace(user) == "" {
			errs = append(errs, fmt.Errorf("%s: maintenance.allowedUsers holds an empty name", where))
		}
	}

	for node, host := range cm.Hosts {
		// A node named by an empty string, or pointing at nothing, quietly
		// takes itself out of the map the executor looks in -- and the first
		// anyone hears of it is a node that cannot be drained.
		if strings.TrimSpace(node) == "" {
			errs = append(errs, fmt.Errorf("%s: maintenance.hosts holds an empty node name", where))
			continue
		}
		if strings.TrimSpace(host) == "" {
			errs = append(errs, fmt.Errorf("%s: maintenance.hosts[%q] is empty, it must name the address a session is opened to", where, node))
		}
	}

	return errs
}

// checkOpenBaoAddress enforces an absolute https endpoint without a path, the
// way checkURL does for a Proxmox node: the caller appends /v1/... to it, so a
// path written here would end up in the middle of a request.
//
// https only, and no insecure mode anywhere near it -- what comes back from
// this address signs what opens a privileged session on a hypervisor.
func checkOpenBaoAddress(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%q is not a valid url: %w", raw, err)
	}
	if !u.IsAbs() {
		return fmt.Errorf("%q must be an absolute url", raw)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%q must use https, got %q", raw, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%q has no host", raw)
	}
	if u.User != nil {
		// Redacted, not raw, exactly as checkURL does: the message goes to a
		// log, and the rule exists because the url may carry a password.
		return fmt.Errorf("%q must not carry credentials", u.Redacted())
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		return fmt.Errorf("%q must not have a path: the client appends the /v1 prefix itself", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%q must not have a query or a fragment", raw)
	}
	return nil
}

// resolvePath makes a configured path absolute against the directory of the
// configuration file, the way tls.caFile is: the container image has no
// WORKDIR, so "ssh/known_hosts" next to /etc/moxy/config.json was looked up in
// /ssh. An empty baseDir -- the exported constructors -- leaves the path as it
// was written.
func resolvePath(path, baseDir string) string {
	if path == "" || baseDir == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}
