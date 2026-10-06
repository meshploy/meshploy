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
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ErrSingleOrganization refuses a second organization. Community runs one per
// server: the first registration creates it and everyone else joins it by
// invitation.
var ErrSingleOrganization = errors.New("this server already has its organization; Community runs one organization per server")

type OrgService struct {
	db *gorm.DB
	// notif reports who joined. Assigned after construction in service.New.
	notif *NotificationService
}

type CreateOrgInput struct {
	Name string
	Slug string
}

type UpdateOrgInput struct {
	Name string
}

type AddMemberInput struct {
	Email string
	Role  db.MemberRole
}

func (s *OrgService) ListForUser(ctx context.Context, userID uuid.UUID) ([]db.Organization, error) {
	orgs := make([]db.Organization, 0)
	err := s.db.WithContext(ctx).
		Joins("JOIN organization_members ON organization_members.organization_id = organizations.id").
		Where("organization_members.user_id = ?", userID).
		Find(&orgs).Error
	return orgs, err
}

// First returns the server's first organization, the one the first
// registration created: the organization whose owner owns the server, and
// whose admins may act for it. gorm.ErrRecordNotFound when there is none.
func (s *OrgService) First(ctx context.Context) (*db.Organization, error) {
	return firstOrganization(ctx, s.db)
}

func firstOrganization(ctx context.Context, conn *gorm.DB) (*db.Organization, error) {
	var org db.Organization
	if err := conn.WithContext(ctx).Order("created_at ASC, id ASC").First(&org).Error; err != nil {
		return nil, err
	}
	return &org, nil
}

func (s *OrgService) Get(ctx context.Context, orgID uuid.UUID) (*db.Organization, error) {
	var org db.Organization
	err := s.db.WithContext(ctx).First(&org, "id = ?", orgID).Error
	return &org, err
}

// Create makes an organization owned by userID, only on a server that has none.
//
// Community runs one organization per server, by product decision. The schema
// and the API are org-scoped throughout, so a paid edition can lift this
// without a migration; until one does, a second organization would only split
// the server between owners: its owner would hold nothing on the server
// (IsInstanceOwner is the first organization's owner) while looking like an
// owner everywhere else. A server with none, after its organization was
// deleted, may make one again.
func (s *OrgService) Create(ctx context.Context, userID uuid.UUID, in CreateOrgInput) (*db.Organization, error) {
	var org *db.Organization
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&db.Organization{}).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ErrSingleOrganization
		}
		org = &db.Organization{Name: in.Name, Slug: in.Slug}
		if err := tx.Create(org).Error; err != nil {
			return err
		}
		return tx.Create(&db.OrganizationMember{
			OrganizationID: org.ID,
			UserID:         userID,
			Role:           db.RoleOwner,
		}).Error
	})
	return org, err
}

func (s *OrgService) Update(ctx context.Context, orgID uuid.UUID, in UpdateOrgInput) (*db.Organization, error) {
	var org db.Organization
	err := s.db.WithContext(ctx).First(&org, "id = ?", orgID).Error
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Model(&org).Update("name", in.Name).Error
	return &org, err
}

func (s *OrgService) Delete(ctx context.Context, orgID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.Organization{}, "id = ?", orgID).Error
}

func (s *OrgService) ListMembers(ctx context.Context, orgID uuid.UUID) ([]db.OrganizationMember, error) {
	members := make([]db.OrganizationMember, 0)
	err := s.db.WithContext(ctx).Preload("User").Where("organization_id = ?", orgID).Find(&members).Error
	return members, err
}

func (s *OrgService) AddMember(ctx context.Context, orgID uuid.UUID, in AddMemberInput) (*db.OrganizationMember, error) {
	var user db.User
	if err := s.db.WithContext(ctx).Where("email = ?", in.Email).First(&user).Error; err != nil {
		return nil, err
	}
	member := &db.OrganizationMember{
		OrganizationID: orgID,
		UserID:         user.ID,
		Role:           in.Role,
	}
	if err := s.db.WithContext(ctx).Create(member).Error; err != nil {
		return nil, err
	}
	member.User = user
	return member, nil
}

