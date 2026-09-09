package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func servicesWithSetupToken(t *testing.T, token string) *service.Services {
	t.Helper()
	return service.New(newTestDB(t), &config.Config{
		SetupToken:    token,
		JWTSecret:     "test-secret",
		EncryptionKey: "0123456789abcdef0123456789abcdef",
	})
}

// The first account on a server owns it. Without a token, anyone who reaches
// the box during the install window can claim it — which is exactly what
// publishing the console on an IP would expose.
func TestFirstRegistrationRequiresTheSetupToken(t *testing.T) {
	ctx := context.Background()
	svcs := servicesWithSetupToken(t, "ms_secret_token")

	_, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "intruder", Email: "intruder@example.com", Password: "password123",
	})
	require.Error(t, err, "registration with no token must be refused")
	assert.ErrorIs(t, err, service.ErrInvalidSetupToken)

	_, err = svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "intruder", Email: "intruder@example.com", Password: "password123",
		SetupToken: "ms_wrong_token",
	})
	assert.ErrorIs(t, err, service.ErrInvalidSetupToken, "a wrong token must be refused")

	owner, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "owner", Email: "owner@example.com", Password: "password123",
		SetupToken: "ms_secret_token",
	})
	require.NoError(t, err, "the correct token must be accepted")
	assert.Equal(t, "owner", owner.Username)
}

// An install that predates the token, and a developer running `go run main.go`,
// both have no token configured. Neither may be locked out of registering.
func TestNoSetupTokenConfiguredLeavesRegistrationOpen(t *testing.T) {
	ctx := context.Background()
	svcs := servicesWithSetupToken(t, "")

	owner, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "owner", Email: "owner@example.com", Password: "password123",
	})
	require.NoError(t, err, "an unconfigured token must not block the first registration")
	assert.Equal(t, "owner", owner.Username)
}

// The token only guards the empty-server window. Once an owner exists the
// server is claimed, and that refusal must be reported as such rather than as a
// token problem — the operator would otherwise go hunting for a token that
// would not have helped.
func TestSetupTokenIsIrrelevantOnceClaimed(t *testing.T) {
	ctx := context.Background()
	svcs := servicesWithSetupToken(t, "ms_secret_token")

	_, err := svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "owner", Email: "owner@example.com", Password: "password123",
		SetupToken: "ms_secret_token",
	})
	require.NoError(t, err)

	_, err = svcs.Auth.Register(ctx, service.RegisterInput{
		Username: "second", Email: "second@example.com", Password: "password123",
		SetupToken: "ms_secret_token",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrRegistrationClosed)
	assert.False(t, errors.Is(err, service.ErrInvalidSetupToken))
}
