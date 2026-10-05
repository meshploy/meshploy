package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
)

// The Access page's rules: every grant and every network rule in one list,
// each as Source -> Destination -> Ports. A grant is the same grant a
// resource's Access tab shows, made or removed from either place; a network
// rule exists only here, for what is not a Meshploy resource.

// AccessEnd is a rule's source or destination.
type AccessEnd struct {
	Kind      string `json:"kind"` // person, machine, all, service, stack, project, job, route
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Email     string `json:"email,omitempty"`      // a person's, to tell people apart
	Member    bool   `json:"member,omitempty"`     // a person in the organisation, with a page of their own
	ProjectID string `json:"project_id,omitempty"` // where a resource lives, for a link
	Database  bool   `json:"database,omitempty"`
	// A derived grant's source: what it is called as a kind, where it is
	// managed, and how many people it covers.
	Label  string `json:"label,omitempty"`
	Link   string `json:"link,omitempty"`
	People int    `json:"people,omitempty"`
}

// AccessRule is one line of the Access page.
type AccessRule struct {
	ID   string    `json:"id"`
	Kind string    `json:"kind"` // grant or network
	From AccessEnd `json:"from"`
	To   AccessEnd `json:"to"`
	// Ports are the ports reached: a network rule's own, or where what a
	// grant covers answers on the mesh. Empty on a network rule is every port.
	Ports []int `json:"ports"`
	// Opens is what a grant opens from its person's machines: ports and
	// internal routes, of what it covers that they reach. Off counts what it
	// covers that is switched off (a database until switched on).
	Opens []AccessOpen `json:"opens,omitempty"`
	Off   int          `json:"off,omitempty"`
	// Actions are a grant's console permissions.
	Actions []string `json:"actions,omitempty"`
	// Reach is whether a grant's person reaches it from their machines; nil
	// while the default decides on a stack or project, whose services differ.
	Reach *bool  `json:"reach,omitempty"`
	Note  string `json:"note,omitempty"`
}

// AccessOpen is one thing a grant opens from its person's machines.
type AccessOpen struct {
	Kind    string `json:"kind"`              // port or route
	Service string `json:"service,omitempty"` // what answers, when the grant covers several
	// Port is where it answers on the mesh: a NodePort on every cluster
	// machine (On "cluster") or a TCP route's port on the gateway (On
	// "gateway").
	Port int    `json:"port,omitempty"`
	On   string `json:"on,omitempty"`
	// Hostname is an internal route's, which the proxy opens to them.
	Hostname string `json:"hostname,omitempty"`
}

// AccessRuleInput is a network rule to add.
type AccessRuleInput struct {
	FromKind string     `json:"from_kind"`
	FromID   *uuid.UUID `json:"from_id,omitempty"`
	ToNodeID uuid.UUID  `json:"to_node_id"`
	Ports    string     `json:"ports"`
	Note     string     `json:"note"`
}

var ErrAccessRule = errors.New("a network rule needs a person, a connected machine or every connected machine as its source, and a machine of this organisation as its destination")

// ErrRuleFromCluster is a network rule from the gateway or a cluster machine:
// those reach every machine already, so the rule would add nothing.
var ErrRuleFromCluster = errors.New("the gateway and the cluster's machines already reach every machine, so a rule from one adds nothing: a machine source is a connected (mesh-only) machine")

