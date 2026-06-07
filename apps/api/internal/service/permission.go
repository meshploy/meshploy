package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type PermissionService struct {
	db *gorm.DB
}

type PermissionWithUser struct {
	UserID    uuid.UUID         `json:"user_id"`
	UserName  string            `json:"user_name"`
	UserEmail string            `json:"user_email"`
	Action    db.ResourceAction `json:"action"`
}

// PermissionWithContext enriches a grant with the resource's display name and
// parent project ID (for service/stack/job grants resolved via JOIN).
type PermissionWithContext struct {
	ID              uuid.UUID         `json:"id"`
	ResourceType    db.ResourceType   `json:"resource_type"`
	ResourceID      uuid.UUID         `json:"resource_id"`
	Action          db.ResourceAction `json:"action"`
	ResourceName    string            `json:"resource_name,omitempty"`
	ParentProjectID *uuid.UUID        `json:"parent_project_id,omitempty"`
}

// Grant creates a permission grant. Silently succeeds if the grant already exists.
func (s *PermissionService) Grant(ctx context.Context, orgID, userID, resourceID uuid.UUID, resourceType db.ResourceType, action db.ResourceAction) error {
	perm := db.ResourcePermission{
		OrganizationID: orgID,
		UserID:         userID,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Action:         action,
	}
	return s.db.WithContext(ctx).
		Where(perm).
		FirstOrCreate(&perm).Error
}

// Revoke removes a permission grant.
func (s *PermissionService) Revoke(ctx context.Context, orgID, userID, resourceID uuid.UUID, resourceType db.ResourceType, action db.ResourceAction) error {
	return s.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ? AND resource_type = ? AND resource_id = ? AND action = ?",
			orgID, userID, resourceType, resourceID, action).
		Delete(&db.ResourcePermission{}).Error
}

// ListForUser returns all permission grants for a user, enriched with resource
// names and parent project IDs resolved via JOIN for service/stack/job grants.
func (s *PermissionService) ListForUser(ctx context.Context, orgID, userID uuid.UUID) ([]PermissionWithContext, error) {
	var rows []struct {
		ID              uuid.UUID
		ResourceType    db.ResourceType
		ResourceID      uuid.UUID
		Action          db.ResourceAction
		ResourceName    *string
		ParentProjectID *uuid.UUID
	}
	err := s.db.WithContext(ctx).Raw(`
		SELECT rp.id, rp.resource_type, rp.resource_id, rp.action,
		       COALESCE(s.name, st.name, j.name)                         AS resource_name,
		       COALESCE(s.project_id, st.project_id, j.project_id)       AS parent_project_id
		FROM resource_permissions rp
		LEFT JOIN services s  ON rp.resource_type = 'service' AND s.id  = rp.resource_id
		LEFT JOIN stacks   st ON rp.resource_type = 'stack'   AND st.id = rp.resource_id
		LEFT JOIN jobs     j  ON rp.resource_type = 'job'     AND j.id  = rp.resource_id
		WHERE rp.organization_id = ? AND rp.user_id = ?
		ORDER BY rp.resource_type, rp.action
	`, orgID, userID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]PermissionWithContext, len(rows))
	for i, r := range rows {
		out[i] = PermissionWithContext{
			ID:              r.ID,
			ResourceType:    r.ResourceType,
			ResourceID:      r.ResourceID,
			Action:          r.Action,
			ParentProjectID: r.ParentProjectID,
		}
		if r.ResourceName != nil {
			out[i].ResourceName = *r.ResourceName
		}
	}
	return out, nil
}

// ListForResource returns all users with any permission grant on a specific resource.
func (s *PermissionService) ListForResource(ctx context.Context, orgID uuid.UUID, resourceType db.ResourceType, resourceID uuid.UUID) ([]PermissionWithUser, error) {
	var rows []struct {
		UserID    uuid.UUID
		Username  string
		Email     string
		Action    db.ResourceAction
	}
	err := s.db.WithContext(ctx).Raw(`
		SELECT rp.user_id, u.username, u.email, rp.action
		FROM resource_permissions rp
		JOIN users u ON u.id = rp.user_id
		WHERE rp.organization_id = ? AND rp.resource_type = ? AND rp.resource_id = ?
		ORDER BY u.username, rp.action
	`, orgID, resourceType, resourceID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]PermissionWithUser, len(rows))
	for i, r := range rows {
		result[i] = PermissionWithUser{
			UserID:    r.UserID,
			UserName:  r.Username,
			UserEmail: r.Email,
			Action:    r.Action,
		}
	}
	return result, nil
}

// CheckAccess returns nil if the caller can perform action on the resource.
// owner/admin bypass all checks. For members, checks direct resource grant first,
// then falls back to a parent project grant if parentProjectID is provided.
func (s *PermissionService) CheckAccess(ctx context.Context, orgID, userID, resourceID uuid.UUID, resourceType db.ResourceType, action db.ResourceAction, parentProjectID *uuid.UUID) error {
	var member db.OrganizationMember
	if err := s.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errForbidden
		}
		return err
	}
	if member.Role == db.RoleOwner || member.Role == db.RoleAdmin {
		return nil
	}

	// Direct resource grant
	if ok, err := s.hasGrant(ctx, orgID, userID, resourceType, resourceID, action); err != nil {
		return err
	} else if ok {
		return nil
	}

	// Project-level fallback grant
	if parentProjectID != nil {
		if ok, err := s.hasGrant(ctx, orgID, userID, db.ResourceProject, *parentProjectID, action); err != nil {
			return err
		} else if ok {
			return nil
		}
	}

	return errForbidden
}

var errForbidden = errors.New("access denied")

// IsAdminOrOwner returns true if the user is owner or admin of the org.
func (s *PermissionService) IsAdminOrOwner(ctx context.Context, orgID, userID uuid.UUID) (bool, error) {
	var m db.OrganizationMember
	if err := s.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return m.Role == db.RoleOwner || m.Role == db.RoleAdmin, nil
}

// VisibleProjectIDs returns the set of project IDs a member has any permission on.
// Returns (nil, true, nil) for admins/owners — caller should skip filtering.
func (s *PermissionService) VisibleProjectIDs(ctx context.Context, orgID, userID uuid.UUID) (map[uuid.UUID]bool, bool, error) {
	admin, err := s.IsAdminOrOwner(ctx, orgID, userID)
	if err != nil {
		return nil, false, err
	}
	if admin {
		return nil, true, nil
	}
	var perms []db.ResourcePermission
	if err := s.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ? AND resource_type = ?",
			orgID, userID, db.ResourceProject).
		Distinct("resource_id").
		Find(&perms).Error; err != nil {
		return nil, false, err
	}
	ids := make(map[uuid.UUID]bool, len(perms))
	for _, p := range perms {
		ids[p.ResourceID] = true
	}
	return ids, false, nil
}

func (s *PermissionService) hasGrant(ctx context.Context, orgID, userID uuid.UUID, resourceType db.ResourceType, resourceID uuid.UUID, action db.ResourceAction) (bool, error) {
	var perm db.ResourcePermission
	err := s.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ? AND resource_type = ? AND resource_id = ? AND action = ?",
			orgID, userID, resourceType, resourceID, action).
		First(&perm).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}
