package dokploy

import (
	"fmt"
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

// A port Docker published on every address is reachable from anywhere now, so
// the plan says it opens on the gateway at the same number and records it for
// the move. Loopback is kept as loopback; a specific address, UDP and
// container-only ports are neither.
func TestAPortPublishedEverywhereOpensOnTheGateway(t *testing.T) {
	compose := `
services:
  mq:
    image: rabbitmq:3
    ports:
      - "5672:5672"
      - "0.0.0.0:15672:15672"
      - "127.0.0.1:25672:25672"
      - "100.80.0.1:4369:4369"
      - "9000/udp"
      - "${MQTT_PORT:-1883}:1883/tcp"
  web:
    image: nginx
    ports:
      - target: 80
        published: 8088
`
	findings, err := checkCompose(compose, nil)
	if err != nil {
		t.Fatal(err)
	}
	it := Item{Kind: "compose", ID: "c1"}
	composeDecisions(&it, findings)
	if got := it.Details["public_ports"]; got != "mq:5672:5672,mq:15672:15672,mq:1883:1883,web:8088:80" {
		t.Errorf("public_ports = %q", got)
	}
	if got := it.Details["local_ports"]; got != "mq:25672:25672" {
		t.Errorf("local_ports = %q", got)
	}
	found := false
	for _, r := range it.Reasons {
		if r == "mq publishes port 5672 on every address, which becomes a TCP route on the gateway at 5672" {
			found = true
		}
	}
	if !found {
		t.Errorf("the plan should say so: %v", it.Reasons)
	}
}

// Services start in the order compose starts them: each after what it
// depends_on, a tools profile left out.
func TestComposeStartOrder(t *testing.T) {
	got := composeStartOrder(`
services:
  db: {image: postgres}
  broker: {image: redpanda}
  migrator:
    image: app
    depends_on: {db: {condition: service_healthy}}
  topics:
    image: rpk
    depends_on: [broker]
  api:
    image: app
    depends_on:
      migrator: {condition: service_completed_successfully}
      topics: {condition: service_completed_successfully}
  seed:
    image: app
    profiles: [tools]
`)
	if fmt.Sprint(got) != "[[broker db] [migrator topics] [api]]" {
		t.Errorf("order = %v", got)
	}
}
