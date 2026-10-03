package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
)

// The mesh's access policy, generated from what the console already knows:
// which machines there are, whose they are, and what each person may use. A
// person's machine reaches exactly the mesh ports of what they may view, and
// nothing else on those machines; owners and admins reach everything; the
// cluster's own traffic is always allowed. It is rendered as Headscale's
// policy, which names machines by address because every node joins as one
// Headscale user.
//
// Report-only for now: it is built and shown, not set, so an install sees what
// would be blocked before anything is.

// MeshMachineKind is what a machine is to the mesh, which decides its rules
// before any person's access does.
type MeshMachineKind string

const (
	MeshGateway   MeshMachineKind = "gateway"   // the k3s server, the edge, the API
	MeshCluster   MeshMachineKind = "cluster"   // a k3s agent: runs or builds workloads
	MeshConnected MeshMachineKind = "connected" // mesh-only: someone's machine, or a server whose data apps use
)

// MeshNode is a machine as the policy sees it.
type MeshNode struct {
	ID       uuid.UUID
	OrgID    uuid.UUID
	Name     string
	IP       string
	IP6      string // its IPv6 mesh address, when Headscale gave it one
	Kind     MeshMachineKind
	OwnerID  *uuid.UUID
	HostName string // its name in the policy, set by BuildMeshPolicy
}

// MeshMember is a person in an organisation, with their role there.
type MeshMember struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
	Name   string
	Role   db.MemberRole
}

// MeshGrant is something a member may view that listens on the mesh: a
// service's NodePorts, answering on every cluster machine, and its TCP routes'
// ports on the gateway.
type MeshGrant struct {
	UserID       uuid.UUID
	What         string // "postgres in shop (production)", for the report
	NodePorts    []int
	GatewayPorts []int
}

// MeshInputs is everything the policy is made from.
type MeshInputs struct {
	Nodes   []MeshNode
	Members []MeshMember
	Grants  []MeshGrant
	// Unknown are machines on the mesh that Meshploy has no record of: joined
	// with a Headscale key by hand. The policy gives them nothing.
	Unknown []MeshUnknown
	// Rules are network rules: a machine and ports on it, for a person's
	// machines, one machine or every machine of an organisation.
	Rules []MeshNetRule
}

// MeshNetRule is a network rule as the policy uses it.
type MeshNetRule struct {
	ID       uuid.UUID
	OrgID    uuid.UUID
	FromKind string     // db.MeshRuleFromPerson, db.MeshRuleFromMachine or db.MeshRuleFromAll
	FromID   *uuid.UUID // the person or machine
	ToNodeID uuid.UUID
	Ports    []int // none is every port
	Note     string
}

// Why is a network rule's reason, as the report and the policy show it.
func (r MeshNetRule) Why() string {
	if r.Note != "" {
		return "Network rule: " + r.Note
	}
	return "Network rule " + r.ID.String()[:8]
}

// MeshUnknown is a machine Headscale knows and Meshploy does not.
type MeshUnknown struct {
	Name string   `json:"name"`
	IPs  []string `json:"ips"`
}

// MeshDest is a machine and the ports reached on it; no ports means all.
type MeshDest struct {
	Host  string `json:"host"`
	Ports []int  `json:"ports,omitempty"`
}

// MeshRule allows From to reach To, for the reason in Why.
type MeshRule struct {
	From []string   `json:"from"`
	To   []MeshDest `json:"to"`
	Why  string     `json:"why"`
	// everything marks a rule reaching every machine its sources may know:
	// the shared gateway and cluster, and their own organisation's machines.
	everything bool
}

// MeshReach is one thing a machine may reach, as the report shows it.
type MeshReach struct {
	To    string `json:"to"` // "every machine", "gateway", "worker-1"
	Ports []int  `json:"ports,omitempty"`
	Why   string `json:"why"`
}

// MeshMachine is a machine's line in the report.
type MeshMachine struct {
	ID        uuid.UUID       `json:"id"`
	OrgID     uuid.UUID       `json:"-"`
	Name      string          `json:"name"`
	IP        string          `json:"ip"`
	Kind      MeshMachineKind `json:"kind"`
	OwnerID   *uuid.UUID      `json:"owner_id,omitempty"`
	OwnerName string          `json:"owner_name,omitempty"`
	// Everything says it may reach every machine on every port.
	Everything bool        `json:"everything"`
	Reaches    []MeshReach `json:"reaches"`
}

// MeshPolicy is the generated policy: the rules, and each machine's view of
// them.
type MeshPolicy struct {
	Hosts map[string]string `json:"hosts"` // policy name -> address/32
	// Hosts6 are the same machines' IPv6 addresses (/128), by the same name;
	// a rule naming a machine covers both.
	Hosts6   map[string]string `json:"hosts6,omitempty"`
	Rules    []MeshRule        `json:"rules"`
	Machines []MeshMachine     `json:"machines"`
	Unknown  []MeshUnknown     `json:"unknown"`
}

// MeshAllowAll is the policy that lets every machine reach every other: what
// Headscale is given while the generated policy is not enforced.
const MeshAllowAll = `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`

