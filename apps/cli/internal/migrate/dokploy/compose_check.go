package dokploy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Reading a compose app's file before it moves.
//
// A compose app becomes a stack, and a stack runs most of what Docker Compose
// runs: services by name on any port, builds from their Dockerfiles, one-shot
// steps, the repository's own files. What it cannot run is what reaches past
// the container into the machine: host networking, privileges, devices, a
// folder or socket of the host. Those need the operator, before the move and
// not after: an app moved without its host path starts without its data.
//
// So each compose file is read, loosely, for exactly those things. A finding
// that changes whether the app works asks a question; one that only changes
// how it runs is said, so the plan reads as what will happen.

// composeFinding is one thing the file asks of the host.
type composeFinding struct {
	// Service is the compose service it concerns.
	Service string
	// Key identifies it within the app, for a stable decision id.
	Key string
	// Text says it, as the rest of a sentence after the service name.
	Text string
	// Blocks is whether the app may not work without it.
	Blocks bool
}

type composeSpec struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Build         any      `yaml:"build"`
	Image         string   `yaml:"image"`
	ContainerName string   `yaml:"container_name"`
	NetworkMode   string   `yaml:"network_mode"`
	Pid           string   `yaml:"pid"`
	Ipc           string   `yaml:"ipc"`
	Privileged    bool     `yaml:"privileged"`
	CapAdd        []string `yaml:"cap_add"`
	Devices       []any    `yaml:"devices"`
	Volumes       []any    `yaml:"volumes"`
	Ports         []any    `yaml:"ports"`
	DependsOn     any      `yaml:"depends_on"`
	Networks      any      `yaml:"networks"`
	Profiles      []string `yaml:"profiles"`
}

// checkCompose reads a compose file for what a stack cannot carry, and what it
// carries differently.
//
// env is the app's environment, which compose fills ${NAME} from: a port is
// often published as ${DB1_HOST_PORT:-5433}:5432, with the address inside the
// variable.
func checkCompose(content string, env map[string]string) ([]composeFinding, error) {
	var spec composeSpec
	if err := yaml.Unmarshal([]byte(content), &spec); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(spec.Services))
	for n := range spec.Services {
		names = append(names, n)
	}
	sort.Strings(names)

	runsOnce := map[string]bool{}
	for _, n := range names {
		if deps, ok := spec.Services[n].DependsOn.(map[string]any); ok {
			for dep, v := range deps {
				if m, ok := v.(map[string]any); ok && m["condition"] == "service_completed_successfully" {
					runsOnce[dep] = true
				}
			}
		}
	}

	var out []composeFinding
	add := func(svc, key, text string, blocks bool) {
		out = append(out, composeFinding{Service: svc, Key: key, Text: text, Blocks: blocks})
	}
	for _, n := range names {
		s := spec.Services[n]
		// Compose does not start a service in a profile unless asked, and
		// neither does a stack: it is a tool, not part of the running app.
		if len(s.Profiles) > 0 {
			add(n, "profile", "is not started: it is in the "+strings.Join(s.Profiles, ", ")+" profile, as on Dokploy", false)
			continue
		}
		switch {
		case s.NetworkMode == "host":
			add(n, "host-network", "uses the host's network, which a stack cannot give it: it would get its own", true)
		case strings.HasPrefix(s.NetworkMode, "container:"), strings.HasPrefix(s.NetworkMode, "service:"):
			add(n, "shared-network", "shares another container's network ("+s.NetworkMode+"), which a stack does not do", true)
		}
		if s.Pid == "host" || s.Ipc == "host" {
			add(n, "host-namespace", "shares the host's process or IPC namespace, which a stack does not give", true)
		}
		if s.Privileged || len(s.CapAdd) > 0 {
			add(n, "privileges", "runs with extra privileges (privileged or cap_add), which a stack does not grant", true)
		}
		if len(s.Devices) > 0 {
			add(n, "devices", "uses host devices, which a stack cannot pass through", true)
		}
		for _, v := range s.Volumes {
			src, target := volumeSource(v)
			switch {
			case strings.HasSuffix(src, "docker.sock"):
				add(n, "docker-socket", "mounts the Docker socket, which does not exist under Kubernetes", true)
			case strings.HasPrefix(src, "/"):
				add(n, "host-path:"+target, fmt.Sprintf("mounts %s from the host at %s, which is left behind", src, target), true)
			case strings.HasPrefix(src, "../files/"):
				add(n, "dokploy-file:"+target, fmt.Sprintf("mounts Dokploy's file %s at %s, which is not carried yet", strings.TrimPrefix(src, "../files/"), target), true)
			case strings.HasPrefix(src, "."):
				add(n, "repo-files:"+target, fmt.Sprintf("gets %s from the repository at %s, read-only", src, target), false)
			}
		}
		if s.Build != nil {
			add(n, "build", "moves on the image it runs now; the next deploy builds it from its Dockerfile", false)
		}
		if runsOnce[n] {
			add(n, "run-once", "runs once on each deploy, as a step the others wait for", false)
		}
		if s.ContainerName != "" && s.ContainerName != n {
			add(n, "container-name", "also answers to "+s.ContainerName+", as it does now", false)
		}
		for _, p := range s.Ports {
			if ip := publishedAddress(p, env); ip != "" && ip != "127.0.0.1" && ip != "0.0.0.0" && ip != "::" {
				add(n, "publish:"+ip, "is published on "+ip+", which becomes a TCP route bound to that address", false)
				break
			}
		}
	}
	return out, nil
}

