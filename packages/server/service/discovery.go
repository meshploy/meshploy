package service

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
	"gorm.io/gorm/clause"
)

// Discovery: what runs on this infrastructure that Meshploy does not route.
//
// Two sources, both from the host agent, merged into one endpoint per port:
// the host's listening sockets, and the container inventory. Neither contains
// the other - a container published without the userland proxy has no listening
// socket, and a container on the host's network publishes nothing - so both are
// read and a port that appears in both is one row carrying its container.
//
// Read-only. Every action on a row is a route the person creates or a request
// the agent picks up by name.

// Discovery is the whole answer: one entry per node that reports.
type Discovery struct {
	Nodes []NodeDiscovery `json:"nodes"`
	// Silent are nodes with no agent reporting, named so the list never
	// silently omits a machine. Today that is every node but the gateway.
	Silent []SilentNode `json:"silent"`
}

// SilentNode is a node that cannot be discovered yet, and why.
type SilentNode struct {
	NodeID uuid.UUID `json:"node_id"`
	Name   string    `json:"name"`
	Reason string    `json:"reason"`
}

// NodeDiscovery is one node's endpoints and containers.
type NodeDiscovery struct {
	NodeID  uuid.UUID `json:"node_id"`
	Name    string    `json:"name"`
	MeshIP  string    `json:"mesh_ip,omitempty"`
	Gateway bool      `json:"gateway"`

	Endpoints  []Endpoint      `json:"endpoints"`
	Containers []HostContainer `json:"containers"`

	Runtime   string     `json:"runtime,omitempty"`
	Version   string     `json:"version,omitempty"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	Stale     bool       `json:"stale"`
	// Hidden is how many endpoints belong to Meshploy itself and were left
	// out, so the count is never a mystery.
	Hidden int `json:"hidden"`
	// Ignored is how many of this node's endpoints carry that record.
	Ignored int    `json:"ignored"`
	Error   string `json:"error,omitempty"`
}

// Endpoint is one port something holds on a node.
type Endpoint struct {
	// ID is stable across reports: node, address and port. The console keys
	// rows by it, and a later "ignore" is stored against it.
	ID       string `json:"id"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Process  string `json:"process,omitempty"`
	// Label is a guess from the port and the image - "HTTP", "Postgres" - and
	// reads as one. HTTP says whether serving it on a hostname is worth
	// offering, and TLS whether the hop to it has to be HTTPS.
	Label string `json:"label,omitempty"`
	HTTP  bool   `json:"http"`
	TLS   bool   `json:"tls,omitempty"`
	// SuggestedPort is a gateway port a TCP route could take, or 0 when the
	// endpoint's own port is one the gateway keeps for itself - offering 443
	// to something that would be refused is worse than offering nothing.
	SuggestedPort int `json:"suggested_port,omitempty"`

	// Source is "listener" when a process holds the port, "published" when only
	// the runtime publishes it (DNAT, with no userland proxy to listen).
	Source string `json:"source"`
	// Scope is where it can be reached from: "host" (loopback only), "all"
	// (every interface) or "address" (one address).
	Scope string `json:"scope"`

	// Container is what this port belongs to, where it belongs to one.
	Container *EndpointContainer `json:"container,omitempty"`
	// ViaRuntime is a container's port whose container could not be named.
	ViaRuntime bool `json:"via_runtime,omitempty"`

	// Routed is what already sends traffic here.
	Routed []EndpointRoute `json:"routed,omitempty"`
	// Ignored is set when somebody has recorded that this one is correct as it
	// is. Nothing in the console writes it today - see IgnoredEndpoint - but it
	// is served so the state is visible to anything that asks.
	Ignored    bool       `json:"ignored,omitempty"`
	IgnoreID   *uuid.UUID `json:"ignore_id,omitempty"`
	IgnoreNote string     `json:"ignore_note,omitempty"`
	// Routable is whether Meshploy could route it as it is bound, and Reason
	// says why not where it could not.
	Routable bool   `json:"routable"`
	Reason   string `json:"reason,omitempty"`
}

// EndpointContainer is the container behind a port.
type EndpointContainer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image,omitempty"`
	Project string `json:"compose_project,omitempty"`
	// HostNetwork matters here for the same reason it does everywhere: it
	// decides whether this could ever become a Meshploy service.
	HostNetwork bool `json:"host_network,omitempty"`
}

