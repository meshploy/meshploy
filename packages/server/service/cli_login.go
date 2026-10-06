package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// CLILoginService logs a CLI in through a browser, as OAuth's device flow
// does: the CLI starts a login and polls; a person signed in to the console
// sees the code the terminal shows and approves it; the CLI then collects a
// token that acts as that person. The password and two-factor code are never
// typed into the terminal, which over SSH is someone else's machine.
type CLILoginService struct {
	db *gorm.DB
}

const (
	// CLILoginTTL is how long a login waits for someone to approve it.
	CLILoginTTL = 10 * time.Minute
	// CLILoginInterval is how often the CLI asks whether it was approved.
	CLILoginInterval = 5 * time.Second
	// CLITokenIdle is how long a CLI token lasts unused: a CLI in use stays
	// logged in, a forgotten one lapses.
	CLITokenIdle = 90 * 24 * time.Hour
	// cliTokenTouchEvery bounds how often a token's last use is written, so a
	// busy CLI is not a write per request.
	cliTokenTouchEvery = time.Hour
	// cliLoginKept is how long a finished or expired login is kept, for
	// nothing more than answering a late poll truthfully.
	cliLoginKept = 24 * time.Hour
)

var (
	ErrCLILoginNotFound = errors.New("no login is waiting with that code: it may have expired, so start again from the terminal")
	ErrCLILoginSettled  = errors.New("this login was already approved or denied")
	ErrCLILoginDoor     = errors.New("not a console this server serves")
	ErrCLIApprover      = errors.New("only a person can approve a CLI login, from a browser signed in to the console")
)

// CLILoginStart is what the CLI is told when it starts a login. DeviceCode is
// its own secret, to poll with; UserCode is what both sides show.
type CLILoginStart struct {
	DeviceCode string
	UserCode   string
	Door       string
	ExpiresAt  time.Time
	Interval   time.Duration
}

// CLILoginPoll is the answer to a CLI asking about its login: the state, and
// the token, once, when it was approved.
type CLILoginPoll struct {
	Status string
	Token  string
}

// CLILoginExpired is the state a poll reports for a login nobody acted on in
// time. It is never stored: expiry is read from ExpiresAt.
const CLILoginExpired = "expired"

// Start begins a login for the machine named host, approved at door: the
// console, or a name an extension registered as one.
func (s *CLILoginService) Start(ctx context.Context, host, door string) (*CLILoginStart, error) {
	if door == "" {
		door = "console"
	}
	if !IsConsoleName(door) {
		return nil, ErrCLILoginDoor
	}
	host = strings.TrimSpace(host)
	if host == "" {
		host = "an unnamed machine"
	}
	if len(host) > 100 {
		host = host[:100]
	}
	device, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	// Finished logins are only kept to answer a late poll; clear the old ones
	// as new ones start, rather than with a reaper of their own.
	s.db.WithContext(ctx).Where("expires_at < ?", time.Now().Add(-cliLoginKept)).Delete(&db.CLILogin{})

	row := db.CLILogin{DeviceCodeHash: hashToken(device), Host: host, Door: door,
		Status: db.CLILoginPending, ExpiresAt: time.Now().Add(CLILoginTTL)}
	// A user code is short so a person can compare it; one in use is drawn again.
	for range 5 {
		if row.UserCode, err = newUserCode(); err != nil {
			return nil, err
		}
		row.ID = uuid.New()
		if err = s.db.WithContext(ctx).Create(&row).Error; err == nil || !isUniqueViolation(err) {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	return &CLILoginStart{DeviceCode: device, UserCode: row.UserCode, Door: door,
		ExpiresAt: row.ExpiresAt, Interval: CLILoginInterval}, nil
}

// Poll is the CLI asking whether its login was approved. An approved login
// gives its token once: the first poll after approval collects it, and a
// second is told it was collected.
func (s *CLILoginService) Poll(ctx context.Context, deviceCode string) (*CLILoginPoll, error) {
	var login db.CLILogin
	if err := s.db.WithContext(ctx).First(&login, "device_code_hash = ?", hashToken(deviceCode)).Error; err != nil {
		return nil, ErrCLILoginNotFound
	}
	if login.Status == db.CLILoginPending && !login.ExpiresAt.After(time.Now()) {
		return &CLILoginPoll{Status: CLILoginExpired}, nil
	}
	if login.Status != db.CLILoginApproved {
		return &CLILoginPoll{Status: login.Status}, nil
	}
	var token string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Collecting wins for one of two polls at once.
		res := tx.Model(&db.CLILogin{}).Where("id = ? AND status = ?", login.ID, db.CLILoginApproved).
			Update("status", db.CLILoginCollected)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		var err error
		token, err = s.mint(tx, *login.UserID, login.Host)
		return err
	})
	if err != nil {
		return nil, err
	}
	if token == "" {
		return &CLILoginPoll{Status: db.CLILoginCollected}, nil
	}
	return &CLILoginPoll{Status: db.CLILoginApproved, Token: token}, nil
}

