package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// ConsoleSessionService keeps the console's sign-ins: one row per sign-in,
// named by the token the browser holds, so a session can be listed and ended.
type ConsoleSessionService struct {
	db *gorm.DB

	mu    sync.Mutex
	cache map[uuid.UUID]sessionCheck
}

const (
	// ConsoleSessionTTL is how long a sign-in lasts: the token's own expiry.
	ConsoleSessionTTL = 24 * time.Hour
	// sessionCheckTTL is how long a live session's check is reused. Ending a
	// session through this process clears it at once; the TTL bounds how long
	// anything else may go unnoticed.
	sessionCheckTTL = 10 * time.Second
	// sessionTouchEvery is how often last seen is written, so a busy console
	// does not write on every request.
	sessionTouchEvery = time.Minute
)

type sessionCheck struct {
	userID uuid.UUID
	at     time.Time
}

// Client is where a sign-in came from, as the console shows it.
type Client struct {
	UserAgent string
	IP        string
}

// ErrSessionNotFound is a session that is not live, or not the caller's.
var ErrSessionNotFound = errors.New("session not found")

// ErrSessionElsewhere is an admin ending the session of someone who also
// belongs to another organisation, where it signs them in too.
var ErrSessionElsewhere = errors.New("this person also belongs to another organisation, where this sign-in works too, so only they can end it")

// startConsoleSession records a sign-in and returns its id, for the token.
func startConsoleSession(ctx context.Context, q *gorm.DB, userID uuid.UUID, c Client) (uuid.UUID, error) {
	now := time.Now()
	row := db.ConsoleSession{UserID: userID, UserAgent: truncateString(c.UserAgent, 300), IP: truncateString(c.IP, 64),
		LastSeenAt: now, ExpiresAt: now.Add(ConsoleSessionTTL)}
	row.ID = uuid.New()
	return row.ID, q.WithContext(ctx).Create(&row).Error
}

// Start records a sign-in for userID and returns the session's id.
func (s *ConsoleSessionService) Start(ctx context.Context, userID uuid.UUID, c Client) (uuid.UUID, error) {
	return startConsoleSession(ctx, s.db, userID, c)
}

func truncateString(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func liveConsoleSessions(q *gorm.DB) *gorm.DB {
	return q.Where("console_sessions.revoked_at IS NULL AND console_sessions.expires_at > ?", time.Now())
}

// Resolve says whether sid is a live session of userID, for the auth
// middleware. A live answer is reused for a few seconds, and last seen is
// written at most once a minute.
func (s *ConsoleSessionService) Resolve(ctx context.Context, sid, userID uuid.UUID) bool {
	now := time.Now()
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[uuid.UUID]sessionCheck{}
	}
	c, ok := s.cache[sid]
	s.mu.Unlock()
	if ok && c.userID == userID && now.Sub(c.at) < sessionCheckTTL {
		return true
	}

	var row db.ConsoleSession
	if err := liveConsoleSessions(s.db.WithContext(ctx)).Limit(1).Find(&row, "id = ? AND user_id = ?", sid, userID).Error; err != nil || row.ID == uuid.Nil {
		s.forget(sid)
		return false
	}
	if now.Sub(row.LastSeenAt) > sessionTouchEvery {
		// Detached from the request, so a finished request does not cancel it.
		go func() { _ = s.db.Model(&db.ConsoleSession{}).Where("id = ?", sid).Update("last_seen_at", now).Error }()
	}
	s.mu.Lock()
	s.cache[sid] = sessionCheck{userID: userID, at: now}
	s.mu.Unlock()
	return true
}

func (s *ConsoleSessionService) forget(ids ...uuid.UUID) {
	s.mu.Lock()
	for _, id := range ids {
		delete(s.cache, id)
	}
	s.mu.Unlock()
}

