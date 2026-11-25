package service_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/meshploy/packages/fleet"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// fakePlane is an account plane as small as the contract allows: it redeems
// one secret, checks every connection's signature, and records what arrives.
type fakePlane struct {
	t      *testing.T
	srv    *httptest.Server
	secret string
	key    ed25519.PrivateKey

	mu         sync.Mutex
	installKey ed25519.PublicKey
	conn       *websocket.Conn
	unlinked   bool

	messages chan fleet.Message
}

func newFakePlane(t *testing.T) *fakePlane {
	_, priv, _ := ed25519.GenerateKey(nil)
	p := &fakePlane{t: t, secret: "s3cret", key: priv, messages: make(chan fleet.Message, 16)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+fleet.EnrollPath, func(w http.ResponseWriter, r *http.Request) {
		var req fleet.EnrollRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Secret != p.secret {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"detail":"this link token was used or has expired"}`))
			return
		}
		k, _ := base64.StdEncoding.DecodeString(req.PublicKey)
		p.mu.Lock()
		p.installKey = ed25519.PublicKey(k)
		p.mu.Unlock()
		_ = json.NewEncoder(w).Encode(fleet.EnrollResponse{
			LinkID:    "lnk_1",
			PublicKey: base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)),
			Account:   "owner@example.com",
		})
	})
	verify := func(r *http.Request) bool {
		p.mu.Lock()
		k := p.installKey
		p.mu.Unlock()
		return k != nil && fleet.VerifyConnect(r.Header.Get(fleet.HeaderLink), r.Header.Get(fleet.HeaderTime),
			r.Header.Get(fleet.HeaderSignature), k, time.Now()) == nil
	}
	mux.HandleFunc("GET "+fleet.ConnectPath, func(w http.ResponseWriter, r *http.Request) {
		if !verify(r) {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		p.mu.Lock()
		p.conn = c
		p.mu.Unlock()
		for {
			var m fleet.Message
			if err := c.ReadJSON(&m); err != nil {
				return
			}
			p.messages <- m
		}
	})
	mux.HandleFunc("POST "+fleet.UnlinkPath, func(w http.ResponseWriter, r *http.Request) {
		if verify(r) {
			p.mu.Lock()
			p.unlinked = true
			p.mu.Unlock()
		}
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakePlane) token(secret string) string {
	return fleet.Token{URL: p.srv.URL, Secret: secret}.String()
}

func (p *fakePlane) next(t *testing.T) fleet.Message {
	t.Helper()
	select {
	case m := <-p.messages:
		return m
	case <-time.After(30 * time.Second):
		t.Fatal("nothing arrived from the install")
	}
	return fleet.Message{}
}

func TestFleetLinkReportsPausesAndEnds(t *testing.T) {
	db := newTestDB(t)
	svc := newServices(db)
	plane := newFakePlane(t)
	ctx := context.Background()
	owner := uuid.New()

	_, err := svc.Fleet.Enroll(ctx, plane.token("wrong"), owner)
	require.ErrorContains(t, err, "used or has expired", "the account plane's reason is passed on")

	st, err := svc.Fleet.Enroll(ctx, plane.token(plane.secret), owner)
	require.NoError(t, err)
	require.True(t, st.Linked)
	require.Equal(t, "owner@example.com", st.Account)

	_, err = svc.Fleet.Enroll(ctx, plane.token(plane.secret), owner)
	require.ErrorIs(t, err, service.ErrFleetLinked)

	m := plane.next(t)
	require.Equal(t, fleet.TypeReport, m.Type)
	require.NotNil(t, m.Report)
	require.Equal(t, "dev", m.Report.About.Version)

	require.Eventually(t, func() bool {
		s, _ := svc.Fleet.Status(ctx)
		return s.Connected
	}, 10*time.Second, 50*time.Millisecond)

	// Reporting off: the link stands, and the next frame says nothing but that.
	// Reports already on their way (a reconnect right after enrolling sends
	// one more) are passed over; what follows the switch is the pause.
	_, err = svc.Fleet.SetReport(ctx, false)
	require.NoError(t, err)
	for m = plane.next(t); m.Type == fleet.TypeReport; m = plane.next(t) {
	}
	require.Equal(t, fleet.TypePaused, m.Type)
	require.Nil(t, m.Report)

	// The account plane removes the link: the install forgets it.
	plane.mu.Lock()
	require.NoError(t, plane.conn.WriteJSON(fleet.Message{Type: fleet.TypeUnlinked}))
	plane.mu.Unlock()
	require.Eventually(t, func() bool {
		s, _ := svc.Fleet.Status(ctx)
		return !s.Linked
	}, 10*time.Second, 50*time.Millisecond)

	// Linked again, then unlinked here: the account plane is told.
	_, err = svc.Fleet.Enroll(ctx, plane.token(plane.secret), owner)
	require.NoError(t, err)
	plane.next(t)
	require.NoError(t, svc.Fleet.Unlink(ctx))
	plane.mu.Lock()
	told := plane.unlinked
	plane.mu.Unlock()
	require.True(t, told)
	s, _ := svc.Fleet.Status(ctx)
	require.False(t, s.Linked)
	require.ErrorIs(t, svc.Fleet.Unlink(ctx), service.ErrFleetNotLinked)
}