// Waiting is a login by its user code, for the approval page to show, while
// it can still be approved.
func (s *CLILoginService) Waiting(ctx context.Context, userCode string) (*db.CLILogin, error) {
	var login db.CLILogin
	if err := s.db.WithContext(ctx).First(&login, "user_code = ?", NormalizeUserCode(userCode)).Error; err != nil {
		return nil, ErrCLILoginNotFound
	}
	if login.Status != db.CLILoginPending {
		return nil, ErrCLILoginSettled
	}
	if !login.ExpiresAt.After(time.Now()) {
		return nil, ErrCLILoginNotFound
	}
	return &login, nil
}

// Decide approves or denies a waiting login, as userID. Only a person may:
// an agent principal approving a login would turn its own token into a CLI
// token that outlives it. The caller makes sure userID signed in through a
// browser, two-factor included.
func (s *CLILoginService) Decide(ctx context.Context, userCode string, userID uuid.UUID, approve bool) error {
	var user db.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil || user.Kind != db.UserHuman {
		return ErrCLIApprover
	}
	status := db.CLILoginDenied
	if approve {
		status = db.CLILoginApproved
	}
	res := s.db.WithContext(ctx).Model(&db.CLILogin{}).
		Where("user_code = ? AND status = ? AND expires_at > ?", NormalizeUserCode(userCode), db.CLILoginPending, time.Now()).
		Updates(map[string]any{"status": status, "user_id": userID})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		_, err := s.Waiting(ctx, userCode)
		if err == nil {
			err = ErrCLILoginSettled
		}
		return err
	}
	return nil
}

// mint makes a CLI token for userID on host. The plaintext is returned once
// and never kept.
func (s *CLILoginService) mint(tx *gorm.DB, userID uuid.UUID, host string) (string, error) {
	raw, err := randomHex(32)
	if err != nil {
		return "", err
	}
	token := "mcli-" + raw
	row := db.CLIToken{UserID: userID, Host: host, TokenHash: hashToken(token), TokenPrefix: token[:tokenPrefixLen]}
	row.ID = uuid.New()
	if err := tx.Create(&row).Error; err != nil {
		return "", err
	}
	return token, nil
}

// liveCLITokens narrows a query to tokens that still work: not revoked, and used (or
// made) within CLITokenIdle.
func liveCLITokens(q *gorm.DB) *gorm.DB {
	return q.Where("revoked_at IS NULL AND COALESCE(last_used_at, created_at) > ?", time.Now().Add(-CLITokenIdle))
}

// ResolveToken authenticates a CLI token, for the auth middleware: the person
// it acts as, while it still works. Its last use is recorded at most hourly.
func (s *CLILoginService) ResolveToken(ctx context.Context, plaintext string) (uuid.UUID, bool) {
	var tok db.CLIToken
	if err := liveCLITokens(s.db.WithContext(ctx)).First(&tok, "token_hash = ?", hashToken(plaintext)).Error; err != nil {
		return uuid.Nil, false
	}
	if tok.LastUsedAt == nil || time.Since(*tok.LastUsedAt) > cliTokenTouchEvery {
		// Detached from the request, so a finished request doesn't cancel it.
		go func(id uuid.UUID) {
			_ = s.db.Model(&db.CLIToken{}).Where("id = ?", id).Update("last_used_at", time.Now()).Error
		}(tok.ID)
	}
	return tok.UserID, true
}

// CLISession is a CLI token that still works, as its owner sees it. Current
// marks the one the asking request was made with.
type CLISession struct {
	db.CLIToken
	Current bool
}

// Sessions are a person's CLI tokens that still work, the most recently used
// first. current is the credential the asking request carried, if any.
func (s *CLILoginService) Sessions(ctx context.Context, userID uuid.UUID, current string) ([]CLISession, error) {
	var rows []db.CLIToken
	err := liveCLITokens(s.db.WithContext(ctx)).Where("user_id = ?", userID).
		Order("COALESCE(last_used_at, created_at) DESC").Find(&rows).Error
	out := make([]CLISession, len(rows))
	for i, r := range rows {
		out[i] = CLISession{CLIToken: r, Current: current != "" && r.TokenHash == hashToken(current)}
	}
	return out, err
}

