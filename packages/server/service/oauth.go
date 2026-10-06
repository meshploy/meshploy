package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// OAuthService makes Meshploy the OAuth authorization server for its own MCP
// endpoint, so a client that can only connect through OAuth (Claude on the web
// and on the desktop) can connect: the client registers itself, a person
// signed in to the console approves it, and the client trades the code for
// tokens. A connection acts as the person who approved it, or as one of the
// organisation's agents an admin chose, with exactly that principal's grants.
// Its tokens open /mcp and nothing else (middleware.MCPHopHeader).
type OAuthService struct {
	db *gorm.DB
}

const (
	// OAuthAccessPrefix and OAuthRefreshPrefix mark the two token kinds.
	OAuthAccessPrefix  = "moat-"
	OAuthRefreshPrefix = "mort-"

	OAuthCodeTTL   = 5 * time.Minute
	OAuthAccessTTL = time.Hour
	// OAuthRefreshTTL is how long a refresh token waits to be used. Each use
	// replaces it, so a client in use stays connected and a forgotten one
	// lapses.
	OAuthRefreshTTL = 90 * 24 * time.Hour
	// oauthTouchEvery bounds how often a connection's last use is written.
	oauthTouchEvery = time.Hour
)

var (
	ErrOAuthClient    = errors.New("unknown client")
	ErrOAuthRedirect  = errors.New("redirect URI is not one the client registered")
	ErrOAuthRedirects = errors.New("redirect URIs must be https, or http to this machine (localhost), with no fragment")
	ErrOAuthPKCE      = errors.New("a code challenge (PKCE, S256) is required")
	// ErrOAuthGrant is RFC 6749's invalid_grant: a code or refresh token that
	// is unknown, used, expired, revoked or for another client.
	ErrOAuthGrant  = errors.New("the code or refresh token is invalid, used or expired")
	ErrOAuthSecret = errors.New("client authentication failed")
	ErrOAuthAgent  = errors.New("only an owner or admin can connect a client as an agent")
)

// OAuthClientInput is a client registering itself (RFC 7591).
type OAuthClientInput struct {
	ClientName   string
	RedirectURIs []string
	// AuthMethod is "none" for a public client, which proves itself with PKCE
	// alone, or "client_secret_post" / "client_secret_basic" for one that
	// asks for a secret too.
	AuthMethod string
}

// Register records a client. The secret, for a client that asked for one, is
// returned once.
func (s *OAuthService) Register(ctx context.Context, in OAuthClientInput) (*db.OAuthClient, string, error) {
	if len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > 10 {
		return nil, "", ErrOAuthRedirects
	}
	for _, u := range in.RedirectURIs {
		if !validRedirect(u) {
			return nil, "", ErrOAuthRedirects
		}
	}
	uris, _ := json.Marshal(in.RedirectURIs)
	name := strings.TrimSpace(in.ClientName)
	if len(name) > 100 {
		name = name[:100]
	}
	id, err := randomHex(16)
	if err != nil {
		return nil, "", err
	}
	c := db.OAuthClient{ClientID: id, ClientName: name, RedirectURIs: string(uris)}
	c.ID = uuid.New()
	var secret string
	if in.AuthMethod == "client_secret_post" || in.AuthMethod == "client_secret_basic" {
		raw, err := randomHex(32)
		if err != nil {
			return nil, "", err
		}
		secret = "mcs-" + raw
		c.SecretHash = hashToken(secret)
	}
	if err := s.db.WithContext(ctx).Create(&c).Error; err != nil {
		return nil, "", err
	}
	return &c, secret, nil
}

// validRedirect is an https URI, or http to the client's own machine (a
// local client listening for the code), with no fragment.
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		h := u.Hostname()
		if h == "localhost" {
			return true
		}
		ip := net.ParseIP(h)
		return ip != nil && ip.IsLoopback()
	}
	return false
}

// OAuthAuthorize is a client asking a person for access, as the console's
// approval page shows it.
type OAuthAuthorize struct {
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	// ChallengeMethod must be S256.
	ChallengeMethod string
}

