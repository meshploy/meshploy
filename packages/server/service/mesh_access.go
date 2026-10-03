package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MeshAccessService builds the mesh's access policy from the database, and
// gives it to Headscale when it is enforced.
type MeshAccessService struct {
	db        *gorm.DB
	headscale *HeadscaleService // nil without a mesh: nothing to give a policy to
}

// Policy is the install's policy: every organisation's machines, since one
// Headscale serves them all.
func (s *MeshAccessService) Policy(ctx context.Context) (MeshPolicy, error) {
	in, err := s.inputs(ctx)
	if err != nil {
		return MeshPolicy{}, err
	}
	return BuildMeshPolicy(in), nil
}

func (s *MeshAccessService) inputs(ctx context.Context) (MeshInputs, error) {
	q := s.db.WithContext(ctx)
	var in MeshInputs

	var nodes []db.Node
	if err := q.Where("removal_requested_at IS NULL").Find(&nodes).Error; err != nil {
		return in, err
	}
	// Headscale knows each machine's IPv6 address, and the machines Meshploy
	// does not know at all.
	var peers []HeadscaleNode
	if s.headscale != nil {
		peers, _ = s.headscale.ListNodes(ctx)
	}
	v6 := map[string]string{}
	known := map[string]bool{}
	for _, n := range nodes {
		known[n.TailscaleIP] = true
	}
	for _, p := range peers {
		var v4, v6addr string
		for _, ip := range p.IPAddresses {
			if strings.Contains(ip, ":") {
				v6addr = ip
			} else {
				v4 = ip
			}
		}
		if v4 != "" && v6addr != "" {
			v6[v4] = v6addr
		}
		if !known[v4] {
			name := p.GivenName
			if name == "" {
				name = p.Name
			}
			in.Unknown = append(in.Unknown, MeshUnknown{Name: name, IPs: p.IPAddresses})
		}
	}

	for _, n := range nodes {
		kind := MeshCluster
		switch {
		case n.K3sRole == db.K3sRoleServer:
			kind = MeshGateway
		case n.MeshRole == db.MeshRoleMesh:
			kind = MeshConnected
		}
		in.Nodes = append(in.Nodes, MeshNode{ID: n.ID, OrgID: n.OrganizationID, Name: n.Name, IP: n.TailscaleIP, IP6: v6[n.TailscaleIP], Kind: kind, OwnerID: n.OwnerID})
	}

	var members []struct {
		OrganizationID uuid.UUID
		UserID         uuid.UUID
		Username       string
		Role           db.MemberRole
	}
	if err := q.Model(&db.OrganizationMember{}).
		Select("organization_members.organization_id, organization_members.user_id, users.username, organization_members.role").
		Joins("JOIN users ON users.id = organization_members.user_id").
		Where("users.kind = ?", db.UserHuman).
		Scan(&members).Error; err != nil {
		return in, err
	}
	var rules []db.MeshRule
	if err := q.Find(&rules).Error; err != nil {
		return in, err
	}
	for _, r := range rules {
		in.Rules = append(in.Rules, MeshNetRule{ID: r.ID, OrgID: r.OrganizationID, FromKind: r.FromKind, FromID: r.FromID,
			ToNodeID: r.ToNodeID, Ports: ParsePorts(r.Ports), Note: r.Note})
	}

	// Only members whose machines are on the mesh need their grants worked out.
	owners := map[uuid.UUID]bool{}
	for _, n := range nodes {
		if n.OwnerID != nil && n.MeshRole == db.MeshRoleMesh {
			owners[*n.OwnerID] = true
		}
	}
	for _, m := range members {
		in.Members = append(in.Members, MeshMember{OrgID: m.OrganizationID, UserID: m.UserID, Name: m.Username, Role: m.Role})
		if owners[m.UserID] && m.Role == db.RoleMember {
			grants, err := s.grantsOf(ctx, m.OrganizationID, m.UserID)
			if err != nil {
				return in, err
			}
			in.Grants = append(in.Grants, grants...)
		}
	}
	return in, nil
}

// ReachesByDefault is whether a grant on svc reaches it from the member's
// machines when nobody chose: seeing a database in the console does not mean
// connecting to it from a laptop, so a database is off; anything else is on.
func ReachesByDefault(svc *db.Service) bool { return svc.Type != db.ServiceTypeDatabase }

type reachKey struct {
	t  db.ResourceType
	id uuid.UUID
}