// EndpointRoute is an existing route that already points at this endpoint.
type EndpointRoute struct {
	Kind string    `json:"kind"` // http, tcp
	ID   uuid.UUID `json:"id"`
	// Name is the hostname for an HTTP route, or the gateway port for a TCP one.
	Name string `json:"name"`
}

const (
	endpointFromListener  = "listener"
	endpointFromPublished = "published"

	scopeHost    = "host"
	scopeAll     = "all"
	scopeAddress = "address"
)

// GetDiscovery reports what runs on this org's nodes that Meshploy does not
// route.
//
// Gateway-only today, because that is where the agent runs. Every other node is
// listed as silent with the reason, rather than left out - a page called
// Discovery that quietly skips half the machines is worse than one that says
// which half it can see.
func (s *SystemService) GetDiscovery(ctx context.Context, orgID uuid.UUID) (Discovery, error) {
	out := Discovery{Nodes: []NodeDiscovery{}, Silent: []SilentNode{}}

	var nodes []meshdb.Node
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Order("k3s_role, name").Find(&nodes).Error; err != nil {
		return out, err
	}

	routed, err := s.routedEndpoints(ctx, orgID)
	if err != nil {
		return out, err
	}
	ignored, err := s.ignoredEndpoints(ctx, orgID)
	if err != nil {
		return out, err
	}

	for _, n := range nodes {
		if n.K3sRole != meshdb.K3sRoleServer {
			out.Silent = append(out.Silent, SilentNode{
				NodeID: n.ID, Name: n.Name,
				Reason: "the host agent runs on the gateway; this node reports nothing yet",
			})
			continue
		}
		nd, ok := s.discoverGateway(n, routed, ignored)
		if !ok {
			out.Silent = append(out.Silent, SilentNode{NodeID: n.ID, Name: n.Name, Reason: nd.Error})
			continue
		}
		out.Nodes = append(out.Nodes, nd)
	}
	return out, nil
}

func (s *SystemService) discoverGateway(node meshdb.Node, routed map[routeKey][]EndpointRoute, ignored map[routeKey]meshdb.IgnoredEndpoint) (NodeDiscovery, bool) {
	out := NodeDiscovery{
		NodeID: node.ID, Name: node.Name, MeshIP: node.TailscaleIP, Gateway: true,
		Endpoints: []Endpoint{}, Containers: []HostContainer{},
	}
	dir := s.hostDir()
	if dir == "" {
		out.Error = "this API is not configured with a host directory, so nothing reports"
		return out, false
	}

	containers := s.HostContainers()
	out.Containers, out.Runtime, out.Version = containers.Containers, containers.Runtime, containers.Version
	out.CheckedAt, out.Stale = containers.CheckedAt, containers.Stale

	listeners, listenerStale, err := hostagent.ReadListeners(dir, time.Now())
	if err != nil {
		out.Error = err.Error()
		return out, false
	}
	if listeners == nil && !containers.Available {
		out.Error = "the host agent has not reported yet"
		return out, false
	}
	if listeners != nil {
		out.Stale = out.Stale || listenerStale
		if out.CheckedAt == nil || listeners.CheckedAt.Before(*out.CheckedAt) {
			at := listeners.CheckedAt
			out.CheckedAt = &at
		}
		if listeners.Error != "" && out.Error == "" {
			out.Error = listeners.Error
		}
	}

	out.Endpoints, out.Hidden = mergeEndpoints(node, listeners, containers.Containers, routed)
	for i := range out.Endpoints {
		e := &out.Endpoints[i]
		if ig, ok := ignored[routeKey{NodeID: node.ID, Address: e.Address, Port: e.Port}]; ok {
			e.Ignored, e.IgnoreNote = true, ig.Note
			id := ig.ID
			e.IgnoreID = &id
			out.Ignored++
		}
	}
	return out, true
}

