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

// AgentService manages agent principals (machine users) and their bearer tokens.
// An agent is a users row with Kind == UserAgent that reuses the entire
// membership + permission model; it differs from a human only in authentication.
type AgentService struct {
	db *gorm.DB
}

// AgentView is an agent principal plus its token metadata (never plaintext).
type AgentView struct {
	ID        uuid.UUID       `json:"id"`
	Name      string          `json:"name"`
	Role      db.MemberRole   `json:"role"`
	CreatedAt time.Time       `json:"created_at"`
	Tokens    []db.AgentToken `json:"tokens"`
}

var (
	// ErrAgentOwnerRole — agents may never hold the org owner role.
	ErrAgentOwnerRole = errors.New("agents cannot be granted the owner role")
	ErrAgentNotFound  = errors.New("agent not found")
	ErrTokenNotFound  = errors.New("token not found")
	ErrNameTaken      = errors.New("a user or agent with that name already exists")
)

// tokenPrefixLen is how many leading characters of the plaintext token are
// retained for display (e.g. "magt-a1b2c3"). Enough to identify, too short to use.
const tokenPrefixLen = 12

// generateAgentToken returns a fresh plaintext token (format: magt-<64 hex>).
func generateAgentToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return "magt-" + hex.EncodeToString(raw), nil
}

// Agent tokens are stored as their SHA-256 hash (never plaintext); hashToken is
// the shared helper defined in auth.go.

// normalizeAgentRole rejects the owner role and defaults anything unrecognised
// to member (least privilege). Agents may be admin or member only.
func normalizeAgentRole(role db.MemberRole) (db.MemberRole, error) {
	switch role {
	case db.RoleOwner:
		return "", ErrAgentOwnerRole
	case db.RoleAdmin:
		return db.RoleAdmin, nil
	default:
		return db.RoleMember, nil
	}
}

// CreateAgent creates an agent principal in the org and mints its first token.
// The plaintext token is returned once and never persisted. name becomes the
// agent's username (globally unique). role must be admin or member.
func (s *AgentService) CreateAgent(ctx context.Context, orgID uuid.UUID, name string, role db.MemberRole, tokenName string, expiresAt *time.Time, createdBy uuid.UUID) (*AgentView, string, error) {
	role, err := normalizeAgentRole(role)
	if err != nil {
		return nil, "", err
	}
	if tokenName == "" {
		tokenName = "default"
	}

	plaintext, err := generateAgentToken()
	if err != nil {
		return nil, "", err
	}

	agent := db.User{
		Username: name,
		Email:    "", // agents have no email — partial unique index permits many ""
		Kind:     db.UserAgent,
	}
	tok := db.AgentToken{
		Name:        tokenName,
		TokenHash:   hashToken(plaintext),
		TokenPrefix: plaintext[:tokenPrefixLen],
		ExpiresAt:   expiresAt,
		CreatedBy:   createdBy,
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&agent).Error; err != nil {
			if isUniqueViolation(err) {
				return ErrNameTaken
			}
			return err
		}
		member := db.OrganizationMember{
			OrganizationID: orgID,
			UserID:         agent.ID,
			Role:           role,
		}
		if err := tx.Create(&member).Error; err != nil {
			return err
		}
		tok.AgentID = agent.ID
		if err := tx.Create(&tok).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	return &AgentView{
		ID:        agent.ID,
		Name:      agent.Username,
		Role:      role,
		CreatedAt: agent.CreatedAt,
		Tokens:    []db.AgentToken{tok},
	}, plaintext, nil
}

// ListAgents returns all agent principals in an org with their token metadata.
func (s *AgentService) ListAgents(ctx context.Context, orgID uuid.UUID) ([]AgentView, error) {
	var rows []struct {
		ID        uuid.UUID
		Username  string
		CreatedAt time.Time
		Role      db.MemberRole
	}
	err := s.db.WithContext(ctx).Raw(`
		SELECT u.id, u.username, u.created_at, om.role
		FROM users u
		JOIN organization_members om ON om.user_id = u.id
		WHERE om.organization_id = ? AND u.kind = ?
		ORDER BY u.created_at
	`, orgID, db.UserAgent).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]AgentView, 0, len(rows))
	for _, r := range rows {
		var tokens []db.AgentToken
		if err := s.db.WithContext(ctx).
			Where("agent_id = ?", r.ID).
			Order("created_at").
			Find(&tokens).Error; err != nil {
			return nil, err
		}
		out = append(out, AgentView{
			ID:        r.ID,
			Name:      r.Username,
			Role:      r.Role,
			CreatedAt: r.CreatedAt,
			Tokens:    tokens,
		})
	}
	return out, nil
}

