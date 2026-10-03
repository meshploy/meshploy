package meshgate

import (
	"testing"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
)

type world struct {
	org, other           uuid.UUID
	owner, admin, member uuid.UUID
	project, level       uuid.UUID
	stack                uuid.UUID
	app, database        uuid.UUID
	route                uuid.UUID
	gatewayID, laptopID  uuid.UUID
	s                    *Snapshot
}

// newWorld is one organisation with a gateway, a worker and a laptop for each
// of its owner, an admin and a member, and a project with an app and a
// database; the member is granted nothing yet.
func newWorld() *world {
	w := &world{org: uuid.New(), other: uuid.New(), owner: uuid.New(), admin: uuid.New(), member: uuid.New(),
		project: uuid.New(), level: uuid.New(), stack: uuid.New(), app: uuid.New(), database: uuid.New(),
		route: uuid.New(), gatewayID: uuid.New(), laptopID: uuid.New()}
	w.s = &Snapshot{
		Enforced: true,
		machines: map[string]machine{
			"100.64.0.1": {id: w.gatewayID, org: w.org, name: "gateway", kind: "gateway"},
			"100.64.0.2": {id: uuid.New(), org: w.org, name: "worker", kind: "cluster"},
			"100.64.0.3": {id: uuid.New(), org: w.org, name: "owner-laptop", kind: "connected", owner: &w.owner},
			"100.64.0.4": {id: uuid.New(), org: w.org, name: "admin-laptop", kind: "connected", owner: &w.admin},
			"100.64.0.5": {id: w.laptopID, org: w.org, name: "member-laptop", kind: "connected", owner: &w.member},
			"100.64.0.6": {id: uuid.New(), org: w.org, name: "kiosk", kind: "connected"},
			"100.64.0.7": {id: uuid.New(), org: w.other, name: "elsewhere", kind: "connected", owner: &w.owner},
		},
		roles: map[memberKey]db.MemberRole{
			{w.org, w.owner}:  db.RoleOwner,
			{w.org, w.admin}:  db.RoleAdmin,
			{w.org, w.member}: db.RoleMember,
		},
		grants: map[memberKey]map[grantKey]bool{},
		reach:  map[memberKey]map[grantKey]bool{},
		services: map[uuid.UUID]service{
			w.app:      {project: w.project, stack: &w.stack},
			w.database: {project: w.project, database: true},
		},
		parent:   map[uuid.UUID]uuid.UUID{w.level: w.project},
		gateways: map[uuid.UUID]bool{w.gatewayID: true},
	}
	return w
}

func (w *world) grant(t db.ResourceType, id uuid.UUID) {
	k := memberKey{w.org, w.member}
	if w.s.grants[k] == nil {
		w.s.grants[k] = map[grantKey]bool{}
	}
	w.s.grants[k][grantKey{t, id}] = true
}

func (w *world) setReach(t db.ResourceType, id uuid.UUID, on bool) {
	k := memberKey{w.org, w.member}
	if w.s.reach[k] == nil {
		w.s.reach[k] = map[grantKey]bool{}
	}
	w.s.reach[k][grantKey{t, id}] = on
}

func (w *world) to(service uuid.UUID) Target {
	return Target{RouteID: w.route, ServiceID: service, ProjectID: w.project, OrgID: w.org}
}

func TestWhoOpensAnInternalRoute(t *testing.T) {
	w := newWorld()
	app := w.to(w.app)
	for _, c := range []struct {
		addr string
		want bool
		why  string
	}{
		{"100.64.0.1", true, "the gateway"},
		{"100.64.0.2", true, "a cluster machine"},
		{"100.64.0.3", true, "the owner's laptop"},
		{"100.64.0.4", true, "an admin's laptop"},
		{"100.64.0.5", false, "a member granted nothing"},
		{"100.64.0.6", false, "a machine with no owner"},
		{"100.64.0.7", false, "a machine of another organisation, though its owner owns this one"},
		{"100.64.9.9", false, "a mesh address Meshploy does not know"},
		{"fd7a:115c:a1e0::9", false, "an IPv6 mesh address Meshploy does not know"},
		{"127.0.0.1", true, "the gateway's loopback"},
		{"172.18.0.4", true, "a container on the gateway"},
		{"203.0.113.9", false, "the internet"},
		{"garbage", false, "an unreadable address"},
	} {
		if got := w.s.Decide(c.addr, app); got.Allowed != c.want {
			t.Errorf("%s (%s): allowed=%v, want %v (%s)", c.why, c.addr, got.Allowed, c.want, got.Reason)
		}
	}
}

