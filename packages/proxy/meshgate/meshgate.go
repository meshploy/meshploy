// Package meshgate decides who on the mesh may open an internal route.
//
// The mesh policy works in ports, and every internal route shares the
// gateway's 443, so the policy lets a member's machine reach all of them and
// leaves the choice to the proxy. The proxy knows the caller's mesh address,
// which is a machine, which has an owner, who has grants: an internal route
// answers them only when they may see what it leads to. Owners and admins of
// the route's organisation, and the cluster's own machines, always pass.
//
// It applies only while the mesh policy is enforced: until then every machine
// reaches every other anyway, and an install moving to the policy sees what it
// would block on the Access page before anything is closed.
package meshgate

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// Target is what an internal route leads to.
type Target struct {
	RouteID   uuid.UUID
	ServiceID uuid.UUID // zero for a node or address target
	ProjectID uuid.UUID
	OrgID     uuid.UUID
}

// Verdict is the answer for one caller and one route.
type Verdict struct {
	Allowed bool
	// Machine is the caller's machine name, when it is one Meshploy knows.
	Machine string
	// Reason says why, for the log and the refusal page.
	Reason string
}

type machine struct {
	id    uuid.UUID
	org   uuid.UUID
	name  string
	kind  string // "gateway", "cluster" or "connected"
	owner *uuid.UUID
}

type service struct {
	project  uuid.UUID
	stack    *uuid.UUID
	database bool
}

type memberKey struct{ org, user uuid.UUID }

type grantKey struct {
	t  db.ResourceType
	id uuid.UUID
}

// Snapshot is what the gate decides from, loaded at once so one decision
// never mixes two states of the database.
type Snapshot struct {
	Enforced bool
	machines map[string]machine // by mesh address
	roles    map[memberKey]db.MemberRole
	grants   map[memberKey]map[grantKey]bool
	reach    map[memberKey]map[grantKey]bool // the "from their machines" switches
	services map[uuid.UUID]service
	parent   map[uuid.UUID]uuid.UUID // a level's project
	rules    []db.MeshRule
	gateways map[uuid.UUID]bool
}

// Gate holds the latest snapshot.
type Gate struct {
	db   *gorm.DB
	mu   sync.RWMutex
	snap *Snapshot
}

func New(database *gorm.DB) *Gate { return &Gate{db: database} }

// Start loads the snapshot, then again within a second of any change to what
// the mesh policy is made from (db.EdgeMesh) and every refresh regardless.
func (g *Gate) Start(refresh time.Duration) {
	g.reload()
	db.FollowEdgeVersion(g.db, db.EdgeMesh, refresh, g.reload)
}

func (g *Gate) reload() {
	s, err := Load(g.db)
	if err != nil {
		log.Printf("meshgate: load failed, keeping the last state: %v", err)
		return
	}
	g.Set(s)
}

// Set replaces the snapshot; Start does this from the database.
func (g *Gate) Set(s *Snapshot) {
	g.mu.Lock()
	g.snap = s
	g.mu.Unlock()
}

// Check is the verdict for a caller's address on an internal route. Before a
// snapshot has loaded, the policy cannot be known to be off, so nobody on the
// mesh is let through but the server's own machines.
func (g *Gate) Check(addr string, t Target) Verdict {
	g.mu.RLock()
	s := g.snap
	g.mu.RUnlock()
	if s == nil {
		s = &Snapshot{Enforced: true}
	}
	return s.Decide(addr, t)
}

// Decide is the verdict for a caller's address on an internal route.
func (s *Snapshot) Decide(addr string, t Target) Verdict {
	if !s.Enforced {
		return Verdict{Allowed: true, Reason: "the mesh policy is not enforced"}
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return Verdict{Reason: "the caller's address is unreadable"}
	}
	m, known := s.machines[ip.String()]
	if !known {
		// Caddy listens for internal names on the gateway's mesh address
		// alone, so a caller from outside the mesh is on the gateway itself:
		// a process or a container there.
		if !onMesh(ip) && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
			return Verdict{Allowed: true, Reason: "the gateway itself"}
		}
		return Verdict{Reason: "a machine Meshploy does not know"}
	}
	v := Verdict{Machine: m.name}
	if m.kind != "connected" {
		v.Allowed, v.Reason = true, "one of the cluster's machines"
		return v
	}
	if m.org != t.OrgID {
		v.Reason = "a machine of another organisation"
		return v
	}
	if s.ruleOpens(m) {
		v.Allowed, v.Reason = true, "a network rule opens the gateway's web port to it"
		return v
	}
	if m.owner == nil {
		v.Reason = "the machine has no owner"
		return v
	}
	key := memberKey{t.OrgID, *m.owner}
	switch s.roles[key] {
	case db.RoleOwner, db.RoleAdmin:
		v.Allowed, v.Reason = true, "its owner is an "+string(s.roles[key])+" of the organisation"
		return v
	case db.RoleMember:
	default:
		v.Reason = "its owner is not a member of the organisation"
		return v
	}
	if s.mayUse(key, t) {
		v.Allowed, v.Reason = true, "its owner may use what the route leads to"
		return v
	}
	v.Reason = "its owner has no access to what the route leads to"
	return v
}

