package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
)

// --- Input / Output types ---

type MemberPermissionsPathInput struct {
	OrgID  string `path:"orgId"`
	UserID string `path:"userId"`
}

type ListPermissionsOutput struct {
	Body []db.ResourcePermission
}

type PermissionBody struct {
	ResourceType db.ResourceType   `json:"resource_type" enum:"project,service,stack,job,route"`
	ResourceID   string            `json:"resource_id"`
	Action       db.ResourceAction `json:"action" enum:"view,deploy,create,update,delete"`
}

type GrantPermissionInput struct {
	OrgID  string `path:"orgId"`
	UserID string `path:"userId"`
	Body   PermissionBody
}

type RevokePermissionInput struct {
	OrgID  string `path:"orgId"`
	UserID string `path:"userId"`
	Body   PermissionBody
}

type ResourcePermissionsPathInput struct {
	OrgID      string `path:"orgId"`
	ResourceID string `path:"resourceId"`
}

type ListResourcePermissionsOutput struct {
	Body []PermissionsWithUserDTO
}

type PermissionsWithUserDTO struct {
	UserID    string            `json:"user_id"`
	UserName  string            `json:"user_name"`
	UserEmail string            `json:"user_email"`
	Action    db.ResourceAction `json:"action"`
}

// --- Route registration ---

func (h *Handler) registerPermissionRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-member-permissions",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/members/{userId}/permissions",
		Summary:     "List all permission grants for a member",
		Tags:        []string{"Permissions"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListMemberPermissions)

	huma.Register(api, huma.Operation{
		OperationID: "grant-permission",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/members/{userId}/permissions",
		Summary:     "Grant a permission to a member",
		Tags:        []string{"Permissions"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GrantPermission)

	huma.Register(api, huma.Operation{
		OperationID: "revoke-permission",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/members/{userId}/permissions",
		Summary:     "Revoke a permission from a member",
		Tags:        []string{"Permissions"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.RevokePermission)

	huma.Register(api, huma.Operation{
		OperationID: "list-project-permissions",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{resourceId}/permissions",
		Summary:     "List permissions on a project",
		Tags:        []string{"Permissions"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *ResourcePermissionsPathInput) (*ListResourcePermissionsOutput, error) {
		return h.listResourcePermissions(ctx, input, db.ResourceProject)
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-service-permissions",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/services/{resourceId}/permissions",
		Summary:     "List permissions on a service",
		Tags:        []string{"Permissions"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *ResourcePermissionsPathInput) (*ListResourcePermissionsOutput, error) {
		return h.listResourcePermissions(ctx, input, db.ResourceService)
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-stack-permissions",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/stacks/{resourceId}/permissions",
		Summary:     "List permissions on a stack",
		Tags:        []string{"Permissions"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *ResourcePermissionsPathInput) (*ListResourcePermissionsOutput, error) {
		return h.listResourcePermissions(ctx, input, db.ResourceStack)
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-job-permissions",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/jobs/{resourceId}/permissions",
		Summary:     "List permissions on a job",
		Tags:        []string{"Permissions"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *ResourcePermissionsPathInput) (*ListResourcePermissionsOutput, error) {
		return h.listResourcePermissions(ctx, input, db.ResourceJob)
	})
}

// --- Handlers ---

func (h *Handler) ListMemberPermissions(ctx context.Context, input *MemberPermissionsPathInput) (*ListPermissionsOutput, error) {
	callerID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	targetID, err := parseUUID(input.UserID)
	if err != nil {
		return nil, err
	}
	// Admins/owners can view any member's permissions; members can only view their own.
	if callerID != targetID {
		if err := h.enforceAdminRole(ctx, orgID, callerID); err != nil {
			return nil, err
		}
	}
	perms, err := h.svc.Permissions.ListForUser(ctx, orgID, targetID)
	if err != nil {
		return nil, err
	}
	return &ListPermissionsOutput{Body: perms}, nil
}

func (h *Handler) GrantPermission(ctx context.Context, input *GrantPermissionInput) (*struct{}, error) {
	callerID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	if err := h.enforceAdminRole(ctx, orgID, callerID); err != nil {
		return nil, err
	}
	targetID, err := parseUUID(input.UserID)
	if err != nil {
		return nil, err
	}
	resourceID, err := parseUUID(input.Body.ResourceID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.Permissions.Grant(ctx, orgID, targetID, resourceID, input.Body.ResourceType, input.Body.Action)
}

func (h *Handler) RevokePermission(ctx context.Context, input *RevokePermissionInput) (*struct{}, error) {
	callerID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	if err := h.enforceAdminRole(ctx, orgID, callerID); err != nil {
		return nil, err
	}
	targetID, err := parseUUID(input.UserID)
	if err != nil {
		return nil, err
	}
	resourceID, err := parseUUID(input.Body.ResourceID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.Permissions.Revoke(ctx, orgID, targetID, resourceID, input.Body.ResourceType, input.Body.Action)
}

func (h *Handler) listResourcePermissions(ctx context.Context, input *ResourcePermissionsPathInput, rt db.ResourceType) (*ListResourcePermissionsOutput, error) {
	callerID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	if err := h.enforceAdminRole(ctx, orgID, callerID); err != nil {
		return nil, err
	}
	resourceID, err := parseUUID(input.ResourceID)
	if err != nil {
		return nil, err
	}
	rows, err := h.svc.Permissions.ListForResource(ctx, orgID, rt, resourceID)
	if err != nil {
		return nil, err
	}
	dtos := make([]PermissionsWithUserDTO, len(rows))
	for i, r := range rows {
		dtos[i] = PermissionsWithUserDTO{
			UserID:    r.UserID.String(),
			UserName:  r.UserName,
			UserEmail: r.UserEmail,
			Action:    r.Action,
		}
	}
	return &ListResourcePermissionsOutput{Body: dtos}, nil
}

// enforceAdminRole returns 403 unless the caller is owner or admin of the org.
func (h *Handler) enforceAdminRole(ctx context.Context, orgID, callerID uuid.UUID) error {
	role, err := h.svc.Orgs.MemberRole(ctx, orgID, callerID)
	if err != nil {
		return huma.Error403Forbidden("not a member of this organization")
	}
	if role == db.RoleMember {
		return huma.Error403Forbidden("insufficient permissions")
	}
	return nil
}

