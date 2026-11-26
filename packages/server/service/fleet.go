package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/fleet"
	"github.com/meshploy/packages/server/version"
	"gorm.io/gorm"
)

// FleetService is this install's fleet connector: its link to an account
// plane (Meshploy Cloud). It does nothing until the owner enrols the install
// with a link token. Linked, it holds one connection it opened itself, and
// while reporting is on it sends the install's state over it: counts and
// states, never names, addresses or secrets. The install works the same
// whether or not the connection is up.
type FleetService struct {
	db      *gorm.DB
	orgs    *OrgService
	domains *DomainService
	system  *SystemService

	httpc  *http.Client
	dialer *websocket.Dialer

	// events reaches the running connection: fleetSend to report now (the
	// switch moved), fleetRestart to drop it and read the link again
	// (enrolled or unlinked).
	events chan string
	// outbox carries what the install sends unasked: a person withdrawing
	// their account here.
	outbox chan fleet.Message

	// For running calls: the JWT secret and the API's own port, to call
	// itself as the person a call acts for.
	auth      *AuthService
	jwtSecret string
	apiPort   int

	mu        sync.Mutex
	connected bool

	// pending holds the one-time codes of people connecting their accounts.
	pending map[string]pendingPerson
	// seen holds the ids of calls already run, against replay.
	seen map[string]time.Time
	// tokens holds a console session per person and app, reused while it
	// lasts, so each app shows as one sign-in on the person's list.
	tokens map[string]appToken
}

const (
	fleetSend    = "send"
	fleetRestart = "restart"

	// fleetReportEvery is how often a report goes while nothing changes.
	fleetReportEvery = time.Minute
	// fleetPingEvery keeps the connection alive through proxies and NAT, and
	// finds a dead one: no pong within fleetReadTimeout ends it.
	fleetPingEvery   = 30 * time.Second
	fleetReadTimeout = 90 * time.Second
	fleetMaxBackoff  = 5 * time.Minute
	// fleetMaxFrame bounds one frame from the account plane: a call carries
	// a request body, as large as an API request may be.
	fleetMaxFrame = 8 << 20
)

var (
	ErrFleetLinked    = errors.New("this server is already linked; unlink it first")
	ErrFleetNotLinked = errors.New("this server is not linked")
)

func NewFleetService(database *gorm.DB, orgs *OrgService, domains *DomainService, system *SystemService) *FleetService {
	return &FleetService{
		db: database, orgs: orgs, domains: domains, system: system,
		httpc:   &http.Client{Timeout: 20 * time.Second},
		dialer:  &websocket.Dialer{HandshakeTimeout: 20 * time.Second, Proxy: http.ProxyFromEnvironment},
		events:  make(chan string, 4),
		outbox:  make(chan fleet.Message, 16),
		pending: map[string]pendingPerson{},
		seen:    map[string]time.Time{},
		tokens:  map[string]appToken{},
	}
}

// RunCallsAs lets the service run agent apps' calls: it signs in as a person
// with the API's JWT secret and calls the API on its own port. Without it,
// calls are refused.
func (s *FleetService) RunCallsAs(auth *AuthService, jwtSecret string, apiPort int) {
	s.auth, s.jwtSecret, s.apiPort = auth, jwtSecret, apiPort
}

