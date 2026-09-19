package dokploy

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Moving a domain to Meshploy while Dokploy's Traefik still holds 80 and 443.
//
// A group's domains move with the group, before cutover. Without this, a host
// with no room for a second copy would serve nothing for a moved app until
// cutover - possibly days.
//
// Dokploy routes in two ways, and each needs its own switch:
//
//   - **A file** in /etc/dokploy/traefik/dynamic/<appName>.yml, for an
//     application. Its service URL is rewritten to Meshploy's proxy. The
//     original is kept, and rollback writes it back.
//   - **Labels**, for a compose app, read by Traefik's docker and swarm
//     providers. Labels cannot be changed without redeploying the thing we are
//     migrating, so instead a new file declares a router for the same hosts at
//     a higher priority, and Traefik prefers it. Rollback deletes the file.
//
// Either way TLS still terminates at Dokploy's Traefik; Meshploy's proxy
// receives a plain HTTP request and routes it on the Host header, which is why
// passHostHeader matters more than anything else here.

// OverridePriority is the priority given to a router that takes a host over
// from a label-routed app.
//
// Traefik's default priority is the length of the rule, so a rule like
// Host(`app.example.com`) scores in the tens. Any number far above that wins
// without having to know what the app's own rule looks like, and a round large
// number is recognisable in Traefik's dashboard as something deliberate.
const OverridePriority = 100000

// OverrideFilePrefix names the files this writes, so finish and rollback can
// find them without a list.
const OverrideFilePrefix = "meshploy-"

// SwitchServiceURL rewrites every load-balancer URL in a Traefik dynamic file
// to target, and returns the rewritten file.
//
// The file is re-marshalled rather than patched as text: a service URL can be
// written in several shapes, and a regular expression over YAML is how a
// migration corrupts somebody's edge. Comments are lost, which is why the
// original is kept in the journal rather than reconstructed.
func SwitchServiceURL(content []byte, target string) ([]byte, int, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, 0, fmt.Errorf("read dynamic file: %w", err)
	}
	http, _ := doc["http"].(map[string]any)
	if http == nil {
		return nil, 0, fmt.Errorf("no http section")
	}
	services, _ := http["services"].(map[string]any)
	if services == nil {
		return nil, 0, fmt.Errorf("no http.services section")
	}

	changed := 0
	for _, svc := range services {
		m, _ := svc.(map[string]any)
		if m == nil {
			continue
		}
		lb, _ := m["loadBalancer"].(map[string]any)
		if lb == nil {
			continue
		}
		servers, _ := lb["servers"].([]any)
		for i, s := range servers {
			sm, _ := s.(map[string]any)
			if sm == nil {
				continue
			}
			if _, ok := sm["url"]; !ok {
				continue
			}
			sm["url"] = target
			servers[i] = sm
			changed++
		}
		// The proxy routes on the Host header, so it has to arrive intact. It
		// is Traefik's default, but a file that turned it off would send every
		// request to the proxy's 404 page.
		lb["passHostHeader"] = true
	}
	if changed == 0 {
		return nil, 0, fmt.Errorf("no load-balancer server URLs to switch")
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, 0, err
	}
	return out, changed, nil
}

// OverrideFile builds a dynamic file that takes the given hosts over from
// whatever else Traefik has learned about them.
//
// Used for compose apps, whose routers come from labels. The router matches the
// same hosts at OverridePriority, on both entry points, so a request arriving
// on either 80 or 443 reaches Meshploy.
func OverrideFile(name string, hosts []string, target string) ([]byte, error) {
	clean := make([]string, 0, len(hosts))
	seen := map[string]bool{}
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		clean = append(clean, h)
	}
	if len(clean) == 0 {
		return nil, fmt.Errorf("no hosts to take over")
	}
	if target == "" {
		return nil, fmt.Errorf("no target to forward to")
	}
	sort.Strings(clean)

	rules := make([]string, 0, len(clean))
	for _, h := range clean {
		rules = append(rules, fmt.Sprintf("Host(`%s`)", h))
	}
	router := "meshploy-" + safeName(name)

	doc := map[string]any{
		"http": map[string]any{
			"routers": map[string]any{
				router: map[string]any{
					"rule":        strings.Join(rules, " || "),
					"entryPoints": []string{"web", "websecure"},
					"service":     router,
					"priority":    OverridePriority,
					"tls":         map[string]any{},
				},
			},
			"services": map[string]any{
				router: map[string]any{
					"loadBalancer": map[string]any{
						"servers":        []any{map[string]any{"url": target}},
						"passHostHeader": true,
					},
				},
			},
		},
	}
	body, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	header := "# Written by Meshploy while migrating " + name + ".\n" +
		"# It takes this app's hostnames over from Dokploy at a higher priority.\n" +
		"# Removing this file gives them back.\n"
	return append([]byte(header), body...), nil
}

// OverrideFileName is where OverrideFile's content belongs in the dynamic
// directory. The prefix is what makes a Meshploy file recognisable among
// Dokploy's own.
func OverrideFileName(name string) string {
	return OverrideFilePrefix + safeName(name) + ".yml"
}

// safeName reduces an app name to something usable as a file name and a Traefik
// router name.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune('-')
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "app"
	}
	return out
}

// ProxyTarget is the URL Dokploy's Traefik should forward a switched domain to.
//
// Meshploy's proxy runs on the gateway's own network namespace, so from inside
// Traefik's container it is the host, reached at the Docker bridge's gateway
// address - the same address the API container uses to reach node_exporter.
func ProxyTarget(bridgeGatewayIP string, port int) string {
	if port == 0 {
		port = 8081
	}
	return fmt.Sprintf("http://%s:%d", bridgeGatewayIP, port)
}