// Rules are an organisation's grants and network rules.
func (s *MeshAccessService) Rules(ctx context.Context, orgID uuid.UUID) ([]AccessRule, error) {
	q := s.db.WithContext(ctx)
	out := []AccessRule{}

	// Everyone a grant or rule names: members, and people an app was shared
	// with, who hold a grant without being members.
	var users []db.User
	q.Where("id IN (?) OR id IN (?) OR id IN (?)",
		q.Model(&db.OrganizationMember{}).Select("user_id").Where("organization_id = ?", orgID),
		q.Model(&db.ResourcePermission{}).Select("user_id").Where("organization_id = ?", orgID),
		q.Model(&db.MeshRule{}).Select("from_id").Where("organization_id = ? AND from_kind = ?", orgID, db.MeshRuleFromPerson),
	).Find(&users)
	userName, userEmail := map[uuid.UUID]string{}, map[uuid.UUID]string{}
	for _, u := range users {
		userName[u.ID], userEmail[u.ID] = u.Username, u.Email
	}
	var memberIDs []uuid.UUID
	q.Model(&db.OrganizationMember{}).Where("organization_id = ?", orgID).Pluck("user_id", &memberIDs)
	member := map[uuid.UUID]bool{}
	for _, id := range memberIDs {
		member[id] = true
	}
	var nodes []db.Node
	q.Where("organization_id = ?", orgID).Find(&nodes)
	nodeName := map[uuid.UUID]string{}
	for _, n := range nodes {
		nodeName[n.ID] = n.Name
	}

	// Grants, one line per person and resource with its actions together. A
	// grant derived from another source is one line for the source, however
	// many people it covers: it is given and removed there, not per person.
	var perms []db.ResourcePermission
	if err := q.Where("organization_id = ?", orgID).Order("created_at").Find(&perms).Error; err != nil {
		return nil, err
	}
	type key struct {
		user   uuid.UUID // a direct grant's person
		source string    // a derived grant's source
		t      db.ResourceType
		id     uuid.UUID
	}
	actions := map[key][]string{}
	people := map[key]map[uuid.UUID]bool{}
	var order []key
	for _, p := range perms {
		k := key{t: p.ResourceType, id: p.ResourceID}
		if p.Source == "" {
			k.user = p.UserID
		} else {
			k.source = p.Source
		}
		if _, ok := actions[k]; !ok {
			order = append(order, k)
			people[k] = map[uuid.UUID]bool{}
		}
		if !contains(actions[k], string(p.Action)) {
			actions[k] = append(actions[k], string(p.Action))
		}
		people[k][p.UserID] = true
	}
	choicesOf := map[uuid.UUID]map[reachKey]bool{}
	for _, k := range order {
		to, ok := s.resourceEnd(ctx, k.t, k.id)
		if !ok {
			continue // a grant on something deleted
		}
		if k.source != "" {
			via := grantVia(ctx, k.source)
			r := AccessRule{ID: "source:" + k.source + ":" + string(k.t) + ":" + k.id.String(), Kind: "grant",
				From: AccessEnd{Kind: via.Kind, ID: via.ID, Name: via.Name, Label: via.Label, Link: via.Link, People: len(people[k])},
				To:   to, Ports: []int{}, Actions: actions[k]}
			// What it opens from a covered person's machines, before anyone's
			// own switches.
			r.Opens, r.Off = s.opens(ctx, k.t, k.id, nil)
			for _, o := range r.Opens {
				if o.Kind == "port" {
					r.Ports = append(r.Ports, o.Port)
				}
			}
			out = append(out, r)
			continue
		}
		r := AccessRule{ID: "grant:" + k.user.String() + ":" + string(k.t) + ":" + k.id.String(), Kind: "grant",
			From: AccessEnd{Kind: "person", ID: k.user.String(), Name: userName[k.user], Email: userEmail[k.user], Member: member[k.user]}, To: to, Ports: []int{}, Actions: actions[k]}
		if _, ok := choicesOf[k.user]; !ok {
			choicesOf[k.user], _ = s.choices(ctx, orgID, k.user)
		}
		choices := choicesOf[k.user]
		switch k.t {
		case db.ResourceService:
			var svc db.Service
			if q.Preload("Project").First(&svc, "id = ?", k.id).Error == nil {
				reach, _ := reaches(svc, choices)
				r.Reach = &reach
			}
		case db.ResourceStack, db.ResourceProject:
			if v, ok := choices[reachKey{k.t, k.id}]; ok {
				r.Reach = &v
			}
		}
		// Someone outside the organisation has no machines on the mesh, so a
		// grant of theirs opens nothing there.
		if member[k.user] {
			r.Opens, r.Off = s.opens(ctx, k.t, k.id, choices)
			for _, o := range r.Opens {
				if o.Kind == "port" {
					r.Ports = append(r.Ports, o.Port)
				}
			}
		}
		out = append(out, r)
	}

	var rules []db.MeshRule
	if err := q.Where("organization_id = ?", orgID).Order("created_at").Find(&rules).Error; err != nil {
		return nil, err
	}
	for _, mr := range rules {
		from := AccessEnd{Kind: mr.FromKind, Name: "Every machine"}
		if mr.FromID != nil {
			from.ID = mr.FromID.String()
			if mr.FromKind == db.MeshRuleFromPerson {
				from.Name, from.Email, from.Member = userName[*mr.FromID], userEmail[*mr.FromID], member[*mr.FromID]
			} else {
				from.Name = nodeName[*mr.FromID]
			}
		}
		ports := ParsePorts(mr.Ports)
		if ports == nil {
			ports = []int{}
		}
		out = append(out, AccessRule{ID: mr.ID.String(), Kind: "network", From: from,
			To: AccessEnd{Kind: "machine", ID: mr.ToNodeID.String(), Name: nodeName[mr.ToNodeID]}, Ports: ports, Note: mr.Note})
	}
	return out, nil
}

