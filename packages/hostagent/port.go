package hostagent

import (
	"strconv"
	"strings"
	"time"
)

// Port states, as far as the host firewall decides. None of them says the port
// is reachable: a firewall at the hosting provider is invisible from the host.
const (
	PortOpen       = "open"       // nothing on the host blocks it
	PortRestricted = "restricted" // allowed only from Sources
	PortBlocked    = "blocked"    // the host firewall drops it
	PortUnknown    = "unknown"    // no current report, or rules not interpreted
)

// PortVerdict is what the host firewall does with a port.
type PortVerdict struct {
	State     string     `json:"state"`
	Tool      string     `json:"tool,omitempty"`
	Sources   []string   `json:"sources,omitempty"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	// Reason says why the state is unknown.
	Reason string `json:"reason,omitempty"`
}

// Evaluate decides what the host firewall does with a connection from the
// internet to port/proto. agent and fw may be nil; now decides staleness.
//
// Rules are walked in order, first match wins, as ufw applies them. Rules
// bound to an interface are skipped: they admit the mesh or a bridge, not the
// internet. Rules from a specific source only widen who may connect, until a
// rule from anywhere settles it.
func Evaluate(agent *Agent, fw *Firewall, port int, proto string, now time.Time) PortVerdict {
	switch {
	case agent == nil || fw == nil:
		return PortVerdict{State: PortUnknown, Reason: "the host agent has not reported"}
	case now.Sub(agent.HeartbeatAt) > StaleAfter || now.Sub(fw.CheckedAt) > StaleAfter:
		return PortVerdict{State: PortUnknown, Reason: "the host agent's last report is out of date"}
	case fw.Error != "":
		return PortVerdict{State: PortUnknown, Tool: fw.Tool, Reason: "the host agent could not read the firewall: " + fw.Error}
	}
	checked := fw.CheckedAt
	v := PortVerdict{Tool: fw.Tool, CheckedAt: &checked}

	if !fw.Active || fw.Tool == ToolNone {
		v.State = PortOpen
		return v
	}

	var sources []string
	for _, r := range fw.Rules {
		if r.Interface != "" || !protoMatches(r.Proto, proto) {
			continue
		}
		if r.App != "" {
			v.State, v.Reason = PortUnknown, "rule for application profile "+r.App+" was not resolved"
			return v
		}
		if !portMatches(r.Ports, port) {
			continue
		}
		allow := r.Action == "allow" || r.Action == "limit"
		if r.From != "any" {
			if allow {
				sources = append(sources, r.From)
			}
			continue
		}
		if allow {
			v.State = PortOpen
			return v
		}
		return settle(v, sources, PortBlocked)
	}

	if fw.Unparsed {
		v.State, v.Reason = PortUnknown, "the firewall has rules the host agent does not interpret"
		return v
	}
	if fw.DefaultIncoming == "allow" {
		v.State = PortOpen
		return v
	}
	return settle(v, sources, PortBlocked)
}

func settle(v PortVerdict, sources []string, otherwise string) PortVerdict {
	if len(sources) > 0 {
		v.State, v.Sources = PortRestricted, sources
		return v
	}
	v.State = otherwise
	return v
}

func protoMatches(rule, want string) bool { return rule == "" || rule == want }

// portMatches reads "22", "60000:61000" and "80,443", with an empty spec
// matching every port.
func portMatches(spec string, port int) bool {
	if spec == "" {
		return true
	}
	for _, part := range strings.Split(spec, ",") {
		lo, hi, isRange := strings.Cut(part, ":")
		a, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil {
			continue
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(strings.TrimSpace(hi)); err != nil {
				continue
			}
		}
		if port >= a && port <= b {
			return true
		}
	}
	return false
}