// choices are the reach switches a member's grants carry, where someone chose.
func (s *MeshAccessService) choices(ctx context.Context, orgID, userID uuid.UUID) (map[reachKey]bool, error) {
	var rows []db.MeshReach
	if err := s.db.WithContext(ctx).Where("organization_id = ? AND user_id = ?", orgID, userID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[reachKey]bool, len(rows))
	for _, r := range rows {
		out[reachKey{r.ResourceType, r.ResourceID}] = r.Reach
	}
	return out, nil
}

// reaches is whether a member reaches svc on the mesh: the choice on the
// service, else on its stack, else on its project (or the project a level
// belongs to), else the default. chosen says a choice decided it.
//
// A stack or project switched on turns on what it holds but its databases:
// a database is reached only when switched on itself, since connecting to one
// from a laptop is a decision about that database. Switched off, a stack or
// project turns off everything in it.
func reaches(svc db.Service, choices map[reachKey]bool) (reach, chosen bool) {
	keys := []reachKey{{db.ResourceService, svc.ID}}
	if svc.StackID != nil {
		keys = append(keys, reachKey{db.ResourceStack, *svc.StackID})
	}
	keys = append(keys, reachKey{db.ResourceProject, svc.ProjectID})
	if svc.Project.ParentProjectID != nil {
		keys = append(keys, reachKey{db.ResourceProject, *svc.Project.ParentProjectID})
	}
	for i, k := range keys {
		if v, ok := choices[k]; ok {
			if v && i > 0 && svc.Type == db.ServiceTypeDatabase {
				continue
			}
			return v, true
		}
	}
	return ReachesByDefault(&svc), false
}

// grantedServices are the services a member may view: granted directly,
// through their project (and its levels) or through their stack.
func (s *MeshAccessService) grantedServices(ctx context.Context, orgID, userID uuid.UUID) ([]db.Service, error) {
	q := s.db.WithContext(ctx)
	var perms []db.ResourcePermission
	if err := q.Where("organization_id = ? AND user_id = ? AND resource_type IN ?", orgID, userID,
		[]db.ResourceType{db.ResourceService, db.ResourceProject, db.ResourceStack}).Find(&perms).Error; err != nil {
		return nil, err
	}
	var serviceIDs, projectIDs, stackIDs []uuid.UUID
	for _, p := range perms {
		switch p.ResourceType {
		case db.ResourceService:
			serviceIDs = append(serviceIDs, p.ResourceID)
		case db.ResourceProject:
			projectIDs = append(projectIDs, p.ResourceID)
		case db.ResourceStack:
			stackIDs = append(stackIDs, p.ResourceID)
		}
	}
	if len(projectIDs) > 0 {
		// A grant on a project covers its levels.
		var levels []uuid.UUID
		q.Model(&db.Project{}).Where("parent_project_id IN ?", projectIDs).Pluck("id", &levels)
		projectIDs = append(projectIDs, levels...)
	}
	var services []db.Service
	sq := q.Preload("Project").Where("id IN ?", append(serviceIDs, uuid.Nil))
	if len(projectIDs) > 0 {
		sq = sq.Or("project_id IN ?", projectIDs)
	}
	if len(stackIDs) > 0 {
		sq = sq.Or("stack_id IN ?", stackIDs)
	}
	err := sq.Find(&services).Error
	return services, err
}

// meshPorts are where svc listens on the mesh: its NodePorts, on every cluster
// machine, and its TCP routes' ports on the gateway.
func (s *MeshAccessService) meshPorts(ctx context.Context, svcID uuid.UUID) (nodePorts, gatewayPorts []int) {
	q := s.db.WithContext(ctx)
	q.Model(&db.ServicePort{}).Where("service_id = ? AND is_public AND node_port > 0", svcID).Order("node_port").Pluck("node_port", &nodePorts)
	q.Model(&db.TCPRoute{}).Where("service_id = ? AND zone <> ?", svcID, db.TCPZoneLocal).Order("gateway_port").Pluck("gateway_port", &gatewayPorts)
	return nodePorts, gatewayPorts
}

// grantsOf is what a member may view that listens on the mesh and that their
// grants let them reach from their machines.
func (s *MeshAccessService) grantsOf(ctx context.Context, orgID, userID uuid.UUID) ([]MeshGrant, error) {
	services, err := s.grantedServices(ctx, orgID, userID)
	if err != nil {
		return nil, err
	}
	choices, err := s.choices(ctx, orgID, userID)
	if err != nil {
		return nil, err
	}
	var out []MeshGrant
	for _, svc := range services {
		if ok, _ := reaches(svc, choices); !ok {
			continue
		}
		nodePorts, gatewayPorts := s.meshPorts(ctx, svc.ID)
		if len(nodePorts) == 0 && len(gatewayPorts) == 0 {
			continue
		}
		out = append(out, MeshGrant{UserID: userID, What: fmt.Sprintf("%s in %s", svc.Name, svc.Project.Name),
			NodePorts: nodePorts, GatewayPorts: gatewayPorts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].What < out[j].What })
	return out, nil
}

