package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/fleet"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// fakeAPI stands in for the install's own API: it answers every request,
// saying whom the bearer token signs in and what was asked.
func fakeAPI(t *testing.T, secret string) (port int, seen func() []string) {
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, err := jwt.Parse(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
			func(*jwt.Token) (any, error) { return []byte(secret), nil })
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		uid := tok.Claims.(jwt.MapClaims)["uid"].(string)
		mu.Lock()
		got = append(got, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"uid": uid, "agent": r.Header.Get("User-Agent")})
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	p, _ := strconv.Atoi(u.Port())
	return p, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), got...) }
}

func TestAgentAppsActAsTheLinkedPersonOnly(t *testing.T) {
	gdb := newTestDB(t)
	svc := newServices(gdb)
	plane := newFakePlane(t)
	ctx := context.Background()

	person := meshdb.User{Email: "ada@example.com", Username: "ada-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&person).Error)
	const secret = "jwt-test-secret"
	port, seen := fakeAPI(t, secret)
	svc.Fleet.RunCallsAs(svc.Auth, secret, port)

	_, err := svc.Fleet.Enroll(ctx, plane.token(plane.secret), uuid.New())
	require.NoError(t, err)
	plane.next(t) // the first report

	// Agent apps are off until the owner turns them on: a person cannot even
	// start linking.
	_, err = svc.Fleet.StartPersonLink(ctx, person.ID)
	require.ErrorIs(t, err, service.ErrAgentAppsOff)
	_, err = svc.Fleet.SetAgentApps(ctx, true, person.ID)
	require.NoError(t, err)

	// The person starts the link here, and approves on the account plane,
	// which sends their code back signed.
	approve, err := svc.Fleet.StartPersonLink(ctx, person.ID)
	require.NoError(t, err)
	u, _ := url.Parse(approve)
	require.Equal(t, "/connect-server", u.Path)
	require.Equal(t, "lnk_1", u.Query().Get("link"))
	send := func(m fleet.Message) {
		plane.mu.Lock()
		defer plane.mu.Unlock()
		require.NoError(t, plane.conn.WriteJSON(m))
	}
	linked := fleet.Person{Code: u.Query().Get("code"), Account: "acc-1", Email: "ada@cloud.example"}
	forged := linked
	forged.Account = "acc-evil"
	forged.Signature = "AAAA"
	send(fleet.Message{Type: fleet.TypePersonLinked, Person: &forged})
	fleet.SignPerson(fleet.TypePersonLinked, "lnk_1", &linked, plane.key)
	send(fleet.Message{Type: fleet.TypePersonLinked, Person: &linked})
	require.Eventually(t, func() bool {
		me, _ := svc.Fleet.MyAccount(ctx, person.ID)
		return me.Linked && me.Email == "ada@cloud.example"
	}, 10*time.Second, 50*time.Millisecond)

	n := 0
	call := func(account, method, path string, sign bool) fleet.Result {
		t.Helper()
		n++
		c := fleet.Call{ID: "call-" + strconv.Itoa(n), Account: account, App: "Claude", Method: method, Path: path, Expires: time.Now().Add(30 * time.Second)}
		if sign {
			fleet.SignCall("lnk_1", &c, plane.key)
		} else {
			c.Signature = "AAAA"
		}
		send(fleet.Message{Type: fleet.TypeCall, Call: &c})
		for {
			m := plane.next(t)
			if m.Type == fleet.TypeResult && m.Result != nil && m.Result.ID == c.ID {
				return *m.Result
			}
		}
	}

	// A call runs as the person, named after its app.
	r := call("acc-1", "GET", "/api/v1/orgs/x/projects?limit=5", true)
	require.Equal(t, http.StatusOK, r.Status)
	var answer map[string]string
	require.NoError(t, json.Unmarshal(r.Body, &answer))
	require.Equal(t, person.ID.String(), answer["uid"])
	require.Equal(t, "Claude via Meshploy Cloud", answer["agent"])
	require.Equal(t, []string{"GET /api/v1/orgs/x/projects?limit=5"}, seen())

	// Refused: an unknown account, a forged call, the never list.
	require.Equal(t, http.StatusForbidden, call("acc-2", "GET", "/api/v1/orgs/x/projects", true).Status)
	require.Equal(t, http.StatusForbidden, call("acc-1", "GET", "/api/v1/orgs/x/projects", false).Status)
	for _, p := range []string{"/api/v1/system/fleet", "/api/v1/me/meshploy-account", "/api/v1/orgs/x/members",
		"/api/v1/orgs/x/projects/p/services/s/db/query", "/api/v1/auth/login", "/health", "/api/v1/orgs/../system/fleet"} {
		require.Equal(t, http.StatusForbidden, call("acc-1", "GET", p, true).Status, p)
	}
	require.Len(t, seen(), 1, "a refused call reached the API")

	// Replayed: the same signed call twice runs once.
	c := fleet.Call{ID: "replay", Account: "acc-1", App: "Claude", Method: "GET", Path: "/api/v1/orgs/x/nodes", Expires: time.Now().Add(30 * time.Second)}
	fleet.SignCall("lnk_1", &c, plane.key)
	statuses := map[int]int{}
	send(fleet.Message{Type: fleet.TypeCall, Call: &c})
	send(fleet.Message{Type: fleet.TypeCall, Call: &c})
	for len(statuses) < 2 && statuses[http.StatusOK]+statuses[http.StatusConflict] < 2 {
		m := plane.next(t)
		if m.Type == fleet.TypeResult && m.Result.ID == "replay" {
			statuses[m.Result.Status]++
		}
	}
	require.Equal(t, map[int]int{http.StatusOK: 1, http.StatusConflict: 1}, statuses)

	// The owner turns agent apps off: nothing runs, the person stays linked.
	_, err = svc.Fleet.SetAgentApps(ctx, false, person.ID)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, call("acc-1", "GET", "/api/v1/orgs/x/projects", true).Status)
	me, _ := svc.Fleet.MyAccount(ctx, person.ID)
	require.True(t, me.Linked)

	// Everything is on the person's record, refusals with their reason.
	calls, err := svc.Fleet.CallsFor(ctx, person.ID, 100)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(calls), 3)
	require.Equal(t, "Claude", calls[0].App)
	require.Contains(t, calls[0].Refused, "turned off")

	// The person withdraws here; the account plane is told.
	require.NoError(t, svc.Fleet.UnlinkPerson(ctx, person.ID))
	for {
		m := plane.next(t)
		if m.Type == fleet.TypePersonUnlinked {
			require.Equal(t, "acc-1", m.Person.Account)
			break
		}
	}
	me, _ = svc.Fleet.MyAccount(ctx, person.ID)
	require.False(t, me.Linked)
}

