package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/service"
	"github.com/meshploy/packages/db"
)

type StackPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	StackID   string `path:"stackId"`
}

type StackProjectPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
}

type ListStacksOutput struct {
	Body []db.Stack
}

type GetStackOutput struct {
	Body *db.Stack
}

type CreateStackBody struct {
	Name      string            `json:"name"`
	Spec      string            `json:"spec"`
	Variables map[string]string `json:"variables,omitempty"`
	// Git source
	GitMode          string  `json:"git_mode,omitempty"`           // "" | "file" | "repo"
	GitRepo          string  `json:"git_repo,omitempty"`
	GitBranch        string  `json:"git_branch,omitempty"`
	GitPath          string  `json:"git_path,omitempty"`           // default: docker-compose.yml
	GitIntegrationID *string `json:"git_integration_id,omitempty"` // nil = public
}

type CreateStackInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      CreateStackBody
}

type UpdateStackBody struct {
	Name      string            `json:"name,omitempty"`
	Spec      string            `json:"spec"`
	Variables map[string]string `json:"variables,omitempty"`
	// Git source
	GitMode          string  `json:"git_mode,omitempty"`
	GitRepo          string  `json:"git_repo,omitempty"`
	GitBranch        string  `json:"git_branch,omitempty"`
	GitPath          string  `json:"git_path,omitempty"`
	GitIntegrationID *string `json:"git_integration_id,omitempty"` // "" = clear, UUID = set
}

type SyncStackInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	StackID   string `path:"stackId"`
}

type SyncResultOutput struct {
	Body *syncResultBody
}

type syncResultBody struct {
	Stack         *db.Stack `json:"stack"`
	Created       []string  `json:"created"`
	Updated       []string  `json:"updated"`
	Deleted       []string  `json:"deleted"`
	Errors        []string  `json:"errors"`
	SuggestedMode string    `json:"suggested_mode,omitempty"`
	Warning       string    `json:"warning,omitempty"`
}

type UpdateStackInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	StackID   string `path:"stackId"`
	Body      UpdateStackBody
}

type ListStackServicesOutput struct {
	Body []db.Service
}

type ApplyStackBody struct {
	EnvOverrides map[string]string `json:"env_overrides,omitempty"`
}

type ApplyStackInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	StackID   string `path:"stackId"`
	Body      ApplyStackBody
}

type ApplyResultOutput struct {
	Body *applyResultBody
}

type applyResultBody struct {
	Stack   *db.Stack `json:"stack"`
	Created []string  `json:"created"`
	Updated []string  `json:"updated"`
	Deleted []string  `json:"deleted"`
	Errors  []string  `json:"errors"`
}

