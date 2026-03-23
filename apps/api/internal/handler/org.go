package handler

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/middleware"
	svc "github.com/meshploy/apps/api/internal/service"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// --- Input / Output types ---

type ListOrgsOutput struct {
	Body []db.Organization
}

type OrgPathInput struct {
	OrgID string `path:"orgId"`
}

type GetOrgOutput struct {
	Body *db.Organization
}

type CreateOrgInput struct {
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
		Slug string `json:"slug" minLength:"1" maxLength:"50" pattern:"^[a-z0-9-]+$"`
	}
}

type CreateOrgOutput struct {
	Body *db.Organization
}

type UpdateOrgInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
	}
}

type UpdateOrgOutput struct {
	Body *db.Organization
}

type ListMembersOutput struct {
	Body []db.OrganizationMember
}

type AddMemberInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Email string        `json:"email" format:"email"`
		Role  db.MemberRole `json:"role" enum:"admin,member"`
	}
}

type AddMemberOutput struct {
	Body *db.OrganizationMember
}

type UpdateMemberInput struct {
	OrgID  string `path:"orgId"`
	UserID string `path:"userId"`
	Body   struct {
		Role db.MemberRole `json:"role" enum:"admin,member"`
	}
}

type RemoveMemberInput struct {
	OrgID  string `path:"orgId"`
	UserID string `path:"userId"`
}

// --- Route registration ---

func (h *Handler) registerOrgRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-orgs",
		Method:      "GET",
		Path:        "/api/v1/orgs",
		Summary:     "List organizations for the authenticated user",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListOrgs)

	huma.Register(api, huma.Operation{
		OperationID: "create-org",
		Method:      "POST",
		Path:        "/api/v1/orgs",
		Summary:     "Create an organization",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.CreateOrg)

	huma.Register(api, huma.Operation{
		OperationID: "get-org",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}",
		Summary:     "Get an organization",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetOrg)

	huma.Register(api, huma.Operation{
		OperationID: "update-org",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}",
		Summary:     "Update an organization",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateOrg)

	huma.Register(api, huma.Operation{
		OperationID: "delete-org",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}",
		Summary:     "Delete an organization (owner only)",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteOrg)

	huma.Register(api, huma.Operation{
		OperationID: "list-org-members",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/members",
		Summary:     "List organization members",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListMembers)

	huma.Register(api, huma.Operation{
		OperationID: "add-org-member",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/members",
		Summary:     "Add a member to an organization",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.AddMember)

	huma.Register(api, huma.Operation{
		OperationID: "update-org-member",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/members/{userId}",
		Summary:     "Update a member's role",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateMember)

	huma.Register(api, huma.Operation{
		OperationID: "remove-org-member",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/members/{userId}",
		Summary:     "Remove a member from an organization",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.RemoveMember)
}

// --- Helpers ---

func requireUser(ctx context.Context) (uuid.UUID, error) {
	id, ok := middleware.UserFromContext(ctx)
	if !ok {
		return uuid.Nil, huma.Error401Unauthorized("authentication required")
	}
	return id, nil
}

func parseUUID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, huma.Error400BadRequest("invalid id: " + s)
	}
	return id, nil
}

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return huma.Error404NotFound("resource not found")
	}
	return err
}

// --- Handlers ---

func (h *Handler) ListOrgs(ctx context.Context, _ *struct{}) (*ListOrgsOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgs, err := h.svc.Orgs.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &ListOrgsOutput{Body: orgs}, nil
}

func (h *Handler) CreateOrg(ctx context.Context, input *CreateOrgInput) (*CreateOrgOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	org, err := h.svc.Orgs.Create(ctx, userID, svc.CreateOrgInput{
		Name: input.Body.Name,
		Slug: input.Body.Slug,
	})
	if err != nil {
		return nil, huma.Error409Conflict("slug already taken")
	}
	return &CreateOrgOutput{Body: org}, nil
}

func (h *Handler) GetOrg(ctx context.Context, input *OrgPathInput) (*GetOrgOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	org, err := h.svc.Orgs.Get(ctx, orgID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetOrgOutput{Body: org}, nil
}

func (h *Handler) UpdateOrg(ctx context.Context, input *UpdateOrgInput) (*UpdateOrgOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	org, err := h.svc.Orgs.Update(ctx, orgID, svc.UpdateOrgInput{Name: input.Body.Name})
	if err != nil {
		return nil, notFound(err)
	}
	return &UpdateOrgOutput{Body: org}, nil
}

func (h *Handler) DeleteOrg(ctx context.Context, input *OrgPathInput) (*struct{}, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	role, err := h.svc.Orgs.MemberRole(ctx, orgID, userID)
	if err != nil {
		return nil, notFound(err)
	}
	if role != db.RoleOwner {
		return nil, huma.Error403Forbidden("only the owner can delete an organization")
	}
	return nil, h.svc.Orgs.Delete(ctx, orgID)
}

func (h *Handler) ListMembers(ctx context.Context, input *OrgPathInput) (*ListMembersOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	members, err := h.svc.Orgs.ListMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return &ListMembersOutput{Body: members}, nil
}

func (h *Handler) AddMember(ctx context.Context, input *AddMemberInput) (*AddMemberOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	member, err := h.svc.Orgs.AddMember(ctx, orgID, svc.AddMemberInput{
		Email: input.Body.Email,
		Role:  input.Body.Role,
	})
	if err != nil {
		return nil, notFound(err)
	}
	return &AddMemberOutput{Body: member}, nil
}

func (h *Handler) UpdateMember(ctx context.Context, input *UpdateMemberInput) (*struct{}, error) {
	userID, err := requireUser(ctx)
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
	// Prevent demoting the owner
	targetRole, err := h.svc.Orgs.MemberRole(ctx, orgID, targetID)
	if err != nil {
		return nil, notFound(err)
	}
	if targetRole == db.RoleOwner {
		return nil, huma.Error403Forbidden("cannot change the owner's role")
	}
	// Only admins/owners can update roles
	callerRole, err := h.svc.Orgs.MemberRole(ctx, orgID, userID)
	if err != nil {
		return nil, huma.Error403Forbidden("not a member of this organization")
	}
	if callerRole == db.RoleMember {
		return nil, huma.Error403Forbidden("insufficient permissions")
	}
	return nil, h.svc.Orgs.UpdateMemberRole(ctx, orgID, targetID, input.Body.Role)
}

func (h *Handler) RemoveMember(ctx context.Context, input *RemoveMemberInput) (*struct{}, error) {
	userID, err := requireUser(ctx)
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
	targetRole, err := h.svc.Orgs.MemberRole(ctx, orgID, targetID)
	if err != nil {
		return nil, notFound(err)
	}
	if targetRole == db.RoleOwner {
		return nil, huma.Error403Forbidden("cannot remove the owner")
	}
	callerRole, err := h.svc.Orgs.MemberRole(ctx, orgID, userID)
	if err != nil {
		return nil, huma.Error403Forbidden("not a member of this organization")
	}
	if callerRole == db.RoleMember && targetID != userID {
		return nil, huma.Error403Forbidden("insufficient permissions")
	}
	return nil, h.svc.Orgs.RemoveMember(ctx, orgID, targetID)
}
