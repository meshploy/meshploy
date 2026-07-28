package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
)

// These tests pin the authorization contract of the pod terminal, which hands
// out an interactive shell inside a running container.
//
// Regression guard: ServiceTerminal used to check only MemberRole(orgID,
// userID) and then load the service by ID alone, taking the namespace from THAT
// service's project. The orgId path segment therefore never had to match the
// target, so any member of any org could exec into any other org's container.
//
// K8s is nil under test, which makes the assertions sharp: a request that fails
// authorization gets 403, while one that passes falls through to the K8s
// availability check and gets 503. 503 means "authorization allowed this".

// terminalRouter mounts the pod terminal on a real chi router so URL params are
// populated exactly as they are in production.
func terminalRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/pods/{podName}/terminal",
		h.ServiceTerminal)
	return r
}

type terminalFixture struct {
	h        *Handler
	svc      *service.Services
	router   http.Handler
	orgA     string // victim org
	projectA string
	serviceA string
	victimID string
	// attacker is an owner of a DIFFERENT org, with no relationship to org A.
	attackerOrg string
	attackerID  string
}

func setupTerminalAuthz(t *testing.T) terminalFixture {
	t.Helper()
	ctx := context.Background()
	database := newAuthzTestDB(t)
	svc := service.New(database)
	h := New(nil, svc)

	// ── Victim org, with a real project and service ──────────────────────────
	victim, err := svc.Auth.Register(ctx, service.RegisterInput{
		Username: "victim", Email: "victim@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("register victim: %v", err)
	}
	orgsA, err := svc.Orgs.ListForUser(ctx, victim.ID)
	if err != nil || len(orgsA) != 1 {
		t.Fatalf("list victim orgs: %v (n=%d)", err, len(orgsA))
	}
	orgA := orgsA[0]

	projA, err := svc.Projects.Create(ctx, orgA.ID, "victim-proj", "victim-proj")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	svcA, err := svc.Workloads.Create(ctx, projA.ID, service.CreateWorkloadInput{
		Name:  "victim-svc",
		Image: "nginx:alpine",
		Ports: []service.PortInput{{Port: 80}},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	// ── Attacker: owner of an unrelated org ──────────────────────────────────
	// Register() is single-owner per instance, and the invitation flow would put
	// this principal inside org A — the very thing under test. So the row is
	// created directly and given its own org. The attacker never authenticates
	// by password here; the test mints a ticket for the user ID directly.
	attacker := &db.User{
		Username: "attacker", Email: "attacker@example.com",
		Password: "unused", Kind: db.UserHuman,
	}
	if err := database.Create(attacker).Error; err != nil {
		t.Fatalf("create attacker: %v", err)
	}
	orgB, err := svc.Orgs.Create(ctx, attacker.ID, service.CreateOrgInput{
		Name: "attacker-org", Slug: "attacker-org",
	})
	if err != nil {
		t.Fatalf("create attacker org: %v", err)
	}

	return terminalFixture{
		h: h, svc: svc, router: terminalRouter(h),
		orgA: orgA.ID.String(), projectA: projA.ID.String(), serviceA: svcA.ID.String(),
		victimID:    victim.ID.String(),
		attackerOrg: orgB.ID.String(), attackerID: attacker.ID.String(),
	}
}

// get issues a terminal request carrying a freshly minted ticket for userID.
func (f terminalFixture) get(t *testing.T, orgID, projectID, serviceID, pod string, ticket string) int {
	t.Helper()
	url := fmt.Sprintf("/api/v1/orgs/%s/projects/%s/services/%s/pods/%s/terminal?ticket=%s",
		orgID, projectID, serviceID, pod, ticket)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec.Code
}

func (f terminalFixture) ticketFor(t *testing.T, userIDStr string) string {
	t.Helper()
	uid, err := parseUUID(userIDStr)
	if err != nil {
		t.Fatalf("parse uuid: %v", err)
	}
	tok, _, err := f.svc.Tickets.Mint(uid)
	if err != nil {
		t.Fatalf("mint ticket: %v", err)
	}
	return tok
}

// The core regression: an outsider naming their OWN org in the path, but
// another org's service, must be refused.
func TestPodTerminalRejectsCrossOrgService(t *testing.T) {
	f := setupTerminalAuthz(t)

	code := f.get(t, f.attackerOrg, f.projectA, f.serviceA, "any-pod", f.ticketFor(t, f.attackerID))
	if code != http.StatusForbidden {
		t.Fatalf("a member of another org must not reach a foreign service's terminal: got %d, want 403", code)
	}

	// Also refused when naming the victim's org directly, since they are not a
	// member of it.
	code = f.get(t, f.orgA, f.projectA, f.serviceA, "any-pod", f.ticketFor(t, f.attackerID))
	if code != http.StatusForbidden {
		t.Fatalf("a non-member naming the victim org must be refused: got %d, want 403", code)
	}
}

func TestPodTerminalRequiresAValidTicket(t *testing.T) {
	f := setupTerminalAuthz(t)

	for name, ticket := range map[string]string{
		"missing": "",
		"forged":  "deadbeef",
	} {
		if code := f.get(t, f.orgA, f.projectA, f.serviceA, "any-pod", ticket); code != http.StatusUnauthorized {
			t.Errorf("%s ticket: got %d, want 401", name, code)
		}
	}
}

// A ticket is single-use: replaying one recovered from a request log must fail.
func TestPodTerminalTicketCannotBeReplayed(t *testing.T) {
	f := setupTerminalAuthz(t)
	ticket := f.ticketFor(t, f.attackerID)

	// First use is consumed even though authorization then rejects the request.
	f.get(t, f.attackerOrg, f.projectA, f.serviceA, "any-pod", ticket)

	if code := f.get(t, f.attackerOrg, f.projectA, f.serviceA, "any-pod", ticket); code != http.StatusUnauthorized {
		t.Fatalf("a replayed ticket must be rejected: got %d, want 401", code)
	}
}

// The session token must no longer be accepted in the query string, or the
// logging exposure this change removed would still be reachable.
func TestPodTerminalNoLongerAcceptsATokenParam(t *testing.T) {
	f := setupTerminalAuthz(t)

	url := fmt.Sprintf("/api/v1/orgs/%s/projects/%s/services/%s/pods/p/terminal?token=anything",
		f.orgA, f.projectA, f.serviceA)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("?token= must no longer authenticate a terminal: got %d, want 401", rec.Code)
	}
}

// The positive case: the org's own owner gets past authorization and is stopped
// only by K8s being unavailable in tests. Without this, every test above would
// still pass if the handler simply rejected everything.
func TestPodTerminalAllowsTheOwningOrg(t *testing.T) {
	f := setupTerminalAuthz(t)

	code := f.get(t, f.orgA, f.projectA, f.serviceA, "any-pod", f.ticketFor(t, f.victimID))
	if code != http.StatusServiceUnavailable {
		t.Fatalf("the owning org must pass authorization and reach the K8s check: got %d, want 503", code)
	}
}
