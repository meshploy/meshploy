package dokploy

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A Dokploy application's dynamic file, in the shape Traefik's file provider
// reads: one router, one service, one server URL naming the app on Dokploy's
// network.
const appDynamicFile = `http:
  routers:
    api-router-app:
      rule: Host(` + "`api.example.com`" + `) && PathPrefix(` + "`/`" + `)
      service: api-service-app
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt
  services:
    api-service-app:
      loadBalancer:
        servers:
          - url: http://api-abc123:3000
        passHostHeader: true
`

func TestSwitchServiceURLPointsTheAppAtMeshploy(t *testing.T) {
	out, changed, err := SwitchServiceURL([]byte(appDynamicFile), "http://172.17.0.1:8081")
	if err != nil || changed != 1 {
		t.Fatalf("changed %d: %v", changed, err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	http := doc["http"].(map[string]any)
	svc := http["services"].(map[string]any)["api-service-app"].(map[string]any)
	lb := svc["loadBalancer"].(map[string]any)
	servers := lb["servers"].([]any)
	if got := servers[0].(map[string]any)["url"]; got != "http://172.17.0.1:8081" {
		t.Errorf("url = %v", got)
	}
	// The proxy routes on the Host header, so it must still arrive.
	if lb["passHostHeader"] != true {
		t.Errorf("passHostHeader = %v", lb["passHostHeader"])
	}
	// The router is untouched: same hostname, same TLS, so the certificate and
	// the match keep working while the backend changes underneath.
	router := http["routers"].(map[string]any)["api-router-app"].(map[string]any)
	if !strings.Contains(router["rule"].(string), "api.example.com") {
		t.Errorf("rule = %v", router["rule"])
	}
	if router["tls"] == nil {
		t.Error("the router lost its TLS section")
	}
}

// A file with nothing to switch is a mistake worth reporting, not a silent
// no-op that leaves the domain pointing at Dokploy after the group has moved.
func TestSwitchServiceURLRefusesAFileItCannotSwitch(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"no http", "tcp:\n  routers: {}\n"},
		{"no services", "http:\n  routers:\n    r: {}\n"},
		{"no server urls", "http:\n  services:\n    s:\n      loadBalancer:\n        servers: []\n"},
		{"not yaml", ":\n  - this is not\n   valid yaml\n"},
	} {
		if _, _, err := SwitchServiceURL([]byte(tc.in), "http://172.17.0.1:8081"); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
}

// A compose app routes through labels, which cannot be changed without
// redeploying the thing being migrated. A file at a higher priority takes the
// hosts instead.
func TestOverrideFileTakesTheHostsAtAHigherPriority(t *testing.T) {
	out, err := OverrideFile("productivity-n8n", []string{"n8n.example.com", "flow.example.com", "n8n.example.com", " "}, "http://172.17.0.1:8081")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "# Written by Meshploy") {
		t.Error("the file should say who wrote it and how to undo it")
	}

	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	routers := doc["http"].(map[string]any)["routers"].(map[string]any)
	if len(routers) != 1 {
		t.Fatalf("routers = %v", routers)
	}
	r := routers["meshploy-productivity-n8n"].(map[string]any)
	rule := r["rule"].(string)
	// Sorted and de-duplicated, so the same input always writes the same file.
	if rule != "Host(`flow.example.com`) || Host(`n8n.example.com`)" {
		t.Errorf("rule = %q", rule)
	}
	if r["priority"].(int) != OverridePriority {
		t.Errorf("priority = %v, want above any rule-length default", r["priority"])
	}
	eps := r["entryPoints"].([]any)
	if len(eps) != 2 {
		t.Errorf("entryPoints = %v, want both 80 and 443", eps)
	}
	lb := doc["http"].(map[string]any)["services"].(map[string]any)["meshploy-productivity-n8n"].(map[string]any)["loadBalancer"].(map[string]any)
	if lb["passHostHeader"] != true {
		t.Error("passHostHeader must stay on: the proxy routes on Host")
	}
	if lb["servers"].([]any)[0].(map[string]any)["url"] != "http://172.17.0.1:8081" {
		t.Errorf("servers = %v", lb["servers"])
	}
}

func TestOverrideFileRefusesNothingToDo(t *testing.T) {
	if _, err := OverrideFile("app", nil, "http://172.17.0.1:8081"); err == nil {
		t.Error("no hosts should be an error")
	}
	if _, err := OverrideFile("app", []string{"a.example.com"}, ""); err == nil {
		t.Error("no target should be an error")
	}
}

func TestOverrideFileNameIsRecognisableAndSafe(t *testing.T) {
	for in, want := range map[string]string{
		"productivity-n8n":    "meshploy-productivity-n8n.yml",
		"App With Spaces":     "meshploy-app-with-spaces.yml",
		"../../etc/passwd":    "meshploy-etc-passwd.yml",
		"UPPER.case_and-dots": "meshploy-upper-case-and-dots.yml",
	} {
		if got := OverrideFileName(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestProxyTarget(t *testing.T) {
	if got := ProxyTarget("172.17.0.1", 0); got != "http://172.17.0.1:8081" {
		t.Errorf("got %q", got)
	}
	if got := ProxyTarget("172.18.0.1", 9090); got != "http://172.18.0.1:9090" {
		t.Errorf("got %q", got)
	}
}
