package service

import (
	"sort"
	"strings"
	"time"

	"github.com/meshploy/packages/hostagent"
)

// What else runs on this machine.
//
// The host agent writes the inventory; this reads it and decides what the
// console is shown. Nothing here acts on a container: every action is a
// request type the agent picks up by name, added one at a time.

// HostContainers is one node's containers, as the console sees them.
type HostContainers struct {
	// Available is false on a node with no agent, no runtime, or no report
	// yet. The console shows no tab rather than an empty one implying
	// something is broken.
	Available bool `json:"available"`
	// Runtime is "docker" or "podman", with its version.
	Runtime string `json:"runtime,omitempty"`
	Version string `json:"version,omitempty"`

	Containers []HostContainer `json:"containers"`
	// Groups are the compose projects among them, in the order they appear.
	// A project moves as one or not at all, so it is named as one thing.
	Groups []HostContainerGroup `json:"groups"`

	CheckedAt *time.Time `json:"checked_at,omitempty"`
	StatsAt   *time.Time `json:"stats_at,omitempty"`
	// Stale is true when the report is older than the agent's own interval
	// allows: the numbers are still shown, with their age.
	Stale bool `json:"stale"`
	// Mine is how many of this host's containers are Meshploy's own and were
	// left out, so the count is never a mystery.
	Mine int `json:"mine"`
	// Error is why the report is empty or partial, in the agent's words.
	Error string `json:"error,omitempty"`
}

// HostContainer is one container Meshploy does not run.
type HostContainer struct {
	hostagent.Container

	// HostNetwork is what decides whether it could ever become a Meshploy
	// service, so it is stated rather than left to be derived from the mode.
	HostNetwork bool `json:"host_network"`
	// Group is the compose project or swarm service it belongs to, empty for
	// a container started on its own.
	Group string `json:"group,omitempty"`
	// Kind is compose, swarm or standalone.
	Kind string `json:"kind"`
}

// HostContainerGroup is a compose project or swarm service: the unit an import
// would have to take whole.
type HostContainerGroup struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Count int    `json:"count"`
	// Running is how many of them are up, which is what says at a glance
	// whether a project is healthy.
	Running int `json:"running"`
	// HostNetwork is true when any member shares the host's network, which
	// keeps the whole project where it is.
	HostNetwork bool `json:"host_network"`
}

const (
	containerKindCompose    = "compose"
	containerKindSwarm      = "swarm"
	containerKindStandalone = "standalone"
)

// HostContainers reads the gateway's container inventory.
//
// Gateway-only, because that is where the agent runs. A worker returns
// Available false until the agent runs there too.
func (s *SystemService) HostContainers() HostContainers {
	out := HostContainers{Containers: []HostContainer{}, Groups: []HostContainerGroup{}}
	dir := s.hostDir()
	if dir == "" {
		return out
	}
	report, stale, err := hostagent.ReadDocker(dir, time.Now())
	if err != nil {
		out.Error = err.Error()
		return out
	}
	if report == nil {
		return out
	}

	out.Runtime, out.Version, out.Error, out.Stale = report.Runtime, report.Version, report.Error, stale
	if !report.CheckedAt.IsZero() {
		at := report.CheckedAt
		out.CheckedAt = &at
	}
	if !report.StatsAt.IsZero() {
		at := report.StatsAt
		out.StatsAt = &at
	}
	// A runtime that did not answer is not a node with no containers: the tab
	// stays hidden, and the reason travels with it for the server settings.
	out.Available = report.Error == ""

	groups := map[string]*HostContainerGroup{}
	for _, c := range report.Containers {
		if isMeshployOwn(c) {
			out.Mine++
			continue
		}
		item := HostContainer{Container: c, HostNetwork: c.HostNetwork(), Kind: containerKindStandalone}
		switch {
		case c.Project != "":
			item.Kind, item.Group = containerKindCompose, c.Project
		case c.Swarm != "":
			item.Kind, item.Group = containerKindSwarm, c.Swarm
		}
		out.Containers = append(out.Containers, item)

		if item.Group == "" {
			continue
		}
		g, ok := groups[item.Group]
		if !ok {
			g = &HostContainerGroup{Name: item.Group, Kind: item.Kind}
			groups[item.Group] = g
		}
		g.Count++
		if c.State == "running" {
			g.Running++
		}
		g.HostNetwork = g.HostNetwork || item.HostNetwork
	}

	for _, g := range groups {
		out.Groups = append(out.Groups, *g)
	}
	sort.Slice(out.Groups, func(i, j int) bool { return out.Groups[i].Name < out.Groups[j].Name })
	return out
}

// isMeshployOwn is whether Meshploy runs this container itself.
//
// Its compose project is the install directory's name, and its images are
// published under one organisation - either is enough, because a podman
// install labels the project differently while the images stay the same.
// Getting this wrong in the safe direction hides one of our own containers;
// getting it wrong the other way offers somebody the console they are reading.
func isMeshployOwn(c hostagent.Container) bool {
	if c.Project == "meshploy" {
		return true
	}
	if strings.HasPrefix(c.Image, "ghcr.io/meshploy/") {
		return true
	}
	return strings.HasPrefix(c.Name, "meshploy-") || strings.HasPrefix(c.Name, "meshploy_")
}
