package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type OrgService struct {
	db *gorm.DB
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
	var orgs []db.Organization
	err := s.db.WithContext(ctx).
		Joins("JOIN organization_members ON organization_members.organization_id = organizations.id").
		Where("organization_members.user_id = ? AND organization_members.deleted_at IS NULL", userID).
		Find(&orgs).Error
	return orgs, err
}

func (s *OrgService) Get(ctx context.Context, orgID uuid.UUID) (*db.Organization, error) {
	var org db.Organization
	err := s.db.WithContext(ctx).First(&org, "id = ?", orgID).Error
	return &org, err
}

func (s *OrgService) Create(ctx context.Context, userID uuid.UUID, in CreateOrgInput) (*db.Organization, error) {
	var org *db.Organization
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	var members []db.OrganizationMember
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
	return member, s.db.WithContext(ctx).Create(member).Error
}

func (s *OrgService) UpdateMemberRole(ctx context.Context, orgID, userID uuid.UUID, role db.MemberRole) error {
	return s.db.WithContext(ctx).
		Model(&db.OrganizationMember{}).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		Update("role", role).Error
}

func (s *OrgService) RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		Delete(&db.OrganizationMember{}).Error
}

// MemberRole returns the role of a user within an org, or an error if not a member.
func (s *OrgService) MemberRole(ctx context.Context, orgID, userID uuid.UUID) (db.MemberRole, error) {
	var m db.OrganizationMember
	err := s.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		First(&m).Error
	return m.Role, err
}