// The gateway's ports every machine needs: DNS, and the API a node registers
// and deregisters through.
var meshGatewayBasics = []int{53, 4000}

var notHostName = regexp.MustCompile(`[^a-z0-9]+`)

// meshEveryone names every machine on the mesh in a rule's sources, those
// Meshploy does not know included: Headscale's "*".
const meshEveryone = "*"

// BuildMeshPolicy turns what the console knows into the policy. It reads
// nothing but its inputs, so the same inputs always give the same policy.
func BuildMeshPolicy(in MeshInputs) MeshPolicy {
	nodes := append([]MeshNode(nil), in.Nodes...)
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Name != nodes[j].Name {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].ID.String() < nodes[j].ID.String()
	})
	hosts, hosts6 := map[string]string{}, map[string]string{}
	for i := range nodes {
		name := strings.Trim(notHostName.ReplaceAllString(strings.ToLower(nodes[i].Name), "-"), "-")
		if name == "" {
			name = "node"
		}
		id := nodes[i].ID.String()
		name += "-" + id[len(id)-4:]
		nodes[i].HostName = name
		if nodes[i].IP != "" {
			hosts[name] = nodes[i].IP + "/32"
			if nodes[i].IP6 != "" {
				hosts6[name] = nodes[i].IP6 + "/128"
			}
		}
	}
	members := map[[2]uuid.UUID]MeshMember{}
	for _, m := range in.Members {
		members[[2]uuid.UUID{m.OrgID, m.UserID}] = m
	}
	grants := map[uuid.UUID][]MeshGrant{}
	for _, g := range in.Grants {
		grants[g.UserID] = append(grants[g.UserID], g)
	}

	var gateway, cluster, connected, all []string
	connectedOf := map[uuid.UUID][]string{}
	for _, n := range nodes {
		if n.IP == "" {
			continue // not on the mesh yet: nothing to name
		}
		all = append(all, n.HostName)
		if n.Kind == MeshConnected {
			connectedOf[n.OrgID] = append(connectedOf[n.OrgID], n.HostName)
		}
		switch n.Kind {
		case MeshGateway:
			gateway = append(gateway, n.HostName)
			cluster = append(cluster, n.HostName)
		case MeshCluster:
			cluster = append(cluster, n.HostName)
		default:
			connected = append(connected, n.HostName)
		}
	}
	every := func(hs []string) []MeshDest {
		out := make([]MeshDest, 0, len(hs))
		for _, h := range hs {
			out = append(out, MeshDest{Host: h})
		}
		return out
	}
	on := func(hs []string, ports []int) []MeshDest {
		out := make([]MeshDest, 0, len(hs))
		for _, h := range hs {
			out = append(out, MeshDest{Host: h, Ports: ports})
		}
		return out
	}

	var rules []MeshRule
	add := func(from []string, to []MeshDest, why string) {
		if len(from) > 0 && len(to) > 0 {
			rules = append(rules, MeshRule{From: from, To: to, Why: why})
		}
	}
	add(cluster, every(cluster), "Kubernetes runs across the cluster's machines")
	add(gateway, every(all), "The gateway's proxy, metrics and discovery reach every machine")
	if len(rules) > 0 && len(gateway) > 0 {
		rules[len(rules)-1].everything = true
	}
	// Every machine on the mesh, known or not: a machine joining is unknown
	// until it registers, and it registers through this API over the mesh. The
	// API answers publicly through Caddy anyway, and asks every call for a token.
	add([]string{meshEveryone}, on(gateway, meshGatewayBasics), "Every machine resolves names and registers through the gateway")
	add(cluster, every(connected), "Apps reach data on connected machines, on any port until a machine says which it offers")

	// People's machines: owners and admins everything, members what they
	// were granted. A machine with no owner gets only the basics above.
	for _, n := range nodes {
		if n.IP == "" || n.Kind != MeshConnected || n.OwnerID == nil {
			continue
		}
		m, ok := members[[2]uuid.UUID{n.OrgID, *n.OwnerID}]
		if !ok {
			continue
		}
		from := []string{n.HostName}
		if m.Role == db.RoleOwner || m.Role == db.RoleAdmin {
			// Everything of their own organisation's, never another's.
			mine := append(append([]string(nil), cluster...), connectedOf[n.OrgID]...)
			sort.Strings(mine)
			add(from, every(mine), fmt.Sprintf("%s is an %s of the organisation", m.Name, m.Role))
			rules[len(rules)-1].everything = true
			continue
		}
		add(from, on(gateway, []int{80, 443}), "Internal web routes, checked per person at the proxy")
		for _, g := range grants[m.UserID] {
			var to []MeshDest
			if len(g.NodePorts) > 0 {
				to = append(to, on(cluster, g.NodePorts)...)
			}
			if len(g.GatewayPorts) > 0 {
				to = append(to, on(gateway, g.GatewayPorts)...)
			}
			add(from, to, fmt.Sprintf("%s may use %s", m.Name, g.What))
		}
	}

	// Network rules, scoped to their organisation's machines.
	hostOf := map[uuid.UUID]MeshNode{}
	for _, n := range nodes {
		if n.IP != "" {
			hostOf[n.ID] = n
		}
	}
	for _, r := range in.Rules {
		to, ok := hostOf[r.ToNodeID]
		if !ok || to.OrgID != r.OrgID {
			continue
		}
		var from []string
		switch r.FromKind {
		case db.MeshRuleFromAll:
			from = connectedOf[r.OrgID]
		case db.MeshRuleFromMachine:
			if r.FromID != nil {
				if n, ok := hostOf[*r.FromID]; ok && n.OrgID == r.OrgID {
					from = []string{n.HostName}
				}
			}
		case db.MeshRuleFromPerson:
			for _, n := range nodes {
				if r.FromID != nil && n.IP != "" && n.Kind == MeshConnected && n.OrgID == r.OrgID && n.OwnerID != nil && *n.OwnerID == *r.FromID {
					from = append(from, n.HostName)
				}
			}
		}
		dest := []MeshDest{{Host: to.HostName, Ports: r.Ports}}
		add(from, dest, r.Why())
	}

	unknown := append([]MeshUnknown{}, in.Unknown...)
	sort.Slice(unknown, func(i, j int) bool { return unknown[i].Name < unknown[j].Name })
	return MeshPolicy{Hosts: hosts, Hosts6: hosts6, Rules: rules, Machines: machinesOf(nodes, rules, members), Unknown: unknown}
}

