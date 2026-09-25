package handler

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
	svc "github.com/meshploy/packages/server/service"
)

// Environment levels: a project's production level and the levels below it.
// See service/environment.go for the model.

type ListEnvironmentsOutput struct {
	Body []svc.EnvironmentLevel
}

type CreateEnvironmentInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		Name string `json:"name" minLength:"1" maxLength:"20" doc:"Lower-case letters, digits and hyphens, starting with a letter"`
		// RelativeTo is the level the new one goes next to, and Placement
		// which side of it.
		RelativeTo string `json:"relative_to" format:"uuid"`
		Placement  string `json:"placement" enum:"above,below"`
	}
}

type CreateEnvironmentOutput struct {
	Body svc.EnvironmentLevel
}

func (h *Handler) registerEnvironmentRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-environments",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/environments",
		Summary:     "List a project's environment levels, production first",
		Tags:        []string{"Projects"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListEnvironments)

	huma.Register(api, huma.Operation{
		OperationID:   "create-environment",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/environments",
		Summary:       "Add an environment level above or below an existing one",
		Tags:          []string{"Projects"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 201,
	}, h.CreateEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "rename-environment",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/environment",
		Summary:     "Rename an environment level; its hostnames are re-derived, its namespace stays",
		Tags:        []string{"Projects"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.RenameEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "delete-environment",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/environment",
		Summary:     "Delete an environment level and everything in it; groups skip it from then on",
		Tags:        []string{"Projects"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteEnvironment)
}

type DeleteEnvironmentOutput struct {
	Body *svc.DeletedLevel
}

// DeleteEnvironment deletes the level projectId: its workloads, its namespace
// and its row, taking it out of every group's path.
func (h *Handler) DeleteEnvironment(ctx context.Context, input *ProjectPathInput) (*DeleteEnvironmentOutput, error) {
	_, _, level, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionDelete, "")
	if err != nil {
		return nil, err
	}
	out, err := h.svc.Promotions.DeleteLevel(ctx, level)
	if err != nil {
		if errors.Is(err, svc.ErrDeleteProduction) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if nf := notFound(err); nf != err {
			return nil, nf
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &DeleteEnvironmentOutput{Body: out}, nil
}

type RenameEnvironmentInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		Name string `json:"name" minLength:"1" maxLength:"20"`
	}
}

type RenameEnvironmentOutput struct {
	Body struct {
		// HostnamesChanged is how many of the level's routes now answer on a
		// new name, the old one no longer served.
		HostnamesChanged int `json:"hostnames_changed"`
	}
}

// RenameEnvironment renames the level projectId and re-derives its hostnames.
func (h *Handler) RenameEnvironment(ctx context.Context, input *RenameEnvironmentInput) (*RenameEnvironmentOutput, error) {
	_, _, level, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
	if err != nil {
		return nil, err
	}
	n, err := h.svc.Projects.RenameLevel(ctx, level, input.Body.Name)
	if err != nil {
		if errors.Is(err, svc.ErrLevelTaken) {
			return nil, huma.Error409Conflict(err.Error())
		}
		if nf := notFound(err); nf != err {
			return nil, nf
		}
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	out := &RenameEnvironmentOutput{}
	out.Body.HostnamesChanged = n
	return out, nil
}

func (h *Handler) ListEnvironments(ctx context.Context, input *ProjectPathInput) (*ListEnvironmentsOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionView, "")
	if err != nil {
		return nil, err
	}
	levels, err := h.svc.Projects.Levels(ctx, projectID)
	if err != nil {
		return nil, notFound(err)
	}
	return &ListEnvironmentsOutput{Body: levels}, nil
}

func (h *Handler) CreateEnvironment(ctx context.Context, input *CreateEnvironmentInput) (*CreateEnvironmentOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
	if err != nil {
		return nil, err
	}
	relativeTo, err := parseUUID(input.Body.RelativeTo)
	if err != nil {
		return nil, err
	}
	level, err := h.svc.Projects.CreateLevel(ctx, projectID, input.Body.Name, relativeTo, input.Body.Placement == "above")
	switch {
	case errors.Is(err, svc.ErrLevelTaken):
		return nil, huma.Error409Conflict(err.Error())
	case errors.Is(err, svc.ErrLevelNotInChain):
		return nil, huma.Error404NotFound(err.Error())
	case err != nil:
		if nf := notFound(err); nf != err {
			return nil, nf
		}
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return &CreateEnvironmentOutput{Body: svc.EnvironmentLevel{
		ProjectID: level.ID,
		Name:      level.EnvName,
		Level:     level.EnvLevel,
		Namespace: level.Slug,
	}}, nil
}
