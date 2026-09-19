package service

import (
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
)

func gatewayNode() meshdb.Node {
	n := meshdb.Node{Name: "gateway", TailscaleIP: "100.64.0.1", PublicIP: "203.0.113.10", K3sRole: meshdb.K3sRoleServer}
	n.ID = uuid.New()
	return n
}

// The shape a real gateway produces: a container's published port held by
// docker's proxy, a container on the host's network holding its own port, a
// service bound to loopback, sshd, and k3s - which is ours and must not appear.
func TestMergeEndpointsIsOneRowPerPort(t *testing.T) {
	node := gatewayNode()
	containers := []HostContainer{
		{Container: hostagent.Container{
			ID: "grafana", Name: "monitoring-grafana-1", Image: "grafana/grafana:11.2.0", Project: "monitoring",
			Ports: []hostagent.Port{{HostIP: "0.0.0.0", HostPort: 3001, Port: 3000, Protocol: "tcp"}},
		}},
		{Container: hostagent.Container{ID: "edge", Name: "edge-caddy", Image: "caddy:2", NetworkMode: "host"}, HostNetwork: true},
		{Container: hostagent.Container{
			ID: "pg", Name: "app-db-1", Image: "postgres:16", Project: "app",
			Ports: []hostagent.Port{{HostIP: "0.0.0.0", HostPort: 5433, Port: 5432, Protocol: "tcp"}},
		}},
	}
	listeners := &hostagent.Listeners{Listeners: []hostagent.Listener{
		{Address: "0.0.0.0", Port: 3001, Protocol: "tcp", Process: "docker-proxy", PID: 10, ContainerID: "grafana", ContainerName: "monitoring-grafana-1", ContainerImage: "grafana/grafana:11.2.0"},
		{Address: "0.0.0.0", Port: 8443, Protocol: "tcp", Process: "caddy", PID: 20, ContainerID: "edge", ContainerName: "edge-caddy", ContainerImage: "caddy:2"},
		{Address: "127.0.0.1", Port: 9999, Protocol: "tcp", Process: "someapp", PID: 30},
		{Address: "0.0.0.0", Port: 22, Protocol: "tcp", Process: "sshd", PID: 1},
		{Address: "0.0.0.0", Port: 6443, Protocol: "tcp", Process: "k3s-server", PID: 40},
		{Address: "0.0.0.0", Port: 4000, Protocol: "tcp", Process: "docker-proxy", PID: 50, ContainerID: "meshploy-api", ViaRuntime: false},
	}}

	got, hidden := mergeEndpoints(node, listeners, containers, map[routeKey][]EndpointRoute{})

	// k3s by process, and the API by its container being one of ours.
	if hidden != 2 {
		t.Errorf("hid %d, want k3s and Meshploy's own API", hidden)
	}
	byPort := map[int]Endpoint{}
	for _, e := range got {
		if _, dup := byPort[e.Port]; dup {
			t.Errorf("port %d appears twice", e.Port)
		}
		byPort[e.Port] = e
	}
	if len(got) != 5 {
		t.Fatalf("got %d endpoints: %+v", len(got), byPort)
	}

	// The published port is one row, carrying its container.
	if e := byPort[3001]; e.Container == nil || e.Container.Name != "monitoring-grafana-1" || e.Source != endpointFromListener {
		t.Errorf("published port: %+v", e)
	}
	// The host-network container's own port, which only the cgroup could link.
	if e := byPort[8443]; e.Container == nil || !e.Container.HostNetwork {
		t.Errorf("host network: %+v", e)
	}
	// Nothing listens for this one: the runtime forwards it in the kernel, and
	// it is still an endpoint.
	if e := byPort[5433]; e.Source != endpointFromPublished || e.Container == nil || e.Label != "PostgreSQL" {
		t.Errorf("DNAT-published port: %+v", e)
	}
	// Loopback on the gateway is routable - Caddy and the proxy are on the
	// host's network there.
	if e := byPort[9999]; e.Scope != scopeHost || !e.Routable {
		t.Errorf("loopback on the gateway: %+v", e)
	}
	if e := byPort[22]; e.Label != "SSH" || e.HTTP {
		t.Errorf("sshd: %+v", e)
	}
}

