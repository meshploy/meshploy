// Package migrate reads a host that runs another platform, so Meshploy can
// plan taking it over. This package holds the layers that know nothing about
// any one platform: the Docker inventory, what holds ports 80 and 443, and the
// host's resources. A platform's own reader (dokploy) builds on them.
//
// Everything here only reads. See internal-docs/plans/migrate-from-dokploy.md.
package migrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// Runner runs a command and returns its standard output. Standard error is
// part of the error, never the output, so a warning cannot corrupt a parse.
type Runner interface {
	Output(name string, args ...string) (string, error)
}

// ExecRunner runs commands on this host.
type ExecRunner struct{}

func (ExecRunner) Output(name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return stdout.String(), err
		}
		return stdout.String(), fmt.Errorf("%s: %w: %s", name, err, firstLine(msg))
	}
	return stdout.String(), nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

// ── Docker ───────────────────────────────────────────────────────────────────

// Container is one container, running or not.
type Container struct {
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	State   string            `json:"state"` // running, exited, ...
	Ports   string            `json:"ports,omitempty"`
	Labels  map[string]string `json:"-"`
	Service string            `json:"swarm_service,omitempty"`
	Project string            `json:"compose_project,omitempty"`
	// MemoryMB is what a running container uses now, from `docker stats`.
	MemoryMB int `json:"memory_mb,omitempty"`
	// BindSources are the host paths bind-mounted into it.
	BindSources []string `json:"bind_sources,omitempty"`
}

// MountsPath reports whether the container bind-mounts path, or a folder
// inside or around it.
func (c Container) MountsPath(path string) bool {
	path = strings.TrimSuffix(path, "/")
	for _, src := range c.BindSources {
		src = strings.TrimSuffix(src, "/")
		if src == path || strings.HasPrefix(src, path+"/") || strings.HasPrefix(path, src+"/") {
			return true
		}
	}
	return false
}

// SwarmService is one Swarm service.
type SwarmService struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	Running int    `json:"running"`
	Desired int    `json:"desired"`
	Ports   string `json:"ports,omitempty"`
}

// Docker is the host's containers and Swarm services.
type Docker struct {
	Containers []Container    `json:"containers"`
	Services   []SwarmService `json:"services"`
	Volumes    []Volume       `json:"volumes"`
}

// Volume is a Docker volume and the size of its data.
type Volume struct {
	Name string `json:"name"`
	MB   int    `json:"mb"`
}

// ReadDocker lists containers, Swarm services and volume sizes.
func ReadDocker(r Runner) (Docker, error) {
	var d Docker
	out, err := r.Output("docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return d, fmt.Errorf("list containers: %w", err)
	}
	d.Containers = ParseContainers(out)

	// Bind mounts, for whether a host path an app mounts is shared.
	if ids, err := r.Output("docker", "ps", "-aq"); err == nil && strings.TrimSpace(ids) != "" {
		args := append([]string{"inspect", "--format", `{{.Name}}|{{range .Mounts}}{{if eq .Type "bind"}}{{.Source}};{{end}}{{end}}`}, strings.Fields(ids)...)
		if out, err := r.Output("docker", args...); err == nil {
			binds := ParseBindSources(out)
			for i, c := range d.Containers {
				d.Containers[i].BindSources = binds[c.Name]
			}
		}
	}

	// What each running container uses, for whether a second copy of the
	// workloads fits beside the first.
	if out, err := r.Output("docker", "stats", "--no-stream", "--format", "{{json .}}"); err == nil {
		usage := ParseStats(out)
		for i, c := range d.Containers {
			d.Containers[i].MemoryMB = usage[c.Name]
		}
	}

	// A host that is not a Swarm manager has no services; that is not an error.
	if out, err := r.Output("docker", "service", "ls", "--format", "{{json .}}"); err == nil {
		d.Services = ParseServices(out)
	}
	if out, err := r.Output("sh", "-c", "du -sm /var/lib/docker/volumes/*/_data 2>/dev/null"); err == nil || out != "" {
		d.Volumes = ParseVolumeSizes(out)
	}
	return d, nil
}

// ParseContainers reads `docker ps --format '{{json .}}'`.
func ParseContainers(out string) []Container {
	var list []Container
	for _, line := range strings.Split(out, "\n") {
		var raw struct {
			Names, Image, State, Ports, Labels string
		}
		if strings.TrimSpace(line) == "" || json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		c := Container{Name: raw.Names, Image: raw.Image, State: raw.State, Ports: raw.Ports, Labels: parseLabels(raw.Labels)}
		c.Service = c.Labels["com.docker.swarm.service.name"]
		c.Project = c.Labels["com.docker.compose.project"]
		list = append(list, c)
	}
	return list
}

// parseLabels reads Docker's "k=v,k=v". A value may itself contain commas, so
// a part without "=" belongs to the value before it.
func parseLabels(s string) map[string]string {
	labels := map[string]string{}
	last := ""
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			if last != "" {
				labels[last] += "," + part
			}
			continue
		}
		labels[k], last = v, k
	}
	return labels
}

// ParseBindSources reads `docker inspect --format '{{.Name}}|<sources;>'`.
func ParseBindSources(out string) map[string][]string {
	binds := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		name, sources, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok {
			continue
		}
		name = strings.TrimPrefix(name, "/")
		for _, s := range strings.Split(sources, ";") {
			if s != "" {
				binds[name] = append(binds[name], s)
			}
		}
	}
	return binds
}

