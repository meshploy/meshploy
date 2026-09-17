package service

import (
	"time"

	"github.com/meshploy/packages/hostagent"
)

// HostAgentStatus is whether the gateway's host agent is reporting, for the
// console's server settings. A stopped agent means firewall verdicts are
// unknown, and the operator should see why.
type HostAgentStatus struct {
	// Reporting is true when the agent's last heartbeat is recent.
	Reporting   bool                      `json:"reporting"`
	Version     string                    `json:"version,omitempty"`
	StartedAt   *time.Time                `json:"started_at,omitempty"`
	HeartbeatAt *time.Time                `json:"heartbeat_at,omitempty"`
	Tasks       map[string]hostagent.Task `json:"tasks,omitempty"`
	// Firewall is the tool the agent found: ufw, firewalld, iptables or none.
	Firewall string `json:"firewall,omitempty"`
}

// hostAgentReporting reports whether the host agent, which picks up upgrade
// requests, has reported recently. An API configured without a host directory
// has no agent to wait for.
func (s *SystemService) hostAgentReporting() bool {
	if s.cfg == nil || s.cfg.HostDir == "" {
		return true
	}
	return s.HostAgentStatus().Reporting
}

func (s *SystemService) HostAgentStatus() HostAgentStatus {
	if s.cfg == nil || s.cfg.HostDir == "" {
		return HostAgentStatus{}
	}
	agent, fw, err := hostagent.ReadState(s.cfg.HostDir)
	if err != nil || agent == nil {
		return HostAgentStatus{}
	}
	out := HostAgentStatus{
		Reporting:   time.Since(agent.HeartbeatAt) <= hostagent.StaleAfter,
		Version:     agent.Version,
		StartedAt:   &agent.StartedAt,
		HeartbeatAt: &agent.HeartbeatAt,
		Tasks:       agent.Tasks,
	}
	if fw != nil {
		out.Firewall = fw.Tool
	}
	return out
}