func (h *Handler) registerStackRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-stacks",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/stacks",
		Summary:     "List stacks for a project",
		Tags:        []string{"Stacks"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListStacks)

	huma.Register(api, huma.Operation{
		OperationID:   "create-stack",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/stacks",
		Summary:       "Create a new stack",
		Tags:          []string{"Stacks"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 201,
	}, h.CreateStack)

	huma.Register(api, huma.Operation{
		OperationID: "get-stack",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/stacks/{stackId}",
		Summary:     "Get a stack",
		Tags:        []string{"Stacks"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetStack)

	huma.Register(api, huma.Operation{
		OperationID: "update-stack",
		Method:      "PUT",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/stacks/{stackId}",
		Summary:     "Update a stack's spec and variables",
		Tags:        []string{"Stacks"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateStack)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-stack",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/stacks/{stackId}",
		Summary:       "Delete a stack",
		Tags:          []string{"Stacks"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.DeleteStack)

	huma.Register(api, huma.Operation{
		OperationID: "list-stack-services",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/stacks/{stackId}/services",
		Summary:     "List services belonging to a stack",
		Tags:        []string{"Stacks"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListStackServices)

	huma.Register(api, huma.Operation{
		OperationID:   "apply-stack",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/stacks/{stackId}/apply",
		Summary:       "Apply the stack spec — reconcile services",
		Tags:          []string{"Stacks"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 200,
	}, h.ApplyStack)

	huma.Register(api, huma.Operation{
		OperationID:   "sync-stack",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/stacks/{stackId}/sync",
		Summary:       "Fetch spec from git source and re-apply",
		Tags:          []string{"Stacks"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 200,
	}, h.SyncStack)
}

func (h *Handler) ListStacks(ctx context.Context, input *StackProjectPathInput) (*ListStacksOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	stacks, err := h.svc.Stacks.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &ListStacksOutput{Body: stacks}, nil
}

func (h *Handler) CreateStack(ctx context.Context, input *CreateStackInput) (*GetStackOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	var gitIntegrationID *uuid.UUID
	if input.Body.GitIntegrationID != nil && *input.Body.GitIntegrationID != "" {
		id, err := parseUUID(*input.Body.GitIntegrationID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid git_integration_id")
		}
		gitIntegrationID = &id
	}
	stack, err := h.svc.Stacks.Create(ctx, projectID, service.CreateStackInput{
		Name:             input.Body.Name,
		Spec:             input.Body.Spec,
		Variables:        input.Body.Variables,
		GitMode:          db.StackGitMode(input.Body.GitMode),
		GitRepo:          input.Body.GitRepo,
		GitBranch:        input.Body.GitBranch,
		GitPath:          input.Body.GitPath,
		GitIntegrationID: gitIntegrationID,
	})
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &GetStackOutput{Body: stack}, nil
}

func (h *Handler) GetStack(ctx context.Context, input *StackPathInput) (*GetStackOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	stackID, err := parseUUID(input.StackID)
	if err != nil {
		return nil, err
	}
	stack, err := h.svc.Stacks.Get(ctx, stackID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetStackOutput{Body: stack}, nil
}

func (h *Handler) UpdateStack(ctx context.Context, input *UpdateStackInput) (*GetStackOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	stackID, err := parseUUID(input.StackID)
	if err != nil {
		return nil, err
	}
	in := service.UpdateStackInput{
		Name:      input.Body.Name,
		Spec:      input.Body.Spec,
		Variables: input.Body.Variables,
		GitMode:   db.StackGitMode(input.Body.GitMode),
		GitRepo:   input.Body.GitRepo,
		GitBranch: input.Body.GitBranch,
		GitPath:   input.Body.GitPath,
	}
	if input.Body.GitIntegrationID != nil {
		in.UpdateGitIntegration = true
		if *input.Body.GitIntegrationID != "" {
			id, err := parseUUID(*input.Body.GitIntegrationID)
			if err != nil {
				return nil, huma.Error400BadRequest("invalid git_integration_id")
			}
			in.GitIntegrationID = &id
		}
	}
	stack, err := h.svc.Stacks.Update(ctx, stackID, in)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &GetStackOutput{Body: stack}, nil
}

func (h *Handler) SyncStack(ctx context.Context, input *SyncStackInput) (*SyncResultOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	stackID, err := parseUUID(input.StackID)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.Stacks.Sync(ctx, stackID, userID)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &SyncResultOutput{Body: &syncResultBody{
		Stack:         result.Stack,
		Created:       result.Created,
		Updated:       result.Updated,
		Deleted:       result.Deleted,
		Errors:        result.Errors,
		SuggestedMode: string(result.SuggestedMode),
		Warning:       result.Warning,
	}}, nil
}

func (h *Handler) DeleteStack(ctx context.Context, input *StackPathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	stackID, err := parseUUID(input.StackID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Stacks.Delete(ctx, stackID); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return nil, nil
}

func (h *Handler) ListStackServices(ctx context.Context, input *StackPathInput) (*ListStackServicesOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	stackID, err := parseUUID(input.StackID)
	if err != nil {
		return nil, err
	}
	services, err := h.svc.Stacks.ListServices(ctx, stackID)
	if err != nil {
		return nil, err
	}
	return &ListStackServicesOutput{Body: services}, nil
}

func (h *Handler) ApplyStack(ctx context.Context, input *ApplyStackInput) (*ApplyResultOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	stackID, err := parseUUID(input.StackID)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.Stacks.Apply(ctx, stackID, userID, input.Body.EnvOverrides)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &ApplyResultOutput{Body: &applyResultBody{
		Stack:   result.Stack,
		Created: result.Created,
		Updated: result.Updated,
		Deleted: result.Deleted,
		Errors:  result.Errors,
	}}, nil
}
