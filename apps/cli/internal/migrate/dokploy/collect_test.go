package dokploy

import (
	"testing"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
)

// Detect reports the edge from the same reading the plan does: Dokploy's
// Traefik publishing in host mode is held by docker-proxy, and only the holder
// the host found says it is Dokploy's.
func TestDetectKeepsTheEdgesHolder(t *testing.T) {
	src := Source{
		Rows:       map[string][]Row{"project": {{"projectId": "p1"}}},
		Listeners:  []migrate.Listener{{Port: 80, Process: "docker-proxy"}, {Port: 443, Process: "docker-proxy"}},
		EdgeHolder: migrate.PortHolder{Kind: "swarm", Name: "dokploy-traefik"},
	}
	m := src.Machine()
	if m.Rows != nil {
		t.Errorf("detect should not carry Dokploy's rows")
	}
	if got := BuildPlan(m, time.Now()).Edge.Kind; got != "traefik-service" {
		t.Errorf("edge = %q, want traefik-service", got)
	}
}