// opens is what a grant on a resource opens from a member's machines, given
// their switches: the ports and internal routes of each service it covers
// that they reach, and, for a project or a route, the internal routes it
// covers that lead to a node or an address. off counts the services it covers
// that are switched off.
func (s *MeshAccessService) opens(ctx context.Context, t db.ResourceType, id uuid.UUID, choices map[reachKey]bool) (out []AccessOpen, off int) {
	q := s.db.WithContext(ctx)
	var services []db.Service
	var projects []uuid.UUID
	switch t {
	case db.ResourceService:
		q.Preload("Project").Where("id = ?", id).Find(&services)
	case db.ResourceStack:
		q.Preload("Project").Where("stack_id = ?", id).Order("name").Find(&services)
	case db.ResourceProject:
		// A grant on a project covers its levels.
		projects = []uuid.UUID{id}
		var levels []uuid.UUID
		q.Model(&db.Project{}).Where("parent_project_id = ?", id).Pluck("id", &levels)
		projects = append(projects, levels...)
		q.Preload("Project").Where("project_id IN ?", projects).Order("name").Find(&services)
	case db.ResourceRoute:
		var r db.Route
		if q.First(&r, "id = ? AND zone = ?", id, db.RouteZoneInternal).Error == nil && r.Hostname != "" {
			out = append(out, AccessOpen{Kind: "route", Hostname: r.Hostname})
		}
		return out, 0
	default:
		return nil, 0
	}
	several := len(services) > 1
	for _, svc := range services {
		if ok, _ := reaches(svc, choices); !ok {
			off++
			continue
		}
		name := ""
		if several {
			name = svc.Name
		}
		for _, p := range s.reachPorts(ctx, svc.ID) {
			out = append(out, AccessOpen{Kind: "port", Service: name, Port: p.MeshPort, On: p.On})
		}
		for _, h := range s.internalHostnames(ctx, "route_targets.service_id = ?", svc.ID) {
			out = append(out, AccessOpen{Kind: "route", Service: name, Hostname: h})
		}
	}
	if len(projects) > 0 {
		for _, h := range s.internalHostnames(ctx, "route_targets.service_id IS NULL AND routes.project_id IN ?", projects) {
			out = append(out, AccessOpen{Kind: "route", Hostname: h})
		}
	}
	return out, off
}

// internalHostnames are the published internal routes with a target matching
// where, a condition on route_targets and routes.
func (s *MeshAccessService) internalHostnames(ctx context.Context, where string, args ...any) []string {
	var out []string
	s.db.WithContext(ctx).Model(&db.Route{}).
		Joins("JOIN route_targets ON route_targets.route_id = routes.id").
		Where("routes.zone = ? AND routes.published AND routes.hostname <> ''", db.RouteZoneInternal).
		Where(where, args...).
		Distinct().Order("routes.hostname").Pluck("routes.hostname", &out)
	return out
}

