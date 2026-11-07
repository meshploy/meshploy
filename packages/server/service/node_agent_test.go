package service_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	meshdb "github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentCert is a self-signed certificate naming the cluster agent, as
// meshploy-cluster-agent init makes one.
func agentCert(t *testing.T, name string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func agentToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return service.ClusterAgentTokenPrefix + hex.EncodeToString(b)
}

func validAgent(t *testing.T) *service.ClusterAgentRegistration {
	return &service.ClusterAgentRegistration{CertPEM: agentCert(t, appk8s.ClusterAgentServerName), Token: agentToken(), Port: 7443}
}

// agentMode is an API that reaches its cluster through a cluster agent, with
// a mesh whose peers include 100.64.0.2.
func agentMode(t *testing.T) (*service.Services, *appk8s.AgentCluster, *fakeHeadscale, meshdb.Organization) {
	t.Helper()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	hs := &fakeHeadscale{peers: map[string]string{"1": "100.64.0.2"}}
	srv := httptest.NewServer(hs)
	t.Cleanup(srv.Close)
	svcs.Nodes.SetHeadscaleForTest(service.NewHeadscaleService(srv.URL, "test-key"))
	a := service.UseClusterAgentForTest(svcs)
	org := meshdb.Organization{Name: "edge", Slug: "edge-" + hex.EncodeToString([]byte(t.Name()))[:8]}
	require.NoError(t, gdb.Create(&org).Error)
	return svcs, a, hs, org
}

// noAgentStored asserts no node carries an agent's identity.
func noAgentStored(t *testing.T, svcs *service.Services) {
	t.Helper()
	var n int64
	require.NoError(t, svcs.DB.Model(&meshdb.Node{}).Where("cluster_agent_cert <> ''").Count(&n).Error)
	assert.Zero(t, n, "a refused registration stored an agent's identity")
}

func TestAClusterAgentRegisters(t *testing.T) {
	ctx := context.Background()
	svcs, agent, _, org := agentMode(t)
	tok, row, err := svcs.Nodes.CreateClusterAgentToken(ctx, org.ID, nil, "edge server", nil)
	require.NoError(t, err)
	assert.True(t, row.ClusterAgent)

	reg := validAgent(t)
	node, secret, err := svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "edge-1", "100.64.0.2", "", "", reg)
	require.NoError(t, err)
	assert.NotEmpty(t, secret)
	assert.Equal(t, meshdb.K3sRoleServer, node.K3sRole, "the agent's node holds the cluster's server")
	assert.True(t, node.HasClusterAgent())

	cur, ok := agent.Current()
	require.True(t, ok, "the API's cluster points at the agent once it registers")
	assert.Equal(t, "100.64.0.2:7443", cur.Addr)
	assert.Equal(t, reg.Token, cur.Token)
	assert.Equal(t, reg.CertPEM, string(cur.CertPEM))

	// The token is stored encrypted and never leaves in a node's JSON.
	var raw struct{ ClusterAgentToken string }
	require.NoError(t, svcs.DB.Raw("SELECT cluster_agent_token FROM nodes WHERE id = ?", node.ID).Scan(&raw).Error)
	assert.NotContains(t, raw.ClusterAgentToken, reg.Token)
	out, _ := json.Marshal(node)
	assert.NotContains(t, string(out), reg.Token)
	assert.NotContains(t, string(out), "BEGIN CERTIFICATE")
}

