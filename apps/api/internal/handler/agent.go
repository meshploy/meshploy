package handler

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/apps/api/internal/service"
	"github.com/meshploy/packages/db"
)

// --- DTOs ---

type AgentTokenDTO struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type AgentDTO struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Role      db.MemberRole   `json:"role"`
	CreatedAt time.Time       `json:"created_at"`
	Tokens    []AgentTokenDTO `json:"tokens"`
}

func tokenDTO(t db.AgentToken) AgentTokenDTO {
	return AgentTokenDTO{
		ID:          t.ID.String(),
		Name:        t.Name,
		TokenPrefix: t.TokenPrefix,
		LastUsedAt:  t.LastUsedAt,
		ExpiresAt:   t.ExpiresAt,
		RevokedAt:   t.RevokedAt,
		CreatedAt:   t.CreatedAt,
	}
}

func agentDTO(a service.AgentView) AgentDTO {
	toks := make([]AgentTokenDTO, len(a.Tokens))
	for i, t := range a.Tokens {
		toks[i] = tokenDTO(t)
	}
	return AgentDTO{
		ID:        a.ID.String(),
		Name:      a.Name,
		Role:      a.Role,
		CreatedAt: a.CreatedAt,
		Tokens:    toks,
	}
}

// --- Inputs / Outputs ---

type ListAgentsInput struct {
	OrgID string `path:"orgId"`
}

type ListAgentsOutput struct {
	Body []AgentDTO
}

type CreateAgentInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Name      string        `json:"name"      minLength:"1" maxLength:"100"`
		Role      db.MemberRole `json:"role"      enum:"admin,member"`
		TokenName string        `json:"token_name,omitempty"`
		ExpiresAt *time.Time    `json:"expires_at,omitempty"`
	}
}

// CreateAgentOutput returns the created agent and the plaintext token, which is
// shown exactly once and never retrievable again.
type CreateAgentOutput struct {
	Body struct {
		Agent AgentDTO `json:"agent"`
		Token string   `json:"token"`
	}
}

type AgentTokenPathInput struct {
	OrgID   string `path:"orgId"`
	AgentID string `path:"agentId"`
	Body    struct {
		Name      string     `json:"name,omitempty"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
}

type AddTokenOutput struct {
	Body struct {
		Token    string        `json:"token"`
		Metadata AgentTokenDTO `json:"metadata"`
	}
}

type RevokeTokenInput struct {
	OrgID   string `path:"orgId"`
	AgentID string `path:"agentId"`
	TokenID string `path:"tokenId"`
}

type DeleteAgentInput struct {
	OrgID   string `path:"orgId"`
	AgentID string `path:"agentId"`
}

// --- Route registration ---

func (h *Handler) registerAgentRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-agents",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/agents",
		Summary:     "List agent principals in an org",
		Tags:        []string{"Agents"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListAgents)

	huma.Register(api, huma.Operation{
		OperationID:   "create-agent",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/agents",
		Summary:       "Create an agent principal and mint its first token",
		Tags:          []string{"Agents"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 201,
	}, h.CreateAgent)

	huma.Register(api, huma.Operation{
		OperationID:   "create-agent-token",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/agents/{agentId}/tokens",
		Summary:       "Mint an additional token for an agent (rotation)",
		Tags:          []string{"Agents"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 201,
	}, h.CreateAgentToken)

	huma.Register(api, huma.Operation{
		OperationID: "revoke-agent-token",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/agents/{agentId}/tokens/{tokenId}",
		Summary:     "Revoke an agent token",
		Tags:        []string{"Agents"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.RevokeAgentToken)

	huma.Register(api, huma.Operation{
		OperationID: "delete-agent",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/agents/{agentId}",
		Summary:     "Delete an agent principal and all its tokens/grants",
		Tags:        []string{"Agents"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteAgent)
}

// --- Handlers ---

func (h *Handler) ListAgents(ctx context.Context, input *ListAgentsInput) (*ListAgentsOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	agents, err := h.svc.Agents.ListAgents(ctx, orgID)
	if err != nil {
		return nil, err
	}
	dtos := make([]AgentDTO, len(agents))
	for i, a := range agents {
		dtos[i] = agentDTO(a)
	}
	return &ListAgentsOutput{Body: dtos}, nil
}

func (h *Handler) CreateAgent(ctx context.Context, input *CreateAgentInput) (*CreateAgentOutput, error) {
	callerID, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	agent, token, err := h.svc.Agents.CreateAgent(
		ctx, orgID, input.Body.Name, input.Body.Role, input.Body.TokenName, input.Body.ExpiresAt, callerID,
	)
	if err != nil {
		return nil, mapAgentErr(err)
	}
	out := &CreateAgentOutput{}
	out.Body.Agent = agentDTO(*agent)
	out.Body.Token = token
	return out, nil
}

func (h *Handler) CreateAgentToken(ctx context.Context, input *AgentTokenPathInput) (*AddTokenOutput, error) {
	callerID, orgID, agentID, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.AgentID)
	if err != nil {
		return nil, err
	}
	token, meta, err := h.svc.Agents.AddToken(ctx, orgID, agentID, input.Body.Name, input.Body.ExpiresAt, callerID)
	if err != nil {
		return nil, mapAgentErr(err)
	}
	out := &AddTokenOutput{}
	out.Body.Token = token
	out.Body.Metadata = tokenDTO(*meta)
	return out, nil
}

func (h *Handler) RevokeAgentToken(ctx context.Context, input *RevokeTokenInput) (*struct{}, error) {
	_, orgID, agentID, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.AgentID)
	if err != nil {
		return nil, err
	}
	tokenID, err := parseUUID(input.TokenID)
	if err != nil {
		return nil, err
	}
	return nil, mapAgentErr(h.svc.Agents.RevokeToken(ctx, orgID, agentID, tokenID))
}

func (h *Handler) DeleteAgent(ctx context.Context, input *DeleteAgentInput) (*struct{}, error) {
	_, orgID, agentID, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.AgentID)
	if err != nil {
		return nil, err
	}
	return nil, mapAgentErr(h.svc.Agents.DeleteAgent(ctx, orgID, agentID))
}

// mapAgentErr translates service errors to HTTP problem responses.
func mapAgentErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, service.ErrAgentOwnerRole):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, service.ErrNameTaken):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, service.ErrAgentNotFound), errors.Is(err, service.ErrTokenNotFound):
		return huma.Error404NotFound(err.Error())
	default:
		return err
	}
}