// mergeEndpoints turns the two reports into one row per port.
//
// Listeners come first, because a process holding a port is the more complete
// fact; a container's published port is added only where nothing listens for
// it, which is the runtime forwarding without a userland proxy.
func mergeEndpoints(node meshdb.Node, listeners *hostagent.Listeners, containers []HostContainer, routed map[routeKey][]EndpointRoute) ([]Endpoint, int) {
	byContainer := map[string]HostContainer{}
	for _, c := range containers {
		byContainer[c.ID] = c
	}

	out := []Endpoint{}
	seen := map[string]bool{}
	hidden := 0

	if listeners != nil {
		for _, l := range listeners.Listeners {
			c, known := byContainer[l.ContainerID]
			// A port belonging to a container the API left out is Meshploy's
			// own; so is one held by Meshploy's or k3s's own processes.
			if (l.ContainerID != "" && !known) || ours(l) {
				hidden++
				continue
			}
			e := Endpoint{
				ID:         endpointID(node.ID, l.Address, l.Port),
				Address:    l.Address,
				Port:       l.Port,
				Protocol:   protocolOr(l.Protocol),
				Process:    l.Process,
				Source:     endpointFromListener,
				Scope:      scopeOf(l),
				ViaRuntime: l.ViaRuntime,
			}
			if known {
				e.Container = &EndpointContainer{
					ID: c.ID, Name: c.Name, Image: c.Image,
					Project: c.Project, HostNetwork: c.HostNetwork,
				}
			}
			decorate(&e, node, routed)
			seen[portKey(l.Address, l.Port)] = true
			out = append(out, e)
		}
	}

	// Published ports nothing listens for: the runtime forwards them in the
	// kernel, so they are real and invisible to `ss`.
	for _, c := range containers {
		for _, p := range c.Ports {
			addr := p.HostIP
			if addr == "" {
				addr = "0.0.0.0"
			}
			if seen[portKey(addr, p.HostPort)] || seen[portKey("0.0.0.0", p.HostPort)] {
				continue
			}
			e := Endpoint{
				ID:       endpointID(node.ID, addr, p.HostPort),
				Address:  addr,
				Port:     p.HostPort,
				Protocol: protocolOr(p.Protocol),
				Source:   endpointFromPublished,
				Scope:    scopeOfAddress(addr),
				Container: &EndpointContainer{
					ID: c.ID, Name: c.Name, Image: c.Image,
					Project: c.Project, HostNetwork: c.HostNetwork,
				},
			}
			decorate(&e, node, routed)
			seen[portKey(addr, p.HostPort)] = true
			out = append(out, e)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Address < out[j].Address
	})
	return out, hidden
}

// decorate fills in what is derived rather than read: the label, whether
// Meshploy could route it, and what already does.
func decorate(e *Endpoint, node meshdb.Node, routed map[routeKey][]EndpointRoute) {
	e.Label, e.HTTP, e.TLS = label(*e)
	if !slices.Contains(gatewayOwnPorts, e.Port) {
		e.SuggestedPort = e.Port
	}
	e.Routed = routed[routeKey{NodeID: node.ID, Port: e.Port}]
	if len(e.Routed) == 0 {
		for _, addr := range nodeAddresses(node) {
			if r, ok := routed[routeKey{Address: addr, Port: e.Port}]; ok {
				e.Routed = r
				break
			}
		}
	}
	e.Routable, e.Reason = routable(*e, node)
}

// routable says whether the gateway could reach this endpoint as it is bound.
//
// On the gateway, loopback is reachable: Caddy and the proxy run on the host's
// network there, so a service bound to 127.0.0.1 can be served without ever
// being exposed. That is not true of any other node, and when the agent reports
// from workers this is where that difference lives.
func routable(e Endpoint, node meshdb.Node) (bool, string) {
	if node.K3sRole == meshdb.K3sRoleServer {
		return true, ""
	}
	if e.Scope == scopeHost {
		return false, "bound to this machine's loopback, which the gateway cannot reach"
	}
	return true, ""
}

func scopeOf(l hostagent.Listener) string {
	switch {
	case l.Loopback():
		return scopeHost
	case l.Wildcard():
		return scopeAll
	}
	return scopeAddress
}

func scopeOfAddress(a string) string {
	return scopeOf(hostagent.Listener{Address: a})
}

func protocolOr(p string) string {
	if p == "" {
		return "tcp"
	}
	return p
}

func endpointID(nodeID uuid.UUID, address string, port int) string {
	return fmt.Sprintf("%s:%s:%d", nodeID, address, port)
}

