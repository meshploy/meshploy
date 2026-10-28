package handler

import (
	"context"
	"errors"
	"log"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	svc "github.com/meshploy/packages/server/service"
)

type ListProjectsInput struct {
	OrgID  string `path:"orgId"`
	Search string `query:"search" maxLength:"100" doc:"Only projects whose name or slug contains this, ignoring case"`
	Sort   string `query:"sort" enum:"recent,name" default:"recent" doc:"recent: newest first; name: A to Z"`
}

type ListProjectsOutput struct {
	Body []svc.ProjectWithCounts
}

type ProjectPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
}

type GetProjectOutput struct {
	Body *svc.ProjectWithCounts
}

type CreateProjectInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
		Slug string `json:"slug" minLength:"1" maxLength:"50" pattern:"^[a-z0-9-]+$"`
		// Sandboxed starts the project's apps and jobs on gVisor. Absent
		// follows the organization's sandbox_new_projects.
		Sandboxed *bool `json:"sandboxed,omitempty" doc:"Run the project's apps and jobs on gVisor; absent follows the organization's setting"`
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

type SetProjectNetworkOpenInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		Open bool `json:"open"`
	}
}

type SetProjectSandboxInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		On bool `json:"on"`
	}
}

type ProjectSandboxOutput struct {
	Body svc.SandboxState
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
		OperationID: "set-project-network-open",
		Method:      "PUT",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/network-open",
		Summary:     "Exempt a project from network isolation, or put it back (owner or admin)",
		Tags:        []string{"Projects"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.SetProjectNetworkOpen)

	huma.Register(api, huma.Operation{
		OperationID: "get-project-sandbox",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/sandbox",
		Summary:     "Whether a project's pods run sandboxed, and whether that is required",
		Tags:        []string{"Projects"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetProjectSandbox)

	huma.Register(api, huma.Operation{
		OperationID: "set-project-sandbox",
		Method:      "PUT",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/sandbox",
		Summary:     "Run a project's pods sandboxed, or not (owner or admin)",
		Tags:        []string{"Projects"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.SetProjectSandbox)

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
	callerID, orgID, _, err := h.checkOrgMemberAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	projects, err := h.svc.Projects.ListWithCounts(ctx, orgID, svc.ProjectListOptions{Search: input.Search, Sort: input.Sort})
	if err != nil {
		return nil, err
	}
	visibleIDs, isAdmin, err := h.svc.Permissions.VisibleProjectIDs(ctx, orgID, callerID)
	if err != nil {
		return nil, err
	}
	if !isAdmin {
		filtered := projects[:0]
		for _, p := range projects {
			if !visibleIDs[p.ID] {
				continue
			}
			// Seen for what was granted inside it, not the project: its card
			// says so, and counts what this member may open.
			if h.svc.Permissions.CheckAccess(ctx, orgID, callerID, p.ID, db.ResourceProject, db.ActionView, &p.ID) != nil {
				inner, err := h.svc.Permissions.GrantsInProject(ctx, orgID, callerID, p.ID)
				if err != nil {
					return nil, err
				}
				mine, err := h.svc.Projects.LimitedView(ctx, p.ID, inner)
				if err != nil {
					return nil, err
				}
				p.ProjectCounts, p.Stats, p.Limited = mine.ProjectCounts, nil, true
			}
			filtered = append(filtered, p)
		}
		projects = filtered
	}
	return &ListProjectsOutput{Body: projects}, nil
}

func (h *Handler) CreateProject(ctx context.Context, input *CreateProjectInput) (*CreateProjectOutput, error) {
	userID, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	sandboxed, err := h.svc.Sandbox.NewProject(ctx, orgID, input.Body.Sandboxed)
	if err != nil {
		if errors.Is(err, svc.ErrSandboxRequired) || errors.Is(err, appk8s.ErrNoSandboxNode) {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError("sandbox: " + err.Error())
	}
	project, err := h.svc.Projects.CreateSandboxed(ctx, orgID, input.Body.Name, input.Body.Slug, sandboxed)
	if err != nil {
		return nil, huma.Error409Conflict("slug already taken")
	}
	h.publishProjectNames(ctx, orgID, userID)
	return &CreateProjectOutput{Body: project}, nil
}

func (h *Handler) GetProject(ctx context.Context, input *ProjectPathInput) (*GetProjectOutput, error) {
	r, err := h.readProject(ctx, input.OrgID, input.ProjectID)
	if err != nil {
		return nil, err
	}
	can := h.svc.Permissions.Actions(ctx, r.orgID, r.userID, r.projectID, db.ResourceProject, &r.projectID)
	if r.limited() {
		project, err := h.svc.Projects.LimitedView(ctx, r.projectID, *r.inner)
		if err != nil {
			return nil, notFound(err)
		}
		project.Can = can
		return &GetProjectOutput{Body: project}, nil
	}
	project, err := h.svc.Projects.GetWithCounts(ctx, r.projectID)
	if err != nil {
		return nil, notFound(err)
	}
	project.Can = can
	return &GetProjectOutput{Body: project}, nil
}

func (h *Handler) UpdateProject(ctx context.Context, input *UpdateProjectInput) (*UpdateProjectOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
	if err != nil {
		return nil, err
	}
	project, err := h.svc.Projects.Update(ctx, projectID, input.Body.Name)
	if err != nil {
		return nil, notFound(err)
	}
	return &UpdateProjectOutput{Body: project}, nil
}

// SetProjectNetworkOpen is for an owner or admin, not the project's editors:
// opening a project undoes, for it, a choice the organization made.
func (h *Handler) SetProjectNetworkOpen(ctx context.Context, input *SetProjectNetworkOpenInput) (*UpdateProjectOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	project, err := h.svc.Projects.Get(ctx, projectID)
	if err != nil || project.OrganizationID != orgID {
		return nil, huma.Error404NotFound("project not found")
	}
	if err := h.svc.Isolation.SetProjectOpen(ctx, projectID, input.Body.Open); err != nil {
		return nil, huma.Error500InternalServerError("network isolation: " + err.Error())
	}
	project.NetworkOpen = input.Body.Open
	return &UpdateProjectOutput{Body: project}, nil
}

func (h *Handler) GetProjectSandbox(ctx context.Context, input *ProjectPathInput) (*ProjectSandboxOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionView, "")
	if err != nil {
		return nil, err
	}
	project, err := h.svc.Projects.Get(ctx, projectID)
	if err != nil {
		return nil, notFound(err)
	}
	state, err := h.svc.Sandbox.State(ctx, project)
	if err != nil {
		return nil, huma.Error500InternalServerError("sandbox: " + err.Error())
	}
	return &ProjectSandboxOutput{Body: state}, nil
}

// SetProjectSandbox is for an owner or admin, as network isolation is: it
// restarts the project's databases and changes what its pods cost.
func (h *Handler) SetProjectSandbox(ctx context.Context, input *SetProjectSandboxInput) (*ProjectSandboxOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	project, err := h.svc.Projects.Get(ctx, projectID)
	if err != nil || project.OrganizationID != orgID {
		return nil, huma.Error404NotFound("project not found")
	}
	if err := h.svc.Sandbox.Set(ctx, projectID, input.Body.On); err != nil {
		if errors.Is(err, svc.ErrSandboxRequired) || errors.Is(err, appk8s.ErrNoSandboxNode) {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError("sandbox: " + err.Error())
	}
	project.Sandboxed = input.Body.On
	state, err := h.svc.Sandbox.State(ctx, project)
	if err != nil {
		return nil, huma.Error500InternalServerError("sandbox: " + err.Error())
	}
	return &ProjectSandboxOutput{Body: state}, nil
}

func (h *Handler) DeleteProject(ctx context.Context, input *ProjectPathInput) (*struct{}, error) {
	userID, orgID, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionDelete, "")
	if err != nil {
		return nil, err
	}
	// Out of the cluster first, then the rows; a level also leaves every
	// group's path.
	if err := h.svc.Promotions.DeleteProject(ctx, projectID); err != nil {
		if errors.Is(err, svc.ErrProjectHasLevels) {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, notFound(err)
	}
	h.publishProjectNames(ctx, orgID, userID)
	return nil, nil
}

// publishProjectNames asks the edge to serve the projects' console names as
// they now are. A failure is logged, not returned: the project change itself
// succeeded, and the next one, or the API's next start, publishes again.
func (h *Handler) publishProjectNames(ctx context.Context, orgID, by uuid.UUID) {
	if err := h.svc.System.PublishProjectNames(ctx, orgID, by); err != nil {
		log.Printf("warning: could not ask the host agent to serve the projects' console names: %v", err)
	}
}

func (h *Handler) ClearBuildCache(ctx context.Context, input *ProjectPathInput) (*struct{}, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
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