// FleetStatus is what the console shows about the link.
type FleetStatus struct {
	Linked      bool       `json:"linked"`
	Account     string     `json:"account,omitempty"`
	ConnectURL  string     `json:"connect_url,omitempty"`
	Report      bool       `json:"report"`
	Connected   bool       `json:"connected"`
	ConnectedAt *time.Time `json:"connected_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	LinkedAt    *time.Time `json:"linked_at,omitempty"`
	// AgentApps is the owner's switch for agent apps acting as people here.
	AgentApps bool `json:"agent_apps"`
	// People is how many people here linked their accounts.
	People int `json:"people"`
}

func (s *FleetService) link(ctx context.Context) (*db.FleetLink, error) {
	var l db.FleetLink
	err := s.db.WithContext(ctx).Order("created_at").First(&l).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *FleetService) Status(ctx context.Context) (*FleetStatus, error) {
	l, err := s.link(ctx)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return &FleetStatus{}, nil
	}
	s.mu.Lock()
	up := s.connected
	s.mu.Unlock()
	created := l.CreatedAt
	var people int64
	s.db.WithContext(ctx).Model(&db.FleetPerson{}).Count(&people)
	return &FleetStatus{
		Linked: true, Account: l.Account, ConnectURL: l.ConnectURL, Report: l.Report,
		Connected: up, ConnectedAt: l.ConnectedAt, LastError: l.LastError, LinkedAt: &created,
		AgentApps: l.AgentApps, People: int(people),
	}, nil
}

// Enroll redeems a link token: it makes this install's key, registers its
// public half with the account plane, and keeps the account plane's.
func (s *FleetService) Enroll(ctx context.Context, token string, userID uuid.UUID) (*FleetStatus, error) {
	if l, err := s.link(ctx); err != nil {
		return nil, err
	} else if l != nil {
		return nil, ErrFleetLinked
	}
	t, err := fleet.ParseToken(token)
	if err != nil {
		return nil, err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(fleet.EnrollRequest{
		Secret:    t.Secret,
		PublicKey: base64.StdEncoding.EncodeToString(pub),
		Install:   s.about(ctx),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL+fleet.EnrollPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", t.URL, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("the account plane refused the link: %s", remoteMessage(raw, resp.Status))
	}
	var out fleet.EnrollResponse
	if err := json.Unmarshal(raw, &out); err != nil || out.LinkID == "" || out.PublicKey == "" {
		return nil, errors.New("the account plane answered with something that is not a link")
	}
	if k, err := base64.StdEncoding.DecodeString(out.PublicKey); err != nil || len(k) != ed25519.PublicKeySize {
		return nil, errors.New("the account plane sent a key that is not an Ed25519 public key")
	}
	l := db.FleetLink{
		ConnectURL: t.URL, LinkID: out.LinkID, Account: out.Account, AppURL: out.AppURL,
		PrivateKey: db.EncryptedString(base64.StdEncoding.EncodeToString(priv)),
		CloudKey:   out.PublicKey, Report: true, EnrolledBy: userID,
	}
	if err := s.db.WithContext(ctx).Create(&l).Error; err != nil {
		return nil, err
	}
	s.signal(fleetRestart)
	return s.Status(ctx)
}

// remoteMessage pulls the reason out of an error body: Huma's "detail", or
// the body itself when it is short text.
func remoteMessage(raw []byte, status string) string {
	var e struct {
		Detail  string `json:"detail"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &e) == nil {
		if e.Detail != "" {
			return e.Detail
		}
		if e.Message != "" {
			return e.Message
		}
	}
	if t := strings.TrimSpace(string(raw)); t != "" && len(t) < 200 && !strings.HasPrefix(t, "<") {
		return t
	}
	return status
}

// SetReport is the owner's switch.
func (s *FleetService) SetReport(ctx context.Context, on bool) (*FleetStatus, error) {
	l, err := s.link(ctx)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, ErrFleetNotLinked
	}
	if err := s.db.WithContext(ctx).Model(&db.FleetLink{}).Where("id = ?", l.ID).Update("report", on).Error; err != nil {
		return nil, err
	}
	s.signal(fleetSend)
	return s.Status(ctx)
}

// Unlink forgets the link here and drops the connection. The account plane
// sees the connection end and its key never answer again; it is told as well
// when it can be reached, so it can say the owner unlinked rather than that
// the server went quiet.
func (s *FleetService) Unlink(ctx context.Context) error {
	l, err := s.link(ctx)
	if err != nil {
		return err
	}
	if l == nil {
		return ErrFleetNotLinked
	}
	s.tellUnlinked(ctx, l)
	if err := s.db.WithContext(ctx).Where("id = ?", l.ID).Delete(&db.FleetLink{}).Error; err != nil {
		return err
	}
	// Every person's account goes with the link it came through.
	s.db.WithContext(ctx).Where("1 = 1").Delete(&db.FleetPerson{})
	s.mu.Lock()
	s.tokens, s.pending = map[string]appToken{}, map[string]pendingPerson{}
	s.mu.Unlock()
	s.signal(fleetRestart)
	return nil
}

