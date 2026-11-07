package handler

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	appk8s "github.com/meshploy/packages/server/k8s"
	"github.com/meshploy/packages/server/middleware"
	"github.com/meshploy/packages/server/service"
)

func clusterAgentBody(t *testing.T) *ClusterAgentIdentityBody {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: appk8s.ClusterAgentServerName},
		DNSNames: []string{appk8s.ClusterAgentServerName}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	return &ClusterAgentIdentityBody{
		Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		Token:       service.ClusterAgentTokenPrefix + hex.EncodeToString(raw),
		Port:        7443,
	}
}

func statusOf(err error) int {
	var se huma.StatusError
	if errors.As(err, &se) {
		return se.GetStatus()
	}
	return 0
}

// An API that reaches its cluster through a cluster agent, its owner, and a
// member.
func setupAgentMode(t *testing.T) (*Handler, *service.Services, uuid.UUID, context.Context, context.Context) {
	t.Helper()
	ctx := context.Background()
	database := newAuthzTestDB(t)
	svc := service.New(database, &config.Config{ClusterAgent: true})
	h := New(nil, svc)
	owner, err := svc.Auth.Register(ctx, service.RegisterInput{Username: "owner", Email: "owner@example.com", Password: "password123"})
	if err != nil {
		t.Fatal(err)
	}
	orgs, _ := svc.Orgs.ListForUser(ctx, owner.ID)
	org := orgs[0]
	inv, err := svc.Orgs.CreateInvitation(ctx, org.ID, owner.ID, "member@example.com", db.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	member, err := svc.Orgs.AcceptInvitation(ctx, inv.Token, "member", "password123")
	if err != nil {
		t.Fatal(err)
	}
	return h, svc, org.ID, middleware.ContextWithUser(ctx, owner.ID), middleware.ContextWithUser(ctx, member.ID)
}

func TestClusterAgentRegistrationOverHTTP(t *testing.T) {
	h, svc, orgID, adminCtx, memberCtx := setupAgentMode(t)
	ctx := context.Background()

	// Only an admin mints the agent's token.
	mint := &CreateProvisioningTokenInput{OrgID: orgID.String()}
	mint.Body.Label = "edge"
	mint.Body.ClusterAgent = true
	if _, err := h.CreateProvisioningToken(memberCtx, mint); err == nil {
		t.Fatal("a member minted a cluster agent token")
	}
	out, err := h.CreateProvisioningToken(adminCtx, mint)
	if err != nil {
		t.Fatalf("admin mint: %v", err)
	}
	if !out.Body.ClusterAgent {
		t.Fatal("the token is not marked for a cluster agent")
	}

	// The reusable org token never registers an agent, whatever it is.
	reg := &SelfRegisterNodeInput{}
	reg.Body.Token = "mreg-" + "00"
	reg.Body.Name = "evil"
	reg.Body.TailscaleIP = "100.64.0.9"
	reg.Body.ClusterAgent = clusterAgentBody(t)
	if _, err := h.SelfRegisterNode(ctx, reg); statusOf(err) != http.StatusUnprocessableEntity {
		t.Fatalf("an agent with the reusable token: %v, want 422", err)
	}

	// The agent's own token: registered, the cluster pointed at it, and no
	// k3s join token handed back, since this node is the cluster's server.
	reg.Body.Token = out.Body.Token
	reg.Body.Name = "edge"
	reg.Body.TailscaleIP = "100.64.0.2"
	res, err := h.SelfRegisterNode(ctx, reg)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if res.Body.K3sToken != "" || res.Body.K3sServerURL != "" {
		t.Error("the agent's node was handed the k3s join token")
	}
	if res.Body.NodeSecret == "" {
		t.Error("no node secret")
	}
	if _, ok := svc.Agent.Current(); !ok {
		t.Fatal("the API's cluster does not point at the agent")
	}

	// A second agent: 409, minting one: 409.
	if _, err := h.CreateProvisioningToken(adminCtx, mint); statusOf(err) != http.StatusConflict {
		t.Fatalf("minting a second agent token: %v, want 409", err)
	}

	// Revoking: members cannot, an admin of another organisation cannot reach
	// this node through their own, this organisation's admin can.
	nodeID := res.Body.ID
	path := &NodePathInput{OrgID: orgID.String(), NodeID: nodeID.String()}
	if _, err := h.RevokeClusterAgent(memberCtx, path); err == nil {
		t.Fatal("a member revoked the cluster agent")
	}
	// Community runs one organisation per server; an edition may run more,
	// and the same person may administer two.
	ownerID, _ := middleware.UserFromContext(adminCtx)
	other := db.Organization{Name: "Other", Slug: "other"}
	if err := svc.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DB.Create(&db.OrganizationMember{OrganizationID: other.ID, UserID: ownerID, Role: db.RoleOwner}).Error; err != nil {
		t.Fatal(err)
	}
	cross := &NodePathInput{OrgID: other.ID.String(), NodeID: nodeID.String()}
	if _, err := h.RevokeClusterAgent(adminCtx, cross); statusOf(err) != http.StatusNotFound {
		t.Fatalf("revoking through another organisation: %v, want 404", err)
	}
	if _, ok := svc.Agent.Current(); !ok {
		t.Fatal("the cross-organisation call revoked the agent")
	}
	if _, err := h.RevokeClusterAgent(adminCtx, path); err != nil {
		t.Fatalf("admin revoke: %v", err)
	}
	if _, ok := svc.Agent.Current(); ok {
		t.Fatal("the agent is still reached after revoking")
	}
	if _, err := h.RevokeClusterAgent(adminCtx, path); statusOf(err) != http.StatusNotFound {
		t.Fatalf("revoking twice: %v, want 404", err)
	}
}

// On an API that reaches its cluster directly, no agent token is minted.
func TestNoClusterAgentOnADirectAPI(t *testing.T) {
	h, orgID, adminCtx, _ := setupClusterAuthz(t)
	mint := &CreateProvisioningTokenInput{OrgID: orgID}
	mint.Body.Label = "edge"
	mint.Body.ClusterAgent = true
	if _, err := h.CreateProvisioningToken(adminCtx, mint); statusOf(err) != http.StatusUnprocessableEntity {
		t.Fatalf("minting on a direct API: %v, want 422", err)
	}
}