// requireAgentInOrg verifies the id is an agent that belongs to the org, and
// returns its role. Guards every per-agent mutation against cross-org access
// and against acting on a human user by id.
func (s *AgentService) requireAgentInOrg(ctx context.Context, orgID, agentID uuid.UUID) error {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&db.OrganizationMember{}).
		Joins("JOIN users u ON u.id = organization_members.user_id").
		Where("organization_members.organization_id = ? AND organization_members.user_id = ? AND u.kind = ?",
			orgID, agentID, db.UserAgent).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrAgentNotFound
	}
	return nil
}

// AgentOrg returns the organization an agent principal belongs to. Agents are
// created with exactly one membership. Errors with ErrAgentNotFound if the id is
// not an agent (e.g. a human user id).
func (s *AgentService) AgentOrg(ctx context.Context, agentID uuid.UUID) (uuid.UUID, error) {
	var m db.OrganizationMember
	err := s.db.WithContext(ctx).
		Joins("JOIN users u ON u.id = organization_members.user_id").
		Where("organization_members.user_id = ? AND u.kind = ?", agentID, db.UserAgent).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return uuid.Nil, ErrAgentNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	return m.OrganizationID, nil
}

// AddToken mints an additional token for an existing agent (rotation). Returns
// the plaintext once.
func (s *AgentService) AddToken(ctx context.Context, orgID, agentID uuid.UUID, name string, expiresAt *time.Time, createdBy uuid.UUID) (string, *db.AgentToken, error) {
	if err := s.requireAgentInOrg(ctx, orgID, agentID); err != nil {
		return "", nil, err
	}
	if name == "" {
		name = "rotated"
	}
	plaintext, err := generateAgentToken()
	if err != nil {
		return "", nil, err
	}
	tok := db.AgentToken{
		AgentID:     agentID,
		Name:        name,
		TokenHash:   hashToken(plaintext),
		TokenPrefix: plaintext[:tokenPrefixLen],
		ExpiresAt:   expiresAt,
		CreatedBy:   createdBy,
	}
	if err := s.db.WithContext(ctx).Create(&tok).Error; err != nil {
		return "", nil, err
	}
	return plaintext, &tok, nil
}

// RevokeToken marks a token revoked. Idempotent — re-revoking is a no-op.
func (s *AgentService) RevokeToken(ctx context.Context, orgID, agentID, tokenID uuid.UUID) error {
	if err := s.requireAgentInOrg(ctx, orgID, agentID); err != nil {
		return err
	}
	now := time.Now()
	tx := s.db.WithContext(ctx).Model(&db.AgentToken{}).
		Where("id = ? AND agent_id = ? AND revoked_at IS NULL", tokenID, agentID).
		Update("revoked_at", now)
	if tx.Error != nil {
		return tx.Error
	}
	return nil
}

// DeleteAgent removes an agent principal entirely. The organization_members and
// resource_permissions FKs on user_id are RESTRICT (they guard human users from
// stray deletes), so those rows are removed explicitly first; agent_tokens then
// CASCADE when the users row is deleted.
func (s *AgentService) DeleteAgent(ctx context.Context, orgID, agentID uuid.UUID) error {
	if err := s.requireAgentInOrg(ctx, orgID, agentID); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", agentID).Delete(&db.ResourcePermission{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", agentID).Delete(&db.OrganizationMember{}).Error; err != nil {
			return err
		}
		// Guard: only ever delete an agent row, never a human, via this path.
		return tx.Where("id = ? AND kind = ?", agentID, db.UserAgent).Delete(&db.User{}).Error
	})
}

// ResolveToken authenticates a plaintext agent token. It returns the agent's
// principal id (users.id) when the token is valid (exists, not revoked, not
// expired). The bool is false for any invalid/unknown token. LastUsedAt is
// bumped best-effort, asynchronously.
func (s *AgentService) ResolveToken(ctx context.Context, plaintext string) (uuid.UUID, bool) {
	var tok db.AgentToken
	err := s.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL", hashToken(plaintext)).
		First(&tok).Error
	if err != nil {
		return uuid.Nil, false
	}
	if tok.ExpiresAt != nil && !tok.ExpiresAt.After(time.Now()) {
		return uuid.Nil, false
	}

	// Best-effort, non-blocking last-used timestamp. Detached from the request
	// context so a finished request doesn't cancel the write.
	go func(id uuid.UUID) {
		now := time.Now()
		_ = s.db.Model(&db.AgentToken{}).Where("id = ?", id).Update("last_used_at", now).Error
	}(tok.ID)

	return tok.AgentID, true
}

// isUniqueViolation reports whether err is a Postgres unique-constraint failure.
// The DB is opened without gorm's TranslateError, so match on the SQLSTATE /
// message rather than relying on gorm.ErrDuplicatedKey.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "UNIQUE")
}