func (s *FleetService) tellUnlinked(ctx context.Context, l *db.FleetLink) {
	key, err := fleetKey(l)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.ConnectURL+fleet.UnlinkPath, nil)
	if err != nil {
		return
	}
	for k, v := range fleet.SignConnect(l.LinkID, key, time.Now()) {
		req.Header.Set(k, v)
	}
	if resp, err := s.httpc.Do(req); err == nil {
		resp.Body.Close()
	}
}

func (s *FleetService) signal(ev string) {
	select {
	case s.events <- ev:
	default:
	}
}

// Start keeps the connection up while the install is linked, reconnecting
// with backoff. It returns when ctx ends.
func (s *FleetService) Start(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		l, err := s.link(ctx)
		if err != nil || l == nil {
			// Not linked: wait to be enrolled.
			select {
			case <-ctx.Done():
				return
			case <-s.events:
			case <-time.After(time.Minute):
			}
			continue
		}
		err = s.session(ctx, l)
		if errors.Is(err, errFleetRestart) {
			backoff = time.Second
			continue
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("fleet: connection to %s: %v", l.ConnectURL, err)
			s.db.Model(&db.FleetLink{}).Where("id = ?", l.ID).Update("last_error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case ev := <-s.events:
			if ev == fleetRestart {
				backoff = time.Second
			}
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, fleetMaxBackoff)
	}
}

var errFleetRestart = errors.New("restart")

// session is one connection, from dial to close.
func (s *FleetService) session(ctx context.Context, l *db.FleetLink) error {
	key, err := fleetKey(l)
	if err != nil {
		return err
	}
	h := http.Header{}
	for k, v := range fleet.SignConnect(l.LinkID, key, time.Now()) {
		h.Set(k, v)
	}
	wsURL := "ws" + strings.TrimPrefix(l.ConnectURL, "http") + fleet.ConnectPath
	conn, resp, err := s.dialer.DialContext(ctx, wsURL, h)
	if err != nil {
		if resp != nil {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
			resp.Body.Close()
			if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
				// The account plane no longer knows this link.
				return s.forget(ctx, l, "the account plane no longer has this link")
			}
			return fmt.Errorf("refused: %s", remoteMessage(raw, resp.Status))
		}
		return err
	}
	defer conn.Close()

	now := time.Now()
	s.db.Model(&db.FleetLink{}).Where("id = ?", l.ID).Updates(map[string]any{"connected_at": now, "last_error": ""})
	s.setConnected(true)
	defer s.setConnected(false)

	// One writer at a time: reports, results of calls run alongside, and
	// what the install has to say about its people.
	var writeMu sync.Mutex
	write := func(m fleet.Message) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
		return conn.WriteJSON(m)
	}

	// The reader: pongs keep the deadline moving; an "unlinked" from the
	// account plane ends the link; people and calls are handled as they come,
	// each call on its own so a slow one holds up nothing else.
	read := make(chan error, 1)
	conn.SetReadLimit(fleetMaxFrame)
	_ = conn.SetReadDeadline(time.Now().Add(fleetReadTimeout))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(fleetReadTimeout)) })
	go func() {
		for {
			var m fleet.Message
			if err := conn.ReadJSON(&m); err != nil {
				read <- err
				return
			}
			switch m.Type {
			case fleet.TypeUnlinked:
				read <- errFleetUnlinked
				return
			case fleet.TypePersonLinked, fleet.TypePersonUnlinked:
				if m.Person != nil {
					s.receivePerson(ctx, l, m.Type, *m.Person)
				}
			case fleet.TypeCall:
				if m.Call != nil {
					go func(c fleet.Call) {
						res := s.runCall(ctx, l, c)
						_ = write(fleet.Message{Type: fleet.TypeResult, Result: &res})
					}(*m.Call)
				}
			}
		}
	}()

	send := func() error {
		var cur db.FleetLink
		if err := s.db.WithContext(ctx).Where("id = ?", l.ID).First(&cur).Error; err != nil {
			return errFleetRestart
		}
		m := fleet.Message{Type: fleet.TypePaused}
		if cur.Report {
			r := s.Report(ctx)
			m = fleet.Message{Type: fleet.TypeReport, Report: &r}
		}
		return write(m)
	}
	if err := send(); err != nil {
		return err
	}
	report := time.NewTicker(fleetReportEvery)
	defer report.Stop()
	ping := time.NewTicker(fleetPingEvery)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, ""), time.Now().Add(time.Second))
			return ctx.Err()
		case err := <-read:
			if errors.Is(err, errFleetUnlinked) {
				return s.forget(ctx, l, "")
			}
			return err
		case ev := <-s.events:
			if ev == fleetRestart {
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
				return errFleetRestart
			}
			if err := send(); err != nil {
				return err
			}
		case m := <-s.outbox:
			if err := write(m); err != nil {
				return err
			}
		case <-report.C:
			if err := send(); err != nil {
				return err
			}
		case <-ping.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				return err
			}
		}
	}
}

