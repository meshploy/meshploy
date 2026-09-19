package dockerapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/meshploy/packages/hostagent"
)

// fakeEngine serves the handful of endpoints the reader asks for, over a unix
// socket - the same transport a real runtime answers on, so the whole read path
// is exercised and not just the parsing.
func fakeEngine(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: mux}}
	srv.Start()
	t.Cleanup(srv.Close)
	t.Setenv("DOCKER_HOST", "unix://"+socket)
}

const containersJSON = `[
  {"Id":"abc","Names":["/monitoring-grafana-1"],"Image":"grafana/grafana:11.2.0","State":"running","Status":"Up 9 days",
   "Labels":{"com.docker.compose.project":"monitoring","com.docker.compose.service":"grafana"},
   "Ports":[{"IP":"127.0.0.1","PrivatePort":3000,"PublicPort":3001,"Type":"tcp"},{"PrivatePort":9000,"Type":"tcp"}],
   "Mounts":[{"Type":"volume","Name":"monitoring_grafana-data"},{"Type":"bind","Source":"/srv/grafana.ini"}],
   "HostConfig":{"NetworkMode":"monitoring_default"}},
  {"Id":"def","Names":["/tailscale"],"Image":"tailscale/tailscale:v1.76.1","State":"running","Status":"Up 3 weeks",
   "HostConfig":{"NetworkMode":"host"}}
]`

func TestCollectReadsTheHostsContainers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"Version":"5.2.0","Platform":{"Name":"podman"}}`))
	})
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(containersJSON))
	})
	mux.HandleFunc("/containers/abc/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"Created":"2026-06-15T09:12:00.5Z","RestartCount":2,"State":{"StartedAt":"2026-06-15T09:12:04Z","Health":{"Status":"healthy"}}}`))
	})
	mux.HandleFunc("/containers/def/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"Created":"2026-06-01T11:00:00Z","State":{"StartedAt":"0001-01-01T00:00:00Z"}}`))
	})
	mux.HandleFunc("/containers/abc/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"memory_stats":{"usage":209715200,"stats":{"inactive_file":104857600}},
		  "cpu_stats":{"cpu_usage":{"total_usage":13500},"system_cpu_usage":100000,"online_cpus":4},
		  "precpu_stats":{"cpu_usage":{"total_usage":1000},"system_cpu_usage":50000}}`))
	})
	mux.HandleFunc("/containers/def/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{}`))
	})
	fakeEngine(t, mux)

	got := Collect(context.Background(), true)
	if got.Error != "" || got.Runtime != "podman" || got.Version != "5.2.0" {
		t.Fatalf("report = %+v", got)
	}
	if len(got.Containers) != 2 {
		t.Fatalf("got %d containers", len(got.Containers))
	}

	// Sorted by name, so grafana comes first.
	c := got.Containers[0]
	if c.Name != "monitoring-grafana-1" || c.Project != "monitoring" || c.Service != "grafana" {
		t.Errorf("compose labels: %+v", c)
	}
	// An exposed port with nothing published is not a port on the host.
	if len(c.Ports) != 1 || c.Ports[0].HostPort != 3001 || c.Ports[0].Port != 3000 || c.Ports[0].HostIP != "127.0.0.1" {
		t.Errorf("ports = %+v", c.Ports)
	}
	if len(c.Volumes) != 1 || len(c.BindSources) != 1 {
		t.Errorf("mounts: volumes %v binds %v", c.Volumes, c.BindSources)
	}
	if c.RestartCount != 2 || c.Health != "healthy" || c.StartedAt.IsZero() {
		t.Errorf("inspect: %+v", c)
	}
	// 200 MB used with 100 MB of it reclaimable page cache, and one core's
	// worth of CPU out of four.
	if c.MemoryMB != 100 || c.CPUPercent != 100 {
		t.Errorf("stats: %d MB, %v%%", c.MemoryMB, c.CPUPercent)
	}

	host := got.Containers[1]
	if !host.HostNetwork() || !host.StartedAt.IsZero() {
		t.Errorf("host-network container: %+v", host)
	}
	if got.StatsAt.IsZero() {
		t.Error("a pass that read stats should say when")
	}
}

// A host with no runtime is not a broken host.
func TestCollectWithoutARuntime(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix://"+filepath.Join(t.TempDir(), "nothing.sock"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	got := Collect(context.Background(), false)
	if got.Error == "" || len(got.Containers) != 0 {
		t.Errorf("got %+v, want an empty report with a reason", got)
	}
}

// Numbers are carried over between stats passes, but only for a container that
// has been running since: after a restart they are somebody else's.
func TestCarryStatsOnlyAcrossAContainersOwnRun(t *testing.T) {
	started := time.Date(2026, 6, 15, 9, 12, 4, 0, time.UTC)
	previous := &hostagent.Docker{
		StatsAt: started,
		Containers: []hostagent.Container{
			{ID: "a", StartedAt: started, MemoryMB: 100, CPUPercent: 1.5},
			{ID: "b", StartedAt: started, MemoryMB: 200, CPUPercent: 2.5},
		},
	}
	fresh := hostagent.Docker{Containers: []hostagent.Container{
		{ID: "a", StartedAt: started},
		{ID: "b", StartedAt: started.Add(time.Hour)}, // restarted
		{ID: "c"}, // new
	}}

	got := CarryStats(fresh, previous)
	if got.Containers[0].MemoryMB != 100 || got.Containers[0].CPUPercent != 1.5 {
		t.Errorf("kept container: %+v", got.Containers[0])
	}
	if got.Containers[1].MemoryMB != 0 || got.Containers[2].MemoryMB != 0 {
		t.Errorf("restarted or new containers kept numbers: %+v", got.Containers[1:])
	}
	if !got.StatsAt.Equal(started) {
		t.Errorf("stats_at = %v, want the age of the numbers shown", got.StatsAt)
	}
}

// Nothing to carry from is the first pass, and must not blank what it read.
func TestCarryStatsWithoutAPreviousReport(t *testing.T) {
	fresh := hostagent.Docker{StatsAt: time.Now(), Containers: []hostagent.Container{{ID: "a", MemoryMB: 42}}}
	if got := CarryStats(fresh, nil); got.Containers[0].MemoryMB != 42 || got.StatsAt.IsZero() {
		t.Errorf("got %+v", got)
	}
}
