package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
)

type WorkloadPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
}

type ListWorkloadsInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
}

type ListWorkloadsOutput struct {
	Body []db.Service
}

type GetWorkloadOutput struct {
	Body *db.Service
}

type CreateWorkloadInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		Name         string         `json:"name" minLength:"1" maxLength:"100"`
		Image        string         `json:"image" minLength:"1"`
		NodeID       string         `json:"node_id"`
		InternalPort int            `json:"internal_port" minimum:"1" maximum:"65535"`
		EnvVars      db.EnvVarsMap  `json:"env_vars"`
	}
}

type CreateWorkloadOutput struct {
	Body *db.Service
}

func (h *Handler) registerWorkloadRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-services",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services",
		Summary:     "List services in a project",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListWorkloads)

	huma.Register(api, huma.Operation{
		OperationID: "create-service",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services",
		Summary:     "Create a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.CreateWorkload)

	huma.Register(api, huma.Operation{
		OperationID: "get-service",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}",
		Summary:     "Get a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetWorkload)

	huma.Register(api, huma.Operation{
		OperationID: "delete-service",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}",
		Summary:     "Delete a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteWorkload)
}

func (h *Handler) ListWorkloads(ctx context.Context, input *ListWorkloadsInput) (*ListWorkloadsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	services, err := h.svc.Workloads.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &ListWorkloadsOutput{Body: services}, nil
}

func (h *Handler) CreateWorkload(ctx context.Context, input *CreateWorkloadInput) (*CreateWorkloadOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	nodeID, err := parseUUID(input.Body.NodeID)
	if err != nil {
		return nil, err
	}
	service, err := h.svc.Workloads.Create(ctx, projectID, nodeID,
		input.Body.Name, input.Body.Image, input.Body.InternalPort, input.Body.EnvVars)
	if err != nil {
		return nil, err
	}
	return &CreateWorkloadOutput{Body: service}, nil
}

func (h *Handler) GetWorkload(ctx context.Context, input *WorkloadPathInput) (*GetWorkloadOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	service, err := h.svc.Workloads.Get(ctx, serviceID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetWorkloadOutput{Body: service}, nil
}

func (h *Handler) DeleteWorkload(ctx context.Context, input *WorkloadPathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := uuid.Parse(input.ServiceID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid service id")
	}
	return nil, h.svc.Workloads.Delete(ctx, serviceID)
}
