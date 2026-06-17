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

type MemberDTO struct {
	ID        string        `json:"id"`
	UserID    string        `json:"user_id"`
	Role      db.MemberRole `json:"role"`
	UserName  string        `json:"user_name"`
	UserEmail string        `json:"user_email"`
}

type ListMembersOutput struct {
	Body []MemberDTO
}

type AddMemberInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Email string        `json:"email" format:"email"`
		Role  db.MemberRole `json:"role" enum:"admin,member"`
	}
}

type AddMemberOutput struct {
	Body *MemberDTO
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

type CreateInvitationInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Email string        `json:"email" format:"email"`
		Role  db.MemberRole `json:"role" enum:"admin,member"`
	}
}

type InvitationDTO struct {
	ID        string        `json:"id"`
	OrgID     string        `json:"org_id"`
	Email     string        `json:"email"`
	Role      db.MemberRole `json:"role"`
	ExpiresAt string        `json:"expires_at"`
	Token     string        `json:"token,omitempty"`
}

type CreateInvitationOutput struct {
	Body *InvitationDTO
}

type ListInvitationsOutput struct {
	Body []InvitationDTO
}

type InvitationTokenInput struct {
	Token string `path:"token"`
}

type InvitationInfoOutput struct {
	Body struct {
		Email   string `json:"email"`
		OrgName string `json:"org_name"`
		Role    string `json:"role"`
	}
}

type AcceptInvitationInput struct {
	Token string `path:"token"`
	Body  struct {
		Username string `json:"username" minLength:"3" maxLength:"50"`
		Password string `json:"password" minLength:"8"`
	}
}

type AcceptInvitationOutput struct {
	Body struct {
		Message string `json:"message"`
	}
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

	huma.Register(api, huma.Operation{
		OperationID: "create-invitation",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/invitations",
		Summary:     "Create an invite link for a new member",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.CreateInvitation)

	huma.Register(api, huma.Operation{
		OperationID: "list-invitations",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/invitations",
		Summary:     "List pending invitations",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListInvitations)

	huma.Register(api, huma.Operation{
		OperationID: "get-invitation",
		Method:      "GET",
		Path:        "/api/v1/invitations/{token}",
		Summary:     "Get invitation info by token (public)",
		Tags:        []string{"Organizations"},
	}, h.GetInvitation)

	huma.Register(api, huma.Operation{
		OperationID: "accept-invitation",
		Method:      "POST",
		Path:        "/api/v1/invitations/{token}/accept",
		Summary:     "Accept an invitation and create an account (public)",
		Tags:        []string{"Organizations"},
	}, h.AcceptInvitation)
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
	_, orgID, _, err := h.checkOrgMemberAccess(ctx, input.OrgID, "")
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
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
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

func toMemberDTO(m db.OrganizationMember) MemberDTO {
	return MemberDTO{
		ID:        m.ID.String(),
		UserID:    m.UserID.String(),
		Role:      m.Role,
		UserName:  m.User.Username,
		UserEmail: m.User.Email,
	}
}

func (h *Handler) ListMembers(ctx context.Context, input *OrgPathInput) (*ListMembersOutput, error) {
	_, orgID, _, err := h.checkOrgMemberAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	members, err := h.svc.Orgs.ListMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	dtos := make([]MemberDTO, len(members))
	for i, m := range members {
		dtos[i] = toMemberDTO(m)
	}
	return &ListMembersOutput{Body: dtos}, nil
}

func (h *Handler) AddMember(ctx context.Context, input *AddMemberInput) (*AddMemberOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
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
	dto := toMemberDTO(*member)
	return &AddMemberOutput{Body: &dto}, nil
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

func toInvitationDTO(inv db.OrgInvitation) InvitationDTO {
	return InvitationDTO{
		ID:        inv.ID.String(),
		OrgID:     inv.OrgID.String(),
		Email:     inv.Email,
		Role:      inv.Role,
		ExpiresAt: inv.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		Token:     inv.Token,
	}
}

func (h *Handler) CreateInvitation(ctx context.Context, input *CreateInvitationInput) (*CreateInvitationOutput, error) {
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
		return nil, huma.Error403Forbidden("not a member of this organization")
	}
	if role == db.RoleMember {
		return nil, huma.Error403Forbidden("insufficient permissions")
	}
	inv, err := h.svc.Orgs.CreateInvitation(ctx, orgID, userID, input.Body.Email, input.Body.Role)
	if err != nil {
		return nil, err
	}
	dto := toInvitationDTO(*inv)
	return &CreateInvitationOutput{Body: &dto}, nil
}

func (h *Handler) ListInvitations(ctx context.Context, input *OrgPathInput) (*ListInvitationsOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	invs, err := h.svc.Orgs.ListInvitations(ctx, orgID)
	if err != nil {
		return nil, err
	}
	dtos := make([]InvitationDTO, len(invs))
	for i, inv := range invs {
		dtos[i] = toInvitationDTO(inv)
	}
	return &ListInvitationsOutput{Body: dtos}, nil
}

func (h *Handler) GetInvitation(ctx context.Context, input *InvitationTokenInput) (*InvitationInfoOutput, error) {
	inv, err := h.svc.Orgs.GetInvitationByToken(ctx, input.Token)
	if err != nil {
		return nil, huma.Error404NotFound("invitation not found or expired")
	}
	out := &InvitationInfoOutput{}
	out.Body.Email = inv.Email
	out.Body.OrgName = inv.OrgName
	out.Body.Role = string(inv.Role)
	return out, nil
}

func (h *Handler) AcceptInvitation(ctx context.Context, input *AcceptInvitationInput) (*AcceptInvitationOutput, error) {
	_, err := h.svc.Orgs.AcceptInvitation(ctx, input.Token, input.Body.Username, input.Body.Password)
	if err != nil {
		return nil, huma.Error400BadRequest("could not accept invitation")
	}
	out := &AcceptInvitationOutput{}
	out.Body.Message = "account created successfully"
	return out, nil
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
