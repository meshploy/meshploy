package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
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
	assert.Contains(t, err.Error(), "TXT record")
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

// A CNAME to one of the same project's published addresses proves the name as
// the TXT record does; one to an address elsewhere, or to a paused route,
// proves nothing.
func TestACNAMEToTheProjectsOwnAddressVerifies(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	own := e.httpRoute(t, "blog.acme.example.net", false)
	paused := e.httpRoute(t, "paused.acme.example.net", true)
	custom := e.httpRoute(t, "blog.customer.example", false)
	_, _ = own, paused

	defer service.UseDNSForTest(map[string]string{"blog.customer.example": "elsewhere.example.org."}, nil)()
	_, err := e.svcs.Routes.VerifyCustomHostname(ctx, custom.ID)
	require.Error(t, err, "a CNAME to an address outside the project")

	service.UseDNSForTest(map[string]string{"blog.customer.example": "paused.acme.example.net."}, nil)
	_, err = e.svcs.Routes.VerifyCustomHostname(ctx, custom.ID)
	require.Error(t, err, "a CNAME to a paused route")

	service.UseDNSForTest(map[string]string{"blog.customer.example": "Blog.Acme.Example.NET."}, nil)
	got, err := e.svcs.Routes.VerifyCustomHostname(ctx, custom.ID)
	require.NoError(t, err)
	assert.True(t, got.CustomDomainVerified)
}