var errFleetUnlinked = errors.New("unlinked by the account plane")

// forget removes a link the account plane ended.
func (s *FleetService) forget(ctx context.Context, l *db.FleetLink, why string) error {
	if why != "" {
		log.Printf("fleet: %s; unlinking", why)
	} else {
		log.Printf("fleet: the account plane removed this link; unlinking")
	}
	s.db.WithContext(ctx).Where("id = ?", l.ID).Delete(&db.FleetLink{})
	s.db.WithContext(ctx).Where("1 = 1").Delete(&db.FleetPerson{})
	s.mu.Lock()
	s.tokens, s.pending = map[string]appToken{}, map[string]pendingPerson{}
	s.mu.Unlock()
	return errFleetRestart
}

func (s *FleetService) setConnected(up bool) {
	s.mu.Lock()
	s.connected = up
	s.mu.Unlock()
}

func fleetKey(l *db.FleetLink) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(string(l.PrivateKey))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("this server's link key is damaged; unlink and link again")
	}
	return ed25519.PrivateKey(raw), nil
}

// about describes the install: its workspace's name, its console's address,
// its version and edition.
func (s *FleetService) about(ctx context.Context) fleet.About {
	a := fleet.About{Version: version.Current, Edition: version.Edition}
	if org, err := s.orgs.First(ctx); err == nil && org != nil {
		a.Name = org.Name
		a.ConsoleURL = s.domains.PlatformURL(ctx, org.ID, "console")
	}
	return a
}

// Report is the install's state as the account plane sees it.
func (s *FleetService) Report(ctx context.Context) fleet.Report {
	r := fleet.Report{About: s.about(ctx), At: time.Now().UTC()}
	q := s.db.WithContext(ctx)
	var n int64
	q.Model(&db.Node{}).Count(&n)
	r.Nodes.Total = int(n)
	q.Model(&db.Node{}).Where("status = ?", db.NodeOnline).Count(&n)
	r.Nodes.N = int(n)
	q.Model(&db.Project{}).Where("parent_project_id IS NULL").Count(&n)
	r.Projects = int(n)
	q.Model(&db.Service{}).Count(&n)
	r.Services.Total = int(n)
	q.Model(&db.Service{}).Where("status = ?", db.ServiceRunning).Count(&n)
	r.Services.N = int(n)

	var scheduled, failing int64
	q.Model(&db.BackupConfig{}).Where("enabled").Count(&n)
	scheduled += n
	q.Model(&db.BackupConfig{}).Where("enabled AND last_backup_status = ?", db.BackupFailed).Count(&n)
	failing += n
	q.Model(&db.VolumeBackupConfig{}).Where("enabled").Count(&n)
	scheduled += n
	q.Model(&db.VolumeBackupConfig{}).Where("enabled AND last_backup_status = ?", db.BackupFailed).Count(&n)
	failing += n
	r.Backups.Scheduled, r.Backups.Failing = int(scheduled), int(failing)

	var databases, protected []uuid.UUID
	q.Model(&db.Service{}).Where("type = ?", db.ServiceTypeDatabase).Pluck("id", &databases)
	q.Model(&db.BackupConfig{}).Where("enabled").Pluck("service_id", &protected)
	has := make(map[uuid.UUID]bool, len(protected))
	for _, id := range protected {
		has[id] = true
	}
	for _, id := range databases {
		if !has[id] {
			r.Backups.Unprotected++
		}
	}

	if s.system != nil {
		if v := s.system.GetVersionInfo(ctx); v.UpdateAvailable {
			r.UpdateAvailable = v.Latest
		}
	}
	return r
}