// The person who linked the server turns agent apps on: they are linked too,
// through the account plane's answer for the link's own account, with no
// approval page. Anyone else turning it on is not.
func TestTurningAgentAppsOnLinksTheServersLinker(t *testing.T) {
	gdb := newTestDB(t)
	svc := newServices(gdb)
	plane := newFakePlane(t)
	ctx := context.Background()

	owner := meshdb.User{Email: "owner@example.com", Username: "owner-" + uuid.NewString()[:8]}
	other := meshdb.User{Email: "other@example.com", Username: "other-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&owner).Error)
	require.NoError(t, gdb.Create(&other).Error)
	_, err := svc.Fleet.Enroll(ctx, plane.token(plane.secret), owner.ID)
	require.NoError(t, err)
	plane.next(t) // the first report

	_, err = svc.Fleet.SetAgentApps(ctx, true, other.ID)
	require.NoError(t, err)
	_, err = svc.Fleet.SetAgentApps(ctx, true, owner.ID)
	require.NoError(t, err)
	var ask fleet.Message
	for ask.Type != fleet.TypeLinkOwner {
		ask = plane.next(t)
	}
	require.NotEmpty(t, ask.Person.Code)

	p := fleet.Person{Code: ask.Person.Code, Account: "acc-owner", Email: "owner@cloud.example"}
	fleet.SignPerson(fleet.TypePersonLinked, "lnk_1", &p, plane.key)
	plane.mu.Lock()
	require.NoError(t, plane.conn.WriteJSON(fleet.Message{Type: fleet.TypePersonLinked, Person: &p}))
	plane.mu.Unlock()
	require.Eventually(t, func() bool {
		me, _ := svc.Fleet.MyAccount(ctx, owner.ID)
		return me.Linked && me.Email == "owner@cloud.example"
	}, 10*time.Second, 50*time.Millisecond)
	me, _ := svc.Fleet.MyAccount(ctx, other.ID)
	require.False(t, me.Linked)
}

// A managed install (a Cloud workspace) links itself with FLEET_TOKEN at
// start, has agent apps on, reports who signs in there, and runs a call as
// the person with the account's signed address, with no one linking.
func TestAManagedInstallLinksItselfAndActsAsThePersonByEmail(t *testing.T) {
	gdb := newTestDB(t)
	svc := newServices(gdb)
	plane := newFakePlane(t)
	plane.managed = true
	ctx := context.Background()

	org := meshdb.Organization{Name: "Acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&org).Error)
	person := meshdb.User{Email: "Asha@Example.com", Username: "asha-" + uuid.NewString()[:8]}
	stranger := meshdb.User{Email: "nobody-" + uuid.NewString()[:8] + "@example.com", Username: "nobody-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&person).Error)
	require.NoError(t, gdb.Create(&stranger).Error)
	require.NoError(t, gdb.Create(&meshdb.OrganizationMember{OrganizationID: org.ID, UserID: person.ID, Role: meshdb.RoleMember}).Error)
	const secret = "jwt-test-secret"
	port, seen := fakeAPI(t, secret)
	svc.Fleet.RunCallsAs(svc.Auth, secret, port)
	svc.Fleet.EnrolWith(plane.token(plane.secret))

	runCtx, stop := context.WithCancel(ctx)
	t.Cleanup(stop)
	go svc.Fleet.Start(runCtx)
	var report *fleet.Report
	for report == nil {
		if m := plane.next(t); m.Type == fleet.TypeReport {
			report = m.Report
		}
	}
	require.Equal(t, []string{"Asha@Example.com"}, report.Members, "only people in an organisation are reported")
	st, err := svc.Fleet.Status(ctx)
	require.NoError(t, err)
	require.True(t, st.Managed)
	require.True(t, st.AgentApps)

	n := 0
	call := func(email string) fleet.Result {
		t.Helper()
		n++
		c := fleet.Call{ID: "m-" + strconv.Itoa(n), Account: "acc-" + strconv.Itoa(n), Email: email, App: "Claude",
			Method: "GET", Path: "/api/v1/orgs", Expires: time.Now().Add(30 * time.Second)}
		fleet.SignCall("lnk_1", &c, plane.key)
		plane.mu.Lock()
		require.NoError(t, plane.conn.WriteJSON(fleet.Message{Type: fleet.TypeCall, Call: &c}))
		plane.mu.Unlock()
		for {
			m := plane.next(t)
			if m.Type == fleet.TypeResult && m.Result != nil && m.Result.ID == c.ID {
				return *m.Result
			}
		}
	}
	r := call("asha@example.com")
	require.Equal(t, http.StatusOK, r.Status)
	var answer map[string]string
	require.NoError(t, json.Unmarshal(r.Body, &answer))
	require.Equal(t, person.ID.String(), answer["uid"])

	require.Equal(t, http.StatusForbidden, call("someone-else@example.com").Status)
	require.Equal(t, http.StatusForbidden, call("").Status)
	require.Len(t, seen(), 1)
}
