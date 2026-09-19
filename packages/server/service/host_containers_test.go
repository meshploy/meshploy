package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/meshploy/packages/hostagent"
	"github.com/meshploy/packages/server/config"
)

func writeDockerReport(t *testing.T, d hostagent.Docker) *SystemService {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(hostagent.StateDir(dir), 0755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(d)
	if err := os.WriteFile(filepath.Join(hostagent.StateDir(dir), hostagent.DockerFile), b, 0644); err != nil {
		t.Fatal(err)
	}
	return &SystemService{cfg: &config.Config{HostDir: dir}}
}

// pnath-6-rt's shape: a compose project, a container on the host's network, and
// Meshploy's own alongside them.
func TestHostContainersGroupsAndHidesOurOwn(t *testing.T) {
	now := time.Now().UTC()
	s := writeDockerReport(t, hostagent.Docker{
		Runtime: "docker", Version: "27.3.1", CheckedAt: now, StatsAt: now,
		Containers: []hostagent.Container{
			{ID: "1", Name: "meshploy-api-1", Image: "ghcr.io/meshploy/api:latest", State: "running", Project: "meshploy"},
			{ID: "2", Name: "caddy", Image: "caddy:2", State: "running", NetworkMode: "host"},
			{ID: "3", Name: "hs-headscale-1", Image: "headscale/headscale:0.23", State: "running", Project: "hs", Service: "headscale"},
			{ID: "4", Name: "hs-db-1", Image: "postgres:16", State: "exited", Project: "hs", Service: "db"},
		},
	})

	got := s.HostContainers()
	if !got.Available || got.Runtime != "docker" || got.Stale {
		t.Fatalf("report = %+v", got)
	}
	if got.Mine != 1 || len(got.Containers) != 3 {
		t.Fatalf("kept %d containers, hid %d", len(got.Containers), got.Mine)
	}
	if c := got.Containers[0]; !c.HostNetwork || c.Kind != containerKindStandalone {
		t.Errorf("caddy = %+v, want a standalone container on the host network", c)
	}
	if len(got.Groups) != 1 {
		t.Fatalf("groups = %+v", got.Groups)
	}
	if g := got.Groups[0]; g.Name != "hs" || g.Count != 2 || g.Running != 1 || g.HostNetwork {
		t.Errorf("group = %+v, want hs with 1 of 2 running", g)
	}
}

// A host whose runtime did not answer is not a host with no containers: the
// console shows nothing rather than an empty list.
func TestHostContainersUnavailableWithoutARuntime(t *testing.T) {
	s := writeDockerReport(t, hostagent.Docker{CheckedAt: time.Now().UTC(), Error: "no container runtime socket found"})
	got := s.HostContainers()
	if got.Available || got.Error == "" {
		t.Errorf("got %+v, want unavailable with a reason", got)
	}
}

// An agent that stopped reporting leaves a report that is still readable, and
// the console says how old it is rather than pretending it is current.
func TestHostContainersReportsStaleness(t *testing.T) {
	old := time.Now().UTC().Add(-hostagent.DockerStaleAfter - time.Minute)
	s := writeDockerReport(t, hostagent.Docker{CheckedAt: old, Containers: []hostagent.Container{{ID: "1", Name: "x", State: "running"}}})
	if got := s.HostContainers(); !got.Stale || len(got.Containers) != 1 {
		t.Errorf("got %+v, want one stale container", got)
	}
}

// No report at all is a gateway whose agent predates this, not an error.
func TestHostContainersWithoutAReport(t *testing.T) {
	s := &SystemService{cfg: &config.Config{HostDir: t.TempDir()}}
	if got := s.HostContainers(); got.Available || got.Error != "" || len(got.Containers) != 0 {
		t.Errorf("got %+v, want an empty report", got)
	}
}
