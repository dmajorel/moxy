package main

import (
	"fmt"

	"github.com/dmajorel/moxy/apps/api/internal/config"
	"github.com/dmajorel/moxy/apps/api/internal/maintenance"
)

// This file turns the maintenance block of the configuration into the executor
// POST .../maintenance runs through, and refuses to start when this build
// cannot honour what the file asks for. ADR 0010 is the decision behind it.

// newMaintenance builds the executor, or nil when nothing is configured.
//
// A nil executor is a perfectly good state to run in, and the one the whole
// estate was in before ADR 0010: every cluster answers 404 on the execution
// route and the UI shows no button at all -- never a disabled one.
func newMaintenance(cfg *config.Config) (*maintenance.Service, error) {
	if cfg == nil || cfg.Maintenance == nil {
		return nil, nil
	}
	// Built before the session halves are asked for, so that what the file
	// says is turned into the shape the service takes whether or not this
	// build can open a session with it.
	opts := maintenanceOptions(cfg)

	keys, runner, err := newMaintenanceSession(cfg.Maintenance)
	// The check is written for the build that carries a transport, where this
	// error is the exceptional branch. In THIS build it is the only branch, so
	// staticcheck rightly observes the comparison cannot be false -- and the
	// day the transport lands, this directive stops matching and staticcheck
	// says so, which is the moment to delete it.
	//lint:ignore SA4023 no transport is compiled in yet; see newMaintenanceSession
	if err != nil {
		return nil, err
	}
	return maintenance.NewService(opts, keys, runner), nil
}

// newMaintenanceSession builds the two halves one execution needs: the source
// of the credential, and the SSH transport that opens a session with it.
//
// THERE IS NO SSH TRANSPORT IN THIS BUILD, so this fails, and it fails at
// START-UP rather than at the first click. A maintenance block that loaded
// cleanly and then did nothing would make the file read as though it applied
// something it does not -- the rule the loader already enforces on a mode's
// unused block -- and the first anyone would hear of it is an operator
// clicking on the night they need to drain a node.
//
// It is ONE function and not two because both halves are missing for the same
// reason, and in the same order: the Go standard library has no SSH client, so
// golang.org/x/crypto/ssh has to be vendored first (ADR 0010, ADR 0001); and
// until something can open a session, there is nothing for a private key to be
// read out of internal/config for. Secret.Reveal() is not that way out either
// -- it belongs to the PVE authentication transport, and the SSH key is
// another secret with a bearer of its own.
//
// This is the one seam the transport lands in.
func newMaintenanceSession(m *config.Maintenance) (maintenance.KeyProvider, maintenance.Runner, error) {
	return nil, nil, fmt.Errorf("maintenance is configured in %q mode but this build carries no ssh transport, "+
		"so no node could be drained: remove the maintenance block to start without it, "+
		"or run a moxyd built with one (see ADR 0010)", m.Mode)
}

// maintenanceOptions maps the configuration onto what the service is built
// with.
//
// THE SHAPE IS THE POINT, and it is the one the configuration already makes
// impossible to get wrong: the session settings and the key source are
// process-wide, and only enabled, hosts and the allowed users are per cluster.
// Nothing here can express a mode per cluster, because nothing it reads from
// carries one.
func maintenanceOptions(cfg *config.Config) maintenance.Options {
	clusters := make(map[string]maintenance.ClusterOptions, len(cfg.Clusters))
	for _, cl := range cfg.Clusters {
		if cl.Maintenance == nil {
			continue
		}
		clusters[cl.ID] = maintenance.ClusterOptions{
			Enabled: cl.Maintenance.Enabled,
			Hosts:   cl.Maintenance.Hosts,
		}
	}
	ssh := cfg.Maintenance.SSH
	return maintenance.Options{
		SSH: maintenance.SSHOptions{
			User:        ssh.User,
			Port:        ssh.Port,
			DialTimeout: ssh.DialTimeout,
			RunTimeout:  ssh.RunTimeout,
		},
		Clusters: clusters,
	}
}

// maintenanceUsers is who may drain a node, per cluster, as the HTTP layer
// reads it.
//
// ONLY THE CLUSTERS THAT TAKE PART are listed, and that is load-bearing twice
// over: a cluster missing from the map authorizes no caller the proxy named,
// and its id can never become a metric label. See server.Options.
func maintenanceUsers(cfg *config.Config) map[string][]string {
	users := make(map[string][]string)
	for _, cl := range cfg.Clusters {
		if !cl.MaintenanceEnabled() {
			continue
		}
		users[cl.ID] = cl.Maintenance.AllowedUsers
	}
	if len(users) == 0 {
		return nil
	}
	return users
}
