package proxmox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The shape a real PVE 9 cluster returns: node_status sits inside
// manager_status and there is no flat field at all.
//
// The handoff document assumed the flat form, and the fixture used elsewhere
// carries both, which would have hidden a regression here. This test pins the
// shape observed in production.
func TestHAManagerStatusNestedOnly(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "ha_manager_status_nested.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var envelope struct {
		Data HAManagerStatus `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	status := envelope.Data

	if len(status.NodeStatus) != 6 {
		t.Fatalf("node_status = %v, want 6 entries", status.NodeStatus)
	}
	if got := status.NodeState("prox-qual-2203-cit"); got != HANodeMaintenance {
		t.Errorf("drained node = %q, want %q", got, HANodeMaintenance)
	}
	if got := status.NodeState("prox-qual-2201-cit"); got != HANodeOnline {
		t.Errorf("healthy node = %q, want %q", got, HANodeOnline)
	}
	// A node the manager does not know about must not read as online.
	if got := status.NodeState("prox-qual-9999-cit"); got == HANodeOnline {
		t.Errorf("unknown node = %q, must not be online", got)
	}
}
