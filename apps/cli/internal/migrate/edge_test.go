package migrate

import (
	"fmt"
	"strings"
	"testing"
)

// replyRunner answers the two commands discovery runs.
type replyRunner struct {
	replies map[string]string
	errs    map[string]bool
	ran     []string
}

func (r *replyRunner) Output(name string, args ...string) (string, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	r.ran = append(r.ran, cmd)
	if r.errs[cmd] {
		return "", fmt.Errorf("no")
	}
	return r.replies[cmd], nil
}

func TestParseListenersKeepsThePID(t *testing.T) {
	out := `LISTEN 0 4096 0.0.0.0:443 0.0.0.0:* users:(("caddy",pid=982,fd=7))`
	list := ParseListeners(out)
	if len(list) != 1 || list[0].PID != 982 || list[0].Process != "caddy" {
		t.Fatalf("got %+v", list)
	}
}

func TestFindPortHolderPrefersThePublishingContainer(t *testing.T) {
	d := Docker{Containers: []Container{
		{Name: "dokploy-traefik.1.abc", Ports: "0.0.0.0:80->80/tcp, 0.0.0.0:443->443/tcp", Service: "dokploy-traefik"},
	}}
	// Docker publishes it, so the listener is docker-proxy and its pid leads to
	// docker.service. Following that would name Docker as the edge.
	listeners := []Listener{{Port: 80, Address: "0.0.0.0", Process: "docker-proxy", PID: 12}}
	r := &replyRunner{}
	h := FindPortHolder(r, d, listeners, 80, 443)
	if h.Kind != "swarm" || h.Name != "dokploy-traefik" {
		t.Fatalf("got %+v", h)
	}
	if len(r.ran) != 0 {
		t.Fatalf("asked the host about a port Docker already answered for: %v", r.ran)
	}
}

func TestFindPortHolderNamesAPlainContainer(t *testing.T) {
	d := Docker{Containers: []Container{{Name: "nginx", Ports: "0.0.0.0:80->80/tcp"}}}
	h := FindPortHolder(&replyRunner{}, d, nil, 80, 443)
	if h.Kind != "container" || h.Name != "nginx" || !h.Stoppable() {
		t.Fatalf("got %+v", h)
	}
}

func TestFindPortHolderNamesASystemdUnit(t *testing.T) {
	r := &replyRunner{replies: map[string]string{
		"cat /proc/982/cgroup": "0::/system.slice/caddy.service\n",
	}}
	listeners := []Listener{{Port: 443, Address: "0.0.0.0", Process: "caddy", PID: 982}}
	h := FindPortHolder(r, Docker{}, listeners, 80, 443)
	if h.Kind != "systemd" || h.Name != "caddy.service" {
		t.Fatalf("got %+v", h)
	}
}

func TestFindPortHolderNamesAHostNetworkContainer(t *testing.T) {
	id := "8f4a1c2d3e5b6a7c8d9e0f1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e"
	r := &replyRunner{replies: map[string]string{
		"cat /proc/44/cgroup": "0::/system.slice/docker-" + id + ".scope\n",
		`docker inspect --format {{.Name}}|{{index .Config.Labels "com.docker.swarm.service.name"}} ` + id: "/traefik|\n",
	}}
	listeners := []Listener{{Port: 443, Address: "0.0.0.0", Process: "traefik", PID: 44}}
	h := FindPortHolder(r, Docker{}, listeners, 80, 443)
	if h.Kind != "container" || h.Name != "traefik" {
		t.Fatalf("got %+v", h)
	}
}

// A cgroup under cgroup v1 names the container differently, and a Swarm task
// carries the service label that says what to scale.
func TestFindPortHolderNamesASwarmTaskOnTheHostNetwork(t *testing.T) {
	id := "8f4a1c2d3e5b6a7c8d9e0f1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e"
	r := &replyRunner{replies: map[string]string{
		"cat /proc/44/cgroup": "12:pids:/docker/" + id + "\n11:memory:/docker/" + id + "\n",
		`docker inspect --format {{.Name}}|{{index .Config.Labels "com.docker.swarm.service.name"}} ` + id: "/edge.1.xyz|edge\n",
	}}
	listeners := []Listener{{Port: 80, Address: "0.0.0.0", Process: "traefik", PID: 44}}
	h := FindPortHolder(r, Docker{}, listeners, 80, 443)
	if h.Kind != "swarm" || h.Name != "edge" {
		t.Fatalf("got %+v", h)
	}
}

// A process nobody supervises can be reported but not stopped: there would be
// nothing to start again on a rollback.
func TestFindPortHolderReportsABareProcess(t *testing.T) {
	r := &replyRunner{replies: map[string]string{"cat /proc/7/cgroup": "0::/\n"}}
	listeners := []Listener{{Port: 80, Address: "0.0.0.0", Process: "node", PID: 7}}
	h := FindPortHolder(r, Docker{}, listeners, 80, 443)
	if h.Kind != "process" || h.Stoppable() {
		t.Fatalf("got %+v", h)
	}
}

// Docker holding a port with no container behind it must not come back as
// "stop docker.service".
func TestFindPortHolderNeverNamesDockerItself(t *testing.T) {
	listeners := []Listener{{Port: 80, Address: "0.0.0.0", Process: "docker-proxy", PID: 12}}
	h := FindPortHolder(&replyRunner{}, Docker{}, listeners, 80, 443)
	if h.Kind != "" || h.Stoppable() || h.Detail == "" {
		t.Fatalf("got %+v", h)
	}
}

func TestFindPortHolderIgnoresLoopback(t *testing.T) {
	listeners := []Listener{{Port: 80, Address: "127.0.0.1", Process: "caddy", PID: 3}}
	if h := FindPortHolder(&replyRunner{}, Docker{}, listeners, 80, 443); h.Kind != "" {
		t.Fatalf("got %+v", h)
	}
}
