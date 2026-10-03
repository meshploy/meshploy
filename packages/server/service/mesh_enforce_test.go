package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// policyHeadscale answers the node list and records every policy it is given.
type policyHeadscale struct {
	mu       sync.Mutex
	policies []string
	fail     bool
}

func (f *policyHeadscale) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/node":
			_ = json.NewEncoder(w).Encode(map[string]any{"nodes": []map[string]any{
				{"id": "1", "givenName": "gw-1", "ipAddresses": []string{"100.64.0.1", "fd7a:115c:a1e0::1"}},
				{"id": "2", "givenName": "ravi-laptop", "ipAddresses": []string{"100.64.0.4", "fd7a:115c:a1e0::4"}},
				{"id": "3", "givenName": "joined-by-hand", "ipAddresses": []string{"100.64.0.9", "fd7a:115c:a1e0::9"}},
			}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/policy":
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.fail {
				http.Error(w, `{"message":"policy mode is file"}`, http.StatusInternalServerError)
				return
			}
			var body struct{ Policy string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.policies = append(f.policies, body.Policy)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	})
}

func (f *policyHeadscale) given() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.policies...)
}

// Enforcing gives Headscale the generated policy, IPv6 addresses included;
// an unchanged policy is not given twice; switching off gives every machine
// back every other; a failure is recorded for the console to show; and a
// machine joined by hand is reported, not given anything.
func TestEnforcingGivesHeadscaleThePolicy(t *testing.T) {
	ctx := context.Background()
	fake := &policyHeadscale{}
	hs := httptest.NewServer(fake.handler())
	defer hs.Close()

	gdb := newTestDB(t)
	svcs := service.New(gdb, &config.Config{HeadscaleURL: hs.URL, HeadscaleKey: "test"})
	owner, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "owner", Email: "owner@example.com", Password: "password123"})
	require.NoError(t, err)
	orgs, err := svcs.Orgs.ListForUser(ctx, owner.ID)
	require.NoError(t, err)
	orgID := orgs[0].ID
	_, err = svcs.Nodes.Register(ctx, orgID, "gw-1", "100.64.0.1", meshdb.K3sRoleServer)
	require.NoError(t, err)
	token, _, err := svcs.Nodes.CreateProvisioningTokenBy(ctx, orgID, &owner.ID, "laptop", nil, meshdb.MeshRoleMesh)
	require.NoError(t, err)
	_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, token, "ravi-laptop", "100.64.0.4", meshdb.MeshRoleMesh, meshdb.NodeOSLinux)
	require.NoError(t, err)

	svcs.MeshAccess.Sync(ctx)
	require.Empty(t, fake.given(), "never enforced: Headscale has no policy and is told nothing")

	policy, err := svcs.MeshAccess.Policy(ctx)
	require.NoError(t, err)
	require.Len(t, policy.Unknown, 1)
	require.Equal(t, "joined-by-hand", policy.Unknown[0].Name)

	require.NoError(t, svcs.MeshAccess.SetEnforced(ctx, true, owner.ID))
	svcs.MeshAccess.Sync(ctx)
	given := fake.given()
	require.NotEmpty(t, given)
	last := given[len(given)-1]
	require.Contains(t, last, `"100.64.0.4/32"`)
	require.Contains(t, last, `"fd7a:115c:a1e0::4/128"`, "the IPv6 address is covered too")
	require.NotContains(t, last, "100.64.0.9", "a machine joined by hand is given nothing")
	st, err := svcs.MeshAccess.State(ctx)
	require.NoError(t, err)
	require.True(t, st.Enforced)
	require.NotNil(t, st.AppliedAt)
	require.Empty(t, st.LastError)

	n := len(fake.given())
	svcs.MeshAccess.Sync(ctx)
	require.Len(t, fake.given(), n, "an unchanged policy is not given again")

	require.NoError(t, svcs.MeshAccess.SetEnforced(ctx, false, owner.ID))
	svcs.MeshAccess.Sync(ctx)
	given = fake.given()
	require.Equal(t, service.MeshAllowAll, given[len(given)-1], "switched off, every machine reaches every other")

	fake.mu.Lock()
	fake.fail = true
	fake.mu.Unlock()
	require.NoError(t, svcs.MeshAccess.SetEnforced(ctx, true, owner.ID))
	svcs.MeshAccess.Sync(ctx)
	st, err = svcs.MeshAccess.State(ctx)
	require.NoError(t, err)
	require.True(t, strings.Contains(st.LastError, "policy mode is file"), st.LastError)
}