// machinesOf is each machine's view of the rules, for the report.
func machinesOf(nodes []MeshNode, rules []MeshRule, members map[[2]uuid.UUID]MeshMember) []MeshMachine {
	names := map[string]string{}
	for _, n := range nodes {
		names[n.HostName] = n.Name
	}
	out := make([]MeshMachine, 0, len(nodes))
	for _, n := range nodes {
		m := MeshMachine{ID: n.ID, OrgID: n.OrgID, Name: n.Name, IP: n.IP, Kind: n.Kind, OwnerID: n.OwnerID, Reaches: []MeshReach{}}
		if n.OwnerID != nil {
			m.OwnerName = members[[2]uuid.UUID{n.OrgID, *n.OwnerID}].Name
		}
		for _, r := range rules {
			if !contains(r.From, n.HostName) && !contains(r.From, meshEveryone) {
				continue
			}
			if r.everything {
				m.Everything = true
				m.Reaches = append(m.Reaches, MeshReach{To: "every machine", Why: r.Why})
				continue
			}
			// One line per rule: the machines it names, folded when they share ports.
			byPorts := map[string][]string{}
			var order []string
			for _, d := range r.To {
				if d.Host == n.HostName {
					continue
				}
				key := fmt.Sprint(d.Ports)
				if _, ok := byPorts[key]; !ok {
					order = append(order, key)
				}
				byPorts[key] = append(byPorts[key], names[d.Host])
			}
			for _, key := range order {
				var ports []int
				for _, d := range r.To {
					if fmt.Sprint(d.Ports) == key {
						ports = d.Ports
						break
					}
				}
				m.Reaches = append(m.Reaches, MeshReach{To: strings.Join(byPorts[key], ", "), Ports: ports, Why: r.Why})
			}
		}
		out = append(out, m)
	}
	return out
}

// Headscale renders the policy as Headscale reads it: hosts by address, and
// one accept rule per rule, each destination a host and a port or "*". A
// machine with an IPv6 address has a second host, "<name>-v6", named
// wherever the first is, so the policy holds on both address families.
func (p MeshPolicy) Headscale() ([]byte, error) {
	hosts := make(map[string]string, len(p.Hosts)+len(p.Hosts6))
	for h, a := range p.Hosts {
		hosts[h] = a
	}
	for h, a := range p.Hosts6 {
		hosts[h+"-v6"] = a
	}
	return json.MarshalIndent(struct {
		Hosts map[string]string `json:"hosts"`
		ACLs  []meshACL         `json:"acls"`
	}{hosts, p.acls(p.Rules)}, "", "  ")
}

// meshACL is one of Headscale's accept rules.
type meshACL struct {
	Action string   `json:"action"`
	Src    []string `json:"src"`
	Dst    []string `json:"dst"`
}

// acls renders rules as Headscale's accept rules.
func (p MeshPolicy) acls(rules []MeshRule) []meshACL {
	both := func(h string) []string {
		if _, ok := p.Hosts6[h]; ok {
			return []string{h, h + "-v6"}
		}
		return []string{h}
	}
	acls := make([]meshACL, 0, len(rules))
	for _, r := range rules {
		var src, dst []string
		for _, f := range r.From {
			src = append(src, both(f)...)
		}
		for _, d := range r.To {
			for _, h := range both(d.Host) {
				if len(d.Ports) == 0 {
					dst = append(dst, h+":*")
					continue
				}
				for _, port := range d.Ports {
					dst = append(dst, fmt.Sprintf("%s:%d", h, port))
				}
			}
		}
		acls = append(acls, meshACL{Action: "accept", Src: src, Dst: dst})
	}
	return acls
}
