package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/meshploy/apps/api/internal/service"
	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testJWTSecret = "test-jwt-secret"

func TestRegister(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)

	t.Run("creates user org and owner membership", func(t *testing.T) {
		user, err := svcs.Auth.Register(ctx, service.RegisterInput{
			Username: "alice",
			Email:    "alice@example.com",
			Password: "secret123",
		})
		require.NoError(t, err)
		require.NotEmpty(t, user.ID)
		assert.Equal(t, "alice", user.Username)
		assert.Equal(t, "alice@example.com", user.Email)
		assert.NotEqual(t, "secret123", user.Password, "password must be hashed")

		var orgCount, memberCount int64
		require.NoError(t, db.Model(&meshdb.Organization{}).Count(&orgCount).Error)
		require.NoError(t, db.Model(&meshdb.OrganizationMember{}).Count(&memberCount).Error)
		assert.Equal(t, int64(1), orgCount)
		assert.Equal(t, int64(1), memberCount)
	})

	t.Run("duplicate email returns error", func(t *testing.T) {
		db2 := newTestDB(t)
		svcs2 := newServices(db2)

		_, err := svcs2.Auth.Register(ctx, service.RegisterInput{
			Username: "bob",
			Email:    "bob@example.com",
			Password: "pass",
		})
		require.NoError(t, err)

		_, err = svcs2.Auth.Register(ctx, service.RegisterInput{
			Username: "bob2",
			Email:    "bob@example.com",
			Password: "pass2",
		})
		require.Error(t, err)
	})

	t.Run("duplicate username rolls back — no partial rows", func(t *testing.T) {
		db3 := newTestDB(t)
		svcs3 := newServices(db3)

		_, err := svcs3.Auth.Register(ctx, service.RegisterInput{
			Username: "charlie",
			Email:    "charlie@example.com",
			Password: "pass",
		})
		require.NoError(t, err)

		// Same username → same org slug → unique constraint fails.
		_, err = svcs3.Auth.Register(ctx, service.RegisterInput{
			Username: "charlie",
			Email:    "charlie2@example.com",
			Password: "pass",
		})
		require.Error(t, err)

		// Only the first user must exist (tx rolled back).
		var userCount int64
		require.NoError(t, db3.Model(&meshdb.User{}).Count(&userCount).Error)
		assert.Equal(t, int64(1), userCount)
	})
}

func TestLogin(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)

	_, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "dave",
		Email:    "dave@example.com",
		Password: "correct-password",
	})
	require.NoError(t, err)

	t.Run("valid credentials return JWT", func(t *testing.T) {
		result, err := svcs.Auth.Login(ctx, service.LoginInput{
			Email:    "dave@example.com",
			Password: "correct-password",
		}, testJWTSecret)
		require.NoError(t, err)
		assert.NotEmpty(t, result.Token)
		assert.False(t, result.TOTPRequired)
		assert.Equal(t, 3, len(strings.Split(result.Token, ".")), "JWT must have 3 parts")
	})

	t.Run("wrong password returns error", func(t *testing.T) {
		_, err := svcs.Auth.Login(ctx, service.LoginInput{
			Email:    "dave@example.com",
			Password: "wrong-password",
		}, testJWTSecret)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid credentials")
	})

	t.Run("unknown email returns error", func(t *testing.T) {
		_, err := svcs.Auth.Login(ctx, service.LoginInput{
			Email:    "nobody@example.com",
			Password: "pass",
		}, testJWTSecret)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid credentials")
	})
}