func (s *OrgService) UpdateMemberRole(ctx context.Context, orgID, userID uuid.UUID, role db.MemberRole) error {
	return s.db.WithContext(ctx).
		Model(&db.OrganizationMember{}).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		Update("role", role).Error
}

func (s *OrgService) RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("organization_id = ? AND user_id = ?", orgID, userID).
			Delete(&db.OrganizationMember{}).Error; err != nil {
			return err
		}
		// Clean up all resource grants so re-invitation starts with a clean slate.
		return forgetMemberGrants(ctx, tx, orgID, userID)
	})
}

// StoreHeadscalePreAuthKey encrypts and persists a Headscale preauth key on the org record.
func (s *OrgService) StoreHeadscalePreAuthKey(ctx context.Context, orgID uuid.UUID, key string, expiry time.Time) error {
	return s.db.WithContext(ctx).Model(&db.Organization{}).
		Where("id = ?", orgID).
		Updates(map[string]any{
			"headscale_pre_auth_key":        db.EncryptedString(key),
			"headscale_pre_auth_key_expiry": expiry,
		}).Error
}

// ClearHeadscalePreAuthKey removes the stored Headscale preauth key from the org record.
func (s *OrgService) ClearHeadscalePreAuthKey(ctx context.Context, orgID uuid.UUID) error {
	return s.db.WithContext(ctx).Model(&db.Organization{}).
		Where("id = ?", orgID).
		Updates(map[string]any{
			"headscale_pre_auth_key":        db.EncryptedString(""),
			"headscale_pre_auth_key_expiry": nil,
		}).Error
}

// MemberRole returns the role of a user within an org, or an error if not a member.
func (s *OrgService) MemberRole(ctx context.Context, orgID, userID uuid.UUID) (db.MemberRole, error) {
	var m db.OrganizationMember
	err := s.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		First(&m).Error
	return m.Role, err
}

// ---------------------------------------------------------------------------
// Invitations
// ---------------------------------------------------------------------------

type InvitationWithOrg struct {
	*db.OrgInvitation
	OrgName string
}

// CreateInvitation generates a single-use invite token for an email address.
func (s *OrgService) CreateInvitation(ctx context.Context, orgID, inviterID uuid.UUID, email string, role db.MemberRole) (*db.OrgInvitation, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	inv := &db.OrgInvitation{
		OrgID:     orgID,
		Email:     email,
		Role:      role,
		Token:     hex.EncodeToString(b),
		InvitedBy: inviterID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	if err := s.db.WithContext(ctx).Create(inv).Error; err != nil {
		return nil, err
	}
	return inv, nil
}

// GetInvitationByToken returns a valid (not expired, not accepted) invitation with its org preloaded.
func (s *OrgService) GetInvitationByToken(ctx context.Context, token string) (*InvitationWithOrg, error) {
	var inv db.OrgInvitation
	err := s.db.WithContext(ctx).
		Preload("Organization").
		Where("token = ? AND accepted_at IS NULL AND expires_at > NOW()", token).
		First(&inv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("invitation not found or expired")
		}
		return nil, err
	}
	return &InvitationWithOrg{OrgInvitation: &inv, OrgName: inv.Organization.Name}, nil
}

// ErrInvitationAccountExists is an invitation accepted by creating an account
// for an address whose account can already sign in: that person signs in and
// joins.
var ErrInvitationAccountExists = errors.New("an account with this email already exists: sign in to join")

// ErrInvitationNotYours is a signed-in person joining through an invitation
// sent to another address.
var ErrInvitationNotYours = errors.New("this invitation was sent to another email address")

// InvitationAccountExists says whether the invited address has an account
// that can sign in, so the invitation page offers signing in rather than a
// new one. An account with no password cannot: accepting completes it.
func (s *OrgService) InvitationAccountExists(ctx context.Context, email string) bool {
	var n int64
	s.db.WithContext(ctx).Model(&db.User{}).Where("LOWER(email) = LOWER(?) AND email <> '' AND password <> ''", email).Count(&n)
	return n > 0
}

