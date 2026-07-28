package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var extDBSeq atomic.Int64

// newExtTestDB creates an isolated database for one test. These tests live in
// the internal `service` package (they need the unexported hook slices), so
// they cannot use the external suite's helper in package service_test.
func newExtTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping DB-backed extension test")
	}
	meshdb.SetEncryptionKey("test-encryption-key-32-chars!!!!!")

	name := fmt.Sprintf("meshploy_ext_%d", extDBSeq.Add(1))
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

	i := len(dsn) - 1
	for i >= 0 && dsn[i] != '/' {
		i--
	}
	conn, err := gorm.Open(postgres.Open(dsn[:i+1]+name+"?sslmode=disable"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("connect isolated: %v", err)
	}
	if err := meshdb.Migrate(conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, _ := conn.DB(); sqlDB != nil {
			sqlDB.Close()
		}
	})
	return conn
}

// fakeQuota records what it was asked about and optionally rejects.
type fakeQuota struct {
	seen   []QuotaKind
	reject error
}

func (f *fakeQuota) CheckQuota(_ context.Context, _ uuid.UUID, kind QuotaKind) error {
	f.seen = append(f.seen, kind)
	return f.reject
}

func withQuota(t *testing.T, qc QuotaChecker) {
	t.Helper()
	saved := quotaCheckers
	t.Cleanup(func() { quotaCheckers = saved })
	quotaCheckers = nil
	if qc != nil {
		RegisterQuotaChecker(qc)
	}
}

// The load-bearing CE guarantee: with nothing registered, checkQuota permits
// everything. A CE build must never be gated by a quota it cannot configure.
func TestQuotaNoopInCE(t *testing.T) {
	withQuota(t, nil)
	for _, kind := range []QuotaKind{QuotaProject, QuotaService, QuotaNode} {
		if err := checkQuota(context.Background(), uuid.New(), kind); err != nil {
			t.Fatalf("CE build must not enforce quota for %s: %v", kind, err)
		}
	}
}

func TestQuotaCheckerReceivesKind(t *testing.T) {
	f := &fakeQuota{}
	withQuota(t, f)

	org := uuid.New()
	for _, kind := range []QuotaKind{QuotaProject, QuotaService, QuotaNode} {
		if err := checkQuota(context.Background(), org, kind); err != nil {
			t.Fatalf("unexpected rejection for %s: %v", kind, err)
		}
	}
	if len(f.seen) != 3 {
		t.Fatalf("want 3 quota checks, got %d (%v)", len(f.seen), f.seen)
	}
	if f.seen[0] != QuotaProject || f.seen[1] != QuotaService || f.seen[2] != QuotaNode {
		t.Fatalf("kinds passed through incorrectly: %v", f.seen)
	}
}

// A rejection must propagate verbatim so the user sees the extension's message
// (e.g. "project limit reached (10)") rather than a generic error.
func TestQuotaRejectionPropagates(t *testing.T) {
	want := errors.New("project limit reached (10)")
	withQuota(t, &fakeQuota{reject: want})

	err := checkQuota(context.Background(), uuid.New(), QuotaProject)
	if !errors.Is(err, want) {
		t.Fatalf("want the checker's own error to propagate, got %v", err)
	}
}

// The first rejection wins and later checkers are not consulted.
func TestQuotaFirstRejectionStops(t *testing.T) {
	saved := quotaCheckers
	t.Cleanup(func() { quotaCheckers = saved })
	quotaCheckers = nil

	boom := errors.New("denied")
	second := &fakeQuota{}
	RegisterQuotaChecker(&fakeQuota{reject: boom})
	RegisterQuotaChecker(second)

	if err := checkQuota(context.Background(), uuid.New(), QuotaNode); !errors.Is(err, boom) {
		t.Fatalf("want the first rejection, got %v", err)
	}
	if len(second.seen) != 0 {
		t.Fatal("a checker after a rejection must not be consulted")
	}
}

// End-to-end: a registered checker actually blocks a real ProjectService.Create.
// This proves the hook is wired into the create path, not merely that the
// mechanism works in isolation. Requires DATABASE_URL.
func TestQuotaBlocksRealProjectCreate(t *testing.T) {
	database := newExtTestDB(t)
	svcs := New(database)

	owner, err := svcs.Auth.Register(context.Background(), RegisterInput{
		Username: "quotaowner", Email: "quota@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	orgs, err := svcs.Orgs.ListForUser(context.Background(), owner.ID)
	if err != nil || len(orgs) != 1 {
		t.Fatalf("orgs: %v", err)
	}
	orgID := orgs[0].ID

	// Without a checker the create succeeds — CE behaviour.
	withQuota(t, nil)
	if _, err := svcs.Projects.Create(context.Background(), orgID, "allowed", "allowed"); err != nil {
		t.Fatalf("CE build must allow project creation: %v", err)
	}

	// With a rejecting checker the very same call is refused.
	denied := errors.New("project limit reached (10)")
	withQuota(t, &fakeQuota{reject: denied})
	_, err = svcs.Projects.Create(context.Background(), orgID, "blocked", "blocked")
	if !errors.Is(err, denied) {
		t.Fatalf("quota checker must block the create and surface its message, got %v", err)
	}

	// And nothing was persisted.
	projects, err := svcs.Projects.List(context.Background(), orgID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, p := range projects {
		if p.Slug == "blocked" {
			t.Fatal("a quota-rejected project must not be persisted")
		}
	}
}