// resourceEnd names a granted resource, or says it is gone.
func (s *MeshAccessService) resourceEnd(ctx context.Context, t db.ResourceType, id uuid.UUID) (AccessEnd, bool) {
	q := s.db.WithContext(ctx)
	end := AccessEnd{Kind: string(t), ID: id.String()}
	switch t {
	case db.ResourceService:
		var svc db.Service
		if q.Preload("Project").First(&svc, "id = ?", id).Error != nil {
			return end, false
		}
		end.Name, end.ProjectID, end.Database = svc.Name+" in "+svc.Project.Name, svc.ProjectID.String(), svc.Type == db.ServiceTypeDatabase
	case db.ResourceProject:
		var p db.Project
		if q.First(&p, "id = ?", id).Error != nil {
			return end, false
		}
		end.Name, end.ProjectID = p.Name, p.ID.String()
	case db.ResourceStack:
		var st db.Stack
		if q.First(&st, "id = ?", id).Error != nil {
			return end, false
		}
		end.Name, end.ProjectID = st.Name, st.ProjectID.String()
	case db.ResourceJob:
		var j db.Job
		if q.First(&j, "id = ?", id).Error != nil {
			return end, false
		}
		end.Name, end.ProjectID = j.Name, j.ProjectID.String()
	case db.ResourceRoute:
		var r db.Route
		if q.First(&r, "id = ?", id).Error != nil {
			return end, false
		}
		end.Name, end.ProjectID = r.Hostname, r.ProjectID.String()
	default:
		return end, false
	}
	return end, true
}

// validRule checks a network rule against its organisation.
func (s *MeshAccessService) validRule(ctx context.Context, orgID uuid.UUID, in AccessRuleInput) error {
	q := s.db.WithContext(ctx)
	var n int64
	q.Model(&db.Node{}).Where("id = ? AND organization_id = ?", in.ToNodeID, orgID).Count(&n)
	if n == 0 {
		return ErrAccessRule
	}
	switch in.FromKind {
	case db.MeshRuleFromAll:
		return nil
	case db.MeshRuleFromMachine:
		if in.FromID == nil {
			return ErrAccessRule
		}
		var from db.Node
		if err := q.Where("id = ? AND organization_id = ?", *in.FromID, orgID).First(&from).Error; err != nil {
			return ErrAccessRule
		}
		if from.MeshRole != db.MeshRoleMesh {
			return ErrRuleFromCluster
		}
		return nil
	case db.MeshRuleFromPerson:
		if in.FromID == nil {
			return ErrAccessRule
		}
		q.Model(&db.OrganizationMember{}).Where("organization_id = ? AND user_id = ?", orgID, *in.FromID).Count(&n)
	default:
		return ErrAccessRule
	}
	if n == 0 {
		return ErrAccessRule
	}
	return nil
}

// AddRule adds a network rule.
func (s *MeshAccessService) AddRule(ctx context.Context, orgID, by uuid.UUID, in AccessRuleInput) (*db.MeshRule, error) {
	if err := s.validRule(ctx, orgID, in); err != nil {
		return nil, err
	}
	row := db.MeshRule{OrganizationID: orgID, FromKind: in.FromKind, FromID: in.FromID, ToNodeID: in.ToNodeID,
		Ports: portsText(ParsePorts(in.Ports)), Note: strings.TrimSpace(in.Note), CreatedBy: &by}
	row.ID = uuid.New()
	if in.FromKind == db.MeshRuleFromAll {
		row.FromID = nil
	}
	return &row, s.db.WithContext(ctx).Create(&row).Error
}

