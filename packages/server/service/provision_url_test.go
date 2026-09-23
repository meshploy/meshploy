package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// provisioningHeadscale answers the two calls a provision makes: find the user, mint a
// pre-auth key.
func provisioningHeadscale(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"users": []map[string]any{{"id": "1", "name": "meshploy"}}})
		case "/api/v1/preauthkey":
			_ = json.NewEncoder(w).Encode(map[string]any{"preAuthKey": map[string]any{
				"key": "hskey-test", "expiration": time.Now().Add(time.Hour),
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A machine being provisioned is outside the gateway, so the control URL it is
// handed must be the name the internet reaches Headscale by.
//
// It used to be HEADSCALE_URL, which docker-compose sets to the API's own
// in-network address, http://headscale:8080. Nothing outside the gateway can
// resolve that, so every one-command join failed at `tailscale up`. The earlier
// tests never reached a successful provision - there was no Headscale to mint a
// key - which is how it got through.
func TestProvisionHandsOutThePublicControlURL(t *testing.T) {
	ctx := context.Background()
	hs := provisioningHeadscale(t)
	gdb := newTestDB(t)
	svcs := service.New(gdb, &config.Config{
		HeadscaleURL: hs.URL, HeadscaleKey: "k", // the in-network address, as compose sets it
		Domain: "install.test", GatewayIP: "100.64.0.1",
	})
	org := &meshdb.Organization{Name: "acme", Slug: "acme"}
	require.NoError(t, gdb.Create(org).Error)
	require.NoError(t, svcs.Domains.CreateSeeded(ctx, org.ID, "install.test", meshdb.DNSModeDelegation))

	tok, _, err := svcs.Nodes.CreateProvisioningToken(ctx, org.ID, "worker", nil)
	require.NoError(t, err)
	out, err := svcs.Nodes.Provision(ctx, tok)
	require.NoError(t, err)
	assert.Equal(t, "https://headscale.install.test", out.HeadscaleURL)
	assert.NotEqual(t, hs.URL, out.HeadscaleURL, "never the API's own address for Headscale")
	assert.Equal(t, "hskey-test", out.PreAuthKey)

	// And it follows the primary, which can move.
	second, err := svcs.Domains.Create(ctx, org.ID, "new.test", meshdb.DNSModeDelegation)
	require.NoError(t, err)
	require.NoError(t, gdb.Model(&meshdb.Domain{}).Where("organization_id = ?", org.ID).Update("is_primary", false).Error)
	require.NoError(t, gdb.Model(second).Updates(map[string]any{"verified": true, "is_primary": true}).Error)

	tok, _, err = svcs.Nodes.CreateProvisioningToken(ctx, org.ID, "worker-2", nil)
	require.NoError(t, err)
	out, err = svcs.Nodes.Provision(ctx, tok)
	require.NoError(t, err)
	assert.Equal(t, "https://headscale.new.test", out.HeadscaleURL)
}

// With no domain at all there is nothing public to join through. Saying so
// beats handing over an address that cannot work.
func TestProvisionRefusesWithoutAPublicDomain(t *testing.T) {
	ctx := context.Background()
	hs := provisioningHeadscale(t)
	gdb := newTestDB(t)
	svcs := service.New(gdb, &config.Config{HeadscaleURL: hs.URL, HeadscaleKey: "k"})
	org := &meshdb.Organization{Name: "acme", Slug: "acme"}
	require.NoError(t, gdb.Create(org).Error)

	tok, _, err := svcs.Nodes.CreateProvisioningToken(ctx, org.ID, "worker", nil)
	require.NoError(t, err)
	_, err = svcs.Nodes.Provision(ctx, tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no public domain")
}
