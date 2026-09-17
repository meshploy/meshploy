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