// Client is the client an authorization request names, once the request is
// one it may make: a registered redirect URI and a PKCE challenge.
func (s *OAuthService) Client(ctx context.Context, in OAuthAuthorize) (*db.OAuthClient, error) {
	var c db.OAuthClient
	if err := s.db.WithContext(ctx).First(&c, "client_id = ?", in.ClientID).Error; err != nil {
		return nil, ErrOAuthClient
	}
	var uris []string
	_ = json.Unmarshal([]byte(c.RedirectURIs), &uris)
	ok := false
	for _, u := range uris {
		if u == in.RedirectURI {
			ok = true
			break
		}
	}
	if !ok {
		return nil, ErrOAuthRedirect
	}
	if in.CodeChallenge == "" || in.ChallengeMethod != "S256" {
		return nil, ErrOAuthPKCE
	}
	return &c, nil
}

// Approve records a person's consent and issues the code the client trades
// for tokens. The connection acts in orgID as the person, or as agentID when
// set, which the caller has checked an owner or admin chose. Approving a
// client again replaces the connection it had for the same principal.
func (s *OAuthService) Approve(ctx context.Context, in OAuthAuthorize, orgID, approvedBy uuid.UUID, agentID *uuid.UUID) (string, error) {
	c, err := s.Client(ctx, in)
	if err != nil {
		return "", err
	}
	principal := approvedBy
	if agentID != nil {
		var n int64
		s.db.WithContext(ctx).Model(&db.OrganizationMember{}).
			Joins("JOIN users u ON u.id = organization_members.user_id").
			Where("organization_members.organization_id = ? AND organization_members.user_id = ? AND u.kind = ?", orgID, *agentID, db.UserAgent).
			Count(&n)
		if n == 0 {
			return "", ErrAgentNotFound
		}
		principal = *agentID
	}
	raw, err := randomHex(32)
	if err != nil {
		return "", err
	}
	code := "mcode-" + raw
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old []uuid.UUID
		tx.Model(&db.OAuthGrant{}).Where("principal_id = ? AND client_ref = ? AND organization_id = ? AND revoked_at IS NULL",
			principal, c.ID, orgID).Pluck("id", &old)
		if err := revokeGrants(tx, old); err != nil {
			return err
		}
		g := db.OAuthGrant{OrganizationID: orgID, ApprovedBy: approvedBy, PrincipalID: principal, ClientRef: c.ID, ClientName: c.ClientName}
		g.ID = uuid.New()
		if err := tx.Create(&g).Error; err != nil {
			return err
		}
		row := db.OAuthCode{GrantID: g.ID, CodeHash: hashToken(code), RedirectURI: in.RedirectURI,
			CodeChallenge: in.CodeChallenge, ExpiresAt: time.Now().Add(OAuthCodeTTL)}
		row.ID = uuid.New()
		return tx.Create(&row).Error
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// revokeGrants ends connections and every token issued under them.
func revokeGrants(tx *gorm.DB, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	if err := tx.Model(&db.OAuthGrant{}).Where("id IN ? AND revoked_at IS NULL", ids).Update("revoked_at", now).Error; err != nil {
		return err
	}
	return tx.Model(&db.OAuthToken{}).Where("grant_id IN ? AND revoked_at IS NULL", ids).Update("revoked_at", now).Error
}

// OAuthTokens is what the token endpoint hands a client.
type OAuthTokens struct {
	Access    string
	Refresh   string
	ExpiresIn int
}

// clientFor authenticates the client a token request comes from: by its
// secret when it has one; a public client is bound by PKCE or by its refresh
// token instead.
func (s *OAuthService) clientFor(tx *gorm.DB, clientID, secret string) (*db.OAuthClient, error) {
	var c db.OAuthClient
	if err := tx.First(&c, "client_id = ?", clientID).Error; err != nil {
		return nil, ErrOAuthSecret
	}
	if c.SecretHash != "" && subtle.ConstantTimeCompare([]byte(hashToken(secret)), []byte(c.SecretHash)) != 1 {
		return nil, ErrOAuthSecret
	}
	return &c, nil
}

// Exchange trades a code for tokens (authorization_code). A code is good once:
// presenting it again ends the connection it made, since one of the two
// presenters is not the client it was issued to.
func (s *OAuthService) Exchange(ctx context.Context, clientID, secret, code, redirectURI, verifier string) (*OAuthTokens, error) {
	var out *OAuthTokens
	var replayed bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c, err := s.clientFor(tx, clientID, secret)
		if err != nil {
			return err
		}
		var row db.OAuthCode
		if err := tx.Preload("Grant").First(&row, "code_hash = ?", hashToken(code)).Error; err != nil {
			return ErrOAuthGrant
		}
		if row.Grant.ClientRef != c.ID {
			return ErrOAuthGrant
		}
		if row.UsedAt != nil {
			replayed = true
			return revokeGrants(tx, []uuid.UUID{row.GrantID})
		}
		if time.Now().After(row.ExpiresAt) || row.Grant.RevokedAt != nil || row.RedirectURI != redirectURI || !pkceMatches(row.CodeChallenge, verifier) {
			return ErrOAuthGrant
		}
		res := tx.Model(&db.OAuthCode{}).Where("id = ? AND used_at IS NULL", row.ID).Update("used_at", time.Now())
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrOAuthGrant
		}
		out, err = mintOAuthTokens(tx, row.GrantID)
		return err
	})
	// A replay commits the revocation, then fails like any bad code.
	if err == nil && replayed {
		return nil, ErrOAuthGrant
	}
	return out, err
}

