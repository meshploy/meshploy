package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupAgentFixture creates an org with one owner and returns the services, the
// org id, the owner (human) id, and the raw db handle.
func setupAgentFixture(t *testing.T) (svcs *service.Services, orgID, ownerID uuid.UUID, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	db = newTestDB(t)
	svcs = newServices(db)

	owner, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "owner",
		Email:    "owner@example.com",
		Password: "password123",
	})
	require.NoError(t, err)

	orgs, err := svcs.Orgs.ListForUser(ctx, owner.ID)
	require.NoError(t, err)
	require.Len(t, orgs, 1)

	return svcs, orgs[0].ID, owner.ID, db
}

func TestCreateAgent(t *testing.T) {
	ctx := context.Background()
	svcs, org, owner, _ := setupAgentFixture(t)

	agent, token, err := svcs.Agents.CreateAgent(ctx, org, "deploy-bot", meshdb.RoleMember, "ci", nil, owner)
	require.NoError(t, err)

	assert.Equal(t, "deploy-bot", agent.Name)
	assert.Equal(t, meshdb.RoleMember, agent.Role)
	require.Len(t, agent.Tokens, 1)
	assert.Equal(t, "ci", agent.Tokens[0].Name)

	// Plaintext is returned once, prefixed magt-, and never equals the stored hash.
	assert.True(t, len(token) > len("magt-"))
	assert.Equal(t, "magt-", token[:5])
	assert.NotEqual(t, token, agent.Tokens[0].TokenHash)
	assert.Equal(t, token[:12], agent.Tokens[0].TokenPrefix)

	// The token authenticates to the agent's principal id.
	id, ok := svcs.Agents.ResolveToken(ctx, token)
	require.True(t, ok)
	assert.Equal(t, agent.ID, id)

	// It appears in the org's agent list.
	list, err := svcs.Agents.ListAgents(ctx, org)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, agent.ID, list[0].ID)
}

func TestCreateAgentRejectsOwnerRole(t *testing.T) {
	ctx := context.Background()
	svcs, org, owner, _ := setupAgentFixture(t)

	_, _, err := svcs.Agents.CreateAgent(ctx, org, "root-bot", meshdb.RoleOwner, "", nil, owner)
	require.ErrorIs(t, err, service.ErrAgentOwnerRole)
}

func TestCreateAgentDuplicateNameRejected(t *testing.T) {
	ctx := context.Background()
	svcs, org, owner, _ := setupAgentFixture(t)

	_, _, err := svcs.Agents.CreateAgent(ctx, org, "dupe", meshdb.RoleMember, "", nil, owner)
	require.NoError(t, err)
	_, _, err = svcs.Agents.CreateAgent(ctx, org, "dupe", meshdb.RoleMember, "", nil, owner)
	require.ErrorIs(t, err, service.ErrNameTaken)
}

// TestManyAgentsShareEmptyEmail proves the partial unique index (WHERE email <> ”)
// lets multiple agents coexist while still keeping human emails unique.
func TestManyAgentsShareEmptyEmail(t *testing.T) {
	ctx := context.Background()
	svcs, org, owner, db := setupAgentFixture(t)

	_, _, err := svcs.Agents.CreateAgent(ctx, org, "agent-a", meshdb.RoleMember, "", nil, owner)
	require.NoError(t, err)
	_, _, err = svcs.Agents.CreateAgent(ctx, org, "agent-b", meshdb.RoleMember, "", nil, owner)
	require.NoError(t, err, "second empty-email agent must not collide")

	// But two humans with the same email must still be rejected by the index.
	err = db.Create(&meshdb.User{Username: "h1", Email: "dup@x.com", Kind: meshdb.UserHuman}).Error
	require.NoError(t, err)
	err = db.Create(&meshdb.User{Username: "h2", Email: "dup@x.com", Kind: meshdb.UserHuman}).Error
	require.Error(t, err, "duplicate human email must still violate the partial unique index")
}

func TestResolveTokenRevokedAndExpired(t *testing.T) {
	ctx := context.Background()
	svcs, org, owner, _ := setupAgentFixture(t)

	// Expired token: ResolveToken must reject it.
	past := time.Now().Add(-time.Hour)
	_, expiredTok, err := svcs.Agents.CreateAgent(ctx, org, "exp-bot", meshdb.RoleMember, "", &past, owner)
	require.NoError(t, err)
	_, ok := svcs.Agents.ResolveToken(ctx, expiredTok)
	assert.False(t, ok, "expired token must not resolve")

	// Revoked token: valid, then revoked → rejected.
	agent, tok, err := svcs.Agents.CreateAgent(ctx, org, "rev-bot", meshdb.RoleMember, "", nil, owner)
	require.NoError(t, err)
	_, ok = svcs.Agents.ResolveToken(ctx, tok)
	require.True(t, ok)

	require.NoError(t, svcs.Agents.RevokeToken(ctx, org, agent.ID, agent.Tokens[0].ID))
	_, ok = svcs.Agents.ResolveToken(ctx, tok)
	assert.False(t, ok, "revoked token must not resolve")

	// Unknown token never resolves.
	_, ok = svcs.Agents.ResolveToken(ctx, "magt-deadbeef")
	assert.False(t, ok)
}

