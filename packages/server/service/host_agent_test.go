package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
)

func writeHostReports(t *testing.T, agent hostagent.Agent, fw hostagent.Firewall) string {
	t.Helper()
	dir := t.TempDir()
	state := hostagent.StateDir(dir)
	if err := os.MkdirAll(state, 0755); err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]any{hostagent.AgentFile: agent, hostagent.FirewallFile: fw} {
		b, _ := json.Marshal(v)
		if err := os.WriteFile(filepath.Join(state, name), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// pnath-5-rt's case: ufw denies by default and allows the web ports, so a
// route on 10000 is blocked and one on 443 would not be.
func TestTCPRoutesCarryTheHostFirewallVerdict(t *testing.T) {
	now := time.Now().UTC()
	dir := writeHostReports(t,
		hostagent.Agent{Version: "0.16.0", HeartbeatAt: now},
		hostagent.Firewall{Tool: hostagent.ToolUFW, Active: true, DefaultIncoming: "deny", CheckedAt: now, Rules: []hostagent.Rule{
			{Ports: "443", Proto: "tcp", Action: "allow", From: "any"},
			{Ports: "5432", Proto: "tcp", Action: "allow", From: "203.0.113.0/24"},
		}},
	)
	routes := []db.TCPRoute{{GatewayPort: 10000}, {GatewayPort: 443}, {GatewayPort: 5432}}
	(&TCPRouteService{hostDir: dir}).withHostFirewall(routes)

	for i, want := range []string{hostagent.PortBlocked, hostagent.PortOpen, hostagent.PortRestricted} {
		got := routes[i].HostFirewall
		if got == nil || got.State != want || got.Tool != hostagent.ToolUFW {
			t.Errorf("port %d: %+v, want %s", routes[i].GatewayPort, got, want)
		}
	}
	if s := routes[2].HostFirewall.Sources; len(s) != 1 || s[0] != "203.0.113.0/24" {
		t.Errorf("sources = %v", s)
	}
}

// A gateway without the agent must not guess.
func TestTCPRoutesWithoutAnAgentAreUnknown(t *testing.T) {
	routes := []db.TCPRoute{{GatewayPort: 10000}}
	(&TCPRouteService{hostDir: t.TempDir()}).withHostFirewall(routes)
	if got := routes[0].HostFirewall; got == nil || got.State != hostagent.PortUnknown || got.Reason == "" {
		t.Errorf("got %+v, want unknown with a reason", got)
	}
}