// Refresh trades a refresh token for a new pair (refresh_token). Each refresh
// token is good once; one presented again means it was copied, so the
// connection ends.
func (s *OAuthService) Refresh(ctx context.Context, clientID, secret, refresh string) (*OAuthTokens, error) {
	var out *OAuthTokens
	var replayed bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c, err := s.clientFor(tx, clientID, secret)
		if err != nil {
			return err
		}
		var tok db.OAuthToken
		if err := tx.Preload("Grant").First(&tok, "token_hash = ? AND kind = ?", hashToken(refresh), db.OAuthRefresh).Error; err != nil {
			return ErrOAuthGrant
		}
		if tok.Grant.ClientRef != c.ID {
			return ErrOAuthGrant
		}
		if tok.UsedAt != nil {
			replayed = true
			return revokeGrants(tx, []uuid.UUID{tok.GrantID})
		}
		if tok.RevokedAt != nil || tok.Grant.RevokedAt != nil || time.Now().After(tok.ExpiresAt) {
			return ErrOAuthGrant
		}
		res := tx.Model(&db.OAuthToken{}).Where("id = ? AND used_at IS NULL", tok.ID).Update("used_at", time.Now())
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrOAuthGrant
		}
		out, err = mintOAuthTokens(tx, tok.GrantID)
		return err
	})
	if err == nil && replayed {
		return nil, ErrOAuthGrant
	}
	return out, err
}

