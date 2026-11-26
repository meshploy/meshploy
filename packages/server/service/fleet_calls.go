package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/fleet"
)

// Agent apps through the account: a person here links their account on the
// account plane, and the agent apps that account approved act as them,
// through the link's connection. Three things must all hold for a call to
// run: the owner's Agent apps switch, the person's own link, and the account
// plane's signature on the call. The call then goes through this API as that
// person, with exactly their permissions, and is recorded with the app's name.

var (
	ErrAgentAppsOff   = errors.New("agent apps are turned off on this server")
	ErrPersonNotFound = errors.New("your Meshploy account is not linked here")
)

const (
	// personCodeTTL is how long a person has to approve on the account plane.
	personCodeTTL = 15 * time.Minute
	// fleetCallLimit bounds an answer sent back on the connection.
	fleetCallLimit = 8 << 20
	// fleetCallsKept is how long the record of calls is kept.
	fleetCallsKept = 90 * 24 * time.Hour
)

type pendingPerson struct {
	userID  uuid.UUID
	expires time.Time
}

type appToken struct {
	token   string
	expires time.Time
}

// callsNever are the API's paths no agent app may reach, whatever the
// person's role: what the server's owner and each person's own security rest
// on, and what the console's remote MCP keeps from every agent. Matched
// against the path without its query.
var callsNever = []string{
	"/api/v1/auth/", "/api/v1/oauth", "/api/v1/me", "/api/v1/agents", "/api/v1/cli",
	"/api/v1/sign-in", "/api/v1/system/",
}

// callsNeverContain are path pieces refused anywhere in a path.
var callsNeverContain = []string{
	"/node-registration-token", "/nodes/registration-token", "/provisioning-token",
	"/system-backup", "/members", "/invitations", "/permissions",
	"/db/query", "/db/schema", "/console-sessions",
}

// callPathAllowed says whether an agent app may call method path at all.
func callPathAllowed(method, path string) bool {
	p := path
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if !strings.HasPrefix(p, "/api/v1/") || strings.Contains(p, "..") || strings.Contains(p, "//") {
		return false
	}
	if p == "/api/v1/system/version" && method == http.MethodGet {
		return true
	}
	for _, n := range callsNever {
		if strings.HasPrefix(p, n) || p == strings.TrimSuffix(n, "/") {
			return false
		}
	}
	for _, n := range callsNeverContain {
		if strings.Contains(p, n) {
			return false
		}
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// appURLFor is the account plane's web address: what it said at enrolment,
// or its connector's address with connect. made app.
func appURLFor(l *db.FleetLink) string {
	if l.AppURL != "" {
		return strings.TrimRight(l.AppURL, "/")
	}
	u, err := url.Parse(l.ConnectURL)
	if err != nil {
		return l.ConnectURL
	}
	u.Host = strings.Replace(u.Host, "connect.", "app.", 1)
	return strings.TrimRight(u.String(), "/")
}

// SetAgentApps is the owner's switch. Turned off, every person's link stays
// but no call runs. Turned on by the person who linked the server, it links
// them too: the account plane knows their account made the link, so it needs
// no approval from them.
func (s *FleetService) SetAgentApps(ctx context.Context, on bool, userID uuid.UUID) (*FleetStatus, error) {
	l, err := s.link(ctx)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, ErrFleetNotLinked
	}
	if err := s.db.WithContext(ctx).Model(&db.FleetLink{}).Where("id = ?", l.ID).Update("agent_apps", on).Error; err != nil {
		return nil, err
	}
	if on && userID == l.EnrolledBy {
		var n int64
		s.db.WithContext(ctx).Model(&db.FleetPerson{}).Where("user_id = ?", userID).Count(&n)
		if n == 0 {
			if code, err := s.newPersonCode(userID); err == nil {
				select {
				case s.outbox <- fleet.Message{Type: fleet.TypeLinkOwner, Person: &fleet.Person{Code: code}}:
				default:
				}
			}
		}
	}
	return s.Status(ctx)
}

// newPersonCode makes a one-time code for userID, replacing any they had.
func (s *FleetService) newPersonCode(userID uuid.UUID) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for c, p := range s.pending {
		if now.After(p.expires) || p.userID == userID {
			delete(s.pending, c)
		}
	}
	s.pending[code] = pendingPerson{userID: userID, expires: now.Add(personCodeTTL)}
	return code, nil
}

