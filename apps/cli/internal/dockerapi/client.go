// Package dockerapi reads a container runtime through its Engine API socket.
//
// Not `docker ps`: the API returns records rather than text to parse, it is
// the same socket that carries events and log streams when those are wanted,
// and podman's compatible socket answers it unchanged - which matters, because
// podman gateways are supported and barely exercised.
//
// Reading only. Everything that changes a container is a request the agent
// accepts by name, not a call anything here can make: mounting the socket
// read-only would not stop it, so the guarantee is which calls exist.
package dockerapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Sockets are where a runtime listens, in the order they are tried: docker
// first, then podman's system service, then podman's rootless socket.
func Sockets() []string {
	paths := []string{"/var/run/docker.sock", "/run/docker.sock", "/run/podman/podman.sock"}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		paths = append(paths, dir+"/podman/podman.sock")
	}
	if host := os.Getenv("DOCKER_HOST"); strings.HasPrefix(host, "unix://") {
		paths = append([]string{strings.TrimPrefix(host, "unix://")}, paths...)
	}
	return paths
}

// Client talks to one socket.
type Client struct {
	http   *http.Client
	socket string
}

// Open returns a client for the first socket that answers, or an error naming
// what was tried. A host with no container runtime is not a failure - it is a
// host with no containers, and the caller says so.
func Open(ctx context.Context) (*Client, error) {
	var tried []string
	for _, socket := range Sockets() {
		if _, err := os.Stat(socket); err != nil {
			continue
		}
		c := &Client{socket: socket, http: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socket)
				},
			},
		}}
		if _, err := c.Version(ctx); err != nil {
			tried = append(tried, socket+": "+err.Error())
			continue
		}
		return c, nil
	}
	if len(tried) == 0 {
		return nil, fmt.Errorf("no container runtime socket found")
	}
	return nil, fmt.Errorf("no container runtime answered (%s)", strings.Join(tried, "; "))
}

// Socket is the path this client is talking to.
func (c *Client) Socket() string { return c.socket }

// VersionInfo is what the runtime calls itself.
type VersionInfo struct {
	Version  string `json:"Version"`
	Platform struct {
		Name string `json:"Name"`
	} `json:"Platform"`
	Components []struct {
		Name string `json:"Name"`
	} `json:"Components"`
}

// Runtime is "podman" or "docker", from what the version says about itself.
func (v VersionInfo) Runtime() string {
	if strings.Contains(strings.ToLower(v.Platform.Name), "podman") {
		return "podman"
	}
	for _, c := range v.Components {
		if strings.Contains(strings.ToLower(c.Name), "podman") {
			return "podman"
		}
	}
	return "docker"
}

func (c *Client) Version(ctx context.Context) (VersionInfo, error) {
	var v VersionInfo
	err := c.get(ctx, "/version", &v)
	return v, err
}

// ContainerSummary is one row of /containers/json.
type ContainerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
	Ports  []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
	Mounts []struct {
		Type   string `json:"Type"`
		Name   string `json:"Name"`
		Source string `json:"Source"`
	} `json:"Mounts"`
	HostConfig struct {
		NetworkMode string `json:"NetworkMode"`
	} `json:"HostConfig"`
}

// Name is the container's name without the leading slash the API adds.
func (s ContainerSummary) Name() string {
	if len(s.Names) == 0 {
		return s.ID
	}
	return strings.TrimPrefix(s.Names[0], "/")
}

// Containers lists them, stopped ones included: a container that keeps exiting
// is exactly what somebody wants to see on this page.
func (c *Client) Containers(ctx context.Context) ([]ContainerSummary, error) {
	var out []ContainerSummary
	err := c.get(ctx, "/containers/json?all=1", &out)
	return out, err
}

// ContainerDetail is the part of /containers/{id}/json worth keeping.
type ContainerDetail struct {
	Created string `json:"Created"`
	State   struct {
		StartedAt string `json:"StartedAt"`
		Health    *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	RestartCount int `json:"RestartCount"`
}

func (c *Client) Inspect(ctx context.Context, id string) (ContainerDetail, error) {
	var d ContainerDetail
	err := c.get(ctx, "/containers/"+url.PathEscape(id)+"/json", &d)
	return d, err
}

// Stats is one non-streaming sample.
type Stats struct {
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Stats struct {
			Cache        uint64 `json:"cache"`
			InactiveFile uint64 `json:"inactive_file"`
		} `json:"stats"`
	} `json:"memory_stats"`
	CPUStats    cpuStats `json:"cpu_stats"`
	PreCPUStats cpuStats `json:"precpu_stats"`
}

type cpuStats struct {
	CPUUsage struct {
		Total  uint64   `json:"total_usage"`
		PerCPU []uint64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	SystemUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs  int    `json:"online_cpus"`
}

// MemoryMB is what the container is using, with page cache taken off: cache is
// reclaimable, and counting it reports a container that reads files as if it
// were near its limit.
func (s Stats) MemoryMB() int {
	usage := s.MemoryStats.Usage
	if cache := s.MemoryStats.Stats.InactiveFile; cache > 0 && cache < usage {
		usage -= cache
	} else if cache := s.MemoryStats.Stats.Cache; cache > 0 && cache < usage {
		usage -= cache
	}
	return int(usage / (1 << 20))
}

// CPUPercent is the share of one host's worth of CPU, the way `docker stats`
// reports it: 200% means two cores' worth.
func (s Stats) CPUPercent() float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.Total) - float64(s.PreCPUStats.CPUUsage.Total)
	systemDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	if cpuDelta <= 0 || systemDelta <= 0 {
		return 0
	}
	cpus := float64(s.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = float64(len(s.CPUStats.CPUUsage.PerCPU))
	}
	if cpus == 0 {
		cpus = 1
	}
	return cpuDelta / systemDelta * cpus * 100
}

// StatsOnce takes one sample. The Engine computes the delta against its own
// previous reading, so a single call is enough.
func (c *Client) StatsOnce(ctx context.Context, id string) (Stats, error) {
	var s Stats
	err := c.get(ctx, "/containers/"+url.PathEscape(id)+"/stats?stream=false&one-shot=false", &s)
	return s, err
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