// AcceptInvitation creates the user account and adds them to the org in a single transaction.
//
// An address may already have an account with no password, made for someone
// who never signed in to the console. Accepting completes that account: it
// gets the username and password, becomes an ordinary person's, and joins.
// It keeps its id, so whatever was already the account's stays with it.
func (s *OrgService) AcceptInvitation(ctx context.Context, token, username, password string) (*db.User, error) {
	inv, err := s.GetInvitationByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if s.InvitationAccountExists(ctx, inv.Email) {
		return nil, ErrInvitationAccountExists
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	var user db.User
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing db.User
		if err := tx.Limit(1).Find(&existing, "LOWER(email) = LOWER(?) AND email <> ''", inv.Email).Error; err != nil {
			return err
		}
		switch {
		case existing.ID == uuid.Nil:
			user = db.User{
				Username: username,
				Email:    inv.Email,
				Password: string(hashed),
			}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}
		case existing.Password == "" && existing.Kind != db.UserAgent:
			if err := tx.Model(&existing).Updates(map[string]any{
				"username": username, "password": string(hashed), "kind": db.UserHuman,
			}).Error; err != nil {
				return err
			}
			user = existing
			user.Username, user.Kind = username, db.UserHuman
		default:
			return ErrInvitationAccountExists
		}
		if err := tx.Create(&db.OrganizationMember{
			OrganizationID: inv.OrgID,
			UserID:         user.ID,
			Role:           inv.Role,
		}).Error; err != nil {
			return err
		}
		now := time.Now()
		return tx.Model(inv.OrgInvitation).Update("accepted_at", &now).Error
	})
	if err != nil {
		return nil, err
	}
	// Someone can sign in to this org who could not a moment ago. Whether that
	// was expected is the reader's to judge, which is why it is reported.
	if s.notif != nil {
		s.notif.Dispatch(ctx, inv.OrgID, "member.joined", NotificationData{
			Detail: fmt.Sprintf("%s joined as %s", user.Username, inv.Role),
			Link:   "/users/" + user.ID.String(),
		})
	}
	return &user, nil
}

// JoinInvitation is a person who already has an account accepting an
// invitation sent to their address: they join with its role. One who is
// already a member keeps the role they have.
func (s *OrgService) JoinInvitation(ctx context.Context, token string, userID uuid.UUID) error {
	inv, err := s.GetInvitationByToken(ctx, token)
	if err != nil {
		return err
	}
	var user db.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return err
	}
	if user.Email == "" || !strings.EqualFold(user.Email, inv.Email) {
		return ErrInvitationNotYours
	}
	joined := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&db.OrganizationMember{}).Where("organization_id = ? AND user_id = ?", inv.OrgID, userID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			if err := tx.Create(&db.OrganizationMember{OrganizationID: inv.OrgID, UserID: userID, Role: inv.Role}).Error; err != nil {
				return err
			}
			joined = true
		}
		now := time.Now()
		return tx.Model(inv.OrgInvitation).Update("accepted_at", &now).Error
	})
	if err != nil {
		return err
	}
	if joined && s.notif != nil {
		s.notif.Dispatch(ctx, inv.OrgID, "member.joined", NotificationData{
			Detail: fmt.Sprintf("%s joined as %s", user.Username, inv.Role),
			Link:   "/users/" + user.ID.String(),
		})
	}
	return nil
}

// ListInvitations returns pending (not yet accepted, not expired) invitations for an org.
func (s *OrgService) ListInvitations(ctx context.Context, orgID uuid.UUID) ([]db.OrgInvitation, error) {
	var invs []db.OrgInvitation
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND accepted_at IS NULL AND expires_at > NOW()", orgID).
		Order("created_at DESC").
		Find(&invs).Error
	return invs, err
}
