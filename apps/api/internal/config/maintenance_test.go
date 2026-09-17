package config

import (
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The AppRole secret_id of the fixtures. It is a made-up value, and the point
// of several tests below is that it never appears anywhere but in the Secret.
const maintSecretID = "db02de05-fa39-4855-059b-67221c5c2f63"

// maintPaths are the three files a maintenance block points at, all in one
// directory so that a test can also write a configuration next to them and
// check how relative paths are resolved.
type maintPaths struct {
	dir        string
	key        string
	knownHosts string
	secretID   string
}

// writeMaintFiles lays down a usable set: an unencrypted key, 0600, owned by
// whoever runs the tests, and the two files that go with it.
func writeMaintFiles(t *testing.T) maintPaths {
	t.Helper()
	dir := t.TempDir()
	p := maintPaths{
		dir:        dir,
		key:        filepath.Join(dir, "id_ed25519"),
		knownHosts: filepath.Join(dir, "known_hosts"),
		secretID:   filepath.Join(dir, "openbao-secret-id"),
	}
	writeFileOrFail(t, p.key, opensshKey("none"), 0o600)
	writeFileOrFail(t, p.knownHosts, []byte("prox-qual-2201-cit ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample\n"), 0o644)
	// A secret written by a shell redirection carries a trailing newline.
	writeFileOrFail(t, p.secretID, []byte(maintSecretID+"\n"), 0o600)
	return p
}

func writeFileOrFail(t *testing.T, path string, data []byte, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	// WriteFile honours the umask, and a test that means 0600 must get 0600.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

// opensshKey builds an OPENSSH PRIVATE KEY block whose cipher field says
// cipher. The loader reads exactly one field of the body -- the cipher name --
// so the fixture carries exactly that: generating a real key would need the
// SSH library this revision deliberately does not have.
func opensshKey(cipher string) []byte {
	body := []byte(opensshMagic)
	body = appendSSHString(body, cipher)
	body = appendSSHString(body, "none") // kdfname
	body = appendSSHString(body, "")     // kdfoptions
	return pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: body})
}

// appendSSHString appends a length-prefixed string, the one encoding of the
// OpenSSH key format this package needs to know about.
func appendSSHString(dst []byte, s string) []byte {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(s)))
	dst = append(dst, length[:]...)
	return append(dst, s...)
}

// legacyEncryptedKey is the pre-OpenSSH format, which says it is encrypted in
// a PEM header rather than in its body.
func legacyEncryptedKey() []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY",
		Headers: map[string]string{
			"Proc-Type": "4,ENCRYPTED",
			"DEK-Info":  "AES-128-CBC,0123456789ABCDEF0123456789ABCDEF",
		},
		Bytes: []byte("not a key, and never read: the header is the whole point"),
	})
}

// sshBlock is the transport half, identical in both modes.
func sshBlock(p maintPaths) map[string]any {
	return map[string]any{"knownHostsFile": p.knownHosts}
}

// sshKeyBlock and openBaoBlock are the two shapes a valid file may take. They
// are rebuilt for every case so that a mutation cannot reach the next one.
func sshKeyBlock(p maintPaths) map[string]any {
	return map[string]any{
		"mode":   "ssh-key",
		"sshKey": map[string]any{"keyFile": p.key},
		"ssh":    sshBlock(p),
	}
}

func openBaoBlock(p maintPaths) map[string]any {
	return map[string]any{
		"mode": "openbao",
		"openbao": map[string]any{
			"address":      "https://bao.example.net:8200",
			"roleId":       "role-id-of-the-deployment",
			"secretIdFile": p.secretID,
			"sshRole":      "moxy-maintenance",
		},
		"ssh": sshBlock(p),
	}
}

