package service

import (
	"context"
	"testing"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

func composeServices(t *testing.T, spec string) composetypes.Services {
	t.Helper()
	project, err := loader.LoadWithContext(context.Background(), composetypes.ConfigDetails{
		WorkingDir:  "/",
		ConfigFiles: []composetypes.ConfigFile{{Filename: "docker-compose.yml", Content: []byte(spec)}},
		Environment: map[string]string{},
	}, loader.WithSkipValidation)
	if err != nil {
		t.Fatal(err)
	}
	return project.Services
}

// Ports follow compose: published is public unless bound to loopback, expose
// is internal, and deploy.port is the primary port.
func TestStackPortsFollowCompose(t *testing.T) {
	services := composeServices(t, `
services:
  web:
    image: nginx
    ports: ["8080:80", "127.0.0.1:9090:9090"]
    expose: ["9100", "9200-9201/tcp", "5353/udp"]
  cache:
    image: redis:7-alpine
    ports: ["6379:6379"]
  explicit:
    image: x
    ports:
      - target: 6379
        published: "6380"
        app_protocol: http
        name: admin
      - target: 8000
        app_protocol: grpc
  template:
    image: x
    x-meshploy:
      deploy:
        port: 5050
  sidecar:
    image: x
    expose: ["8080"]
  worker:
    image: alpine
  dns:
    image: x
    ports: ["53:53/udp"]
`)
	type port struct {
		name                  string
		port                  int
		public, http, primary bool
	}
	cases := []struct {
		service    string
		deployPort int
		want       []port
		declared   bool
		notes      int
	}{
		{"web", 0, []port{
			{"http", 80, true, true, true},
			{"http-9090", 9090, false, true, false},
			{"http-9100", 9100, false, true, false},
			{"http-9200", 9200, false, true, false},
			{"http-9201", 9201, false, true, false},
		}, true, 1},
		{"cache", 0, []port{{"tcp", 6379, true, false, true}}, true, 0},
		{"explicit", 0, []port{
			{"admin", 6379, true, true, true},
			{"tcp-8000", 8000, true, false, false},
		}, true, 0},
		{"template", 5050, []port{{"http", 5050, true, true, true}}, true, 0},
		{"sidecar", 8080, []port{{"http", 8080, false, true, true}}, true, 0},
		{"worker", 0, []port{{"http", 3000, false, true, true}}, false, 0},
		{"dns", 0, []port{{"http", 3000, false, true, true}}, false, 1},
	}
	for _, c := range cases {
		got, declared, notes := stackPorts(services[c.service], c.deployPort)
		if declared != c.declared || len(notes) != c.notes {
			t.Errorf("%s: declared %v, notes %v", c.service, declared, notes)
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: ports %+v", c.service, got)
			continue
		}
		for i, w := range c.want {
			g := got[i]
			if g.Name != w.name || g.Port != w.port || g.IsPublic != w.public || g.IsHTTP != w.http || g.IsPrimary != w.primary {
				t.Errorf("%s port %d: got %+v, want %+v", c.service, i, g, w)
			}
		}
	}
}

func TestIsLoopback(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "[::1]": true, "localhost": true,
		"": false, "0.0.0.0": false, "10.0.0.5": false,
	} {
		if isLoopback(host) != want {
			t.Errorf("isLoopback(%q) = %v", host, !want)
		}
	}
}