func TestAMemberOpensWhatTheyAreGranted(t *testing.T) {
	member := "100.64.0.5"
	cases := []struct {
		name  string
		setup func(w *world)
		to    func(w *world) Target
		want  bool
	}{
		{"granted the service", func(w *world) { w.grant(db.ResourceService, w.app) }, func(w *world) Target { return w.to(w.app) }, true},
		{"granted its stack", func(w *world) { w.grant(db.ResourceStack, w.stack) }, func(w *world) Target { return w.to(w.app) }, true},
		{"granted its project", func(w *world) { w.grant(db.ResourceProject, w.project) }, func(w *world) Target { return w.to(w.app) }, true},
		{"granted the route itself", func(w *world) { w.grant(db.ResourceRoute, w.route) }, func(w *world) Target { return w.to(w.app) }, true},
		{"granted another service", func(w *world) { w.grant(db.ResourceService, w.database) }, func(w *world) Target { return w.to(w.app) }, false},
		{"a level, granted the project above it", func(w *world) { w.grant(db.ResourceProject, w.project) },
			func(w *world) Target { return Target{RouteID: w.route, ProjectID: w.level, OrgID: w.org} }, true},
		{"a node target, granted its project", func(w *world) { w.grant(db.ResourceProject, w.project) },
			func(w *world) Target { return Target{RouteID: w.route, ProjectID: w.project, OrgID: w.org} }, true},
		{"a node target, granted a service in its project", func(w *world) { w.grant(db.ResourceService, w.app) },
			func(w *world) Target { return Target{RouteID: w.route, ProjectID: w.project, OrgID: w.org} }, false},
		{"a database, granted but not switched on", func(w *world) { w.grant(db.ResourceService, w.database) },
			func(w *world) Target { return w.to(w.database) }, false},
		{"a database switched on", func(w *world) {
			w.grant(db.ResourceProject, w.project)
			w.setReach(db.ResourceService, w.database, true)
		}, func(w *world) Target { return w.to(w.database) }, true},
		{"a database, its project switched on", func(w *world) {
			w.grant(db.ResourceProject, w.project)
			w.setReach(db.ResourceProject, w.project, true)
		}, func(w *world) Target { return w.to(w.database) }, false},
		{"a database switched on, its project switched off", func(w *world) {
			w.grant(db.ResourceProject, w.project)
			w.setReach(db.ResourceProject, w.project, false)
			w.setReach(db.ResourceService, w.database, true)
		}, func(w *world) Target { return w.to(w.database) }, true},
		{"the app switched off on its project", func(w *world) {
			w.grant(db.ResourceProject, w.project)
			w.setReach(db.ResourceProject, w.project, false)
		}, func(w *world) Target { return w.to(w.app) }, false},
		{"switched off on the project, back on for the service", func(w *world) {
			w.grant(db.ResourceProject, w.project)
			w.setReach(db.ResourceProject, w.project, false)
			w.setReach(db.ResourceService, w.app, true)
		}, func(w *world) Target { return w.to(w.app) }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld()
			c.setup(w)
			if got := w.s.Decide(member, c.to(w)); got.Allowed != c.want {
				t.Errorf("allowed=%v, want %v (%s)", got.Allowed, c.want, got.Reason)
			}
		})
	}
}

func TestANetworkRuleToTheGatewaysWebPortOpensEveryInternalRoute(t *testing.T) {
	kiosk := "100.64.0.6"
	for _, c := range []struct {
		name string
		rule db.MeshRule
		want bool
	}{
		{"every port", db.MeshRule{FromKind: db.MeshRuleFromAll}, true},
		{"443 among others", db.MeshRule{FromKind: db.MeshRuleFromAll, Ports: "22, 443"}, true},
		{"another port only", db.MeshRule{FromKind: db.MeshRuleFromAll, Ports: "5432"}, false},
		{"to a worker, not the gateway", db.MeshRule{FromKind: db.MeshRuleFromAll, ToNodeID: uuid.New()}, false},
		{"from another machine", db.MeshRule{FromKind: db.MeshRuleFromMachine, FromID: ptr(uuid.New())}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld()
			c.rule.OrganizationID = w.org
			if c.rule.ToNodeID == uuid.Nil {
				c.rule.ToNodeID = w.gatewayID
			}
			w.s.rules = []db.MeshRule{c.rule}
			if got := w.s.Decide(kiosk, w.to(w.app)); got.Allowed != c.want {
				t.Errorf("allowed=%v, want %v (%s)", got.Allowed, c.want, got.Reason)
			}
		})
	}

	// A rule from a person opens it from every machine they own.
	w := newWorld()
	w.s.rules = []db.MeshRule{{OrganizationID: w.org, FromKind: db.MeshRuleFromPerson, FromID: &w.member, ToNodeID: w.gatewayID, Ports: "443"}}
	if got := w.s.Decide("100.64.0.5", w.to(w.app)); !got.Allowed {
		t.Errorf("a rule from the member: refused (%s)", got.Reason)
	}
}

func TestNothingIsCheckedUntilThePolicyIsEnforced(t *testing.T) {
	w := newWorld()
	w.s.Enforced = false
	if got := w.s.Decide("203.0.113.9", w.to(w.app)); !got.Allowed {
		t.Errorf("refused while not enforced: %s", got.Reason)
	}
}

func TestBeforeTheFirstLoadOnlyTheGatewayPasses(t *testing.T) {
	g := New(nil)
	if got := g.Check("100.64.0.5", Target{}); got.Allowed {
		t.Error("a mesh machine passed before anything was known")
	}
	if got := g.Check("127.0.0.1", Target{}); !got.Allowed {
		t.Errorf("the gateway itself was refused: %s", got.Reason)
	}
}

func ptr[T any](v T) *T { return &v }