// maintDoc is a valid configuration whose auth is enabled -- maintenance
// without authentication is refused, and every case below is about something
// else.
func maintDoc(maintenance, clusterMaintenance map[string]any) map[string]any {
	document := doc(baseCluster())
	document["auth"] = map[string]any{"mode": "proxy-header", "trustedProxies": []any{"10.0.0.0/24"}}
	if maintenance != nil {
		document["maintenance"] = maintenance
	}
	if clusterMaintenance != nil {
		document["clusters"].([]any)[0].(map[string]any)["maintenance"] = clusterMaintenance
	}
	return document
}

// nested walks into a block the way a case mutates one, and fails rather than
// panics when a fixture has drifted.
func nested(t *testing.T, block map[string]any, key string) map[string]any {
	t.Helper()
	inner, ok := block[key].(map[string]any)
	if !ok {
		t.Fatalf("fixture has no %q block", key)
	}
	return inner
}

// TestLoadValidatesTheMaintenanceBlock walks every way of writing the block
// that would make the file say something it does not apply, or promise a
// session moxy could not open.
func TestLoadValidatesTheMaintenanceBlock(t *testing.T) {
	cases := map[string]struct {
		base   func(maintPaths) map[string]any
		mutate func(*testing.T, map[string]any, maintPaths)
		want   string // "" means the file is accepted
	}{
		"ssh-key mode": {base: sshKeyBlock},
		"openbao mode": {base: openBaoBlock},
		"no mode at all": {base: sshKeyBlock, want: "mode is required",
			mutate: func(_ *testing.T, m map[string]any, _ maintPaths) { delete(m, "mode") }},
		"unknown mode": {base: sshKeyBlock, want: `mode "ssh-agent" is unknown`,
			mutate: func(_ *testing.T, m map[string]any, _ maintPaths) { m["mode"] = "ssh-agent" }},
		// A block that does nothing in the chosen mode makes a file read as
		// though it applied something it does not.
		"openbao block in ssh-key mode": {base: sshKeyBlock, want: `openbao is only used in "openbao" mode`,
			mutate: func(_ *testing.T, m map[string]any, p maintPaths) { m["openbao"] = openBaoBlock(p)["openbao"] }},
		"sshKey block in openbao mode": {base: openBaoBlock, want: `sshKey is only used in "ssh-key" mode`,
			mutate: func(_ *testing.T, m map[string]any, p maintPaths) { m["sshKey"] = sshKeyBlock(p)["sshKey"] }},
		"ssh-key mode without its block": {base: sshKeyBlock, want: "needs the sshKey block",
			mutate: func(_ *testing.T, m map[string]any, _ maintPaths) { delete(m, "sshKey") }},
		"openbao mode without its block": {base: openBaoBlock, want: "needs the openbao block",
			mutate: func(_ *testing.T, m map[string]any, _ maintPaths) { delete(m, "openbao") }},
		"ssh-key mode without keyFile": {base: sshKeyBlock, want: "keyFile is required",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { delete(nested(t, m, "sshKey"), "keyFile") }},

		// Host keys: required in both modes, and there is no way to opt out.
		"without knownHostsFile": {base: sshKeyBlock, want: "knownHostsFile is required",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { delete(nested(t, m, "ssh"), "knownHostsFile") }},
		"openbao mode without knownHostsFile": {base: openBaoBlock, want: "knownHostsFile is required",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { delete(nested(t, m, "ssh"), "knownHostsFile") }},
		"knownHostsFile that is not there": {base: sshKeyBlock, want: "knownHostsFile:",
			mutate: func(t *testing.T, m map[string]any, p maintPaths) {
				nested(t, m, "ssh")["knownHostsFile"] = filepath.Join(p.dir, "absent")
			}},
		"knownHostsFile that is a directory": {base: sshKeyBlock, want: "not a regular file",
			mutate: func(t *testing.T, m map[string]any, p maintPaths) { nested(t, m, "ssh")["knownHostsFile"] = p.dir }},

		// The transport budgets and the account.
		"port out of range": {base: sshKeyBlock, want: "port 70000 is out of range",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { nested(t, m, "ssh")["port"] = 70000 }},
		"negative port": {base: sshKeyBlock, want: "out of range",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { nested(t, m, "ssh")["port"] = -1 }},
		"timeout above the maximum": {base: sshKeyBlock, want: "above the 1m0s maximum",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { nested(t, m, "ssh")["timeout"] = "5m" }},
		"timeout that is not a duration": {base: sshKeyBlock, want: "is malformed",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { nested(t, m, "ssh")["timeout"] = "20 seconds" }},
		"negative timeout": {base: sshKeyBlock, want: "must be positive",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { nested(t, m, "ssh")["timeout"] = "-1s" }},
		"connectTimeout above timeout": {base: sshKeyBlock, want: "connectTimeout 30s is above timeout 10s",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) {
				ssh := nested(t, m, "ssh")
				ssh["timeout"] = "10s"
				ssh["connectTimeout"] = "30s"
			}},
		"user that is not a login name": {base: sshKeyBlock, want: `user "Moxy Admin" must match`,
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { nested(t, m, "ssh")["user"] = "Moxy Admin" }},

		// OpenBao: the shape is checked, the reachability is not.
		"openbao without https": {base: openBaoBlock, want: "must use https",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) {
				nested(t, m, "openbao")["address"] = "http://bao.example.net:8200"
			}},
		"openbao address with a path": {base: openBaoBlock, want: "must not have a path",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) {
				nested(t, m, "openbao")["address"] = "https://bao.example.net:8200/v1"
			}},
		"openbao without an address": {base: openBaoBlock, want: "address is required",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { delete(nested(t, m, "openbao"), "address") }},
		"openbao without roleId": {base: openBaoBlock, want: "roleId is required",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { delete(nested(t, m, "openbao"), "roleId") }},
		"openbao without sshRole": {base: openBaoBlock, want: "sshRole is required",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { delete(nested(t, m, "openbao"), "sshRole") }},
		"openbao with a role that is a path": {base: openBaoBlock, want: "must match",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) {
				nested(t, m, "openbao")["sshRole"] = "../../sys/mounts"
			}},
		"openbao without secretIdFile": {base: openBaoBlock, want: "secretIdFile is required",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) { delete(nested(t, m, "openbao"), "secretIdFile") }},
		"openbao with a secretIdFile that is not there": {base: openBaoBlock, want: "secretIdFile:",
			mutate: func(t *testing.T, m map[string]any, p maintPaths) {
				nested(t, m, "openbao")["secretIdFile"] = filepath.Join(p.dir, "absent")
			}},
		"openbao with an empty secretIdFile": {base: openBaoBlock, want: "is empty",
			mutate: func(t *testing.T, m map[string]any, p maintPaths) {
				empty := filepath.Join(p.dir, "empty-secret-id")
				writeFileOrFail(t, empty, []byte("\n  \n"), 0o600)
				nested(t, m, "openbao")["secretIdFile"] = empty
			}},
		// tls.mode "insecure" loosens the reading of a measurement; here it
		// would loosen the delivery of what opens a privileged session.
		"openbao over an unverified certificate": {base: openBaoBlock, want: "not available here",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) {
				nested(t, m, "openbao")["tls"] = map[string]any{"mode": "insecure"}
			}},
		"openbao pinned without a caFile": {base: openBaoBlock, want: "caFile is required",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) {
				nested(t, m, "openbao")["tls"] = map[string]any{"mode": "pinned"}
			}},
		"openbao with an unknown tls mode": {base: openBaoBlock, want: "is unknown",
			mutate: func(t *testing.T, m map[string]any, _ maintPaths) {
				nested(t, m, "openbao")["tls"] = map[string]any{"mode": "tofu"}
			}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(secretEnv, sentinel)
			paths := writeMaintFiles(t)
			block := tc.base(paths)
			if tc.mutate != nil {
				tc.mutate(t, block, paths)
			}

			_, err := Load(writeConfig(t, maintDoc(block, nil)))
			if tc.want == "" {
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
		})
	}
}

