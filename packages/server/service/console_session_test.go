package service_test

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sidOf is the console session a signed-in token names.
func sidOf(t *testing.T, token string) uuid.UUID {
	t.Helper()
	tok, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return []byte("s"), nil })
	require.NoError(t, err)
	sid, err := uuid.Parse(tok.Claims.(jwt.MapClaims)["sid"].(string))
	require.NoError(t, err)
	return sid
}

// Every sign-in is a session its person can see and end: logging out ends
// it at once, a new password ends all but the one that changed it, and an
// admin ends a member's only while that member is in this organisation alone.
func TestConsoleSignInsAreSessionsThatCanBeEnded(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	ravi, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "ravi", Email: "ravi@example.com", Password: "password123"})
	require.NoError(t, err)

	login := func(agent string) uuid.UUID {
		res, err := svcs.Auth.Login(ctx, service.LoginInput{Email: "ravi@example.com", Password: "password123",
			Client: service.Client{UserAgent: agent, IP: "203.0.113.7"}}, "s")
		require.NoError(t, err)
		return sidOf(t, res.Token)
	}
	laptop, phone, tablet := login("Firefox on Linux"), login("Safari on iPhone"), login("Chrome on iPad")
	for _, sid := range []uuid.UUID{laptop, phone, tablet} {
		assert.True(t, svcs.Sessions.Resolve(ctx, sid, ravi.ID))
	}
	assert.False(t, svcs.Sessions.Resolve(ctx, laptop, uuid.New()), "a session is one person's")

	mine, err := svcs.Sessions.List(ctx, ravi.ID, laptop)
	require.NoError(t, err)
	require.Len(t, mine, 3)
	current := 0
	for _, s := range mine {
		if s.Current {
			current++
			assert.Equal(t, laptop, s.ID)
		}
		assert.Equal(t, "203.0.113.7", s.IP)
	}
	assert.Equal(t, 1, current)

	// Logging out ends that session, even while its check is still reused.
	require.NoError(t, svcs.Sessions.End(ctx, tablet))
	assert.False(t, svcs.Sessions.Resolve(ctx, tablet, ravi.ID))

	// A new password signs out everywhere else.
	require.NoError(t, svcs.Auth.ChangePassword(ctx, ravi.ID, "password123", "password456", laptop))
	assert.True(t, svcs.Sessions.Resolve(ctx, laptop, ravi.ID), "the session that changed it stays")
	assert.False(t, svcs.Sessions.Resolve(ctx, phone, ravi.ID))

	// An admin ends a member's session while the member is theirs alone.
	var orgs []meshdb.Organization
	require.NoError(t, gdb.Find(&orgs).Error)
	require.Len(t, orgs, 1)
	listed, err := svcs.Sessions.OrgSessions(ctx, orgs[0].ID, nil)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.False(t, listed[0].Elsewhere)

	other := meshdb.Organization{Name: "Other", Slug: "other"}
	require.NoError(t, gdb.Create(&other).Error)
	require.NoError(t, gdb.Create(&meshdb.OrganizationMember{OrganizationID: other.ID, UserID: ravi.ID, Role: meshdb.RoleMember}).Error)
	assert.ErrorIs(t, svcs.Sessions.RevokeInOrg(ctx, orgs[0].ID, laptop), service.ErrSessionElsewhere)
	assert.True(t, svcs.Sessions.Resolve(ctx, laptop, ravi.ID))

	require.NoError(t, gdb.Where("organization_id = ?", other.ID).Delete(&meshdb.OrganizationMember{}).Error)
	require.NoError(t, svcs.Sessions.RevokeInOrg(ctx, orgs[0].ID, laptop))
	assert.False(t, svcs.Sessions.Resolve(ctx, laptop, ravi.ID))
}