// end revokes the sessions q selects and forgets their checks.
func (s *ConsoleSessionService) end(ctx context.Context, q *gorm.DB) (int64, error) {
	var ids []uuid.UUID
	if err := liveConsoleSessions(q.WithContext(ctx).Model(&db.ConsoleSession{})).Pluck("console_sessions.id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res := s.db.WithContext(ctx).Model(&db.ConsoleSession{}).Where("id IN ?", ids).Update("revoked_at", time.Now())
	s.forget(ids...)
	return res.RowsAffected, res.Error
}

// End signs out one session: logging out.
func (s *ConsoleSessionService) End(ctx context.Context, sid uuid.UUID) error {
	_, err := s.end(ctx, s.db.Where("id = ?", sid))
	return err
}

// Revoke ends one of a person's own sessions.
func (s *ConsoleSessionService) Revoke(ctx context.Context, userID, sid uuid.UUID) error {
	n, err := s.end(ctx, s.db.Where("id = ? AND user_id = ?", sid, userID))
	if err == nil && n == 0 {
		return ErrSessionNotFound
	}
	return err
}

// RevokeOthers ends every session of a person but keep, which may be uuid.Nil
// to end them all. Changing the password and turning two-factor sign-in off
// call it, so a session someone else holds does not outlive either.
func (s *ConsoleSessionService) RevokeOthers(ctx context.Context, userID, keep uuid.UUID) (int64, error) {
	return s.end(ctx, s.db.Where("user_id = ? AND id <> ?", userID, keep))
}

// ConsoleSessionView is one sign-in as its owner sees it.
type ConsoleSessionView struct {
	ID         uuid.UUID `json:"id"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Current    bool      `json:"current" doc:"The session this request was made with"`
}

// List is a person's live sessions, the most recently seen first.
func (s *ConsoleSessionService) List(ctx context.Context, userID, current uuid.UUID) ([]ConsoleSessionView, error) {
	var rows []db.ConsoleSession
	err := liveConsoleSessions(s.db.WithContext(ctx)).Where("user_id = ?", userID).Order("last_seen_at DESC").Find(&rows).Error
	out := make([]ConsoleSessionView, len(rows))
	for i, r := range rows {
		out[i] = ConsoleSessionView{ID: r.ID, UserAgent: r.UserAgent, IP: r.IP, CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt, Current: r.ID == current}
	}
	return out, err
}

// OrgConsoleSession is a member's sign-in, as an organisation's admin sees it.
type OrgConsoleSession struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"user_id"`
	UserName   string    `json:"user_name"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	// Elsewhere is true when the person also belongs to another organisation,
	// where the sign-in works too: then only they end it.
	Elsewhere bool `json:"elsewhere"`
	// Current is the sign-in the asking request was made with.
	Current bool `json:"current" gorm:"-"`
}

// OrgSessions are the sign-ins of an organisation's members (or of one of
// them), the most recently seen first. A sign-in is the person's in every
// organisation they belong to, so it is listed in each.
// current is the asking request's own session, marked in the list.
func (s *ConsoleSessionService) OrgSessions(ctx context.Context, orgID uuid.UUID, userID *uuid.UUID, current uuid.UUID) ([]OrgConsoleSession, error) {
	q := liveConsoleSessions(s.db.WithContext(ctx).Model(&db.ConsoleSession{})).
		Select("console_sessions.id, console_sessions.user_id, users.username AS user_name, console_sessions.user_agent, console_sessions.ip, "+
			"console_sessions.created_at, console_sessions.last_seen_at, "+
			"EXISTS (SELECT 1 FROM organization_members o WHERE o.user_id = console_sessions.user_id AND o.organization_id <> ?) AS elsewhere", orgID).
		Joins("JOIN users ON users.id = console_sessions.user_id").
		Joins("JOIN organization_members m ON m.user_id = console_sessions.user_id AND m.organization_id = ?", orgID)
	if userID != nil {
		q = q.Where("console_sessions.user_id = ?", *userID)
	}
	out := []OrgConsoleSession{}
	err := q.Order("console_sessions.last_seen_at DESC").Scan(&out).Error
	for i := range out {
		out[i].Current = out[i].ID == current
	}
	return out, err
}

// RevokeInOrg is an organisation's owner or admin ending a member's sign-in,
// while the member belongs to this organisation alone: a sign-in works in
// every organisation its person is in.
func (s *ConsoleSessionService) RevokeInOrg(ctx context.Context, orgID, sid uuid.UUID) error {
	var row db.ConsoleSession
	if err := liveConsoleSessions(s.db.WithContext(ctx)).
		Joins("JOIN organization_members m ON m.user_id = console_sessions.user_id AND m.organization_id = ?", orgID).
		Limit(1).Find(&row, "console_sessions.id = ?", sid).Error; err != nil || row.ID == uuid.Nil {
		return ErrSessionNotFound
	}
	var other int64
	s.db.WithContext(ctx).Model(&db.OrganizationMember{}).Where("user_id = ? AND organization_id <> ?", row.UserID, orgID).Count(&other)
	if other > 0 {
		return ErrSessionElsewhere
	}
	_, err := s.end(ctx, s.db.Where("id = ?", row.ID))
	return err
}

// orgSessionCounts are each member's live sign-ins, for the Users list.
func orgSessionCounts(ctx context.Context, q *gorm.DB, orgID uuid.UUID) ([]struct {
	UserID uuid.UUID
	N      int
}, error) {
	var rows []struct {
		UserID uuid.UUID
		N      int
	}
	err := liveConsoleSessions(q.WithContext(ctx).Model(&db.ConsoleSession{})).Select("console_sessions.user_id, count(*) AS n").
		Joins("JOIN organization_members m ON m.user_id = console_sessions.user_id AND m.organization_id = ?", orgID).
		Group("console_sessions.user_id").Scan(&rows).Error
	return rows, err
}