// ParseStats reads `docker stats --no-stream --format '{{json .}}'` into
// memory in use per container name, in MB.
func ParseStats(out string) map[string]int {
	usage := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		var raw struct{ Name, MemUsage string }
		if strings.TrimSpace(line) == "" || json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		used, _, _ := strings.Cut(raw.MemUsage, "/")
		usage[raw.Name] = parseSizeMB(strings.TrimSpace(used))
	}
	return usage
}

// parseSizeMB reads Docker's sizes: "512KiB", "1.5GiB", "20MB", "0B".
func parseSizeMB(s string) int {
	units := []struct {
		suffix string
		mb     float64
	}{
		{"KiB", 1.0 / 1024}, {"MiB", 1}, {"GiB", 1024}, {"TiB", 1024 * 1024},
		{"kB", 1.0 / 1000}, {"MB", 1}, {"GB", 1000}, {"TB", 1000 * 1000}, {"B", 1.0 / (1 << 20)},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			if err != nil {
				return 0
			}
			return int(n * u.mb)
		}
	}
	return 0
}

// ParseServices reads `docker service ls --format '{{json .}}'`.
func ParseServices(out string) []SwarmService {
	var list []SwarmService
	for _, line := range strings.Split(out, "\n") {
		var raw struct{ Name, Image, Replicas, Ports string }
		if strings.TrimSpace(line) == "" || json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		s := SwarmService{Name: raw.Name, Image: strings.Split(raw.Image, "@")[0], Ports: raw.Ports}
		// "1/1", or "1/1 (max 1 per node)"
		if run, want, ok := strings.Cut(strings.Fields(raw.Replicas + " ")[0], "/"); ok {
			s.Running, _ = strconv.Atoi(run)
			s.Desired, _ = strconv.Atoi(want)
		}
		list = append(list, s)
	}
	return list
}

// ParseVolumeSizes reads `du -sm /var/lib/docker/volumes/*/_data`, largest
// first.
func ParseVolumeSizes(out string) []Volume {
	var list []Volume
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		mb, err := strconv.Atoi(f[0])
		name := strings.TrimSuffix(strings.TrimPrefix(f[1], "/var/lib/docker/volumes/"), "/_data")
		if err != nil || name == f[1] {
			continue
		}
		list = append(list, Volume{Name: name, MB: mb})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].MB > list[j].MB })
	return list
}

// TotalMB is the size of every volume together.
func (d Docker) TotalMB() int {
	total := 0
	for _, v := range d.Volumes {
		total += v.MB
	}
	return total
}

// VolumeMB returns a volume's size, or -1 when it is not there.
func (d Docker) VolumeMB(name string) int {
	for _, v := range d.Volumes {
		if v.Name == name {
			return v.MB
		}
	}
	return -1
}

// Service returns the Swarm service of that name.
func (d Docker) Service(name string) (SwarmService, bool) {
	for _, s := range d.Services {
		if s.Name == name {
			return s, true
		}
	}
	return SwarmService{}, false
}

// ── Edge ─────────────────────────────────────────────────────────────────────

// Listener is a process holding a TCP port on the host.
type Listener struct {
	Port    int    `json:"port"`
	Address string `json:"address"`
	Process string `json:"process"`
}

// ReadListeners lists what listens on TCP, from `ss`.
func ReadListeners(r Runner) ([]Listener, error) {
	out, err := r.Output("ss", "-Hltnp")
	if err != nil {
		return nil, fmt.Errorf("list listening ports: %w", err)
	}
	return ParseListeners(out), nil
}

// ParseListeners reads `ss -Hltnp`:
//
//	LISTEN 0 4096 0.0.0.0:80 0.0.0.0:* users:(("docker-proxy",pid=1234,fd=7))
func ParseListeners(out string) []Listener {
	var list []Listener
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
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
		proc := ""
		if j := strings.Index(line, `(("`); j >= 0 {
			rest := line[j+3:]
			proc, _, _ = strings.Cut(rest, `"`)
		}
		list = append(list, Listener{Port: port, Address: local[:i], Process: proc})
	}
	return list
}

// Holders returns the distinct processes listening on port, loopback left out.
func Holders(listeners []Listener, port int) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range listeners {
		if l.Port != port || strings.HasPrefix(l.Address, "127.") || l.Address == "[::1]" {
			continue
		}
		if !seen[l.Process] {
			seen[l.Process] = true
			out = append(out, l.Process)
		}
	}
	return out
}

// ── Resources ────────────────────────────────────────────────────────────────

// Resources is what the host has to run Meshploy beside what runs now.
type Resources struct {
	Cores          int `json:"cores"`
	MemoryMB       int `json:"memory_mb"`
	AvailableMB    int `json:"available_mb"`
	DiskFreeMB     int `json:"disk_free_mb"`
	DockerVolumeMB int `json:"docker_volume_mb"`
	// WorkloadMemoryMB is what the platform's workloads use now: roughly what
	// a second copy of them needs while both run.
	WorkloadMemoryMB int `json:"workload_memory_mb"`
}

// ParseMeminfo reads MemTotal and MemAvailable from /proc/meminfo, in MB.
func ParseMeminfo(s string) (total, available int) {
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		kb, err := strconv.Atoi(f[1])
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total = kb / 1024
		case "MemAvailable:":
			available = kb / 1024
		}
	}
	return total, available
}