// OrgCLISession is a member's signed-in CLI, as an organisation's admin
// sees it.
type OrgCLISession struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	UserName   string     `json:"user_name"`
	Host       string     `json:"host"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	// Elsewhere is true when its person also belongs to another
	// organisation, where the CLI acts as them too: then only they log it out.
	Elsewhere bool `json:"elsewhere"`
}

// OrgSessions are the CLIs signed in as an organisation's members (or as one
// of them, userID), most recently used first. A CLI acts as its person in
// every organisation they belong to, so it is listed in each.
func (s *CLILoginService) OrgSessions(ctx context.Context, orgID uuid.UUID, userID *uuid.UUID) ([]OrgCLISession, error) {
	q := s.db.WithContext(ctx).Model(&db.CLIToken{}).
		Where("cli_tokens.revoked_at IS NULL AND COALESCE(cli_tokens.last_used_at, cli_tokens.created_at) > ?", time.Now().Add(-CLITokenIdle)).
		Select("cli_tokens.id, cli_tokens.user_id, users.username AS user_name, cli_tokens.host, cli_tokens.created_at, cli_tokens.last_used_at, "+
			"EXISTS (SELECT 1 FROM organization_members o WHERE o.user_id = cli_tokens.user_id AND o.organization_id <> ?) AS elsewhere", orgID).
		Joins("JOIN users ON users.id = cli_tokens.user_id").
		Joins("JOIN organization_members m ON m.user_id = cli_tokens.user_id AND m.organization_id = ?", orgID)
	if userID != nil {
		q = q.Where("cli_tokens.user_id = ?", *userID)
	}
	out := []OrgCLISession{}
	err := q.Order("COALESCE(cli_tokens.last_used_at, cli_tokens.created_at) DESC").Scan(&out).Error
	return out, err
}

// ErrCLIElsewhere is an admin logging out a CLI whose person also belongs to
// another organisation, where it acts as them too.
var ErrCLIElsewhere = errors.New("this person also belongs to another organisation, where the CLI acts as them too, so only they can sign it out")

// RevokeInOrg is an organisation's owner or admin logging out a member's CLI.
// A CLI acts as its person everywhere, so it is theirs to end only while the
// person belongs to this organisation alone.
func (s *CLILoginService) RevokeInOrg(ctx context.Context, orgID, id uuid.UUID) error {
	q := s.db.WithContext(ctx)
	var tok db.CLIToken
	if err := q.Joins("JOIN organization_members m ON m.user_id = cli_tokens.user_id AND m.organization_id = ?", orgID).
		First(&tok, "cli_tokens.id = ? AND cli_tokens.revoked_at IS NULL", id).Error; err != nil {
		return ErrTokenNotFound
	}
	var other int64
	q.Model(&db.OrganizationMember{}).Where("user_id = ? AND organization_id <> ?", tok.UserID, orgID).Count(&other)
	if other > 0 {
		return ErrCLIElsewhere
	}
	return q.Model(&db.CLIToken{}).Where("id = ?", tok.ID).Update("revoked_at", time.Now()).Error
}

// Revoke ends one of a person's CLI sessions.
func (s *CLILoginService) Revoke(ctx context.Context, userID, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Model(&db.CLIToken{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", id, userID).Update("revoked_at", time.Now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// RevokeToken ends the session a CLI token belongs to: logging out.
func (s *CLILoginService) RevokeToken(ctx context.Context, plaintext string) error {
	res := s.db.WithContext(ctx).Model(&db.CLIToken{}).
		Where("token_hash = ? AND revoked_at IS NULL", hashToken(plaintext)).Update("revoked_at", time.Now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// userCodeAlphabet has no vowels, so no code spells a word, and nothing that
// reads as another character (0/O, 1/I).
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

// newUserCode is a code such as WXZB-KQTD: eight letters, about 2^34 of them,
// which a person can compare at a glance and which lives ten minutes.
func newUserCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	for i := range b {
		b[i] = userCodeAlphabet[int(b[i])%len(userCodeAlphabet)]
	}
	return string(b[:4]) + "-" + string(b[4:]), nil
}

// NormalizeUserCode accepts a user code as a person types it: any case, with
// or without its dash or spaces.
func NormalizeUserCode(code string) string {
	code = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
	if len(code) != 8 {
		return code
	}
	return code[:4] + "-" + code[4:]
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}