func portKey(address string, port int) string { return fmt.Sprintf("%s:%d", address, port) }

// nodeAddresses are the addresses a route may name this node by.
func nodeAddresses(node meshdb.Node) []string {
	out := []string{"127.0.0.1", "::1"}
	if node.TailscaleIP != "" {
		out = append(out, node.TailscaleIP)
	}
	if node.PublicIP != "" {
		out = append(out, node.PublicIP)
	}
	return out
}

// ours is whether this port is Meshploy's own or the cluster's.
//
// Meshploy's containers are already excluded by the container list, which
// leaves what runs on the host itself: k3s and its kubelet, the mesh daemon,
// and the node metrics we install. Hiding them is the difference between a page
// somebody reads and forty rows nobody decides about.
func ours(l hostagent.Listener) bool {
	if meshployProcesses[l.Process] {
		return true
	}
	return meshployPorts[l.Port]
}

var meshployProcesses = map[string]bool{
	"k3s": true, "k3s-server": true, "k3s-agent": true, "kubelet": true,
	"containerd": true, "containerd-shim": true, "tailscaled": true,
	"node_exporter": true,
}

// meshployPorts are the ports Meshploy and k3s hold on a gateway, kept as a
// literal for the same reason the exposure advisory keeps its own: the API
// container cannot read the compose file, and two of these are k3s's.
var meshployPorts = map[int]bool{
	6443:  true, // kubernetes api
	10250: true, // kubelet
	10256: true, // kube-proxy health
	10249: true, // kube-proxy metrics
	9100:  true, // node metrics
}

// label guesses what is behind a port, from the port and the container's image.
// A guess, and the console says so: nothing acts on it.
func label(e Endpoint) (string, bool, bool) {
	if e.Container != nil {
		if l, ok := labelFromImage(e.Container.Image); ok {
			return l.name, l.http, l.tls
		}
	}
	if l, ok := wellKnownPorts[e.Port]; ok {
		return l.name, l.http, l.tls
	}
	return "", false, false
}

type portLabel struct {
	name string
	// http: worth serving on a hostname. tls: the target speaks HTTPS, so the
	// hop to it must be HTTPS - which a route target carries as TargetTLS.
	http bool
	tls  bool
}

var wellKnownPorts = map[int]portLabel{
	21: {"FTP", false, false}, 22: {"SSH", false, false}, 25: {"SMTP", false, false}, 53: {"DNS", false, false},
	80: {"HTTP", true, false}, 110: {"POP3", false, false}, 143: {"IMAP", false, false},
	443: {"HTTPS", true, true},
	587: {"SMTP", false, false}, 1883: {"MQTT", false, false}, 3000: {"HTTP", true, false},
	3306: {"MySQL", false, false}, 3389: {"RDP", false, false}, 5000: {"HTTP", true, false},
	5432: {"PostgreSQL", false, false}, 5672: {"AMQP", false, false}, 5900: {"VNC", false, false},
	6379: {"Redis", false, false}, 8000: {"HTTP", true, false}, 8080: {"HTTP", true, false},
	8443: {"HTTPS", true, true}, 9200: {"Elasticsearch", false, false},
	11211: {"Memcached", false, false}, 27017: {"MongoDB", false, false},
}

// labelFromImage reads the engine off an image name, the way the migration's
// planner does - a database is named by what it runs, not by its port.
func labelFromImage(image string) (portLabel, bool) {
	name := strings.ToLower(image)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name, _, _ = strings.Cut(name, ":")
	for prefix, l := range imageLabels {
		if strings.HasPrefix(name, prefix) {
			return l, true
		}
	}
	return portLabel{}, false
}

var imageLabels = map[string]portLabel{
	"postgres": {name: "PostgreSQL"}, "mariadb": {name: "MariaDB"}, "mysql": {name: "MySQL"},
	"mongo": {name: "MongoDB"}, "redis": {name: "Redis"}, "valkey": {name: "Valkey"},
	"rabbitmq": {name: "RabbitMQ"}, "elasticsearch": {name: "Elasticsearch"},
	"clickhouse": {name: "ClickHouse"}, "minio": {name: "MinIO", http: true},
	"nginx": {name: "HTTP", http: true}, "caddy": {name: "HTTP", http: true},
	"traefik": {name: "HTTP", http: true}, "grafana": {name: "HTTP", http: true},
	"prometheus": {name: "HTTP", http: true},
}

