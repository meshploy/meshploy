// Package hostagent is the contract between the gateway's host agent and the
// API: what the agent reports about the host, and how the API reads it.
//
// The agent (`meshploy host serve`, in the CLI) runs as root on the gateway
// and writes reports into a directory mounted read-only into the API. The API
// never runs anything on the host; it reads these files.
package hostagent

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultDir is where the agent and the API meet on a gateway.
const DefaultDir = "/var/lib/meshploy/host"

const (
	AgentFile    = "agent.json"
	FirewallFile = "firewall.json"

	// FirewallInterval is how often the agent checks the firewall.
	FirewallInterval = time.Minute
	// StaleAfter is how old a report may be before it is not believed: the
	// interval, plus room for a slow check and a restart.
	StaleAfter = 3 * FirewallInterval

	// maxReportBytes bounds what the API reads from a report.
	maxReportBytes = 1 << 20
)

// StateDir is the directory the agent writes and the API reads.
func StateDir(dir string) string { return filepath.Join(dir, "state") }

// Agent is state/agent.json: that the agent runs, and how its tasks went.
type Agent struct {
	Version     string          `json:"version"`
	StartedAt   time.Time       `json:"started_at"`
	HeartbeatAt time.Time       `json:"heartbeat_at"`
	Tasks       map[string]Task `json:"tasks"`
}

// Task is the last result of one of the agent's jobs.
type Task struct {
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
	At    time.Time `json:"at"`
}

// Tool names the host firewall found.
const (
	ToolNone      = "none"
	ToolUFW       = "ufw"
	ToolFirewalld = "firewalld"
	// ToolIptables is an iptables policy that drops, with no ufw or
	// firewalld managing it: rules the agent does not interpret.
	ToolIptables = "iptables"
)

// Firewall is state/firewall.json.
type Firewall struct {
	Tool   string `json:"tool"`
	Active bool   `json:"active"`
	// DefaultIncoming is allow, deny or reject: what happens to a connection
	// no rule matches.
	DefaultIncoming string `json:"default_incoming"`
	Rules           []Rule `json:"rules"`
	// Unparsed is true when rules exist that the agent reports but does not
	// interpret (firewalld rich rules, a raw iptables policy). A port they
	// might decide is unknown, never open.
	Unparsed  bool      `json:"unparsed,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error,omitempty"`
}

// Rule is one incoming rule, in the order the firewall applies them.
type Rule struct {
	// Ports is a port, a range "a:b", or a list "a,b"; empty means any port.
	Ports string `json:"ports,omitempty"`
	// Proto is tcp or udp; empty means both.
	Proto  string `json:"proto,omitempty"`
	Action string `json:"action"` // allow, deny, reject, limit
	// From is "any" or an address or range.
	From string `json:"from"`
	// Interface limits the rule to traffic arriving on one interface. Such a
	// rule says nothing about traffic from the internet.
	Interface string `json:"interface,omitempty"`
	// App is an application profile the agent could not resolve to ports.
	App string `json:"app,omitempty"`
}