// An endpoint something already routes is decided, and says which route.
func TestMergeEndpointsMarksWhatIsAlreadyRouted(t *testing.T) {
	node := gatewayNode()
	routeID := uuid.New()
	routed := map[routeKey][]EndpointRoute{
		{Address: "127.0.0.1", Port: 3001}: {{Kind: "http", ID: routeID, Name: "grafana.example.com"}},
		{NodeID: node.ID, Port: 5433}:      {{Kind: "tcp", ID: uuid.New(), Name: "port 15432"}},
	}
	listeners := &hostagent.Listeners{Listeners: []hostagent.Listener{
		{Address: "0.0.0.0", Port: 3001, Protocol: "tcp", Process: "docker-proxy"},
		{Address: "0.0.0.0", Port: 5433, Protocol: "tcp", Process: "docker-proxy"},
		{Address: "0.0.0.0", Port: 7000, Protocol: "tcp", Process: "someapp"},
	}}

	got, _ := mergeEndpoints(node, listeners, nil, routed)

	for _, e := range got {
		switch e.Port {
		case 3001:
			// Matched by an address this node answers on, not the address the
			// listener is bound to.
			if len(e.Routed) != 1 || e.Routed[0].Name != "grafana.example.com" {
				t.Errorf("3001: %+v", e.Routed)
			}
		case 5433:
			if len(e.Routed) != 1 || e.Routed[0].Kind != "tcp" {
				t.Errorf("5433: %+v", e.Routed)
			}
		case 7000:
			if len(e.Routed) != 0 {
				t.Errorf("7000 should be undecided: %+v", e.Routed)
			}
		}
	}
}

// On a worker, loopback is not reachable from the gateway, and the row says so
// instead of offering a route that would not work.
func TestLoopbackOnAWorkerIsNotRoutable(t *testing.T) {
	worker := meshdb.Node{Name: "worker-1", TailscaleIP: "100.64.0.2", K3sRole: meshdb.K3sRoleAgent}
	worker.ID = uuid.New()
	listeners := &hostagent.Listeners{Listeners: []hostagent.Listener{
		{Address: "127.0.0.1", Port: 9999, Protocol: "tcp", Process: "someapp"},
		{Address: "0.0.0.0", Port: 8080, Protocol: "tcp", Process: "someapp"},
	}}

	got, _ := mergeEndpoints(worker, listeners, nil, map[routeKey][]EndpointRoute{})
	if got[0].Port != 8080 || !got[0].Routable {
		t.Errorf("wildcard on a worker: %+v", got[0])
	}
	if got[1].Routable || got[1].Reason == "" {
		t.Errorf("loopback on a worker: %+v, want not routable with a reason", got[1])
	}
}

// The label is a guess from the image first, then the port - a Postgres on a
// port nobody recognises is still a Postgres.
func TestEndpointLabels(t *testing.T) {
	for _, tc := range []struct {
		name  string
		e     Endpoint
		label string
		http  bool
		tls   bool
	}{
		{"by image", Endpoint{Port: 15432, Container: &EndpointContainer{Image: "postgres:16"}}, "PostgreSQL", false, false},
		{"by port", Endpoint{Port: 6379}, "Redis", false, false},
		{"http by port", Endpoint{Port: 8080}, "HTTP", true, false},
		{"registry image", Endpoint{Port: 7777, Container: &EndpointContainer{Image: "ghcr.io/owner/nginx:1.27"}}, "HTTP", true, false},
		// An HTTPS backend is served on a hostname like any other: the hop to
		// it is HTTPS, which is what TLS says.
		{"https", Endpoint{Port: 443}, "HTTPS", true, true},
		{"https on another port", Endpoint{Port: 8443}, "HTTPS", true, true},
		{"unknown", Endpoint{Port: 7777}, "", false, false},
	} {
		gotLabel, gotHTTP, gotTLS := label(tc.e)
		if gotLabel != tc.label || gotHTTP != tc.http || gotTLS != tc.tls {
			t.Errorf("%s: got %q/%v/%v, want %q/%v/%v", tc.name, gotLabel, gotHTTP, gotTLS, tc.label, tc.http, tc.tls)
		}
	}
}

// A TCP route cannot take a port the gateway keeps for itself, so the console
// is not handed one to prefill.
func TestSuggestedPortSkipsTheGatewaysOwn(t *testing.T) {
	node := gatewayNode()
	listeners := &hostagent.Listeners{Listeners: []hostagent.Listener{
		{Address: "0.0.0.0", Port: 443, Protocol: "tcp", Process: "caddy"},
		{Address: "0.0.0.0", Port: 8096, Protocol: "tcp", Process: "jellyfin"},
	}}
	got, _ := mergeEndpoints(node, listeners, nil, map[routeKey][]EndpointRoute{})
	if got[0].Port != 443 || got[0].SuggestedPort != 0 {
		t.Errorf("443: %+v, want no suggestion", got[0])
	}
	if got[1].SuggestedPort != 8096 {
		t.Errorf("8096: %+v, want its own port suggested", got[1])
	}
}
