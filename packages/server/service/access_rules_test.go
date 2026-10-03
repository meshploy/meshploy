package service_test

import (
	"context"
	"strings"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// A network rule opens a machine's chosen ports to a person's machines, shows
// beside the grants on the Access page, previews as the lines it adds, and
// goes when removed; it cannot name another organisation's machine.
func TestANetworkRuleOpensAMachinesPorts(t *testing.T) {
	ctx := context.Background()
	svcs, orgID, ownerID, gdb := setupAgentFixture(t)

	ravi := meshdb.User{Username: "ravi", Email: "ravi@example.com", Kind: meshdb.UserHuman}
	require.NoError(t, gdb.Create(&ravi).Error)
	_, err := svcs.Orgs.AddMember(ctx, orgID, service.AddMemberInput{Email: ravi.Email, Role: meshdb.RoleMember})
	require.NoError(t, err)
	_, err = svcs.Nodes.Register(ctx, orgID, "gw-1", "100.64.0.1", meshdb.K3sRoleServer)
	require.NoError(t, err)
	join := func(owner *meshdb.User, name, ip string) *meshdb.Node {
		t.Helper()
		var by = &ownerID
		if owner != nil {
			by = &owner.ID
		}
		token, _, err := svcs.Nodes.CreateProvisioningTokenBy(ctx, orgID, by, name, nil, meshdb.MeshRoleMesh)
		require.NoError(t, err)
		n, _, err := svcs.Nodes.RegisterWithProvisioningToken(ctx, token, name, ip, meshdb.MeshRoleMesh, meshdb.NodeOSLinux)
		require.NoError(t, err)
		return n
	}
	laptop := join(&ravi, "ravi-laptop", "100.64.0.4")
	nas := join(nil, "office-nas", "100.64.0.20")

	shop := meshdb.Project{OrganizationID: orgID, Name: "shop", Slug: "shop"}
	require.NoError(t, gdb.Create(&shop).Error)
	require.NoError(t, svcs.Permissions.Grant(ctx, orgID, ravi.ID, shop.ID, meshdb.ResourceProject, meshdb.ActionView))

	in := service.AccessRuleInput{FromKind: meshdb.MeshRuleFromPerson, FromID: &ravi.ID, ToNodeID: nas.ID, Ports: "445, 22", Note: "file share"}
	preview, err := svcs.MeshAccess.PreviewRule(ctx, orgID, in)
	require.NoError(t, err)
	require.Contains(t, preview, `"office-nas-`)
	require.Contains(t, preview, `:445"`)
	require.Contains(t, preview, `:22"`)

	rule, err := svcs.MeshAccess.AddRule(ctx, orgID, ownerID, in)
	require.NoError(t, err)
	require.Equal(t, "22,445", rule.Ports)

	reached := func() []int {
		t.Helper()
		p, err := svcs.MeshAccess.Policy(ctx)
		require.NoError(t, err)
		var out []int
		for _, m := range p.Machines {
			if m.ID == laptop.ID {
				for _, r := range m.Reaches {
					if strings.Contains(r.To, "office-nas") {
						out = append(out, r.Ports...)
					}
				}
			}
		}
		return out
	}
	require.ElementsMatch(t, []int{22, 445}, reached())

	rules, err := svcs.MeshAccess.Rules(ctx, orgID)
	require.NoError(t, err)
	var grant, network bool
	for _, r := range rules {
		if r.Kind == "grant" && r.To.Kind == "project" && r.From.Name == "ravi" {
			grant = true
		}
		if r.Kind == "network" && r.To.Name == "office-nas" && r.Note == "file share" {
			network = true
		}
	}
	require.True(t, grant, "the grant shows as a rule")
	require.True(t, network, "the network rule shows")

	// Another organisation's machine is not a destination here.
	other := meshdb.Organization{Name: "Other", Slug: "other"}
	require.NoError(t, gdb.Create(&other).Error)
	elsewhere, err := svcs.Nodes.Register(ctx, other.ID, "elsewhere", "100.64.0.30")
	require.NoError(t, err)
	_, err = svcs.MeshAccess.AddRule(ctx, orgID, ownerID, service.AccessRuleInput{FromKind: meshdb.MeshRuleFromAll, ToNodeID: elsewhere.ID})
	require.ErrorIs(t, err, service.ErrAccessRule)

	// The gateway reaches every machine already: a rule from it adds nothing.
	var gw meshdb.Node
	require.NoError(t, gdb.First(&gw, "name = ?", "gw-1").Error)
	_, err = svcs.MeshAccess.AddRule(ctx, orgID, ownerID, service.AccessRuleInput{FromKind: meshdb.MeshRuleFromMachine, FromID: &gw.ID, ToNodeID: nas.ID})
	require.ErrorIs(t, err, service.ErrRuleFromCluster)

	// Changed to one port, it opens only that one.
	require.NoError(t, svcs.MeshAccess.UpdateRule(ctx, orgID, rule.ID, service.AccessRuleInput{
		FromKind: meshdb.MeshRuleFromPerson, FromID: &ravi.ID, ToNodeID: nas.ID, Ports: "445", Note: "file share only"}))
	require.ElementsMatch(t, []int{445}, reached())

	require.NoError(t, svcs.MeshAccess.RemoveRule(ctx, orgID, rule.ID))
	require.Empty(t, reached(), "removed, the ports close")
}

// A grant's line on the Access page says what it opens from its person's
// machines - where each service answers on the mesh, and the internal routes
// to it - and a grant held by someone outside the organisation opens nothing.
// An internal route says who it answers.
func TestAGrantSaysWhatItOpensAndARouteWhoItAnswers(t *testing.T) {
	ctx := context.Background()
	svcs, orgID, ownerID, gdb := setupAgentFixture(t)

	ravi := meshdb.User{Username: "ravi", Email: "ravi@example.com", Kind: meshdb.UserHuman}
	guest := meshdb.User{Username: "guest-1", Email: "asha@example.com", Kind: meshdb.UserHuman}
	require.NoError(t, gdb.Create(&ravi).Error)
	require.NoError(t, gdb.Create(&guest).Error)
	_, err := svcs.Orgs.AddMember(ctx, orgID, service.AddMemberInput{Email: ravi.Email, Role: meshdb.RoleMember})
	require.NoError(t, err)
	gw, err := svcs.Nodes.Register(ctx, orgID, "gw-1", "100.64.0.1", meshdb.K3sRoleServer)
	require.NoError(t, err)
	token, _, err := svcs.Nodes.CreateProvisioningTokenBy(ctx, orgID, &ravi.ID, "laptop", nil, meshdb.MeshRoleMesh)
	require.NoError(t, err)
	_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, token, "ravi-laptop", "100.64.0.4", meshdb.MeshRoleMesh, meshdb.NodeOSLinux)
	require.NoError(t, err)

	shop := meshdb.Project{OrganizationID: orgID, Name: "shop", Slug: "shop"}
	require.NoError(t, gdb.Create(&shop).Error)
	web := meshdb.Service{ProjectID: shop.ID, Name: "web", Slug: "web"}
	pg := meshdb.Service{ProjectID: shop.ID, Name: "postgres", Slug: "postgres", Type: meshdb.ServiceTypeDatabase}
	require.NoError(t, gdb.Create(&web).Error)
	require.NoError(t, gdb.Create(&pg).Error)
	require.NoError(t, gdb.Create(&meshdb.ServicePort{ServiceID: web.ID, Name: "http", Port: 3000, IsPublic: true, NodePort: 30300}).Error)
	require.NoError(t, gdb.Create(&meshdb.ServicePort{ServiceID: pg.ID, Name: "pg", Port: 5432, IsPublic: true, NodePort: 31432}).Error)
	route := meshdb.Route{OrganizationID: orgID, ProjectID: shop.ID, Zone: meshdb.RouteZoneInternal, Hostname: "web.internal.acme.dev", Published: true}
	require.NoError(t, gdb.Create(&route).Error)
	require.NoError(t, gdb.Create(&meshdb.RouteTarget{RouteID: route.ID, Path: "/", ServiceID: &web.ID, TargetPort: 30300}).Error)

	require.NoError(t, svcs.Permissions.Grant(ctx, orgID, ravi.ID, shop.ID, meshdb.ResourceProject, meshdb.ActionView))
	require.NoError(t, svcs.Permissions.Grant(ctx, orgID, guest.ID, web.ID, meshdb.ResourceService, meshdb.ActionView))

	rules, err := svcs.MeshAccess.Rules(ctx, orgID)
	require.NoError(t, err)
	byPerson := map[string]service.AccessRule{}
	for _, r := range rules {
		byPerson[r.From.Name] = r
	}
	shopRule := byPerson["ravi"]
	require.Equal(t, []service.AccessOpen{
		{Kind: "port", Service: "web", Port: 30300, On: "cluster"},
		{Kind: "route", Service: "web", Hostname: "web.internal.acme.dev"},
	}, shopRule.Opens, "the app's NodePort, not its own port, and its internal route; the database is off")
	require.Equal(t, 1, shopRule.Off, "the database counts as switched off")
	require.Equal(t, []int{30300}, shopRule.Ports)

	guestRule := byPerson["guest-1"]
	require.False(t, guestRule.From.Member)
	require.Empty(t, guestRule.Opens, "someone outside the organisation has no machines on the mesh")
	require.Empty(t, guestRule.Ports)

	who, err := svcs.MeshAccess.RouteOpeners(ctx, orgID, route.ID)
	require.NoError(t, err)
	require.True(t, who.Internal)
	require.Len(t, who.Admins, 1)
	require.Equal(t, ownerID.String(), who.Admins[0].UserID)
	require.Len(t, who.Members, 1, "the guest is not a member, so the route never answers them")
	require.Equal(t, "ravi", who.Members[0].Name)
	require.Equal(t, "shop", who.Members[0].Via)
	require.True(t, who.Members[0].Reach)
	require.Equal(t, []string{"ravi-laptop"}, who.Members[0].Machines)
	require.Empty(t, who.Rules)

	require.NoError(t, svcs.MeshAccess.SetReach(ctx, orgID, ravi.ID, meshdb.ResourceService, web.ID, false))
	who, err = svcs.MeshAccess.RouteOpeners(ctx, orgID, route.ID)
	require.NoError(t, err)
	require.False(t, who.Members[0].Reach, "switched off from their machines")

	_, err = svcs.MeshAccess.AddRule(ctx, orgID, ownerID, service.AccessRuleInput{FromKind: meshdb.MeshRuleFromAll, ToNodeID: gw.ID, Ports: "443"})
	require.NoError(t, err)
	who, err = svcs.MeshAccess.RouteOpeners(ctx, orgID, route.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"Every connected machine"}, who.Rules)
}