// mayUse is whether a member is granted what the route leads to, and has not
// switched off reaching it from their machines.
func (s *Snapshot) mayUse(key memberKey, t Target) bool {
	granted := s.grants[key]
	if granted[grantKey{db.ResourceRoute, t.RouteID}] {
		return true
	}
	projects := []uuid.UUID{t.ProjectID}
	if p, ok := s.parent[t.ProjectID]; ok {
		projects = append(projects, p)
	}
	svc, isService := s.services[t.ServiceID]
	if !isService {
		// A node or address target belongs to its project alone.
		for _, p := range projects {
			if granted[grantKey{db.ResourceProject, p}] {
				return true
			}
		}
		return false
	}
	// The same order the mesh policy reads a member's grant and its switch
	// in: the service, its stack, its project, the project a level belongs to.
	keys := []grantKey{{db.ResourceService, t.ServiceID}}
	if svc.stack != nil {
		keys = append(keys, grantKey{db.ResourceStack, *svc.stack})
	}
	for _, p := range projects {
		keys = append(keys, grantKey{db.ResourceProject, p})
	}
	ok := false
	for _, k := range keys {
		if granted[k] {
			ok = true
			break
		}
	}
	if !ok {
		return false
	}
	// A stack or project switched on turns on what it holds but its
	// databases, each switched on by itself; switched off, it turns off all.
	for i, k := range keys {
		if on, chosen := s.reach[key][k]; chosen {
			if on && i > 0 && svc.database {
				continue
			}
			return on
		}
	}
	// Nobody chose: seeing a database in the console does not mean opening
	// it from a laptop, so a database is off; anything else is on.
	return !svc.database
}

// ruleOpens is whether a network rule lets the machine reach a gateway's web
// port, which is every internal route at once.
func (s *Snapshot) ruleOpens(m machine) bool {
	for _, r := range s.rules {
		if r.OrganizationID != m.org || !s.gateways[r.ToNodeID] || !webPort(r.Ports) {
			continue
		}
		switch r.FromKind {
		case db.MeshRuleFromAll:
			return true
		case db.MeshRuleFromMachine:
			if r.FromID != nil && *r.FromID == m.id {
				return true
			}
		case db.MeshRuleFromPerson:
			if r.FromID != nil && m.owner != nil && *r.FromID == *m.owner {
				return true
			}
		}
	}
	return false
}

// webPort is whether a rule's ports include 443, read as the mesh policy
// reads them; empty is every port.
func webPort(ports string) bool {
	fields := strings.FieldsFunc(ports, func(r rune) bool { return r == ',' || r == ' ' })
	if len(fields) == 0 {
		return true
	}
	for _, f := range fields {
		var p int
		if _, err := fmt.Sscanf(f, "%d", &p); err == nil && p == 443 {
			return true
		}
	}
	return false
}

var meshRanges = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{"100.64.0.0/10", "fd7a:115c:a1e0::/48"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

// onMesh is whether an address is one Headscale hands out.
func onMesh(ip net.IP) bool {
	for _, n := range meshRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Load reads a snapshot from the database.
func Load(database *gorm.DB) (*Snapshot, error) {
	s := &Snapshot{
		machines: map[string]machine{},
		roles:    map[memberKey]db.MemberRole{},
		grants:   map[memberKey]map[grantKey]bool{},
		reach:    map[memberKey]map[grantKey]bool{},
		services: map[uuid.UUID]service{},
		parent:   map[uuid.UUID]uuid.UUID{},
		gateways: map[uuid.UUID]bool{},
	}
	var state db.MeshPolicyState
	if err := database.Where("id = 1").Limit(1).Find(&state).Error; err != nil {
		return nil, err
	}
	s.Enforced = state.Enforced

	var nodes []db.Node
	if err := database.Where("removal_requested_at IS NULL").Find(&nodes).Error; err != nil {
		return nil, err
	}
	for _, n := range nodes {
		kind := "cluster"
		switch {
		case n.K3sRole == db.K3sRoleServer:
			kind = "gateway"
			s.gateways[n.ID] = true
		case n.MeshRole == db.MeshRoleMesh:
			kind = "connected"
		}
		if ip := net.ParseIP(n.TailscaleIP); ip != nil {
			s.machines[ip.String()] = machine{id: n.ID, org: n.OrganizationID, name: n.Name, kind: kind, owner: n.OwnerID}
		}
	}

	var members []db.OrganizationMember
	if err := database.Find(&members).Error; err != nil {
		return nil, err
	}
	for _, m := range members {
		s.roles[memberKey{m.OrganizationID, m.UserID}] = m.Role
	}

	var perms []db.ResourcePermission
	if err := database.Where("resource_type IN ?", []db.ResourceType{db.ResourceService, db.ResourceStack,
		db.ResourceProject, db.ResourceRoute}).Find(&perms).Error; err != nil {
		return nil, err
	}
	for _, p := range perms {
		k := memberKey{p.OrganizationID, p.UserID}
		if s.grants[k] == nil {
			s.grants[k] = map[grantKey]bool{}
		}
		s.grants[k][grantKey{p.ResourceType, p.ResourceID}] = true
	}

	var reach []db.MeshReach
	if err := database.Find(&reach).Error; err != nil {
		return nil, err
	}
	for _, r := range reach {
		k := memberKey{r.OrganizationID, r.UserID}
		if s.reach[k] == nil {
			s.reach[k] = map[grantKey]bool{}
		}
		s.reach[k][grantKey{r.ResourceType, r.ResourceID}] = r.Reach
	}

	var services []db.Service
	if err := database.Select("id", "project_id", "stack_id", "type").Find(&services).Error; err != nil {
		return nil, err
	}
	for _, svc := range services {
		s.services[svc.ID] = service{project: svc.ProjectID, stack: svc.StackID, database: svc.Type == db.ServiceTypeDatabase}
	}

	var levels []db.Project
	if err := database.Select("id", "parent_project_id").Where("parent_project_id IS NOT NULL").Find(&levels).Error; err != nil {
		return nil, err
	}
	for _, p := range levels {
		s.parent[p.ID] = *p.ParentProjectID
	}

	if err := database.Find(&s.rules).Error; err != nil {
		return nil, err
	}
	return s, nil
}
