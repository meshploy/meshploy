package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	svc "github.com/meshploy/apps/api/internal/service"
	db "github.com/meshploy/packages/db"
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
		Name          string  `json:"name" minLength:"1" maxLength:"100"`
		Image         string  `json:"image,omitempty"`
		NodeID        *string `json:"node_id,omitempty"`        // nil = auto-schedule
		EnvVars       string  `json:"env_vars,omitempty"`       // raw .env block, encrypted at rest
		Replicas      int     `json:"replicas,omitempty"`       // 0 = use service layer default (1)
		CPURequest    string  `json:"cpu_request,omitempty"`
		CPULimit      string  `json:"cpu_limit,omitempty"`
		MemoryRequest string  `json:"memory_request,omitempty"`
		MemoryLimit   string  `json:"memory_limit,omitempty"`
		// Optional build config — a BuildConfig row is created alongside the
		// Service when git_repo is provided.
		GitIntegrationID      *string `json:"git_integration_id,omitempty"`
		GitRepo               string  `json:"git_repo,omitempty"`
		Branch                string  `json:"branch,omitempty"`
		Builder               string  `json:"builder,omitempty"`
		DockerfilePath        string  `json:"dockerfile_path,omitempty"`
		RegistryIntegrationID *string `json:"registry_integration_id,omitempty"`
		// BuilderNode is the k8s_node_name to pin builds to ("" = auto-schedule).
		BuilderNode          string `json:"builder_node,omitempty"`
		BuilderCPURequest    string `json:"builder_cpu_request,omitempty"`
		BuilderMemoryRequest string `json:"builder_memory_request,omitempty"`
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
		OperationID: "update-service",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}",
		Summary:     "Update a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.PatchWorkload)

	huma.Register(api, huma.Operation{
		OperationID: "delete-service",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}",
		Summary:     "Delete a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteWorkload)

	huma.Register(api, huma.Operation{
		OperationID: "get-service-env-vars",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/env-vars",
		Summary:     "Get decrypted env vars for a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetServiceEnvVars)

	huma.Register(api, huma.Operation{
		OperationID: "get-service-build-config",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/build-config",
		Summary:     "Get build config for a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetServiceBuildConfig)

	huma.Register(api, huma.Operation{
		OperationID: "upsert-service-build-config",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/build-config",
		Summary:     "Create or update build config for a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpsertServiceBuildConfig)

	huma.Register(api, huma.Operation{
		OperationID: "get-service-build-env-vars",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/build-config/env-vars",
		Summary:     "Get build-time environment variables for a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetServiceBuildEnvVars)

	huma.Register(api, huma.Operation{
		OperationID: "put-service-build-env-vars",
		Method:      "PUT",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/build-config/env-vars",
		Summary:     "Set build-time environment variables for a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.PutServiceBuildEnvVars)
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
	var nodeID *uuid.UUID
	if input.Body.NodeID != nil {
		id, err := parseUUID(*input.Body.NodeID)
		if err != nil {
			return nil, err
		}
		nodeID = &id
	}
	var registryID *uuid.UUID
	if input.Body.RegistryIntegrationID != nil {
		id, err := parseUUID(*input.Body.RegistryIntegrationID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid registry_integration_id")
		}
		registryID = &id
	}
	var gitIntegrationID *uuid.UUID
	if input.Body.GitIntegrationID != nil {
		id, err := parseUUID(*input.Body.GitIntegrationID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid git_integration_id")
		}
		gitIntegrationID = &id
	}

	service, err := h.svc.Workloads.Create(ctx, projectID, svc.CreateWorkloadInput{
		Name:                  input.Body.Name,
		Image:                 input.Body.Image,
		NodeID:                nodeID,
		EnvVars:               input.Body.EnvVars,
		Replicas:              input.Body.Replicas,
		CPURequest:            input.Body.CPURequest,
		CPULimit:              input.Body.CPULimit,
		MemoryRequest:         input.Body.MemoryRequest,
		MemoryLimit:           input.Body.MemoryLimit,
		GitIntegrationID:      gitIntegrationID,
		GitRepo:               input.Body.GitRepo,
		Branch:                input.Body.Branch,
		Builder:               db.BuilderType(input.Body.Builder),
		DockerfilePath:        input.Body.DockerfilePath,
		RegistryIntegrationID: registryID,
		BuilderNode:           input.Body.BuilderNode,
		BuilderCPURequest:     input.Body.BuilderCPURequest,
		BuilderMemoryRequest:  input.Body.BuilderMemoryRequest,
	})
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

// ─── PATCH service ────────────────────────────────────────────────────────────

type PatchWorkloadInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
	Body      struct {
		Name          *string `json:"name"`
		Image         *string `json:"image"`
		// node_id: omit = no change, "" = auto-schedule, UUID = pin to node
		NodeID        *string `json:"node_id"`
		Replicas      *int    `json:"replicas"`
		CPURequest    *string `json:"cpu_request"`
		CPULimit      *string `json:"cpu_limit"`
		MemoryRequest *string `json:"memory_request"`
		MemoryLimit   *string `json:"memory_limit"`
		EnvVars       *string `json:"env_vars"`
	}
}

func (h *Handler) PatchWorkload(ctx context.Context, input *PatchWorkloadInput) (*GetWorkloadOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}

	in := svc.UpdateWorkloadInput{
		Name:          input.Body.Name,
		Image:         input.Body.Image,
		Replicas:      input.Body.Replicas,
		CPURequest:    input.Body.CPURequest,
		CPULimit:      input.Body.CPULimit,
		MemoryRequest: input.Body.MemoryRequest,
		MemoryLimit:   input.Body.MemoryLimit,
		EnvVars:       input.Body.EnvVars,
	}
	if input.Body.NodeID != nil {
		in.UpdateNode = true
		if *input.Body.NodeID != "" {
			id, err := parseUUID(*input.Body.NodeID)
			if err != nil {
				return nil, huma.Error400BadRequest("invalid node_id")
			}
			in.NodeID = &id
		}
		// empty string → NodeID stays nil → auto-schedule
	}

	service, err := h.svc.Workloads.Update(ctx, serviceID, in)
	if err != nil {
		return nil, err
	}
	return &GetWorkloadOutput{Body: service}, nil
}

// ─── Env vars ─────────────────────────────────────────────────────────────────

type GetEnvVarsOutput struct {
	Body struct {
		EnvVars string `json:"env_vars"`
	}
}

func (h *Handler) GetServiceEnvVars(ctx context.Context, input *WorkloadPathInput) (*GetEnvVarsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	envVars, err := h.svc.Workloads.GetEnvVars(ctx, serviceID)
	if err != nil {
		return nil, notFound(err)
	}
	out := &GetEnvVarsOutput{}
	out.Body.EnvVars = envVars
	return out, nil
}

// ─── Build config ─────────────────────────────────────────────────────────────

type GetBuildConfigOutput struct {
	Body *db.BuildConfig
}

func (h *Handler) GetServiceBuildConfig(ctx context.Context, input *WorkloadPathInput) (*GetBuildConfigOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	bc, err := h.svc.Workloads.GetBuildConfig(ctx, serviceID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetBuildConfigOutput{Body: bc}, nil
}

type PatchBuildConfigInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
	Body      struct {
		GitIntegrationID      *string `json:"git_integration_id,omitempty"`
		GitRepo               *string `json:"git_repo,omitempty"`
		Branch                *string `json:"branch,omitempty"`
		Builder               *string `json:"builder,omitempty"`
		DockerfilePath        *string `json:"dockerfile_path,omitempty"`
		RegistryIntegrationID *string `json:"registry_integration_id,omitempty"` // "" = clear
		BuilderNode           *string `json:"builder_node,omitempty"`             // "" = auto-schedule
		BuilderCPURequest     *string `json:"builder_cpu_request,omitempty"`
		BuilderMemoryRequest  *string `json:"builder_memory_request,omitempty"`
	}
}

type GetBuildEnvVarsOutput struct {
	Body struct {
		BuildEnvVars string `json:"build_env_vars"`
	}
}

func (h *Handler) UpsertServiceBuildConfig(ctx context.Context, input *PatchBuildConfigInput) (*GetBuildConfigOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}

	in := svc.UpdateBuildConfigInput{
		GitRepo:              input.Body.GitRepo,
		Branch:               input.Body.Branch,
		DockerfilePath:       input.Body.DockerfilePath,
		BuilderNode:          input.Body.BuilderNode,
		BuilderCPURequest:    input.Body.BuilderCPURequest,
		BuilderMemoryRequest: input.Body.BuilderMemoryRequest,
	}
	if input.Body.GitIntegrationID != nil {
		id, err := parseUUID(*input.Body.GitIntegrationID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid git_integration_id")
		}
		in.GitIntegrationID = &id
	}
	if input.Body.Builder != nil {
		bt := db.BuilderType(*input.Body.Builder)
		in.Builder = &bt
	}
	if input.Body.RegistryIntegrationID != nil {
		if *input.Body.RegistryIntegrationID == "" {
			in.ClearRegistry = true
		} else {
			id, err := parseUUID(*input.Body.RegistryIntegrationID)
			if err != nil {
				return nil, huma.Error400BadRequest("invalid registry_integration_id")
			}
			in.RegistryIntegrationID = &id
		}
	}

	bc, err := h.svc.Workloads.UpsertBuildConfig(ctx, serviceID, in)
	if err != nil {
		return nil, err
	}
	return &GetBuildConfigOutput{Body: bc}, nil
}

func (h *Handler) GetServiceBuildEnvVars(ctx context.Context, input *WorkloadPathInput) (*GetBuildEnvVarsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	val, err := h.svc.Workloads.GetBuildEnvVars(ctx, serviceID)
	if err != nil {
		return nil, notFound(err)
	}
	out := &GetBuildEnvVarsOutput{}
	out.Body.BuildEnvVars = val
	return out, nil
}

type PutBuildEnvVarsInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
	Body      struct {
		BuildEnvVars string `json:"build_env_vars"`
	}
}

func (h *Handler) PutServiceBuildEnvVars(ctx context.Context, input *PutBuildEnvVarsInput) (*GetBuildEnvVarsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	_, err = h.svc.Workloads.UpsertBuildConfig(ctx, serviceID, svc.UpdateBuildConfigInput{
		BuildEnvVars: &input.Body.BuildEnvVars,
	})
	if err != nil {
		return nil, err
	}
	out := &GetBuildEnvVarsOutput{}
	out.Body.BuildEnvVars = input.Body.BuildEnvVars
	return out, nil
}
