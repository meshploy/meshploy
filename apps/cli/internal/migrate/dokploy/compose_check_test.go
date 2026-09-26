package dokploy

import (
	"strings"
	"testing"
)

// PowerInsight v2's shape: what a stack now carries is said, and what reaches
// into the host asks before the app moves.
func TestComposeCheckSaysWhatMovesAndAsksWhatCannot(t *testing.T) {
	findings, err := checkCompose(`
x-env: &env
  TZ: Asia/Kolkata
services:
  db1:
    image: timescale/timescaledb:latest-pg16
    ports: ["${DB1_HOST_PORT:-5433}:5432"]
  migrator:
    image: postgres:16-alpine
    restart: "no"
    volumes: [./migrations:/migrations:ro, ./scripts:/scripts:ro]
  s1:
    build: {context: ., dockerfile: services/s-collector/Dockerfile}
    environment: *env
    depends_on:
      migrator: {condition: service_completed_successfully}
  seed:
    image: postgres:16-alpine
    profiles: [tools]
    volumes: [/srv/seed:/seed]
  redpanda:
    image: redpandadata/redpanda:v24.2.7
    container_name: pi-redpanda
  agent:
    image: portainer/agent
    network_mode: host
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - type: bind
        source: /srv/reports
        target: /reports
`, map[string]string{"DB1_HOST_PORT": "100.81.6.12:5433"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range findings {
		got[f.Service+" "+f.Key+" "+map[bool]string{true: "blocks", false: "says"}[f.Blocks]] = true
	}
	for _, want := range []string{
		"db1 publish:100.81.6.12 says",
		"migrator repo-files:/migrations says",
		"migrator run-once says",
		"s1 build says",
		"seed profile says",
		"redpanda container-name says",
		"agent host-network blocks",
		"agent docker-socket blocks",
		"agent host-path:/reports blocks",
	} {
		if !got[want] {
			t.Errorf("missing %q in %v", want, got)
		}
	}

	it := Item{ID: "c1", Name: "powerinsight"}
	composeDecisions(&it, findings)
	if len(it.Decisions) != 3 {
		t.Fatalf("decisions = %+v", it.Decisions)
	}
	d := it.Decisions[0]
	if d.Default != "" || len(d.Options) != 2 || d.Options[1].ID != "leave" {
		t.Errorf("a blocking finding must be answered, with leaving it as a choice: %+v", d)
	}
	if !strings.Contains(strings.Join(it.Reasons, "\n"), "s1 moves on the image it runs now") {
		t.Errorf("reasons = %v", it.Reasons)
	}
}

// A file that is not YAML is reported, not guessed at.
func TestComposeCheckRefusesAFileItCannotRead(t *testing.T) {
	if _, err := checkCompose("services: [unclosed", nil); err == nil {
		t.Fatal("want an error")
	}
}
