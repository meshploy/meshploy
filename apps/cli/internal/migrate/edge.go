package migrate

import (
	"fmt"
	"regexp"
	"strings"
)

// Finding what holds ports 80 and 443, rather than being told.
//
// A migration has to take those two ports from whatever has them. What that is
// varies more than the platform being migrated does: a Swarm service, a plain
// container, an nginx or a caddy under systemd, a node process somebody started
// by hand. None of that needs the holder's configuration to be understood -
// stopping it is the same three shapes every time - but it does need the holder
// to be named correctly, and an operator naming it is an operator who can be
// wrong.
//
// So it is found: `ss` gives the pid, the pid's cgroup says whether it is in a
// container or under a unit, and Docker says whether that container is a Swarm
// task. One walk, no per-edge knowledge.

// PortHolder is what holds a port, in the terms something can act on.
type PortHolder struct {
	// Kind is swarm, container, systemd, or process for one that is none of
	// those and has to be dealt with by hand. Empty means nothing was found.
	Kind string `json:"kind,omitempty"`
	// Name is the Swarm service, the container, or the unit - whatever Kind
	// says to stop.
	Name string `json:"name,omitempty"`
	// Process is what `ss` called it, kept because it is what an operator
	// recognises even when Kind and Name are the useful part.
	Process string `json:"process,omitempty"`
	PID     int    `json:"pid,omitempty"`
	// Detail says how this was worked out, or why it could not be.
	Detail string `json:"detail,omitempty"`
}

// Stoppable reports whether cutover can take the ports from this holder on its
// own. A bare process cannot be stopped and started again reliably - there is
// no unit to bring it back - so it is reported instead.
func (h PortHolder) Stoppable() bool {
	switch h.Kind {
	case "swarm", "container", "systemd":
		return true
	}
	return false
}

// String is the holder as a line in a plan.
func (h PortHolder) String() string {
	switch {
	case h.Kind == "":
		return "nothing"
	case h.Name == "":
		return h.Kind
	default:
		return h.Kind + " " + h.Name
	}
}

// FindPortHolder works out what holds the first of ports that anything holds.
//
// Docker is consulted before the process table on purpose: a published port is
// held by `docker-proxy`, whose own pid lives under docker.service, and
// following that would name Docker itself as the edge - which is true and
// useless, and stopping it would take the whole host down. A container that
// publishes the port is the real answer, and it is the common one.
func FindPortHolder(r Runner, d Docker, listeners []Listener, ports ...int) PortHolder {
	for _, port := range ports {
		if h, ok := publisherOf(d, port); ok {
			return h
		}
	}
	for _, port := range ports {
		for _, l := range listeners {
			if l.Port != port || loopback(l.Address) || l.PID == 0 {
				continue
			}
			if isDockerProxy(l.Process) {
				// Docker holds it but no container claims it: a stale proxy, or
				// a container this reading did not see. Say so rather than
				// naming Docker.
				return PortHolder{Process: l.Process, PID: l.PID,
					Detail: fmt.Sprintf("Docker publishes port %d but no container was found holding it", port)}
			}
			return holderOfPID(r, l)
		}
	}
	return PortHolder{}
}

// publisherOf is the container publishing this port, as a Swarm service when it
// is one.
func publisherOf(d Docker, port int) (PortHolder, bool) {
	for _, c := range d.Containers {
		if !publishesPort(c.Ports, port) {
			continue
		}
		if c.Service != "" {
			return PortHolder{Kind: "swarm", Name: c.Service, Process: c.Name,
				Detail: fmt.Sprintf("the Swarm service %s publishes port %d", c.Service, port)}, true
		}
		return PortHolder{Kind: "container", Name: c.Name,
			Detail: fmt.Sprintf("the container %s publishes port %d", c.Name, port)}, true
	}
	return PortHolder{}, false
}

// holderOfPID names the container or unit a listening process belongs to.
//
// This is the path for an edge that is not publishing a port through Docker: a
// container on the host's network, or a process under systemd.
func holderOfPID(r Runner, l Listener) PortHolder {
	h := PortHolder{Process: l.Process, PID: l.PID}
	cgroup, err := r.Output("cat", fmt.Sprintf("/proc/%d/cgroup", l.PID))
	if err != nil {
		h.Kind = "process"
		h.Name = l.Process
		h.Detail = fmt.Sprintf("%s (pid %d) holds it; what it belongs to could not be read", l.Process, l.PID)
		return h
	}
	if id := containerIDIn(cgroup); id != "" {
		return containerHolder(r, h, id)
	}
	if unit := unitIn(cgroup); unit != "" {
		h.Kind = "systemd"
		h.Name = unit
		h.Detail = fmt.Sprintf("%s runs under the unit %s", l.Process, unit)
		return h
	}
	h.Kind = "process"
	h.Name = l.Process
	h.Detail = fmt.Sprintf("%s (pid %d) holds it, outside any container or unit", l.Process, l.PID)
	return h
}

// containerHolder turns a container id from a cgroup path into a name, and into
// a Swarm service when the container is a task of one.
func containerHolder(r Runner, h PortHolder, id string) PortHolder {
	out, err := r.Output("docker", "inspect", "--format",
		`{{.Name}}|{{index .Config.Labels "com.docker.swarm.service.name"}}`, id)
	name, service, _ := strings.Cut(strings.TrimSpace(out), "|")
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	service = strings.TrimSpace(service)
	if err != nil || name == "" {
		h.Kind = "container"
		h.Name = id
		h.Detail = fmt.Sprintf("%s runs in container %s, which could not be inspected", h.Process, short(id))
		return h
	}
	if service != "" && service != "<no value>" {
		h.Kind = "swarm"
		h.Name = service
		h.Detail = fmt.Sprintf("%s runs in %s, a task of the Swarm service %s", h.Process, name, service)
		return h
	}
	h.Kind = "container"
	h.Name = name
	h.Detail = fmt.Sprintf("%s runs in the container %s, on the host's network", h.Process, name)
	return h
}

// containerIDRe matches a container id in a cgroup path, in either cgroup
// layout: `docker-<id>.scope` under v2, `/docker/<id>` under v1.
var containerIDRe = regexp.MustCompile(`(?:docker[-/]|libpod-)([0-9a-f]{12,64})`)

func containerIDIn(cgroup string) string {
	if m := containerIDRe.FindStringSubmatch(cgroup); m != nil {
		return m[1]
	}
	return ""
}

// unitRe matches the systemd unit a cgroup path ends in, ignoring the slice
// above it.
var unitRe = regexp.MustCompile(`([a-zA-Z0-9@:_\\.-]+\.service)`)

func unitIn(cgroup string) string {
	last := ""
	for _, line := range strings.Split(cgroup, "\n") {
		if m := unitRe.FindStringSubmatch(line); m != nil {
			last = m[1]
		}
	}
	return last
}

// publishesPort reports whether a `docker ps` port column publishes this port
// on the host, e.g. `0.0.0.0:80->80/tcp`.
func publishesPort(ports string, port int) bool {
	return strings.Contains(ports, fmt.Sprintf(":%d->", port))
}

func isDockerProxy(process string) bool {
	return process == "docker-proxy" || process == "dockerd"
}

func loopback(addr string) bool {
	return strings.HasPrefix(addr, "127.") || addr == "[::1]" || addr == "::1"
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