// TestLoadAppliesMaintenanceDefaults: a file that says only what it has to say
// still describes a complete transport, and the defaults are the ones deploy/
// sets up.
func TestLoadAppliesMaintenanceDefaults(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	paths := writeMaintFiles(t)

	cfg, err := Load(writeConfig(t, maintDoc(openBaoBlock(paths), nil)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	m := cfg.Maintenance
	if m == nil {
		t.Fatal("maintenance block is nil")
	}
	if m.SSH.User != DefaultMaintenanceUser {
		t.Errorf("ssh.user = %q, want %q", m.SSH.User, DefaultMaintenanceUser)
	}
	if m.SSH.Port != DefaultMaintenancePort {
		t.Errorf("ssh.port = %d, want %d", m.SSH.Port, DefaultMaintenancePort)
	}
	if m.SSH.DialTimeout != DefaultMaintenanceConnectTimeout {
		t.Errorf("ssh.connectTimeout = %s, want %s", m.SSH.DialTimeout, DefaultMaintenanceConnectTimeout)
	}
	if m.SSH.RunTimeout != DefaultMaintenanceTimeout {
		t.Errorf("ssh.timeout = %s, want %s", m.SSH.RunTimeout, DefaultMaintenanceTimeout)
	}
	if m.OpenBao.MountPath != DefaultOpenBaoMountPath {
		t.Errorf("openbao.mountPath = %q, want %q", m.OpenBao.MountPath, DefaultOpenBaoMountPath)
	}
	if m.OpenBao.RequestTimeout != DefaultOpenBaoTimeout {
		t.Errorf("openbao.timeout = %s, want %s", m.OpenBao.RequestTimeout, DefaultOpenBaoTimeout)
	}
	// The default TLS policy is the one a cluster gets, minus the mode that
	// is refused here.
	if m.OpenBao.TLS.Mode != TLSModeSystem {
		t.Errorf("openbao.tls.mode = %q, want %q", m.OpenBao.TLS.Mode, TLSModeSystem)
	}
	if !m.OpenBao.SecretID.ConstantTimeEqual(maintSecretID) {
		t.Error("the secret_id was not read from the file, or not trimmed")
	}
}

// TestLoadReadsTheMaintenanceBudgets: what the file writes down is what the
// transport gets, without a unit conversion on the way.
func TestLoadReadsTheMaintenanceBudgets(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	paths := writeMaintFiles(t)
	block := sshKeyBlock(paths)
	block["ssh"] = map[string]any{
		"user":           "drainer",
		"port":           2222,
		"knownHostsFile": paths.knownHosts,
		"connectTimeout": "3s",
		"timeout":        "45s",
	}

	cfg, err := Load(writeConfig(t, maintDoc(block, nil)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	ssh := cfg.Maintenance.SSH
	if ssh.User != "drainer" || ssh.Port != 2222 {
		t.Errorf("user = %q, port = %d", ssh.User, ssh.Port)
	}
	if ssh.DialTimeout != 3*time.Second || ssh.RunTimeout != 45*time.Second {
		t.Errorf("connectTimeout = %s, timeout = %s", ssh.DialTimeout, ssh.RunTimeout)
	}
}

// TestLoadChecksThePrivateKey: the permissions and the format are only cheap
// to look at now. Six weeks later the same problem is a maintenance that fails
// on the first click.
func TestLoadChecksThePrivateKey(t *testing.T) {
	cases := map[string]struct {
		content []byte
		perm    os.FileMode
		want    string
	}{
		"unencrypted openssh key": {opensshKey("none"), 0o600, ""},
		"readable by the group":   {opensshKey("none"), 0o640, "reachable beyond its owner"},
		"readable by everyone":    {opensshKey("none"), 0o644, "reachable beyond its owner"},
		"writable by everyone":    {opensshKey("none"), 0o602, "reachable beyond its owner"},
		// No daemon can be asked for a passphrase, in either format.
		"encrypted openssh key": {opensshKey("aes256-ctr"), 0o600, "is an encrypted private key"},
		"legacy encrypted key":  {legacyEncryptedKey(), 0o600, "is an encrypted private key"},
		"a public key": {[]byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample moxy@deployment\n"), 0o600,
			"holds no PEM block"},
		"a certificate instead of a key": {
			pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a key")}), 0o600,
			"want a private key"},
		"an empty file": {nil, 0o600, "holds no PEM block"},
		"an OpenSSH header over something else": {
			pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: []byte("not an openssh key")}), 0o600,
			"is not an OpenSSH private key"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(secretEnv, sentinel)
			paths := writeMaintFiles(t)
			writeFileOrFail(t, paths.key, tc.content, tc.perm)

			cfg, err := Load(writeConfig(t, maintDoc(sshKeyBlock(paths), nil)))
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Load() error = %v, want none", err)
				}
				// The key is in the Secret, and the Secret is what nothing
				// can print.
				if cfg.Maintenance.SSHKey.Key.IsEmpty() {
					t.Error("the key was not read into the Secret")
				}
				return
			}
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestLoadRefusesAKeyOwnedBySomeoneElse: the image runs as uid 65532, so the
// answer is the uid of the process and never a hardcoded one. Changing the
// owner of a file needs the privilege to do so, and the check is skipped
// rather than faked where the test user does not have it.
func TestLoadRefusesAKeyOwnedBySomeoneElse(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	paths := writeMaintFiles(t)
	other := os.Getuid() + 1
	if err := os.Chown(paths.key, other, -1); err != nil {
		t.Skipf("cannot change the owner of a file here: %v", err)
	}

	_, err := Load(writeConfig(t, maintDoc(sshKeyBlock(paths), nil)))
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("owned by uid %d", other)) {
		t.Errorf("error = %v, want it to name the owner", err)
	}
}

// TestLoadResolvesMaintenancePathsAgainstTheConfigurationFile: the image has
// no WORKDIR, so "ssh/known_hosts" next to /etc/moxy/config.json was looked up
// in /ssh -- the lesson tls.caFile already learned.
func TestLoadResolvesMaintenancePathsAgainstTheConfigurationFile(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	paths := writeMaintFiles(t)
	block := sshKeyBlock(paths)
	nested(t, block, "sshKey")["keyFile"] = "id_ed25519"
	nested(t, block, "ssh")["knownHostsFile"] = "known_hosts"

	// The configuration file sits next to the three files, and nothing is
	// written with an absolute path.
	raw, err := json.MarshalIndent(maintDoc(block, nil), "", "  ")
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	path := filepath.Join(paths.dir, "config.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Maintenance.SSHKey.KeyFile; got != paths.key {
		t.Errorf("keyFile = %q, want %q", got, paths.key)
	}
	if got := cfg.Maintenance.SSH.KnownHostsFile; got != paths.knownHosts {
		t.Errorf("knownHostsFile = %q, want %q", got, paths.knownHosts)
	}
}

// TestLoadValidatesTheClusterMaintenanceBlock: what a cluster may say, and the
// one thing it may not -- take part while nothing says how a session is
// opened.
func TestLoadValidatesTheClusterMaintenanceBlock(t *testing.T) {
	cases := map[string]struct {
		global  bool
		cluster map[string]any
		auth    map[string]any
		want    string
	}{
		"enabled with the global block": {global: true, cluster: map[string]any{
			"enabled":      true,
			"allowedUsers": []any{"alice", "bob"},
			"hosts":        map[string]any{"prox-qual-2201-cit": "10.0.0.11"},
		}},
		// Saying so and leaving it off is how a cluster is prepared before it
		// takes part, so it is not an error.
		"disabled without the global block": {global: false, cluster: map[string]any{"enabled": false}},
		"enabled without the global block": {global: false, cluster: map[string]any{"enabled": true},
			want: "needs the process-wide maintenance block"},
		"an empty allowed user": {global: true, cluster: map[string]any{
			"enabled": true, "allowedUsers": []any{"alice", " "},
		}, want: "empty name"},
		"a host pointing at nothing": {global: true, cluster: map[string]any{
			"enabled": true, "hosts": map[string]any{"prox-qual-2201-cit": ""},
		}, want: "must name the address"},
		"an unnamed node": {global: true, cluster: map[string]any{
			"enabled": true, "hosts": map[string]any{"": "10.0.0.11"},
		}, want: "empty node name"},
		// Executing a command on a hypervisor for whoever reaches the port is
		// not a degraded mode, it is a remote shell.
		"global block without authentication": {global: true, auth: map[string]any{"mode": "none"},
			cluster: map[string]any{"enabled": true}, want: "drain a node"},
		"global block with no auth block at all": {global: true, auth: map[string]any{},
			cluster: map[string]any{"enabled": true}, want: "drain a node"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(secretEnv, sentinel)
			paths := writeMaintFiles(t)
			var global map[string]any
			if tc.global {
				global = sshKeyBlock(paths)
			}
			document := maintDoc(global, tc.cluster)
			if tc.auth != nil {
				document["auth"] = tc.auth
			}

			cfg, err := Load(writeConfig(t, document))
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Load() error = %v, want none", err)
				}
				enabled, _ := tc.cluster["enabled"].(bool)
				if got := cfg.Clusters[0].MaintenanceEnabled(); got != enabled {
					t.Errorf("MaintenanceEnabled() = %v, want %v", got, enabled)
				}
				return
			}
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestMaintenanceEnabledOnASilentCluster: a cluster that says nothing does not
// take part, and no caller has to spell that out with a nil check of its own.
func TestMaintenanceEnabledOnASilentCluster(t *testing.T) {
	cl := &Cluster{ID: "qualification"}
	if cl.MaintenanceEnabled() {
		t.Error("a cluster with no maintenance block takes part")
	}
}

// TestLoadAccumulatesMaintenanceErrors: one run reports every problem of the
// file, the way the rest of the loader does. Reporting them one restart at a
// time is how a deployment spends an afternoon on four typos.
func TestLoadAccumulatesMaintenanceErrors(t *testing.T) {
	t.Setenv(secretEnv, sentinel)
	paths := writeMaintFiles(t)
	block := sshKeyBlock(paths)
	block["openbao"] = openBaoBlock(paths)["openbao"]
	nested(t, block, "ssh")["port"] = 0xdead_beef
	delete(nested(t, block, "ssh"), "knownHostsFile")

	_, err := Load(writeConfig(t, maintDoc(block, map[string]any{"enabled": true, "allowedUsers": []any{""}})))
	if err == nil {
		t.Fatal("want an error")
	}
	var errs ValidationErrors
	if !as(err, &errs) {
		t.Fatalf("error = %T, want ValidationErrors", err)
	}
	for _, want := range []string{"only used", "out of range", "knownHostsFile is required", "empty name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
}

// as is errors.As with the unwrapping Load actually needs: the loader wraps
// its ValidationErrors in a fmt.Errorf naming the file.
func as(err error, target *ValidationErrors) bool {
	for err != nil {
		if errs, ok := err.(ValidationErrors); ok {
			*target = errs
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// TestNewMaintenanceRefusesWhatLoadWouldRefuse: the constructors the tests of
// other packages use must not be a way around the loader's rules -- every one
// of them is a rule about a file on disk that a hand-built struct would skip.
func TestNewMaintenanceRefusesWhatLoadWouldRefuse(t *testing.T) {
	paths := writeMaintFiles(t)
	good := MaintenanceSSH{KnownHostsFile: paths.knownHosts}

	m, err := NewSSHKeyMaintenance(paths.key, good)
	if err != nil {
		t.Fatalf("NewSSHKeyMaintenance: %v", err)
	}
	if m.Mode != MaintenanceModeSSHKey || m.SSHKey.Key.IsEmpty() || m.OpenBao != nil {
		t.Errorf("mode = %q, key empty = %v", m.Mode, m.SSHKey.Key.IsEmpty())
	}
	if m.SSH.User != DefaultMaintenanceUser || m.SSH.Port != DefaultMaintenancePort {
		t.Errorf("the constructor skipped the defaults: user = %q, port = %d", m.SSH.User, m.SSH.Port)
	}

	source := OpenBaoSource{
		Address:      "https://bao.example.net:8200",
		RoleID:       "role-id-of-the-deployment",
		SecretIDFile: paths.secretID,
		SSHRole:      "moxy-maintenance",
	}
	bao, err := NewOpenBaoMaintenance(source, good)
	if err != nil {
		t.Fatalf("NewOpenBaoMaintenance: %v", err)
	}
	if bao.Mode != MaintenanceModeOpenBao || bao.SSHKey != nil || bao.OpenBao.SecretID.IsEmpty() {
		t.Errorf("mode = %q, sshKey = %v", bao.Mode, bao.SSHKey)
	}

	// Everything the loader refuses, refused here too.
	unreadable := filepath.Join(paths.dir, "absent")
	if _, err := NewSSHKeyMaintenance(unreadable, good); err == nil {
		t.Error("NewSSHKeyMaintenance accepted a key that is not there")
	}
	if _, err := NewSSHKeyMaintenance(paths.key, MaintenanceSSH{}); err == nil {
		t.Error("NewSSHKeyMaintenance accepted a transport without known hosts")
	}
	if _, err := NewSSHKeyMaintenance(paths.key, MaintenanceSSH{KnownHostsFile: paths.knownHosts, Timeout: "5m"}); err == nil {
		t.Error("NewSSHKeyMaintenance accepted a budget above the maximum")
	}
	encrypted := filepath.Join(paths.dir, "encrypted")
	writeFileOrFail(t, encrypted, opensshKey("aes256-ctr"), 0o600)
	if _, err := NewSSHKeyMaintenance(encrypted, good); err == nil {
		t.Error("NewSSHKeyMaintenance accepted an encrypted key")
	}
	noRole := source
	noRole.SSHRole = ""
	if _, err := NewOpenBaoMaintenance(noRole, good); err == nil {
		t.Error("NewOpenBaoMaintenance accepted a source without a role")
	}
	insecure := source
	insecure.TLS = TLS{Mode: TLSModeInsecure}
	if _, err := NewOpenBaoMaintenance(insecure, good); err == nil {
		t.Error("NewOpenBaoMaintenance accepted an unverified certificate")
	}
}

// TestMaintenanceNeverPrintsItsSecrets is the non-regression test on
// redaction: the private key and the secret_id are Secrets, and a Secret has
// to survive every way a Go program turns a value into text -- including the
// ones that never consult a Stringer.
func TestMaintenanceNeverPrintsItsSecrets(t *testing.T) {
	paths := writeMaintFiles(t)
	keyPEM := opensshKey("none")
	// The base64 body, so that half a leak is caught as well as a whole one.
	keyBody := strings.Split(strings.TrimSpace(string(keyPEM)), "\n")[1]

	m, err := NewSSHKeyMaintenance(paths.key, MaintenanceSSH{KnownHostsFile: paths.knownHosts})
	if err != nil {
		t.Fatalf("NewSSHKeyMaintenance: %v", err)
	}
	bao, err := NewOpenBaoMaintenance(OpenBaoSource{
		Address:      "https://bao.example.net:8200",
		RoleID:       "role-id-of-the-deployment",
		SecretIDFile: paths.secretID,
		SSHRole:      "moxy-maintenance",
	}, MaintenanceSSH{KnownHostsFile: paths.knownHosts})
	if err != nil {
		t.Fatalf("NewOpenBaoMaintenance: %v", err)
	}

	printed := []string{
		fmt.Sprint(m), fmt.Sprintf("%v", m), fmt.Sprintf("%+v", m), fmt.Sprintf("%#v", m),
		fmt.Sprint(*m.SSHKey), fmt.Sprintf("%+v", *m.SSHKey), fmt.Sprintf("%#v", *m.SSHKey),
		fmt.Sprint(m.SSHKey.Key), fmt.Sprintf("%#v", m.SSHKey.Key),
		// The verbs fmt answers without ever consulting a Stringer.
		fmt.Sprintf("%d", m.SSHKey.Key), fmt.Sprintf("%x", m.SSHKey.Key), fmt.Sprintf("%q", m.SSHKey.Key),
		m.SSHKey.Key.String(), m.SSHKey.Key.GoString(),
		fmt.Sprint(bao), fmt.Sprintf("%+v", bao), fmt.Sprintf("%#v", bao),
		fmt.Sprint(*bao.OpenBao), fmt.Sprintf("%+v", *bao.OpenBao), fmt.Sprintf("%#v", *bao.OpenBao),
		fmt.Sprintf("%d", bao.OpenBao.SecretID), fmt.Sprintf("%x", bao.OpenBao.SecretID),
	}
	for _, v := range []any{m, m.SSHKey, m.SSHKey.Key, bao, bao.OpenBao, bao.OpenBao.SecretID} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		printed = append(printed, string(raw))
	}

	for i, s := range printed {
		for _, secret := range []string{string(keyPEM), keyBody, maintSecretID} {
			if strings.Contains(s, secret) {
				t.Errorf("rendering %d spells a secret out: %s", i, s)
			}
		}
	}
}

// TestLoadMatchesAllowedUsersToTheAuthenticationMode is the pair of rules that
// keeps the list meaning the same thing as the mode can prove.
//
// They are opposites, and both fail closed. In "proxy-header" the list is what
// authorizes, so an empty one names nobody and an enabled cluster with an
// empty list is a switch wired to nothing. In "token" nothing can be compared
// against a name at all -- one shared secret, one caller, no identity -- so a
// list of names is a setting the daemon could never apply.
func TestLoadMatchesAllowedUsersToTheAuthenticationMode(t *testing.T) {
	cases := map[string]struct {
		auth    map[string]any
		cluster map[string]any
		want    string
	}{
		"proxy-header names who may drain": {
			auth:    map[string]any{"mode": "proxy-header", "trustedProxies": []any{"10.0.0.0/24"}},
			cluster: map[string]any{"enabled": true, "allowedUsers": []any{"alice"}},
		},
		"proxy-header with an empty list": {
			auth:    map[string]any{"mode": "proxy-header", "trustedProxies": []any{"10.0.0.0/24"}},
			cluster: map[string]any{"enabled": true},
			want:    "an empty list names nobody",
		},
		// Not enabled is not a switch wired to nothing: it is a cluster being
		// prepared, and it authorizes no one either way.
		"proxy-header with an empty list on a cluster that is off": {
			auth:    map[string]any{"mode": "proxy-header", "trustedProxies": []any{"10.0.0.0/24"}},
			cluster: map[string]any{"enabled": false},
		},
		"token authorizes without naming anyone": {
			auth:    map[string]any{"mode": "token", "tokenEnv": uiTokenEnv},
			cluster: map[string]any{"enabled": true},
		},
		"token with a list of names": {
			auth:    map[string]any{"mode": "token", "tokenEnv": uiTokenEnv},
			cluster: map[string]any{"enabled": true, "allowedUsers": []any{"alice"}},
			want:    "identifies nobody",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(secretEnv, sentinel)
			t.Setenv(uiTokenEnv, uiToken)
			paths := writeMaintFiles(t)
			document := maintDoc(sshKeyBlock(paths), tc.cluster)
			document["auth"] = tc.auth

			_, err := Load(writeConfig(t, document))
			if tc.want == "" {
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
		})
	}
}