func pkceMatches(challenge, verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

func mintOAuthTokens(tx *gorm.DB, grantID uuid.UUID) (*OAuthTokens, error) {
	access, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	refresh, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	out := &OAuthTokens{Access: OAuthAccessPrefix + access, Refresh: OAuthRefreshPrefix + refresh, ExpiresIn: int(OAuthAccessTTL / time.Second)}
	now := time.Now()
	rows := []db.OAuthToken{
		{GrantID: grantID, Kind: db.OAuthAccess, TokenHash: hashToken(out.Access), ExpiresAt: now.Add(OAuthAccessTTL)},
		{GrantID: grantID, Kind: db.OAuthRefresh, TokenHash: hashToken(out.Refresh), ExpiresAt: now.Add(OAuthRefreshTTL)},
	}
	for i := range rows {
		rows[i].ID = uuid.New()
	}
	if err := tx.Create(&rows).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// Resolve is the connection an access token belongs to, while it works.
func (s *OAuthService) Resolve(ctx context.Context, plaintext string) (*db.OAuthGrant, bool) {
	var tok db.OAuthToken
	err := s.db.WithContext(ctx).Preload("Grant").
		Where("token_hash = ? AND kind = ? AND revoked_at IS NULL AND expires_at > ?", hashToken(plaintext), db.OAuthAccess, time.Now()).
		First(&tok).Error
	if err != nil || tok.Grant.RevokedAt != nil {
		return nil, false
	}
	g := tok.Grant
	if g.LastUsedAt == nil || time.Since(*g.LastUsedAt) > oauthTouchEvery {
		go func(id uuid.UUID) {
			_ = s.db.Model(&db.OAuthGrant{}).Where("id = ?", id).Update("last_used_at", time.Now()).Error
		}(g.ID)
	}
	return &g, true
}

// ResolveToken is the principal an access token acts as, for the auth
// middleware.
func (s *OAuthService) ResolveToken(ctx context.Context, plaintext string) (uuid.UUID, bool) {
	g, ok := s.Resolve(ctx, plaintext)
	if !ok {
		return uuid.Nil, false
	}
	return g.PrincipalID, true
}

// OAuthConnection is a connection as the console lists it.
type OAuthConnection struct {
	ID         uuid.UUID `json:"id"`
	ClientName string    `json:"client_name"`
	// AsAgent names the agent it acts as; empty when it acts as the person.
	AsAgent        string     `json:"as_agent,omitempty"`
	AgentID        *uuid.UUID `json:"agent_id,omitempty"`
	ApprovedBy     uuid.UUID  `json:"approved_by"`
	ApprovedByName string     `json:"approved_by_name"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
}

// Connections are an organisation's connections a person made, or that act
// as an agent: by approver, by principal, or all, newest first. Revoked ones
// stay listed, as the record of what could act and until when.
func (s *OAuthService) Connections(ctx context.Context, orgID uuid.UUID, approvedBy, principal *uuid.UUID) ([]OAuthConnection, error) {
	q := s.db.WithContext(ctx).Model(&db.OAuthGrant{}).
		Select("oauth_grants.*, a.username AS approver_name, p.username AS principal_name, p.kind AS principal_kind").
		Joins("JOIN users a ON a.id = oauth_grants.approved_by").
		Joins("JOIN users p ON p.id = oauth_grants.principal_id").
		Where("oauth_grants.organization_id = ?", orgID)
	if approvedBy != nil {
		q = q.Where("oauth_grants.approved_by = ?", *approvedBy)
	}
	if principal != nil {
		q = q.Where("oauth_grants.principal_id = ?", *principal)
	}
	var rows []struct {
		db.OAuthGrant
		ApproverName  string
		PrincipalName string
		PrincipalKind db.UserType
	}
	if err := q.Order("oauth_grants.created_at DESC").Limit(200).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]OAuthConnection, 0, len(rows))
	for _, r := range rows {
		c := OAuthConnection{ID: r.ID, ClientName: r.ClientName, ApprovedBy: r.ApprovedBy, ApprovedByName: r.ApproverName,
			CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt, RevokedAt: r.RevokedAt}
		if r.PrincipalKind == db.UserAgent {
			id := r.PrincipalID
			c.AsAgent, c.AgentID = r.PrincipalName, &id
		}
		out = append(out, c)
	}
	return out, nil
}

// SessionCounts is how many connected sessions each member of an
// organisation has: the assistants they connected there, and their signed-in
// CLIs.
func (s *OAuthService) SessionCounts(ctx context.Context, orgID uuid.UUID) (map[uuid.UUID]int, error) {
	q := s.db.WithContext(ctx)
	var rows []struct {
		UserID uuid.UUID
		N      int
	}
	if err := q.Model(&db.OAuthGrant{}).Select("approved_by AS user_id, count(*) AS n").
		Where("organization_id = ? AND revoked_at IS NULL", orgID).Group("approved_by").Scan(&rows).Error; err != nil {
		return nil, err
	}
	var clis []struct {
		UserID uuid.UUID
		N      int
	}
	if err := q.Model(&db.CLIToken{}).Select("cli_tokens.user_id, count(*) AS n").
		Joins("JOIN organization_members m ON m.user_id = cli_tokens.user_id AND m.organization_id = ?", orgID).
		Where("cli_tokens.revoked_at IS NULL AND COALESCE(cli_tokens.last_used_at, cli_tokens.created_at) > ?", time.Now().Add(-CLITokenIdle)).
		Group("cli_tokens.user_id").Scan(&clis).Error; err != nil {
		return nil, err
	}
	browsers, err := orgSessionCounts(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]int, len(rows)+len(clis))
	for _, r := range append(append(rows, clis...), browsers...) {
		out[r.UserID] += r.N
	}
	return out, nil
}

// ErrOAuthConnection is a connection that is not in the organisation, or not
// the caller's to end.
var ErrOAuthConnection = errors.New("connection not found")

// Revoke ends a connection: its person may end their own, an owner or admin
// any in the organisation.
func (s *OAuthService) Revoke(ctx context.Context, orgID, connectionID, by uuid.UUID, admin bool) error {
	var g db.OAuthGrant
	if err := s.db.WithContext(ctx).First(&g, "id = ? AND organization_id = ?", connectionID, orgID).Error; err != nil {
		return ErrOAuthConnection
	}
	if !admin && g.ApprovedBy != by {
		return ErrOAuthConnection
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return revokeGrants(tx, []uuid.UUID{g.ID})
	})
}
