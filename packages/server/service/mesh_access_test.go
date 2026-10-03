package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// A machine is whoever minted its token's; only someone in the organisation
// can be given one; and a member's machine reaches what that member was
// granted, worked out from the database.
func TestAMachinesOwnerDecidesItsMeshAccess(t *testing.T) {
	ctx := context.Background()
	svcs, orgID, ownerID, gdb := setupAgentFixture(t)

	ravi := meshdb.User{Username: "ravi", Email: "ravi@example.com", Kind: meshdb.UserHuman}
	require.NoError(t, gdb.Create(&ravi).Error)
	_, err := svcs.Orgs.AddMember(ctx, orgID, service.AddMemberInput{Email: ravi.Email, Role: meshdb.RoleMember})
	require.NoError(t, err)
	outsider := meshdb.User{Username: "sam", Email: "sam@example.com", Kind: meshdb.UserHuman}
	require.NoError(t, gdb.Create(&outsider).Error)

	gw, err := svcs.Nodes.Register(ctx, orgID, "gw-1", "100.64.0.1", meshdb.K3sRoleServer)
	require.NoError(t, err)
	_ = gw

	// Ravi mints the token, so the laptop that joins with it is Ravi's.
	token, _, err := svcs.Nodes.CreateProvisioningTokenBy(ctx, orgID, &ravi.ID, "ravi's laptop", nil, meshdb.MeshRoleMesh)
	require.NoError(t, err)
	laptop, _, err := svcs.Nodes.RegisterWithProvisioningToken(ctx, token, "ravi-laptop", "100.64.0.4", meshdb.MeshRoleMesh, meshdb.NodeOSLinux)
	require.NoError(t, err)
	require.NotNil(t, laptop.OwnerID)
	require.Equal(t, ravi.ID, *laptop.OwnerID)

	_, err = svcs.Nodes.SetOwner(ctx, orgID, laptop.ID, &outsider.ID)
	require.ErrorIs(t, err, service.ErrOwnerNotMember)

	// Ravi is granted a project holding a database published on the mesh, and
	// another project's service is not his.
	shop := meshdb.Project{OrganizationID: orgID, Name: "shop", Slug: "shop"}
	other := meshdb.Project{OrganizationID: orgID, Name: "other", Slug: "other"}
	require.NoError(t, gdb.Create(&shop).Error)
	require.NoError(t, gdb.Create(&other).Error)
	pg := meshdb.Service{ProjectID: shop.ID, Name: "postgres", Slug: "postgres"}
	secret := meshdb.Service{ProjectID: other.ID, Name: "vault", Slug: "vault"}
	require.NoError(t, gdb.Create(&pg).Error)
	require.NoError(t, gdb.Create(&secret).Error)
	require.NoError(t, gdb.Create(&meshdb.ServicePort{ServiceID: pg.ID, Name: "pg", Port: 5432, IsPublic: true, NodePort: 31432}).Error)
	require.NoError(t, gdb.Create(&meshdb.ServicePort{ServiceID: secret.ID, Name: "http", Port: 8200, IsPublic: true, NodePort: 31820}).Error)
	require.NoError(t, svcs.Permissions.Grant(ctx, orgID, ravi.ID, shop.ID, meshdb.ResourceProject, meshdb.ActionView))

	policy, err := svcs.MeshAccess.Policy(ctx)
	require.NoError(t, err)
	var reached []int
	for _, m := range policy.Machines {
		if m.ID != laptop.ID {
			continue
		}
		require.False(t, m.Everything)
		require.Equal(t, "ravi", m.OwnerName)
		for _, r := range m.Reaches {
			reached = append(reached, r.Ports...)
		}
	}
	require.Contains(t, reached, 31432, "the granted database's port")
	require.NotContains(t, reached, 31820, "another project's service")

	// Given to the organisation's owner instead, it reaches everything.
	_, err = svcs.Nodes.SetOwner(ctx, orgID, laptop.ID, &ownerID)
	require.NoError(t, err)
	policy, err = svcs.MeshAccess.Policy(ctx)
	require.NoError(t, err)
	for _, m := range policy.Machines {
		if m.ID == laptop.ID {
			require.True(t, m.Everything)
		}
	}
}