// UpdateRule changes a network rule: its source, machine, ports or note.
func (s *MeshAccessService) UpdateRule(ctx context.Context, orgID, id uuid.UUID, in AccessRuleInput) error {
	if err := s.validRule(ctx, orgID, in); err != nil {
		return err
	}
	fromID := in.FromID
	if in.FromKind == db.MeshRuleFromAll {
		fromID = nil
	}
	res := s.db.WithContext(ctx).Model(&db.MeshRule{}).Where("id = ? AND organization_id = ?", id, orgID).
		Updates(map[string]any{"from_kind": in.FromKind, "from_id": fromID, "to_node_id": in.ToNodeID,
			"ports": portsText(ParsePorts(in.Ports)), "note": strings.TrimSpace(in.Note)})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAccessRule
	}
	return nil
}

// RemoveRule removes a network rule.
func (s *MeshAccessService) RemoveRule(ctx context.Context, orgID, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Where("id = ? AND organization_id = ?", id, orgID).Delete(&db.MeshRule{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAccessRule
	}
	return nil
}

// PreviewRule is what a network rule would add to the Headscale policy, as
// the lines it would have there.
func (s *MeshAccessService) PreviewRule(ctx context.Context, orgID uuid.UUID, in AccessRuleInput) (string, error) {
	if err := s.validRule(ctx, orgID, in); err != nil {
		return "", err
	}
	inputs, err := s.inputs(ctx)
	if err != nil {
		return "", err
	}
	r := MeshNetRule{ID: uuid.New(), OrgID: orgID, FromKind: in.FromKind, FromID: in.FromID, ToNodeID: in.ToNodeID,
		Ports: ParsePorts(in.Ports), Note: strings.TrimSpace(in.Note)}
	inputs.Rules = append(inputs.Rules, r)
	p := BuildMeshPolicy(inputs)
	var mine []MeshRule
	for _, pr := range p.Rules {
		if pr.Why == r.Why() {
			mine = append(mine, pr)
		}
	}
	if len(mine) == 0 {
		return "", errors.New("this rule would allow nothing yet: the source has no machine on the mesh")
	}
	b, err := json.MarshalIndent(p.acls(mine), "", "  ")
	return string(b), err
}

func portsText(ports []int) string {
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ",")
}

// RouteOpener is someone an internal route answers.
type RouteOpener struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	Email  string `json:"email,omitempty"`
	Role   string `json:"role"`
	// Via is the grant that opens it to a member: the route, a service, a
	// stack or a project, by name.
	Via string `json:"via,omitempty"`
	// Reach is false when the member is granted it but switched off from
	// their machines, so the route refuses them.
	Reach    bool     `json:"reach"`
	Machines []string `json:"machines"`
}

// RouteOpeners is who an internal route answers on the mesh.
type RouteOpeners struct {
	Internal bool          `json:"internal"`
	Enforced bool          `json:"enforced"`
	Admins   []RouteOpener `json:"admins"`
	Members  []RouteOpener `json:"members"`
	// Rules are the network rules opening the gateway's web port, which is
	// every internal route, to a machine.
	Rules []string `json:"rules"`
}