// Every way a registration could write an agent's identity it should not.
func TestClusterAgentRegistrationRefusals(t *testing.T) {
	ctx := context.Background()

	t.Run("an API that reaches its cluster directly takes no agent", func(t *testing.T) {
		gdb := newTestDB(t)
		svcs := newServices(gdb)
		orgID := seedOrg(t, gdb, "direct", nil)
		_, _, err := svcs.Nodes.CreateClusterAgentToken(ctx, orgID, nil, "x", nil)
		require.ErrorIs(t, err, service.ErrClusterAgentNotExpected)

		tok, row, err := svcs.Nodes.CreateProvisioningToken(ctx, orgID, "forced", nil)
		require.NoError(t, err)
		require.NoError(t, gdb.Model(row).Update("cluster_agent", true).Error)
		_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "edge", "100.64.0.2", "", "", validAgent(t))
		require.ErrorIs(t, err, service.ErrClusterAgentNotExpected)
		var n int64
		require.NoError(t, gdb.Model(&meshdb.Node{}).Where("organization_id = ?", orgID).Count(&n).Error)
		assert.Zero(t, n, "a refused agent leaves no node behind")
		var used meshdb.NodeProvisioningToken
		require.NoError(t, gdb.First(&used, "id = ?", row.ID).Error)
		assert.Nil(t, used.UsedAt, "a refused agent does not spend the token")
	})

	t.Run("an ordinary token with an agent's identity", func(t *testing.T) {
		svcs, agent, _, org := agentMode(t)
		tok, _, err := svcs.Nodes.CreateProvisioningToken(ctx, org.ID, "worker", nil)
		require.NoError(t, err)
		_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "evil", "100.64.0.2", "", "", validAgent(t))
		require.ErrorIs(t, err, service.ErrClusterAgentToken)
		_, ok := agent.Current()
		assert.False(t, ok)
		noAgentStored(t, svcs)
	})

	t.Run("an agent's token without an identity", func(t *testing.T) {
		svcs, _, _, org := agentMode(t)
		tok, _, err := svcs.Nodes.CreateClusterAgentToken(ctx, org.ID, nil, "edge", nil)
		require.NoError(t, err)
		_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "worker", "100.64.0.2", "", "")
		require.ErrorIs(t, err, service.ErrClusterAgentToken)
	})

	t.Run("identities that cannot be an agent's", func(t *testing.T) {
		svcs, _, _, org := agentMode(t)
		good := validAgent(t)
		bad := map[string]*service.ClusterAgentRegistration{
			"port 0":                {CertPEM: good.CertPEM, Token: good.Token, Port: 0},
			"port too high":         {CertPEM: good.CertPEM, Token: good.Token, Port: 70000},
			"another token kind":    {CertPEM: good.CertPEM, Token: "mreg-" + strings.Repeat("a", 64), Port: 7443},
			"a short token":         {CertPEM: good.CertPEM, Token: service.ClusterAgentTokenPrefix + "abc", Port: 7443},
			"a token not in hex":    {CertPEM: good.CertPEM, Token: service.ClusterAgentTokenPrefix + strings.Repeat("z", 64), Port: 7443},
			"not a certificate":     {CertPEM: "hello", Token: good.Token, Port: 7443},
			"two certificates":      {CertPEM: good.CertPEM + agentCert(t, appk8s.ClusterAgentServerName), Token: good.Token, Port: 7443},
			"a certificate for www": {CertPEM: agentCert(t, "www.example.com"), Token: good.Token, Port: 7443},
		}
		for name, reg := range bad {
			tok, _, err := svcs.Nodes.CreateClusterAgentToken(ctx, org.ID, nil, name, nil)
			require.NoError(t, err)
			_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "edge", "100.64.0.2", "", "", reg)
			assert.ErrorIs(t, err, service.ErrClusterAgentIdentity, name)
		}
		noAgentStored(t, svcs)
	})

	t.Run("an address no machine on the mesh holds", func(t *testing.T) {
		svcs, agent, _, org := agentMode(t)
		tok, _, err := svcs.Nodes.CreateClusterAgentToken(ctx, org.ID, nil, "edge", nil)
		require.NoError(t, err)
		_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "edge", "100.64.0.99", "", "", validAgent(t))
		require.ErrorIs(t, err, service.ErrClusterAgentAddress)
		_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "edge", "not-an-ip", "", "", validAgent(t))
		require.ErrorIs(t, err, service.ErrClusterAgentAddress)
		_, ok := agent.Current()
		assert.False(t, ok)
		noAgentStored(t, svcs)
	})

	t.Run("a non-Linux machine", func(t *testing.T) {
		svcs, _, _, org := agentMode(t)
		tok, _, err := svcs.Nodes.CreateClusterAgentToken(ctx, org.ID, nil, "edge", nil)
		require.NoError(t, err)
		_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "mac", "100.64.0.2", "", meshdb.NodeOSDarwin, validAgent(t))
		require.ErrorIs(t, err, service.ErrNonLinuxClusterRole)
	})
}

// The first agent's identity sticks: a second agent, with a token minted
// before the first registered or after, is refused, and the first stays.
func TestTheFirstClusterAgentSticks(t *testing.T) {
	ctx := context.Background()
	svcs, agent, hs, org := agentMode(t)
	hs.set(func(f *fakeHeadscale) { f.peers["2"] = "100.64.0.3" })
	tokA, _, err := svcs.Nodes.CreateClusterAgentToken(ctx, org.ID, nil, "a", nil)
	require.NoError(t, err)
	tokB, _, err := svcs.Nodes.CreateClusterAgentToken(ctx, org.ID, nil, "b", nil)
	require.NoError(t, err)

	first := validAgent(t)
	_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, tokA, "edge-a", "100.64.0.2", "", "", first)
	require.NoError(t, err)

	_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, tokB, "edge-b", "100.64.0.3", "", "", validAgent(t))
	require.ErrorIs(t, err, service.ErrClusterAgentRegistered)
	_, _, err = svcs.Nodes.CreateClusterAgentToken(ctx, org.ID, nil, "c", nil)
	require.ErrorIs(t, err, service.ErrClusterAgentRegistered)

	cur, _ := agent.Current()
	assert.Equal(t, first.Token, cur.Token, "the first agent is still the one reached")
	var n int64
	require.NoError(t, svcs.DB.Model(&meshdb.Node{}).Where("cluster_agent_cert <> ''").Count(&n).Error)
	assert.EqualValues(t, 1, n)
}