// volumeSource reads a compose volume entry, short or long form.
func volumeSource(v any) (source, target string) {
	switch e := v.(type) {
	case string:
		parts := strings.Split(e, ":")
		if len(parts) == 1 {
			return "", parts[0] // an anonymous volume
		}
		return parts[0], parts[1]
	case map[string]any:
		s, _ := e["source"].(string)
		t, _ := e["target"].(string)
		if typ, _ := e["type"].(string); typ != "" && typ != "bind" {
			return "", t
		}
		return s, t
	}
	return "", ""
}

// publishedAddress is the host address a port entry binds, if it names one.
func publishedAddress(p any, env map[string]string) string {
	switch e := p.(type) {
	case string:
		parts := strings.Split(interpolate(e, env), ":")
		if len(parts) == 3 {
			return parts[0]
		}
	case map[string]any:
		ip, _ := e["host_ip"].(string)
		return ip
	}
	return ""
}

// interpolateVar is ${NAME}, ${NAME:-default}, ${NAME-default} or $NAME.
var interpolateVar = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::?-([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// interpolate fills a compose value's variables from env, as compose does:
// an unset variable takes its default, or nothing.
func interpolate(v string, env map[string]string) string {
	return interpolateVar.ReplaceAllStringFunc(v, func(m string) string {
		g := interpolateVar.FindStringSubmatch(m)
		name, def := g[1], g[2]
		if name == "" {
			name = g[3]
		}
		if val, ok := env[name]; ok && val != "" {
			return val
		}
		return def
	})
}

// envLines reads a KEY=VALUE block, as Dokploy stores an app's environment.
func envLines(block string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return out
}

// optLeaveOnDokploy keeps a compose app where it is: excluded from the move,
// still running on Dokploy, and named at cutover.
var optLeaveOnDokploy = Option{"leave", "Leave the app on Dokploy"}

// composeDecisions turns what a file blocks on into the questions an item
// asks, and says the rest.
func composeDecisions(it *Item, findings []composeFinding) {
	for _, f := range findings {
		sentence := f.Service + " " + f.Text
		if !f.Blocks {
			it.Reasons = append(it.Reasons, sentence)
			continue
		}
		it.Decisions = append(it.Decisions, Decision{
			ID:       "compose:" + f.Service + ":" + f.Key,
			Question: sentence,
			Options:  []Option{{"move_without", "Move it anyway, without this"}, optLeaveOnDokploy},
		})
	}
}