// A database a member can see is not one they connect to from a laptop until
// someone says so; switching it on opens its port, and the Access page shows
// the member, the port and their machine.
func TestADatabaseIsReachedFromAMachineOnlyWhenSwitchedOn(t *testing.T) {
	ctx := context.Background()
	svcs, orgID, _, gdb := setupAgentFixture(t)

	ravi := meshdb.User{Username: "ravi", Email: "ravi@example.com", Kind: meshdb.UserHuman}
	require.NoError(t, gdb.Create(&ravi).Error)
	_, err := svcs.Orgs.AddMember(ctx, orgID, service.AddMemberInput{Email: ravi.Email, Role: meshdb.RoleMember})
	require.NoError(t, err)
	_, err = svcs.Nodes.Register(ctx, orgID, "gw-1", "100.64.0.1", meshdb.K3sRoleServer)
	require.NoError(t, err)
	token, _, err := svcs.Nodes.CreateProvisioningTokenBy(ctx, orgID, &ravi.ID, "laptop", nil, meshdb.MeshRoleMesh)
	require.NoError(t, err)
	laptop, _, err := svcs.Nodes.RegisterWithProvisioningToken(ctx, token, "ravi-laptop", "100.64.0.4", meshdb.MeshRoleMesh, meshdb.NodeOSLinux)
	require.NoError(t, err)

	shop := meshdb.Project{OrganizationID: orgID, Name: "shop", Slug: "shop"}
	require.NoError(t, gdb.Create(&shop).Error)
	pg := meshdb.Service{ProjectID: shop.ID, Name: "postgres", Slug: "postgres", Type: meshdb.ServiceTypeDatabase}
	web := meshdb.Service{ProjectID: shop.ID, Name: "web", Slug: "web"}
	require.NoError(t, gdb.Create(&pg).Error)
	require.NoError(t, gdb.Create(&web).Error)
	require.NoError(t, gdb.Create(&meshdb.ServicePort{ServiceID: pg.ID, Name: "pg", Port: 5432, IsPublic: true, NodePort: 31432}).Error)
	require.NoError(t, gdb.Create(&meshdb.ServicePort{ServiceID: web.ID, Name: "http", Port: 3000, IsPublic: true, NodePort: 30300}).Error)
	require.NoError(t, svcs.Permissions.Grant(ctx, orgID, ravi.ID, shop.ID, meshdb.ResourceProject, meshdb.ActionView))

	ports := func() []int {
		t.Helper()
		policy, err := svcs.MeshAccess.Policy(ctx)
		require.NoError(t, err)
		var out []int
		for _, m := range policy.Machines {
			if m.ID == laptop.ID {
				for _, r := range m.Reaches {
					out = append(out, r.Ports...)
				}
			}
		}
		return out
	}

	got := ports()
	require.Contains(t, got, 30300, "an app in a granted project is reached by default")
	require.NotContains(t, got, 31432, "a database is not, until someone says so")

	// Switching the project on is not switching its databases on.
	require.NoError(t, svcs.MeshAccess.SetReach(ctx, orgID, ravi.ID, meshdb.ResourceProject, shop.ID, true))
	got = ports()
	require.Contains(t, got, 30300)
	require.NotContains(t, got, 31432, "a database is switched on by itself, never by its project")
	require.NoError(t, gdb.Where("user_id = ?", ravi.ID).Delete(&meshdb.MeshReach{}).Error)

	rows, err := svcs.MeshAccess.ReachOn(ctx, orgID, meshdb.ResourceService, pg.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, ravi.ID, rows[0].UserID)
	require.False(t, rows[0].Reach)
	require.False(t, rows[0].Chosen)
	require.Equal(t, []string{"ravi-laptop"}, rows[0].Machines)
	require.Equal(t, 31432, rows[0].Services[0].Ports[0].MeshPort)

	require.NoError(t, svcs.MeshAccess.SetReach(ctx, orgID, ravi.ID, meshdb.ResourceService, pg.ID, true))
	require.Contains(t, ports(), 31432, "switched on, the database's port opens")

	// A choice on the project is overruled by one on the service.
	require.NoError(t, svcs.MeshAccess.SetReach(ctx, orgID, ravi.ID, meshdb.ResourceProject, shop.ID, false))
	got = ports()
	require.Contains(t, got, 31432, "the service's own choice wins")
	require.NotContains(t, got, 30300, "the project's choice turns off what has none of its own")

	rows, err = svcs.MeshAccess.ReachOn(ctx, orgID, meshdb.ResourceService, pg.ID)
	require.NoError(t, err)
	require.True(t, rows[0].Reach)
	require.True(t, rows[0].Chosen)
}
