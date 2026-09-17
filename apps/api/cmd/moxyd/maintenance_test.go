package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/config"
)

// maintenanceConfig is a configuration whose shape is the one newMaintenance
// reads. It is built as a struct rather than loaded from a file on purpose:
// what is under test here is the wiring, and the rules about key files and
// known_hosts are internal/config's own and tested there.
func maintenanceConfig(block *config.Maintenance) *config.Config {
	return &config.Config{
		Maintenance: block,
		Clusters: []config.Cluster{
			{ID: "qualification", Maintenance: &config.ClusterMaintenance{
				Enabled:      true,
				AllowedUsers: []string{"alice", "bob"},
				Hosts:        map[string]string{"prox-qual-2201-cit": "10.0.0.11"},
			}},
			{ID: "preproduction", Maintenance: &config.ClusterMaintenance{Enabled: false}},
			{ID: "production"},
		},
	}
}

func sshKeyBlock() *config.Maintenance {
	return &config.Maintenance{
		Mode:   config.MaintenanceModeSSHKey,
		SSHKey: &config.SSHKeySource{KeyFile: "/etc/moxy/ssh/id_ed25519"},
		SSH: config.MaintenanceSSH{
			User:        "moxy",
			Port:        2222,
			DialTimeout: 3 * time.Second,
			RunTimeout:  25 * time.Second,
		},
	}
}

// TestNewMaintenanceWithoutABlockIsNotAFailure: no maintenance block is a
// perfectly good way to run, and the state the whole estate was in before ADR
// 0010 -- every cluster answers 404 and the UI shows no button.
func TestNewMaintenanceWithoutABlockIsNotAFailure(t *testing.T) {
	executor, err := newMaintenance(maintenanceConfig(nil))
	if err != nil {
		t.Fatalf("newMaintenance: %v", err)
	}
	if executor != nil {
		t.Error("an executor was built for a configuration that asks for none")
	}
}

// TestNewMaintenanceRefusesToStartWithoutATransport is the whole point of the
// seam: a maintenance block that loaded cleanly and then did nothing would
// make the file read as though it applied something it does not, and the first
// anyone would hear of it is an operator clicking on the night they need to
// drain a node.
func TestNewMaintenanceRefusesToStartWithoutATransport(t *testing.T) {
	executor, err := newMaintenance(maintenanceConfig(sshKeyBlock()))
	if err == nil {
		t.Fatal("newMaintenance built an executor with no ssh transport")
	}
	if executor != nil {
		t.Error("an executor was returned alongside the error")
	}
	for _, want := range []string{"ssh transport", config.MaintenanceModeSSHKey, "ADR 0010"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}
}

// TestMaintenanceOptionsCarryTheProcessSettingsAndTheClusters: the session
// settings are process-wide and only enabled and hosts are per cluster. A
// mapping that put either on the wrong side would be the mixed estate the
// configuration cannot even express.
func TestMaintenanceOptionsCarryTheProcessSettingsAndTheClusters(t *testing.T) {
	opts := maintenanceOptions(maintenanceConfig(sshKeyBlock()))

	if opts.SSH.User != "moxy" || opts.SSH.Port != 2222 {
		t.Errorf("ssh = %+v, want the configured account and port", opts.SSH)
	}
	if opts.SSH.DialTimeout != 3*time.Second || opts.SSH.RunTimeout != 25*time.Second {
		t.Errorf("budgets = %v/%v, want the configured ones", opts.SSH.DialTimeout, opts.SSH.RunTimeout)
	}
	qual, ok := opts.Clusters["qualification"]
	if !ok || !qual.Enabled {
		t.Fatalf("qualification = %+v, want it enabled", qual)
	}
	if qual.Hosts["prox-qual-2201-cit"] != "10.0.0.11" {
		t.Errorf("hosts = %v, want the configured address", qual.Hosts)
	}
	// Present and off, which is a cluster being prepared, not one missing.
	if pprd, ok := opts.Clusters["preproduction"]; !ok || pprd.Enabled {
		t.Errorf("preproduction = %+v, want it present and disabled", pprd)
	}
	if _, ok := opts.Clusters["production"]; ok {
		t.Error("a cluster with no maintenance block reached the executor")
	}
}

// TestMaintenanceUsersListsOnlyTheClustersThatTakePart: the map is read twice
// over -- it authorizes, and it is the closed set of cluster ids the HTTP
// layer will let become a metric label -- so a cluster that does not take part
// must not appear in it.
func TestMaintenanceUsersListsOnlyTheClustersThatTakePart(t *testing.T) {
	users := maintenanceUsers(maintenanceConfig(sshKeyBlock()))

	if got := users["qualification"]; len(got) != 2 || got[0] != "alice" || got[1] != "bob" {
		t.Errorf("qualification = %v, want the configured names", got)
	}
	for _, absent := range []string{"preproduction", "production"} {
		if _, ok := users[absent]; ok {
			t.Errorf("%s is listed, but it does not take part", absent)
		}
	}
}

// TestMaintenanceUsersIsNilWithoutAParticipatingCluster: nil rather than an
// empty map, so that the server option says "nobody takes part" rather than
// "an empty estate takes part".
func TestMaintenanceUsersIsNilWithoutAParticipatingCluster(t *testing.T) {
	cfg := &config.Config{Clusters: []config.Cluster{{ID: "production"}}}
	if users := maintenanceUsers(cfg); users != nil {
		t.Errorf("users = %v, want nil", users)
	}
}