func TestAddTokenRotation(t *testing.T) {
	ctx := context.Background()
	svcs, org, owner, _ := setupAgentFixture(t)

	agent, first, err := svcs.Agents.CreateAgent(ctx, org, "rot-bot", meshdb.RoleMember, "", nil, owner)
	require.NoError(t, err)

	second, _, err := svcs.Agents.AddToken(ctx, org, agent.ID, "second", nil, owner)
	require.NoError(t, err)

	// Both tokens resolve to the same principal.
	id1, ok1 := svcs.Agents.ResolveToken(ctx, first)
	id2, ok2 := svcs.Agents.ResolveToken(ctx, second)
	require.True(t, ok1)
	require.True(t, ok2)
	assert.Equal(t, agent.ID, id1)
	assert.Equal(t, agent.ID, id2)

	// AddToken on an unknown agent id fails.
	_, _, err = svcs.Agents.AddToken(ctx, org, owner /* a human, not an agent */, "x", nil, owner)
	require.ErrorIs(t, err, service.ErrAgentNotFound)
}

func TestDeleteAgentCascades(t *testing.T) {
	ctx := context.Background()
	svcs, org, owner, _ := setupAgentFixture(t)

	agent, tok, err := svcs.Agents.CreateAgent(ctx, org, "gone-bot", meshdb.RoleMember, "", nil, owner)
	require.NoError(t, err)

	require.NoError(t, svcs.Agents.DeleteAgent(ctx, org, agent.ID))

	// Token no longer resolves; agent no longer listed.
	_, ok := svcs.Agents.ResolveToken(ctx, tok)
	assert.False(t, ok)
	list, err := svcs.Agents.ListAgents(ctx, org)
	require.NoError(t, err)
	assert.Empty(t, list)

	// Deleting a human by id via the agent path is rejected.
	err = svcs.Agents.DeleteAgent(ctx, org, owner)
	require.ErrorIs(t, err, service.ErrAgentNotFound)
}

// TestAgentIsAPrincipal is the load-bearing test: an agent granted a project
// passes CheckAccess exactly like a human member would, with zero authz changes.
func TestAgentIsAPrincipal(t *testing.T) {
	ctx := context.Background()
	svcs, org, owner, _ := setupAgentFixture(t)

	project, err := svcs.Projects.Create(ctx, org, "proj", "proj")
	require.NoError(t, err)

	agent, _, err := svcs.Agents.CreateAgent(ctx, org, "scoped-bot", meshdb.RoleMember, "", nil, owner)
	require.NoError(t, err)

	// Before any grant, the agent (a plain member) is denied deploy.
	err = svcs.Permissions.CheckAccess(ctx, org, agent.ID, project.ID, meshdb.ResourceProject, meshdb.ActionDeploy, &project.ID)
	require.Error(t, err)

	// Grant deploy on the project → CheckAccess now passes.
	require.NoError(t, svcs.Permissions.Grant(ctx, org, agent.ID, project.ID, meshdb.ResourceProject, meshdb.ActionDeploy))
	err = svcs.Permissions.CheckAccess(ctx, org, agent.ID, project.ID, meshdb.ResourceProject, meshdb.ActionDeploy, &project.ID)
	require.NoError(t, err)
}

// The migration needs an identity to create things through the API. It gets an
// agent: attributable, revocable, and with no expiry, because a migration runs
// over days.
func TestMigrationAgentIsMintedOnceAndReusedWithAFreshToken(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "mig", Slug: "mig"}
	require.NoError(t, gdb.Create(&org).Error)
	owner := meshdb.User{Username: "owner", Email: "owner@example.com"}
	require.NoError(t, gdb.Create(&owner).Error)

	agentID, tokenID, token, err := svcs.Agents.EnsureMigrationAgent(ctx, org.ID, owner.ID)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(token, "magt-"), "token = %q", token)
	assert.NotEqual(t, uuid.Nil, agentID)
	assert.NotEqual(t, uuid.Nil, tokenID)

	// Admin, never owner: enough to create projects, services and routes, and
	// short of handing the organisation away.
	var member meshdb.OrganizationMember
	require.NoError(t, gdb.First(&member, "user_id = ?", agentID).Error)
	assert.Equal(t, meshdb.RoleAdmin, member.Role)

	// The API accepts it as a principal.
	resolved, ok := svcs.Agents.ResolveToken(ctx, token)
	require.True(t, ok)
	assert.Equal(t, agentID, resolved)

	// Asked again, the same agent gets a new token rather than the call failing
	// - the old one may be on a host that no longer has it.
	againID, _, second, err := svcs.Agents.EnsureMigrationAgent(ctx, org.ID, owner.ID)
	require.NoError(t, err)
	assert.Equal(t, agentID, againID)
	assert.NotEqual(t, token, second)

	var agents int64
	require.NoError(t, gdb.Model(&meshdb.User{}).Where("kind = ?", meshdb.UserAgent).Count(&agents).Error)
	assert.EqualValues(t, 1, agents, "a second agent should not have been created")
}

// Finish revokes it, and every token it held stops working.
func TestRevokingTheMigrationAgent(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "mig2", Slug: "mig2"}
	require.NoError(t, gdb.Create(&org).Error)
	owner := meshdb.User{Username: "owner2", Email: "owner2@example.com"}
	require.NoError(t, gdb.Create(&owner).Error)

	_, _, token, err := svcs.Agents.EnsureMigrationAgent(ctx, org.ID, owner.ID)
	require.NoError(t, err)
	_, ok := svcs.Agents.ResolveToken(ctx, token)
	require.True(t, ok)

	require.NoError(t, svcs.Agents.RevokeMigrationAgent(ctx, org.ID))
	if _, ok := svcs.Agents.ResolveToken(ctx, token); ok {
		t.Error("the token still works after the agent was revoked")
	}
	// Idempotent: an operator may well have deleted it themselves.
	require.NoError(t, svcs.Agents.RevokeMigrationAgent(ctx, org.ID))
}