// RouteOpeners is who an internal route answers, once the mesh policy is
// enforced: the same decision the proxy makes per request.
func (s *MeshAccessService) RouteOpeners(ctx context.Context, orgID, routeID uuid.UUID) (RouteOpeners, error) {
	q := s.db.WithContext(ctx)
	out := RouteOpeners{Admins: []RouteOpener{}, Members: []RouteOpener{}, Rules: []string{}}
	var route db.Route
	if err := q.Where("id = ? AND organization_id = ?", routeID, orgID).First(&route).Error; err != nil {
		return out, err
	}
	out.Internal = route.Zone == db.RouteZoneInternal
	if st, err := s.State(ctx); err == nil {
		out.Enforced = st.Enforced
	}
	var targets []db.RouteTarget
	q.Where("route_id = ?", routeID).Find(&targets)
	var services []db.Service
	for _, t := range targets {
		if t.ServiceID != nil {
			var svc db.Service
			if q.Preload("Project").First(&svc, "id = ?", *t.ServiceID).Error == nil {
				services = append(services, svc)
			}
		}
	}
	nodeTarget := len(services) < len(targets)
	var project db.Project
	q.First(&project, "id = ?", route.ProjectID)

	var members []struct {
		UserID   uuid.UUID
		Username string
		Email    string
		Role     db.MemberRole
	}
	q.Model(&db.OrganizationMember{}).
		Select("organization_members.user_id, users.username, users.email, organization_members.role").
		Joins("JOIN users ON users.id = organization_members.user_id").
		Where("organization_members.organization_id = ? AND users.kind = ?", orgID, db.UserHuman).
		Order("users.username").Scan(&members)
	for _, m := range members {
		o := RouteOpener{UserID: m.UserID.String(), Name: m.Username, Email: m.Email, Role: string(m.Role), Machines: []string{}}
		q.Model(&db.Node{}).Where("organization_id = ? AND owner_id = ? AND mesh_role = ?", orgID, m.UserID, db.MeshRoleMesh).
			Order("name").Pluck("name", &o.Machines)
		if m.Role == db.RoleOwner || m.Role == db.RoleAdmin {
			o.Reach = true
			out.Admins = append(out.Admins, o)
			continue
		}
		var perms []db.ResourcePermission
		q.Where("organization_id = ? AND user_id = ?", orgID, m.UserID).Find(&perms)
		granted := map[reachKey]bool{}
		for _, p := range perms {
			granted[reachKey{p.ResourceType, p.ResourceID}] = true
		}
		choices, _ := s.choices(ctx, orgID, m.UserID)
		projectKeys := func(p db.Project) []reachKey {
			keys := []reachKey{{db.ResourceProject, p.ID}}
			if p.ParentProjectID != nil {
				keys = append(keys, reachKey{db.ResourceProject, *p.ParentProjectID})
			}
			return keys
		}
		via := func(keys []reachKey) string {
			for _, k := range keys {
				if granted[k] {
					if end, ok := s.resourceEnd(ctx, k.t, k.id); ok {
						return end.Name
					}
				}
			}
			return ""
		}
		if granted[reachKey{db.ResourceRoute, routeID}] {
			o.Via, o.Reach = "this route", true
		}
		for _, svc := range services {
			if o.Via != "" && o.Reach {
				break
			}
			keys := []reachKey{{db.ResourceService, svc.ID}}
			if svc.StackID != nil {
				keys = append(keys, reachKey{db.ResourceStack, *svc.StackID})
			}
			if v := via(append(keys, projectKeys(svc.Project)...)); v != "" {
				reach, _ := reaches(svc, choices)
				if o.Via == "" || reach {
					o.Via, o.Reach = v, reach
				}
			}
		}
		if nodeTarget && !(o.Via != "" && o.Reach) {
			if v := via(projectKeys(project)); v != "" {
				o.Via, o.Reach = v, true
			}
		}
		if o.Via != "" {
			out.Members = append(out.Members, o)
		}
	}

	// Network rules to a gateway's web port.
	var rules []db.MeshRule
	q.Joins("JOIN nodes ON nodes.id = mesh_rules.to_node_id").
		Where("mesh_rules.organization_id = ? AND nodes.k3s_role = ?", orgID, db.K3sRoleServer).Find(&rules)
	for _, r := range rules {
		if p := ParsePorts(r.Ports); len(p) > 0 && !slices.Contains(p, 443) {
			continue
		}
		switch r.FromKind {
		case db.MeshRuleFromAll:
			out.Rules = append(out.Rules, "Every connected machine")
		case db.MeshRuleFromMachine:
			var n db.Node
			if r.FromID != nil && q.First(&n, "id = ?", *r.FromID).Error == nil {
				out.Rules = append(out.Rules, n.Name)
			}
		case db.MeshRuleFromPerson:
			var u db.User
			if r.FromID != nil && q.First(&u, "id = ?", *r.FromID).Error == nil {
				out.Rules = append(out.Rules, u.Username+"'s machines")
			}
		}
	}
	return out, nil
}
