package handler

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
)

type MeshAccessInput struct {
	OrgID string `path:"orgId"`
}

type MeshAccessOutput struct {
	Body struct {
		// Enforced says the mesh obeys this policy; until it does, every
		// machine still reaches every other.
		Enforced bool `json:"enforced"`
		// AppliedAt is when Headscale was last given a policy; LastError why
		// the last attempt failed.
		AppliedAt *time.Time `json:"applied_at,omitempty"`
		LastError string     `json:"last_error,omitempty"`
		// CanEnforce says the caller may switch it: the server's owner, as
		// it covers every organisation's machines.
		CanEnforce bool                  `json:"can_enforce"`
		Machines   []service.MeshMachine `json:"machines"`
		// Unknown are machines on the mesh Meshploy has no record of, which
		// the policy gives nothing.
		Unknown []service.MeshUnknown `json:"unknown"`
		// Policy is the Headscale policy that would be set. It names every
		// organisation's machines, so only the server's owner is given it.
		Policy string `json:"policy,omitempty"`
	}
}

func (h *Handler) registerMeshAccessRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "set-mesh-enforced",
		Method:        "PUT",
		Path:          "/api/v1/orgs/{orgId}/mesh/enforced",
		Summary:       "Enforce the mesh's access policy, or let every machine reach every other",
		Tags:          []string{"Nodes"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.SetMeshEnforced)
	huma.Register(api, huma.Operation{
		OperationID: "get-mesh-access",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/mesh/access",
		Summary:     "What each machine on the mesh may reach, and why",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetMeshAccess)
}

func (h *Handler) GetMeshAccess(ctx context.Context, input *MeshAccessInput) (*MeshAccessOutput, error) {
	userID, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	policy, err := h.svc.MeshAccess.Policy(ctx)
	if err != nil {
		return nil, err
	}
	out := &MeshAccessOutput{}
	out.Body.Machines = []service.MeshMachine{}
	for _, m := range policy.Machines {
		if m.OrgID == orgID {
			out.Body.Machines = append(out.Body.Machines, m)
		}
	}
	out.Body.Unknown = policy.Unknown
	if out.Body.Unknown == nil {
		out.Body.Unknown = []service.MeshUnknown{}
	}
	if st, err := h.svc.MeshAccess.State(ctx); err == nil {
		out.Body.Enforced, out.Body.AppliedAt, out.Body.LastError = st.Enforced, st.AppliedAt, st.LastError
	}
	if owner, _ := h.svc.System.IsInstanceOwner(ctx, userID); owner {
		out.Body.CanEnforce = true
		if b, err := policy.Headscale(); err == nil {
			out.Body.Policy = string(b)
		}
	}
	return out, nil
}

type SetMeshEnforcedInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Enforced bool `json:"enforced"`
	}
}

// SetMeshEnforced switches the mesh between obeying its policy and letting
// every machine reach every other. The server's owner only: the policy covers
// every organisation's machines.
func (h *Handler) SetMeshEnforced(ctx context.Context, input *SetMeshEnforcedInput) (*struct{}, error) {
	userID, _, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	if owner, err := h.svc.System.IsInstanceOwner(ctx, userID); err != nil {
		return nil, err
	} else if !owner {
		return nil, huma.Error403Forbidden("only the server's owner can enforce the mesh's access policy")
	}
	if h.svc.Headscale == nil {
		return nil, huma.Error409Conflict("this server has no mesh to enforce a policy on")
	}
	return nil, h.svc.MeshAccess.SetEnforced(ctx, input.Body.Enforced, userID)
}

type MeshReachInput struct {
	OrgID        string `path:"orgId"`
	ResourceType string `query:"resource_type" enum:"service,stack"`
	ResourceID   string `query:"resource_id"`
}

type MeshReachOutput struct {
	Body []service.MemberReach
}

type SetMeshReachInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		UserID       string `json:"user_id"`
		ResourceType string `json:"resource_type" enum:"service,stack,project"`
		ResourceID   string `json:"resource_id"`
		Reach        bool   `json:"reach"`
	}
}

func (h *Handler) registerMeshReachRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-mesh-reach",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/mesh/reach",
		Summary:     "The members granted a service or stack, and whether their machines reach it on the mesh",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetMeshReach)
	huma.Register(api, huma.Operation{
		OperationID:   "set-mesh-reach",
		Method:        "PUT",
		Path:          "/api/v1/orgs/{orgId}/mesh/reach",
		Summary:       "Whether a member's machines reach a service, stack or project they are granted",
		Tags:          []string{"Nodes"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.SetMeshReach)
}

func (h *Handler) GetMeshReach(ctx context.Context, input *MeshReachInput) (*MeshReachOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(input.ResourceID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid resource id")
	}
	rows, err := h.svc.MeshAccess.ReachOn(ctx, orgID, db.ResourceType(input.ResourceType), id)
	if errors.Is(err, service.ErrReachResource) {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err != nil {
		return nil, err
	}
	return &MeshReachOutput{Body: rows}, nil
}

func (h *Handler) SetMeshReach(ctx context.Context, input *SetMeshReachInput) (*struct{}, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	userID, err := uuid.Parse(input.Body.UserID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid user id")
	}
	resourceID, err := uuid.Parse(input.Body.ResourceID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid resource id")
	}
	err = h.svc.MeshAccess.SetReach(ctx, orgID, userID, db.ResourceType(input.Body.ResourceType), resourceID, input.Body.Reach)
	if errors.Is(err, service.ErrReachResource) {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return nil, err
}
