package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/meshploy/packages/hostagent"
)

// The edge's firewall check is the one its install recorded, carried in the
// facts its host agent reports.
func TestTheEdgesFirewallComesFromItsFacts(t *testing.T) {
	dir := t.TempDir()
	if s, at := edgeFirewall(hostagent.Local(dir)); s != "" || at != "" {
		t.Fatalf("before any facts: %q %q", s, at)
	}
	os.MkdirAll(filepath.Join(dir, "state"), 0o755)
	os.WriteFile(filepath.Join(dir, "state", hostagent.FactsFile),
		[]byte(`{"firewall_state":"none","firewall_checked_at":"2026-11-09T01:00:00Z","checked_at":"2026-11-09T01:00:00Z"}`), 0o644)
	if s, at := edgeFirewall(hostagent.Local(dir)); s != "none" || at != "2026-11-09T01:00:00Z" {
		t.Fatalf("from the facts: %q %q", s, at)
	}
	for _, p := range edgePublishedPorts() {
		if p.Port == 5000 || p.Port == 4000 || p.Port == 9100 {
			t.Errorf("port %d is on the mesh alone or not on the edge", p.Port)
		}
	}
}
