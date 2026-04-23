package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
	svc "github.com/meshploy/apps/api/internal/service"
)

type ListProjectsInput struct {
	OrgID string `path:"orgId"`
}

type ListProjectsOutput struct {
	Body []svc.ProjectWithCounts
}

type ProjectPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
}

type GetProjectOutput struct {
	Body *db.Project
}

type CreateProjectInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
		Slug string `json:"slug" minLength:"1" maxLength:"50" pattern:"^[a-z0-9-]+$"`
	}
}

type CreateProjectOutput struct {
	Body *db.Project
}

type UpdateProjectInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
	}
}

type UpdateProjectOutput struct {
	Body *db.Project
}

func (h *Handler) registerProjectRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-projects",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects",
		Summary:     "List projects in an organization",
		Tags:        []string{"Projects"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListProjects)

	huma.Register(api, huma.Operation{
		OperationID: "create-project",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/projects",
		Summary:     "Create a project",
		Tags:        []string{"Projects"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.CreateProject)

	huma.Register(api, huma.Operation{
		OperationID: "get-project",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}",
		Summary:     "Get a project",
		Tags:        []string{"Projects"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetProject)

	huma.Register(api, huma.Operation{
		OperationID: "update-project",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}",
		Summary:     "Update a project",
		Tags:        []string{"Projects"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateProject)

	huma.Register(api, huma.Operation{
		OperationID: "delete-project",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}",
		Summary:     "Delete a project",
		Tags:        []string{"Projects"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteProject)

	huma.Register(api, huma.Operation{
		OperationID:   "clear-build-cache",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/build-cache",
		Summary:       "Clear the buildah layer cache PVC for a project",
		Tags:          []string{"Projects"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.ClearBuildCache)
}

func (h *Handler) ListProjects(ctx context.Context, input *ListProjectsInput) (*ListProjectsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	projects, err := h.svc.Projects.ListWithCounts(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return &ListProjectsOutput{Body: projects}, nil
}

func (h *Handler) CreateProject(ctx context.Context, input *CreateProjectInput) (*CreateProjectOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	project, err := h.svc.Projects.Create(ctx, orgID, input.Body.Name, input.Body.Slug)
	if err != nil {
		return nil, huma.Error409Conflict("slug already taken")
	}
	return &CreateProjectOutput{Body: project}, nil
}

func (h *Handler) GetProject(ctx context.Context, input *ProjectPathInput) (*GetProjectOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	project, err := h.svc.Projects.Get(ctx, projectID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetProjectOutput{Body: project}, nil
}

func (h *Handler) UpdateProject(ctx context.Context, input *UpdateProjectInput) (*UpdateProjectOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	project, err := h.svc.Projects.Update(ctx, projectID, input.Body.Name)
	if err != nil {
		return nil, notFound(err)
	}
	return &UpdateProjectOutput{Body: project}, nil
}

func (h *Handler) DeleteProject(ctx context.Context, input *ProjectPathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.Projects.Delete(ctx, projectID)
}

func (h *Handler) ClearBuildCache(ctx context.Context, input *ProjectPathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	project, err := h.svc.Projects.Get(ctx, projectID)
	if err != nil {
		return nil, notFound(err)
	}
	if err := h.svc.Deployments.ClearBuildCache(ctx, project.Slug); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return nil, nil
}