// MyAccount is a person's own view: whether agent apps may act as them here.
type MyAccount struct {
	// ServerLinked: the server is linked to an account plane at all.
	ServerLinked bool `json:"server_linked"`
	// Available: the server is linked and its owner allows agent apps.
	Available bool       `json:"available"`
	Linked    bool       `json:"linked"`
	Email     string     `json:"email,omitempty"`
	LinkedAt  *time.Time `json:"linked_at,omitempty"`
	// AppURL is where the person manages their agent apps.
	AppURL string `json:"app_url,omitempty"`
}

func (s *FleetService) MyAccount(ctx context.Context, userID uuid.UUID) (*MyAccount, error) {
	l, err := s.link(ctx)
	if err != nil {
		return nil, err
	}
	out := &MyAccount{}
	if l == nil {
		return out, nil
	}
	out.ServerLinked, out.Available, out.AppURL = true, l.AgentApps, appURLFor(l)
	var p db.FleetPerson
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&p).Error; err == nil {
		at := p.CreatedAt
		out.Linked, out.Email, out.LinkedAt = true, p.Email, &at
	}
	return out, nil
}

// StartPersonLink makes a one-time code for userID and returns the address
// on the account plane where they sign in and approve.
func (s *FleetService) StartPersonLink(ctx context.Context, userID uuid.UUID) (string, error) {
	l, err := s.link(ctx)
	if err != nil {
		return "", err
	}
	if l == nil {
		return "", ErrFleetNotLinked
	}
	if !l.AgentApps {
		return "", ErrAgentAppsOff
	}
	code, err := s.newPersonCode(userID)
	if err != nil {
		return "", err
	}
	return appURLFor(l) + "/connect-server?link=" + url.QueryEscape(l.LinkID) + "&code=" + url.QueryEscape(code), nil
}

// UnlinkPerson withdraws a person's account here, and tells the account plane.
func (s *FleetService) UnlinkPerson(ctx context.Context, userID uuid.UUID) error {
	var p db.FleetPerson
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&p).Error; err != nil {
		return ErrPersonNotFound
	}
	if err := s.db.WithContext(ctx).Where("id = ?", p.ID).Delete(&db.FleetPerson{}).Error; err != nil {
		return err
	}
	s.forgetTokens(userID)
	select {
	case s.outbox <- fleet.Message{Type: fleet.TypePersonUnlinked, Person: &fleet.Person{Account: p.Account}}:
	default:
	}
	return nil
}

func (s *FleetService) forgetTokens(userID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.tokens {
		if strings.HasPrefix(k, userID.String()+"|") {
			delete(s.tokens, k)
		}
	}
}

// receivePerson applies a person frame from the account plane.
func (s *FleetService) receivePerson(ctx context.Context, l *db.FleetLink, kind string, p fleet.Person) {
	pub, err := base64.StdEncoding.DecodeString(l.CloudKey)
	if err != nil || len(pub) != ed25519.PublicKeySize || fleet.VerifyPerson(kind, l.LinkID, p, ed25519.PublicKey(pub)) != nil {
		log.Printf("fleet: a %s frame with a bad signature was ignored", kind)
		return
	}
	if p.Account == "" {
		return
	}
	if kind == fleet.TypePersonUnlinked {
		var gone []db.FleetPerson
		s.db.WithContext(ctx).Where("account = ?", p.Account).Find(&gone)
		s.db.WithContext(ctx).Where("account = ?", p.Account).Delete(&db.FleetPerson{})
		for _, g := range gone {
			s.forgetTokens(g.UserID)
		}
		return
	}
	s.mu.Lock()
	pend, ok := s.pending[p.Code]
	delete(s.pending, p.Code)
	s.mu.Unlock()
	if !ok || time.Now().After(pend.expires) {
		log.Printf("fleet: an approval arrived for a code this server does not hold; ignored")
		return
	}
	// One account per person and one person per account: a new link replaces
	// whatever either had.
	s.db.WithContext(ctx).Where("user_id = ? OR account = ?", pend.userID, p.Account).Delete(&db.FleetPerson{})
	s.forgetTokens(pend.userID)
	if err := s.db.WithContext(ctx).Create(&db.FleetPerson{UserID: pend.userID, Account: p.Account, Email: p.Email}).Error; err != nil {
		log.Printf("fleet: recording a person's link: %v", err)
	}
}