// One token, many registrations at once: one node. For an agent's token and
// for an ordinary one.
func TestAProvisioningTokenRegistersOneNodeUnderARace(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"agent", "ordinary"} {
		t.Run(kind, func(t *testing.T) {
			svcs, _, _, org := agentMode(t)
			var tok string
			var err error
			if kind == "agent" {
				tok, _, err = svcs.Nodes.CreateClusterAgentToken(ctx, org.ID, nil, "race", nil)
			} else {
				tok, _, err = svcs.Nodes.CreateProvisioningToken(ctx, org.ID, "race", nil)
			}
			require.NoError(t, err)
			var wg sync.WaitGroup
			var mu sync.Mutex
			ok := 0
			for i := 0; i < 6; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					var reg *service.ClusterAgentRegistration
					if kind == "agent" {
						reg = validAgent(t)
					}
					if _, _, err := svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "racer-"+string(rune('a'+i)), "100.64.0.2", "", "", reg); err == nil {
						mu.Lock()
						ok++
						mu.Unlock()
					}
				}(i)
			}
			wg.Wait()
			assert.Equal(t, 1, ok, "registrations that succeeded")
			var n int64
			require.NoError(t, svcs.DB.Model(&meshdb.Node{}).Where("organization_id = ?", org.ID).Count(&n).Error)
			assert.EqualValues(t, 1, n, "nodes made")
		})
	}
}

func TestRevokingAClusterAgent(t *testing.T) {
	ctx := context.Background()
	svcs, agent, _, org := agentMode(t)
	tok, _, err := svcs.Nodes.CreateClusterAgentToken(ctx, org.ID, nil, "edge", nil)
	require.NoError(t, err)
	node, _, err := svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "edge", "100.64.0.2", "", "", validAgent(t))
	require.NoError(t, err)

	require.NoError(t, svcs.Nodes.RevokeClusterAgent(ctx, node.ID))
	_, ok := agent.Current()
	assert.False(t, ok, "the cluster is no longer reached through a revoked agent")
	got, err := svcs.Nodes.Get(ctx, node.ID)
	require.NoError(t, err)
	assert.False(t, got.HasClusterAgent())
	assert.Empty(t, string(got.ClusterAgentToken))
	require.ErrorIs(t, svcs.Nodes.RevokeClusterAgent(ctx, node.ID), service.ErrNoClusterAgentOnNode)

	// The same machine registers a new agent: it keeps its node, so no second
	// record holds the cluster's server.
	tok2, _, err := svcs.Nodes.CreateClusterAgentToken(ctx, org.ID, nil, "edge again", nil)
	require.NoError(t, err)
	again, secret, err := svcs.Nodes.RegisterWithProvisioningToken(ctx, tok2, "edge-renamed", "100.64.0.2", "", "", validAgent(t))
	require.NoError(t, err)
	assert.Equal(t, node.ID, again.ID, "the machine's node is kept")
	assert.Equal(t, "edge-renamed", again.Name)
	_, ok = agent.Current()
	assert.True(t, ok)
	var n int64
	require.NoError(t, svcs.DB.Model(&meshdb.Node{}).Where("organization_id = ?", org.ID).Count(&n).Error)
	assert.EqualValues(t, 1, n, "nodes after registering again")
	require.NoError(t, svcs.Nodes.ValidateNodeSecret(ctx, node.ID, secret), "the new node secret is the one that counts")
}

// An API that starts with an agent already registered reaches it at once.
func TestARegisteredClusterAgentIsLoadedAtStart(t *testing.T) {
	ctx := context.Background()
	svcs, _, _, org := agentMode(t)
	tok, _, err := svcs.Nodes.CreateClusterAgentToken(ctx, org.ID, nil, "edge", nil)
	require.NoError(t, err)
	reg := validAgent(t)
	_, _, err = svcs.Nodes.RegisterWithProvisioningToken(ctx, tok, "edge", "100.64.0.2", "", "", reg)
	require.NoError(t, err)

	restarted := newServices(svcs.DB)
	agent := service.UseClusterAgentForTest(restarted)
	require.NoError(t, restarted.Nodes.LoadClusterAgent(ctx))
	cur, ok := agent.Current()
	require.True(t, ok)
	assert.Equal(t, reg.Token, cur.Token, "the token comes back decrypted")
	assert.Equal(t, "100.64.0.2:7443", cur.Addr)
}
