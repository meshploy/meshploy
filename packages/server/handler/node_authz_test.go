package handler

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/middleware"
	"github.com/meshploy/packages/server/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// These tests pin the authorization contract of the cluster-credential
// endpoints. They hand out credentials that let a machine join the WireGuard
// mesh and the k3s cluster, so they must be admin-only — matching their
// siblings GetNodeRegistrationToken and CreateProvisioningToken.
//
// Regression guard: all three previously called only requireUser, so any
// authenticated org member could read the k3s join token and the Headscale
// pre-auth key.

var authzDBSeq atomic.Int64

// newAuthzTestDB creates an isolated database for one test. Requires a running
// Postgres via DATABASE_URL; skips otherwise so the suite stays runnable
// without container infrastructure.
func newAuthzTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping DB-backed authz test")
	}
	db.SetEncryptionKey("test-encryption-key-32-chars!!!!!")

	name := fmt.Sprintf("meshploy_authz_%d", authzDBSeq.Add(1))
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	root.Exec("DROP DATABASE IF EXISTS " + name)
	if err := root.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatalf("create database: %v", err)
	}
	if sqlDB, _ := root.DB(); sqlDB != nil {
		sqlDB.Close()
	}

	// Swap the database name in the DSN.
	i := len(dsn) - 1
	for i >= 0 && dsn[i] != '/' {
		i--
	}
	isolated := dsn[:i+1] + name + "?sslmode=disable"

	conn, err := gorm.Open(postgres.Open(isolated), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("connect isolated: %v", err)
	}
	if err := db.Migrate(conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, _ := conn.DB(); sqlDB != nil {
			sqlDB.Close()
		}
	})
	return conn
}

// setupClusterAuthz returns a handler plus an admin (org owner) and a plain
// member of the same org.
func setupClusterAuthz(t *testing.T) (h *Handler, orgID string, adminCtx, memberCtx context.Context) {
	t.Helper()
	ctx := context.Background()
	database := newAuthzTestDB(t)
	svc := service.New(database)
	h = New(nil, svc)

	owner, err := svc.Auth.Register(ctx, service.RegisterInput{
		Username: "owner", Email: "owner@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	orgs, err := svc.Orgs.ListForUser(ctx, owner.ID)
	if err != nil || len(orgs) != 1 {
		t.Fatalf("list orgs: %v (n=%d)", err, len(orgs))
	}
	org := orgs[0]

	// Registration is single-owner, so a second principal joins via the
	// invitation flow — the same approach the service-layer tests use.
	inv, err := svc.Orgs.CreateInvitation(ctx, org.ID, owner.ID, "member@example.com", db.RoleMember)
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	member, err := svc.Orgs.AcceptInvitation(ctx, inv.Token, "member", "password123")
	if err != nil {
		t.Fatalf("accept invitation: %v", err)
	}

	return h, org.ID.String(),
		middleware.ContextWithUser(ctx, owner.ID),
		middleware.ContextWithUser(ctx, member.ID)
}

func TestClusterJoinTokenRequiresAdmin(t *testing.T) {
	h, orgID, adminCtx, memberCtx := setupClusterAuthz(t)
	in := &ClusterPathInput{OrgID: orgID}

	if _, err := h.GetClusterJoinToken(memberCtx, in); err == nil {
		t.Fatal("a plain org member must NOT be able to read the k3s cluster join token")
	}
	if _, err := h.GetClusterJoinToken(context.Background(), in); err == nil {
		t.Fatal("an unauthenticated caller must be rejected")
	}
	if _, err := h.GetClusterJoinToken(adminCtx, in); err != nil {
		t.Fatalf("an org admin must be allowed: %v", err)
	}
}

func TestHeadscalePreAuthKeyGetRequiresAdmin(t *testing.T) {
	h, orgID, adminCtx, memberCtx := setupClusterAuthz(t)
	in := &ClusterPathInput{OrgID: orgID}

	if _, err := h.GetHeadscalePreAuthKey(memberCtx, in); err == nil {
		t.Fatal("a plain org member must NOT be able to read the Headscale pre-auth key")
	}
	if _, err := h.GetHeadscalePreAuthKey(context.Background(), in); err == nil {
		t.Fatal("an unauthenticated caller must be rejected")
	}
	if _, err := h.GetHeadscalePreAuthKey(adminCtx, in); err != nil {
		t.Fatalf("an org admin must be allowed: %v", err)
	}
}

func TestHeadscalePreAuthKeyCreateRequiresAdmin(t *testing.T) {
	h, orgID, _, memberCtx := setupClusterAuthz(t)
	in := &ClusterPathInput{OrgID: orgID}

	// Denied before reaching the Headscale-not-configured check, proving the
	// authorization gate runs first.
	if _, err := h.CreateHeadscalePreAuthKey(memberCtx, in); err == nil {
		t.Fatal("a plain org member must NOT be able to mint a Headscale pre-auth key")
	}
	if _, err := h.CreateHeadscalePreAuthKey(context.Background(), in); err == nil {
		t.Fatal("an unauthenticated caller must be rejected")
	}
}

// TestClusterEndpointsRejectForeignOrg proves the endpoints are org-scoped:
// an admin of one org cannot use another org's id to reach its credentials.
func TestClusterEndpointsRejectForeignOrg(t *testing.T) {
	h, _, adminCtx, _ := setupClusterAuthz(t)
	foreign := &ClusterPathInput{OrgID: "00000000-0000-0000-0000-000000000001"}

	if _, err := h.GetClusterJoinToken(adminCtx, foreign); err == nil {
		t.Fatal("an admin must NOT read another org's k3s join token")
	}
	if _, err := h.GetHeadscalePreAuthKey(adminCtx, foreign); err == nil {
		t.Fatal("an admin must NOT read another org's Headscale pre-auth key")
	}
}