// routeKey is how an existing route is matched to an endpoint: by the node it
// names, or by the address it points at, plus the port.
type routeKey struct {
	NodeID  uuid.UUID
	Address string
	Port    int
}

// routedEndpoints indexes what this org already routes, so a decided endpoint
// says so instead of being offered again.
func (s *SystemService) routedEndpoints(ctx context.Context, orgID uuid.UUID) (map[routeKey][]EndpointRoute, error) {
	out := map[routeKey][]EndpointRoute{}

	var targets []struct {
		meshdb.RouteTarget
		Hostname string
	}
	if err := s.db.WithContext(ctx).
		Table("route_targets").
		Select("route_targets.*, routes.hostname").
		Joins("JOIN routes ON routes.id = route_targets.route_id").
		Where("routes.organization_id = ?", orgID).
		Where("route_targets.service_id IS NULL AND route_targets.redirect_route_id IS NULL").
		Scan(&targets).Error; err != nil {
		return nil, err
	}
	for _, t := range targets {
		r := EndpointRoute{Kind: "http", ID: t.RouteID, Name: t.Hostname}
		add(out, keyFor(t.NodeID, t.TargetIP, t.TargetPort), r)
	}

	var tcp []meshdb.TCPRoute
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Where("service_id IS NULL").Find(&tcp).Error; err != nil {
		return nil, err
	}
	for _, t := range tcp {
		r := EndpointRoute{Kind: "tcp", ID: t.ID, Name: fmt.Sprintf("port %d", t.GatewayPort)}
		add(out, keyFor(t.NodeID, t.TargetIP, t.TargetPort), r)
	}
	return out, nil
}

// ignoredEndpoints indexes what somebody has already decided about, keyed the
// same way the row is stored: node, address, port.
func (s *SystemService) ignoredEndpoints(ctx context.Context, orgID uuid.UUID) (map[routeKey]meshdb.IgnoredEndpoint, error) {
	var rows []meshdb.IgnoredEndpoint
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[routeKey]meshdb.IgnoredEndpoint, len(rows))
	for _, r := range rows {
		out[routeKey{NodeID: r.NodeID, Address: r.Address, Port: r.Port}] = r
	}
	return out, nil
}

// IgnoreEndpoint records that an endpoint is known and correct as it is.
// Idempotent: ignoring one twice keeps the first decision and updates its note.
func (s *SystemService) IgnoreEndpoint(ctx context.Context, orgID, nodeID, userID uuid.UUID, address string, port int, note string) (*meshdb.IgnoredEndpoint, error) {
	if address == "" || port < 1 || port > 65535 {
		return nil, huma.Error422UnprocessableEntity("an endpoint is an address and a port")
	}
	var node meshdb.Node
	if err := s.db.WithContext(ctx).First(&node, "id = ? AND organization_id = ?", nodeID, orgID).Error; err != nil {
		return nil, huma.Error404NotFound("node not found")
	}

	row := meshdb.IgnoredEndpoint{
		OrganizationID: orgID, NodeID: nodeID, Address: address, Port: port,
		Note: note, IgnoredBy: &userID,
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "node_id"}, {Name: "address"}, {Name: "port"}},
		DoUpdates: clause.AssignmentColumns([]string{"note", "ignored_by", "updated_at"}),
	}).Create(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UnignoreEndpoint removes that record.
func (s *SystemService) UnignoreEndpoint(ctx context.Context, orgID, ignoreID uuid.UUID) error {
	res := s.db.WithContext(ctx).Where("id = ? AND organization_id = ?", ignoreID, orgID).Delete(&meshdb.IgnoredEndpoint{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return huma.Error404NotFound("not found")
	}
	return nil
}

func keyFor(nodeID *uuid.UUID, address string, port int) routeKey {
	if nodeID != nil {
		return routeKey{NodeID: *nodeID, Port: port}
	}
	return routeKey{Address: address, Port: port}
}

func add(m map[routeKey][]EndpointRoute, k routeKey, r EndpointRoute) {
	if k.Port == 0 {
		return
	}
	m[k] = append(m[k], r)
}
