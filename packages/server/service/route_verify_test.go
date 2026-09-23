package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A custom hostname gets a verification token when the route is created, and
// the certificate check refuses it until the TXT record carrying that token is
// found.
func TestCustomHostnameStartsUnverifiedWithAToken(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	route := e.httpRoute(t, "store.customer.example", false)

	assert.False(t, route.CustomDomainVerified)
	assert.Len(t, route.CustomDomainVerifyToken, 32)
	assert.False(t, e.svcs.Routes.IsCustomDomainVerified(ctx, "store.customer.example"),
		"no certificate may be issued for a hostname nobody has proved")

	// The lookup is real DNS, and this name has no record: refused.
	_, err := e.svcs.Routes.VerifyCustomHostname(ctx, route.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TXT record not found")
}

// The column defaults to empty. An empty token must never be accepted: it
// would be "found" in any TXT set that holds an empty string.
func TestVerifyIssuesATokenInsteadOfMatchingAnEmptyOne(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	route := e.httpRoute(t, "legacy.customer.example", false)
	require.NoError(t, e.gdb.Model(route).Update("custom_domain_verify_token", "").Error)

	_, err := e.svcs.Routes.VerifyCustomHostname(ctx, route.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has been issued")

	var stored meshdb.Route
	require.NoError(t, e.gdb.First(&stored, "id = ?", route.ID).Error)
	assert.Len(t, stored.CustomDomainVerifyToken, 32, "a token now exists to publish")
	assert.False(t, stored.CustomDomainVerified)
}

// Verifying twice is not an error, and cannot undo the first.
func TestVerifyingAVerifiedHostnameIsANoOp(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	route := e.httpRoute(t, "done.customer.example", false)
	require.NoError(t, e.gdb.Model(route).Update("custom_domain_verified", true).Error)

	got, err := e.svcs.Routes.VerifyCustomHostname(ctx, route.ID)
	require.NoError(t, err)
	assert.True(t, got.CustomDomainVerified)
}