// runCall runs one call from an agent app, if everything allows it.
func (s *FleetService) runCall(ctx context.Context, l *db.FleetLink, c fleet.Call) fleet.Result {
	refuse := func(status int, why string, userID *uuid.UUID) fleet.Result {
		s.recordCall(ctx, c, userID, status, why)
		body := fmt.Appendf(nil, `{"title":%q,"status":%d,"detail":%q}`, http.StatusText(status), status, why)
		return fleet.Result{ID: c.ID, Status: status, ContentType: "application/problem+json", Body: body}
	}
	pub, err := base64.StdEncoding.DecodeString(l.CloudKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return refuse(http.StatusForbidden, "this server's link has no key for the account plane", nil)
	}
	if err := fleet.VerifyCall(l.LinkID, c, ed25519.PublicKey(pub), time.Now()); err != nil {
		return refuse(http.StatusForbidden, "the call is not the account plane's: "+err.Error(), nil)
	}
	if !s.firstSeen(c.ID, c.Expires) {
		return refuse(http.StatusConflict, "this call was run already", nil)
	}
	// The person first, so even a refusal lands on their record.
	var p db.FleetPerson
	if err := s.db.WithContext(ctx).Where("account = ?", c.Account).First(&p).Error; err != nil {
		return refuse(http.StatusForbidden, "no one here has linked this Meshploy account", nil)
	}
	var cur db.FleetLink
	if err := s.db.WithContext(ctx).Where("id = ?", l.ID).First(&cur).Error; err != nil || !cur.AgentApps {
		return refuse(http.StatusForbidden, ErrAgentAppsOff.Error(), &p.UserID)
	}
	if !callPathAllowed(c.Method, c.Path) {
		return refuse(http.StatusForbidden, "agent apps cannot reach this through Meshploy Cloud", &p.UserID)
	}
	if s.auth == nil || s.apiPort == 0 {
		return refuse(http.StatusServiceUnavailable, "this server cannot run calls", &p.UserID)
	}
	token, err := s.tokenFor(ctx, p.UserID, c.App)
	if err != nil {
		return refuse(http.StatusInternalServerError, "could not act as you here", &p.UserID)
	}

	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, c.Method, fmt.Sprintf("http://127.0.0.1:%d%s", s.apiPort, c.Path), bytes.NewReader(c.Body))
	if err != nil {
		return refuse(http.StatusBadRequest, "the call is not a request", &p.UserID)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if c.ContentType != "" {
		req.Header.Set("Content-Type", c.ContentType)
	}
	req.Header.Set("User-Agent", appLabel(c.App))
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return refuse(http.StatusBadGateway, "this server's API did not answer", &p.UserID)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, fleetCallLimit))
	s.recordCall(ctx, c, &p.UserID, resp.StatusCode, "")
	return fleet.Result{ID: c.ID, Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Body: body}
}

// appLabel is how an app's sign-in shows on a person's list.
func appLabel(app string) string {
	app = strings.TrimSpace(app)
	if app == "" {
		app = "An agent app"
	}
	if len(app) > 60 {
		app = app[:60]
	}
	return app + " via Meshploy Cloud"
}

// firstSeen records a call id, and says whether it was new.
func (s *FleetService) firstSeen(id string, expires time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, exp := range s.seen {
		if now.After(exp.Add(fleet.MaxClockSkew)) {
			delete(s.seen, k)
		}
	}
	if _, ok := s.seen[id]; ok || id == "" {
		return false
	}
	s.seen[id] = expires
	return true
}

// tokenFor is a console session for userID named after the app, made once
// and reused while it lasts: the person sees it among their sign-ins and can
// end it there.
func (s *FleetService) tokenFor(ctx context.Context, userID uuid.UUID, app string) (string, error) {
	key := userID.String() + "|" + app
	s.mu.Lock()
	t, ok := s.tokens[key]
	s.mu.Unlock()
	if ok && time.Now().Before(t.expires) {
		return t.token, nil
	}
	token, err := s.auth.SignInAs(ctx, userID, Client{UserAgent: appLabel(app), IP: "Meshploy Cloud"}, s.jwtSecret)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.tokens[key] = appToken{token: token, expires: time.Now().Add(ConsoleSessionTTL - time.Hour)}
	s.mu.Unlock()
	return token, nil
}

// recordCall keeps the record of a call, and prunes the old.
func (s *FleetService) recordCall(ctx context.Context, c fleet.Call, userID *uuid.UUID, status int, refused string) {
	path := c.Path
	if len(path) > 500 {
		path = path[:500]
	}
	s.db.WithContext(ctx).Create(&db.FleetCall{At: time.Now(), UserID: userID, App: strings.TrimSpace(c.App),
		Method: c.Method, Path: path, Status: status, Refused: refused})
	s.db.WithContext(ctx).Where("at < ?", time.Now().Add(-fleetCallsKept)).Delete(&db.FleetCall{})
}

// CallsFor is the record of calls agent apps made acting as userID, newest
// first.
func (s *FleetService) CallsFor(ctx context.Context, userID uuid.UUID, limit int) ([]db.FleetCall, error) {
	var out []db.FleetCall
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("at DESC").Limit(limit).Find(&out).Error
	return out, err
}
