package hostnet

import (
	"testing"

	"github.com/meshploy/packages/hostagent"
)

// A real `ss -Hltnp` from a gateway: docker's proxy holding a published port,
// a container on the host's network holding its own, a service bound to
// loopback, and sshd on every interface.
const ssOutput = `LISTEN 0      4096         0.0.0.0:3001       0.0.0.0:*    users:(("docker-proxy",pid=1234,fd=7))
LISTEN 0      4096            [::]:3001          [::]:*    users:(("docker-proxy",pid=1240,fd=7))
LISTEN 0      4096         0.0.0.0:443        0.0.0.0:*    users:(("caddy",pid=900,fd=12))
LISTEN 0      511        127.0.0.1:5432       0.0.0.0:*    users:(("postgres",pid=770,fd=6),("postgres",pid=771,fd=6))
LISTEN 0      128          0.0.0.0:22         0.0.0.0:*    users:(("sshd",pid=1,fd=3))
LISTEN 0      4096         0.0.0.0:9100       0.0.0.0:*
LISTEN 0      4096   127.0.0.53%lo:53         0.0.0.0:*    users:(("systemd-resolve",pid=600,fd=17))
`

func TestParseReadsAddressPortAndHolder(t *testing.T) {
	got := Parse(ssOutput)
	if len(got) != 7 {
		t.Fatalf("got %d listeners", len(got))
	}
	if l := got[0]; l.Address != "0.0.0.0" || l.Port != 3001 || l.Process != "docker-proxy" || l.PID != 1234 {
		t.Errorf("published port: %+v", l)
	}
	// ss brackets an IPv6 address; nothing downstream should have to know that.
	if l := got[1]; l.Address != "::" || !l.Wildcard() {
		t.Errorf("ipv6: %+v", l)
	}
	if l := got[3]; l.Address != "127.0.0.1" || !l.Loopback() || l.Process != "postgres" || l.PID != 770 {
		t.Errorf("loopback: %+v", l)
	}
	// No users:(...) - not root, or a socket with no owner to show. Still a
	// port somebody is holding.
	if l := got[5]; l.Port != 9100 || l.Process != "" || l.PID != 0 {
		t.Errorf("unowned: %+v", l)
	}
	// systemd-resolved binds to an address scoped to an interface, on every
	// Fedora and Ubuntu host there is.
	if l := got[6]; l.Address != "127.0.0.53" || !l.Loopback() {
		t.Errorf("interface-scoped address: %+v", l)
	}
}

func TestAttributeLinksPortsToContainers(t *testing.T) {
	containers := []hostagent.Container{
		{
			ID: "abc123abc123", Name: "monitoring-grafana-1", Image: "grafana/grafana:11.2.0",
			Ports: []hostagent.Port{{HostIP: "0.0.0.0", HostPort: 3001, Port: 3000, Protocol: "tcp"}},
		},
		{ID: "def456def456", Name: "edge-caddy", Image: "caddy:2", NetworkMode: "host"},
	}
	// The host-network container's own process is in its cgroup; docker's proxy
	// is in the daemon's, which says nothing.
	cgroup := func(pid int) string {
		if pid == 900 {
			return "def456def456"
		}
		return ""
	}

	got := Attribute(Parse(ssOutput), containers, cgroup)

	// Published port, matched by the port itself.
	if l := got[0]; l.ContainerName != "monitoring-grafana-1" || l.ContainerImage != "grafana/grafana:11.2.0" {
		t.Errorf("published port not attributed: %+v", l)
	}
	if l := got[1]; l.ContainerID != "abc123abc123" {
		t.Errorf("the ipv6 half of the same publish: %+v", l)
	}
	// Host network, matched by cgroup - nothing else could have.
	if l := got[2]; l.ContainerName != "edge-caddy" || l.ViaRuntime {
		t.Errorf("host-network container: %+v", l)
	}
	// Plain host processes stay plain.
	for _, i := range []int{3, 4, 5, 6} {
		if l := got[i]; l.ContainerID != "" || l.ViaRuntime {
			t.Errorf("listener %d was attributed to a container: %+v", i, l)
		}
	}
}

// A published port whose container the inventory did not report - the runtime
// answered the list but not this one, or it stopped between the two reads - is
// still a container's port, and says so rather than looking like a process
// called docker-proxy.
func TestAttributeFallsBackToTheRuntimeProxy(t *testing.T) {
	got := Attribute(Parse(ssOutput), nil, nil)
	if l := got[0]; !l.ViaRuntime || l.ContainerID != "" {
		t.Errorf("got %+v, want a port held via the runtime", l)
	}
	if l := got[4]; l.ViaRuntime {
		t.Errorf("sshd: %+v", l)
	}
}

// Two containers on the same port of different addresses are not each other.
func TestAttributeKeepsAddressesApart(t *testing.T) {
	containers := []hostagent.Container{
		{ID: "a", Name: "one", Ports: []hostagent.Port{{HostIP: "127.0.0.1", HostPort: 8080, Port: 80, Protocol: "tcp"}}},
		{ID: "b", Name: "two", Ports: []hostagent.Port{{HostIP: "10.0.0.5", HostPort: 8080, Port: 80, Protocol: "tcp"}}},
	}
	list := []hostagent.Listener{
		{Address: "10.0.0.5", Port: 8080, Protocol: "tcp", Process: "docker-proxy", PID: 5},
	}
	got := Attribute(list, containers, nil)
	if got[0].ContainerName != "two" {
		t.Errorf("got %+v, want the container publishing on that address", got[0])
	}
}

func TestContainerIDFromCgroup(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"docker systemd", "0::/system.slice/docker-3f4a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a.scope", "3f4a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a"},
		{"docker cgroupfs", "11:cpuset:/docker/9f8e7d6c5b4a39281716151413121110ff", "9f8e7d6c5b4a39281716151413121110ff"},
		{"podman", "0::/machine.slice/libpod-1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d.scope", "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d"},
		{"a plain service", "0::/system.slice/sshd.service", ""},
		{"a user session", "0::/user.slice/user-1000.slice/session-3.scope", ""},
	} {
		if got := ContainerIDFromCgroup(tc.text); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
