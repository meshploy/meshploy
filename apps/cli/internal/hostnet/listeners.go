// Package hostnet reads what is listening on this host, and says which of it
// belongs to a container.
//
// The migration has a reader like this one (`migrate.ParseListeners`), but it
// reads a foreign server over ssh to answer one question - who holds port 80.
// This one reads the machine the agent runs on, keeps the pid, and is the
// source discovery is built from. Reading only.
package hostnet

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/meshploy/packages/hostagent"
)

// runtimeProxies are the processes a container runtime uses to hold a published
// port on the host. The port is a container's; the process is not.
var runtimeProxies = map[string]bool{
	"docker-proxy": true, "rootlessport": true, "rootlessport-c": true,
	"pasta": true, "slirp4netns": true, "containerd": true,
}

// Collect reads the host's listening TCP ports and attributes each to a
// container where it belongs to one.
func Collect(containers []hostagent.Container) hostagent.Listeners {
	out := hostagent.Listeners{CheckedAt: time.Now().UTC()}
	text, err := ss()
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Listeners = Attribute(Parse(text), containers, cgroupContainerID)
	return out
}

func ss() (string, error) {
	cmd := exec.Command("ss", "-Hltnp")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", &ssError{msg}
	}
	return stdout.String(), nil
}

type ssError struct{ msg string }

func (e *ssError) Error() string { return "list listening ports: " + e.msg }

// Parse reads `ss -Hltnp`:
//
//	LISTEN 0 4096 0.0.0.0:80 0.0.0.0:* users:(("docker-proxy",pid=1234,fd=7))
//
// The process and pid are only there when the reader is root, which the agent
// is; without them a row is still a port somebody is holding, and is kept.
func Parse(out string) []hostagent.Listener {
	var list []hostagent.Listener
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		local := f[3]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(local[i+1:])
		if err != nil {
			continue
		}
		l := hostagent.Listener{Address: address(local[:i]), Port: port, Protocol: "tcp"}
		l.Process, l.PID = holder(line)
		list = append(list, l)
	}
	return list
}

// address strips the brackets ss puts around an IPv6 address, so "[::]" and
// "::" are one thing to everything downstream, and the interface a bound
// address is scoped to - "127.0.0.53%lo", which systemd-resolved produces on
// every Fedora and Ubuntu host.
func address(a string) string {
	a = strings.TrimSuffix(strings.TrimPrefix(a, "["), "]")
	if i := strings.IndexByte(a, '%'); i > 0 {
		a = a[:i]
	}
	if a == "*" {
		return "0.0.0.0"
	}
	return a
}

// holder reads the first process out of users:(("name",pid=N,fd=M),...). More
// than one process shares a listening socket often enough - nginx, a forking
// server - and they are the same program, so the first names it.
func holder(line string) (string, int) {
	j := strings.Index(line, `users:(("`)
	if j < 0 {
		return "", 0
	}
	rest := line[j+len(`users:(("`):]
	name, rest, _ := strings.Cut(rest, `"`)
	pid := 0
	if k := strings.Index(rest, "pid="); k >= 0 {
		digits := rest[k+4:]
		end := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' })
		if end > 0 {
			pid, _ = strconv.Atoi(digits[:end])
		}
	}
	return name, pid
}

// Attribute says which listeners are a container's, most reliable link first:
//
//  1. The holding process's cgroup, which carries the container id. This is the
//     only link for a container on the host's network, where the process
//     listening *is* the container's and nothing is published.
//  2. The published port, for an ordinary bridged container - whose listener on
//     the host is the runtime's proxy, owned by the daemon, so its cgroup says
//     nothing about which container the port belongs to.
//  3. Failing both, a runtime proxy is still holding a container's port, and
//     says so without naming one.
//
// cgroup is passed in so tests do not need /proc.
func Attribute(list []hostagent.Listener, containers []hostagent.Container, cgroup func(pid int) string) []hostagent.Listener {
	byID := map[string]hostagent.Container{}
	for _, c := range containers {
		byID[c.ID] = c
	}

	for i, l := range list {
		if id := matchCgroup(cgroup, l.PID, byID); id != "" {
			list[i].ContainerID = id
			list[i].ContainerName, list[i].ContainerImage = byID[id].Name, byID[id].Image
			continue
		}
		if c, ok := publishing(containers, l); ok {
			list[i].ContainerID, list[i].ContainerName, list[i].ContainerImage = c.ID, c.Name, c.Image
			continue
		}
		if runtimeProxies[l.Process] {
			list[i].ViaRuntime = true
		}
	}
	return list
}

func matchCgroup(cgroup func(pid int) string, pid int, byID map[string]hostagent.Container) string {
	if cgroup == nil || pid == 0 {
		return ""
	}
	id := cgroup(pid)
	if id == "" {
		return ""
	}
	if _, ok := byID[id]; ok {
		return id
	}
	// A cgroup carries the full id and the inventory may hold a short one, or
	// the other way round.
	for known := range byID {
		if strings.HasPrefix(id, known) || strings.HasPrefix(known, id) {
			return known
		}
	}
	return ""
}

// publishing finds the container that published this host port. An address the
// container published on must match, so two containers on the same port of
// different addresses stay apart.
func publishing(containers []hostagent.Container, l hostagent.Listener) (hostagent.Container, bool) {
	for _, c := range containers {
		for _, p := range c.Ports {
			if p.HostPort != l.Port {
				continue
			}
			if p.Protocol != "" && p.Protocol != l.Protocol {
				continue
			}
			pa := address(p.HostIP)
			if pa == "" || pa == "0.0.0.0" || pa == "::" || pa == l.Address || l.Wildcard() {
				return c, true
			}
		}
	}
	return hostagent.Container{}, false
}

// cgroupContainerID reads /proc/<pid>/cgroup and returns the container id in
// it, for docker (`docker-<id>.scope`, `/docker/<id>`), podman
// (`libpod-<id>.scope`) and cri-o (`crio-<id>.scope`).
func cgroupContainerID(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cgroup")
	if err != nil {
		return ""
	}
	return ContainerIDFromCgroup(string(b))
}

// ContainerIDFromCgroup pulls a container id out of a cgroup file's contents.
func ContainerIDFromCgroup(text string) string {
	for _, line := range strings.Split(text, "\n") {
		// The last path element holds it, with a prefix and a suffix that vary
		// by runtime and by cgroup driver.
		part := line
		if i := strings.LastIndex(part, "/"); i >= 0 {
			part = part[i+1:]
		}
		part = strings.TrimSuffix(part, ".scope")
		for _, prefix := range []string{"docker-", "libpod-", "crio-", "cri-containerd-", "containerd-"} {
			part = strings.TrimPrefix(part, prefix)
		}
		if isHexID(part) {
			return part
		}
	}
	return ""
}

// isHexID is whether this looks like a container id rather than a systemd unit
// or a slice: 12 hex characters at least, which no unit name is.
func isHexID(s string) bool {
	if len(s) < 12 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