// ReachPort is one way to reach a service from a member's machine.
type ReachPort struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`      // the service's own port
	MeshPort int    `json:"mesh_port"` // where it answers on the mesh
	// On says where MeshPort answers: "cluster" for a NodePort, on every
	// cluster machine; "gateway" for a TCP route.
	On string `json:"on"`
}

// ServiceReach is a service under a grant, and whether the member reaches it.
type ServiceReach struct {
	ServiceID uuid.UUID   `json:"service_id"`
	Name      string      `json:"name"`
	Database  bool        `json:"database"`
	Reach     bool        `json:"reach"`
	Ports     []ReachPort `json:"ports"`
	// Routes are its internal routes, which open to the member's machines
	// with its ports.
	Routes []string `json:"routes"`
}

// MemberReach is a member granted a resource, as the Access page shows them:
// the switch, what it opens, and the machines it opens it to.
type MemberReach struct {
	UserID   uuid.UUID      `json:"user_id"`
	Reach    bool           `json:"reach"`
	Chosen   bool           `json:"chosen"` // false while the default decides
	Services []ServiceReach `json:"services"`
	Machines []string       `json:"machines"`
}

// ErrReachResource is a reach switch on something that is not a service,
// stack or project.
var ErrReachResource = errors.New("reach is set on a service, a stack or a project")

// ReachOn is every member granted a service or stack, with whether their
// machines reach it. Owners and admins are left out: they reach everything.
func (s *MeshAccessService) ReachOn(ctx context.Context, orgID uuid.UUID, resourceType db.ResourceType, resourceID uuid.UUID) ([]MemberReach, error) {
	q := s.db.WithContext(ctx)
	var scope []db.Service
	switch resourceType {
	case db.ResourceService:
		q.Preload("Project").Where("id = ?", resourceID).Find(&scope)
	case db.ResourceStack:
		q.Preload("Project").Where("stack_id = ?", resourceID).Order("name").Find(&scope)
	default:
		return nil, ErrReachResource
	}
	inScope := map[uuid.UUID]bool{}
	for _, svc := range scope {
		inScope[svc.ID] = true
	}

	var members []db.OrganizationMember
	if err := q.Joins("JOIN users ON users.id = organization_members.user_id").
		Where("organization_members.organization_id = ? AND organization_members.role = ? AND users.kind = ?", orgID, db.RoleMember, db.UserHuman).
		Find(&members).Error; err != nil {
		return nil, err
	}
	out := []MemberReach{}
	for _, m := range members {
		granted, err := s.grantedServices(ctx, orgID, m.UserID)
		if err != nil {
			return nil, err
		}
		choices, err := s.choices(ctx, orgID, m.UserID)
		if err != nil {
			return nil, err
		}
		r := MemberReach{UserID: m.UserID, Services: []ServiceReach{}, Machines: []string{}}
		all := true
		for _, svc := range granted {
			if !inScope[svc.ID] {
				continue
			}
			reach, _ := reaches(svc, choices)
			all = all && reach
			r.Services = append(r.Services, ServiceReach{ServiceID: svc.ID, Name: svc.Name,
				Database: svc.Type == db.ServiceTypeDatabase, Reach: reach, Ports: s.reachPorts(ctx, svc.ID),
				Routes: append([]string{}, s.internalHostnames(ctx, "route_targets.service_id = ?", svc.ID)...)})
		}
		if len(r.Services) == 0 {
			continue // not granted this
		}
		sort.Slice(r.Services, func(i, j int) bool { return r.Services[i].Name < r.Services[j].Name })
		r.Reach = all
		if v, ok := choices[reachKey{resourceType, resourceID}]; ok {
			r.Reach, r.Chosen = v, true
		}
		q.Model(&db.Node{}).Where("organization_id = ? AND owner_id = ? AND mesh_role = ?", orgID, m.UserID, db.MeshRoleMesh).
			Order("name").Pluck("name", &r.Machines)
		out = append(out, r)
	}
	return out, nil
}

func (s *MeshAccessService) reachPorts(ctx context.Context, svcID uuid.UUID) []ReachPort {
	q := s.db.WithContext(ctx)
	out := []ReachPort{}
	var ports []db.ServicePort
	q.Where("service_id = ? AND is_public AND node_port > 0", svcID).Order("port").Find(&ports)
	for _, p := range ports {
		out = append(out, ReachPort{Name: p.Name, Port: p.Port, MeshPort: p.NodePort, On: "cluster"})
	}
	var routes []db.TCPRoute
	q.Where("service_id = ? AND zone <> ?", svcID, db.TCPZoneLocal).Order("gateway_port").Find(&routes)
	for _, r := range routes {
		out = append(out, ReachPort{Name: "tcp", Port: r.ServicePort, MeshPort: r.GatewayPort, On: "gateway"})
	}
	return out
}

// SetReach records whether a member's machines reach a service, stack or
// project they are granted.
func (s *MeshAccessService) SetReach(ctx context.Context, orgID, userID uuid.UUID, resourceType db.ResourceType, resourceID uuid.UUID, reach bool) error {
	switch resourceType {
	case db.ResourceService, db.ResourceStack, db.ResourceProject:
	default:
		return ErrReachResource
	}
	row := db.MeshReach{OrganizationID: orgID, UserID: userID, ResourceType: resourceType, ResourceID: resourceID, Reach: reach}
	row.ID = uuid.New()
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "resource_type"}, {Name: "resource_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"reach", "updated_at"}),
	}).Create(&row).Error
}

// ── Enforcing ────────────────────────────────────────────────────────────────

// State is whether the policy is enforced and how giving it to Headscale
// last went.
func (s *MeshAccessService) State(ctx context.Context) (db.MeshPolicyState, error) {
	var st db.MeshPolicyState
	err := s.db.WithContext(ctx).Where("id = 1").Limit(1).Find(&st).Error
	return st, err
}

// SetEnforced switches the mesh between obeying the generated policy and
// letting every machine reach every other. Headscale is given the result at
// once, through the mesh count.
func (s *MeshAccessService) SetEnforced(ctx context.Context, enforced bool, by uuid.UUID) error {
	now := time.Now()
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"enforced", "enforced_by", "changed_at"}),
	}).Create(&db.MeshPolicyState{ID: 1, Enforced: enforced, EnforcedBy: &by, ChangedAt: &now}).Error
}

// Sync gives Headscale the policy it should have now: the generated one when
// enforced, every machine reaching every other when not. It does nothing when
// Headscale already has that policy, and records how the attempt went.
func (s *MeshAccessService) Sync(ctx context.Context) {
	if s.headscale == nil {
		return
	}
	st, err := s.State(ctx)
	if err != nil {
		log.Printf("mesh policy: state: %v", err)
		return
	}
	// Never enforced and never given a policy: Headscale has none, which lets
	// every machine reach every other already. Nothing to say.
	if !st.Enforced && st.AppliedHash == "" {
		return
	}
	want := MeshAllowAll
	if st.Enforced {
		p, err := s.Policy(ctx)
		if err != nil {
			log.Printf("mesh policy: build: %v", err)
			return
		}
		b, err := p.Headscale()
		if err != nil {
			log.Printf("mesh policy: render: %v", err)
			return
		}
		want = string(b)
	}
	sum := sha256.Sum256([]byte(want))
	hash := hex.EncodeToString(sum[:])
	if hash == st.AppliedHash && st.LastError == "" {
		return
	}
	update := map[string]any{"last_error": ""}
	if err := s.headscale.SetPolicy(ctx, want); err != nil {
		update["last_error"] = err.Error()
		log.Printf("mesh policy: %v", err)
	} else {
		now := time.Now()
		update["applied_hash"], update["applied_at"] = hash, &now
	}
	// Only the outcome changes here: the row is created first if it is not
	// there, without moving whether it is enforced.
	s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&db.MeshPolicyState{ID: 1})
	if update["last_error"] != st.LastError || update["applied_hash"] != nil {
		s.db.WithContext(ctx).Model(&db.MeshPolicyState{}).Where("id = 1").Updates(update)
	}
}

// Follow keeps Headscale's policy in step with the database for the life of
// the process: within a second or two of any change it depends on, and every
// few minutes regardless.
func (s *MeshAccessService) Follow(ctx context.Context) {
	if s.headscale == nil {
		return
	}
	s.Sync(ctx)
	db.FollowEdgeVersion(s.db, db.EdgeMesh, 5*time.Minute, func() { s.Sync(ctx) })
}

// ParsePorts reads a network rule's ports: "22, 445" or "" for every port.
// Anything that is not a port number is left out.
func ParsePorts(s string) []int {
	var out []int
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		var p int
		if _, err := fmt.Sscanf(f, "%d", &p); err == nil && p > 0 && p < 65536 {
			out = append(out, p)
		}
	}
	sort.Ints(out)
	return out
}