// ReadState loads the agent status and firewall report from dir. Either is
// nil when its file does not exist.
func ReadState(dir string) (*Agent, *Firewall, error) {
	agent, err := readJSON[Agent](filepath.Join(StateDir(dir), AgentFile))
	if err != nil {
		return nil, nil, err
	}
	fw, err := readJSON[Firewall](filepath.Join(StateDir(dir), FirewallFile))
	if err != nil {
		return agent, nil, err
	}
	return agent, fw, nil
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

func readJSON[T any](path string) (*T, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxReportBytes {
		return nil, errors.New(path + " is not a report")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// ── Containers outside Meshploy ──────────────────────────────────────────────
//
// What else runs on this machine. The agent reads the container runtime and
// writes it here; the API serves it, and the console offers what may be done
// about each one. Nothing here changes a container: acting on one is a request
// type, one per action.

// DockerFile is state/docker.json.
const DockerFile = "docker.json"

const (
	// DockerInterval is how often the inventory is refreshed. The same cadence
	// as the firewall: cheap, and a container that appears should not take
	// minutes to show up.
	DockerInterval = time.Minute
	// DockerStatsInterval is how often memory and CPU are refreshed. Reading
	// them costs about a second per call, so they are allowed to be stale
	// rather than making the agent expensive on a busy host.
	DockerStatsInterval = 5 * time.Minute
	// DockerStaleAfter is when the inventory stops being believed.
	DockerStaleAfter = 3 * DockerInterval
)

// Docker is state/docker.json: every container on the host, Meshploy's own
// included - the API decides what to show, because what counts as Meshploy's
// own is the API's business, not the agent's.
type Docker struct {
	// Runtime is what answered: "docker" or "podman", with its version.
	Runtime    string      `json:"runtime,omitempty"`
	Version    string      `json:"version,omitempty"`
	Containers []Container `json:"containers"`
	CheckedAt  time.Time   `json:"checked_at"`
	// StatsAt is when memory and CPU were last refreshed, which lags the
	// inventory.
	StatsAt time.Time `json:"stats_at,omitzero"`
	// Error is why this report is empty or partial. An unreachable socket is
	// not a failure of the host: many machines run no containers at all.
	Error string `json:"error,omitempty"`
}

// Container is one container as the host sees it.
type Container struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
	// State is the runtime's own word: running, exited, created, paused,
	// restarting, dead.
	State string `json:"state"`
	// Status is its sentence: "Up 3 days", "Exited (0) 2 hours ago".
	Status       string    `json:"status,omitempty"`
	Health       string    `json:"health,omitempty"`
	RestartCount int       `json:"restart_count,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitzero"`
	StartedAt    time.Time `json:"started_at,omitzero"`

	// Ports are the published ones, as the host publishes them.
	Ports []Port `json:"ports,omitempty"`
	// NetworkMode is docker's: bridge, host, a compose network, or
	// container:<id>. A container on the host's network cannot become a
	// Meshploy service.
	NetworkMode string `json:"network_mode,omitempty"`
	// BindSources are host paths mounted into it: where its data lives, and
	// what an import would have to deal with.
	BindSources []string `json:"bind_sources,omitempty"`
	// Volumes are named volumes it mounts.
	Volumes []string `json:"volumes,omitempty"`

	// Project and Service are compose's labels, and Swarm's service name where
	// there is one. A compose project moves as one or not at all.
	Project string `json:"compose_project,omitempty"`
	Service string `json:"compose_service,omitempty"`
	Swarm   string `json:"swarm_service,omitempty"`

	// MemoryMB and CPUPercent are from the last stats pass, which lags.
	MemoryMB   int     `json:"memory_mb,omitempty"`
	CPUPercent float64 `json:"cpu_percent,omitempty"`
}

// Port is a published port on the host.
type Port struct {
	HostIP   string `json:"host_ip,omitempty"`
	HostPort int    `json:"host_port"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol,omitempty"`
}

// ReadDocker returns the container inventory, and whether it is stale. A nil
// report with no error is a host that has never written one.
func ReadDocker(dir string, now time.Time) (*Docker, bool, error) {
	d, err := readJSON[Docker](filepath.Join(StateDir(dir), DockerFile))
	if err != nil || d == nil {
		return nil, false, err
	}
	return d, now.Sub(d.CheckedAt) > DockerStaleAfter, nil
}

// HostNetwork is whether this container shares the host's network, which is
// what a Meshploy service can never do.
func (c Container) HostNetwork() bool { return c.NetworkMode == "host" }

// ── Discovery: what is listening on this host ────────────────────────────────
//
// The other half of the inventory above. A container's published port and a
// process holding a port are the same kind of thing to somebody deciding what
// to route, and neither list contains the other: a container published without
// the userland proxy has no listening socket, and a container on the host's
// network has no published port.

// ListenersFile is state/listeners.json.
const ListenersFile = "listeners.json"

// ListenerInterval is how often what listens is re-read, and ListenerStaleAfter
// when the report stops being believed.
const (
	ListenerInterval   = time.Minute
	ListenerStaleAfter = 3 * ListenerInterval
)

// Listeners is state/listeners.json: every TCP port held on the host, with the
// container behind it where there is one.
type Listeners struct {
	Listeners []Listener `json:"listeners"`
	CheckedAt time.Time  `json:"checked_at"`
	// Error is why this report is empty or partial - no `ss`, or a host that
	// would not answer.
	Error string `json:"error,omitempty"`
}

// Listener is one process holding a port.
type Listener struct {
	// Address is what it is bound to: an address, 0.0.0.0 or :: for every
	// interface, 127.0.0.1 for this host only. Which decides whether anything
	// off the machine can reach it.
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"` // tcp
	// Process is the program holding it, and PID the first of its processes.
	Process string `json:"process,omitempty"`
	PID     int    `json:"pid,omitempty"`

	// The container this port belongs to, where one was found. A port with a
	// container is one row in the console, not two.
	ContainerID    string `json:"container_id,omitempty"`
	ContainerName  string `json:"container_name,omitempty"`
	ContainerImage string `json:"container_image,omitempty"`
	// ViaRuntime is a port held by the runtime's own port forwarder where the
	// container behind it could not be named: still a container's port, and
	// said that way rather than reported as a mystery process.
	ViaRuntime bool `json:"via_runtime,omitempty"`
}

// Wildcard is whether this listener is bound to every interface, which is what
// decides whether the machine's other addresses reach it.
func (l Listener) Wildcard() bool {
	return l.Address == "0.0.0.0" || l.Address == "::" || l.Address == "*" || l.Address == ""
}

// Loopback is whether only this machine can reach it. On the gateway that is
// still routable - Caddy and the proxy run on the host's network there - and on
// any other node it is not.
func (l Listener) Loopback() bool {
	return strings.HasPrefix(l.Address, "127.") || l.Address == "::1"
}

// ReadListeners returns what listens on the host, and whether it is stale. A
// nil report with no error is a host that has never written one.
func ReadListeners(dir string, now time.Time) (*Listeners, bool, error) {
	l, err := readJSON[Listeners](filepath.Join(StateDir(dir), ListenersFile))
	if err != nil || l == nil {
		return nil, false, err
	}
	return l, now.Sub(l.CheckedAt) > ListenerStaleAfter, nil
}
