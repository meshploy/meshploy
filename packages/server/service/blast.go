package service

import (
	"fmt"
	"sort"
)

// What would happen if a node went down, from where everything runs: each
// service with pods on it keeps running on another node, moves to one with
// room, or stays down, and why. The rules are the scheduler's, applied to
// what is known now; the scheduler decides on the day, so it is a forecast.
// Nothing is stopped.

type Outcome string

const (
	OutcomeKeeps Outcome = "keeps"
	OutcomeMoves Outcome = "moves"
	OutcomeDown  Outcome = "down"
)

type ServiceForecast struct {
	Service PlacedService `json:"service"`
	Outcome Outcome       `json:"outcome"`
	// On is where it keeps running; To where it moves; Reason why it stays down.
	On     []string `json:"on,omitempty"`
	To     string   `json:"to,omitempty"`
	Reason string   `json:"reason,omitempty"`
}

type NodeDownForecast struct {
	Node string `json:"node"`
	// ControlPlane: it is the gateway, the cluster's control plane and the
	// edge. Nothing is rescheduled while it is down, and no route answers
	// from outside.
	ControlPlane bool              `json:"control_plane"`
	Services     []ServiceForecast `json:"services"`
}

// ForecastNodeDown works out what taking the named cluster node out would do.
func ForecastNodeDown(p *Placement, node string) (*NodeDownForecast, error) {
	var down *PlacementNode
	for i := range p.Nodes {
		if p.Nodes[i].K8sName == node {
			down = &p.Nodes[i]
		}
	}
	if down == nil {
		return nil, fmt.Errorf("no node %q in the cluster", node)
	}
	out := &NodeDownForecast{Node: node, ControlPlane: down.ControlPlane, Services: []ServiceForecast{}}

	// What each other node has free for workloads.
	free := map[string]*[2]int64{}
	for _, n := range p.Nodes {
		if n.TakesWorkloads && n.K8sName != node {
			free[n.K8sName] = &[2]int64{n.AllocatableCPU - n.RequestedCPU, n.AllocatableMemory - n.RequestedMemory}
		}
	}

	var affected []PlacedService
	for _, s := range p.Services {
		if s.RunOnce && s.Status == "completed" {
			continue
		}
		for _, pod := range s.Pods {
			if pod.Node == node {
				affected = append(affected, s)
				break
			}
		}
	}
	// The largest first: they are the hardest to place.
	sort.SliceStable(affected, func(i, j int) bool { return affected[i].MemoryRequest > affected[j].MemoryRequest })

	for _, s := range affected {
		f := ServiceForecast{Service: s}
		onHere := 0
		seen := map[string]bool{}
		for _, pod := range s.Pods {
			if pod.Node == node {
				onHere++
			} else if pod.Node != "" && !seen[pod.Node] {
				seen[pod.Node] = true
				f.On = append(f.On, pod.Node)
			}
		}
		sort.Strings(f.On)
		switch {
		case len(f.On) > 0:
			f.Outcome = OutcomeKeeps
		case down.ControlPlane:
			f.Outcome, f.Reason = OutcomeDown, "the control plane is on this node: nothing is moved while it is down"
		case s.PinnedNode == node:
			f.Outcome, f.Reason = OutcomeDown, "it is pinned to this node"
		case contains(s.DataOn, node):
			f.Outcome, f.Reason = OutcomeDown, "its volume's data is on this node, and does not move with it"
		default:
			cpu, mem := s.CPURequest*int64(onHere), s.MemoryRequest*int64(onHere)
			best := ""
			for name, room := range free {
				if room[0] >= cpu && room[1] >= mem && (best == "" || room[1] > free[best][1] || (room[1] == free[best][1] && name < best)) {
					best = name
				}
			}
			if best == "" {
				f.Outcome, f.Reason = OutcomeDown, "no other node has room for what it asks"
			} else {
				free[best][0] -= cpu
				free[best][1] -= mem
				f.Outcome, f.To = OutcomeMoves, best
			}
		}
		out.Services = append(out.Services, f)
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
