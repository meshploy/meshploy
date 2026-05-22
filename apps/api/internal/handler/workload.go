package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	appk8s "github.com/meshploy/apps/api/internal/k8s"
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

// PortBody is the wire format for a single port in the create-service request.
type PortBody struct {
	Name      string `json:"name" minLength:"1" maxLength:"64"` // e.g. "http", "grpc"
	Port      int    `json:"port"`                               // container port
	IsHTTP    bool   `json:"is_http"`                            // HTTP/1.1 — routable via proxy
	IsPrimary bool   `json:"is_primary"`                         // health check target
	IsPublic  bool   `json:"is_public"`                          // gets a K8s NodePort
}

type CreateWorkloadInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		Name          string     `json:"name" minLength:"1" maxLength:"100"`
		Image         string     `json:"image,omitempty"`
		NodeID        *string    `json:"node_id,omitempty"`   // nil = auto-schedule
		EnvVars       string     `json:"env_vars,omitempty"`  // raw .env block, encrypted at rest
		Ports         []PortBody `json:"ports,omitempty"`     // empty = default single HTTP port 3000
		Replicas      int        `json:"replicas,omitempty"`  // 0 = use service layer default (1)
		CPURequest    string     `json:"cpu_request,omitempty"`
		CPULimit      string     `json:"cpu_limit,omitempty"`
		MemoryRequest string     `json:"memory_request,omitempty"`
		MemoryLimit   string     `json:"memory_limit,omitempty"`
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
		// Database-specific fields — only used when type == "database"
		Type       string `json:"type,omitempty"`        // "application" | "database"
		Engine     string `json:"engine,omitempty"`      // "postgres" | "mysql" | "redis" | "mongodb"
		Version    string `json:"version,omitempty"`     // e.g. "16", "8.0", "7"
		StorageGB  int    `json:"storage_gb,omitempty"`  // GiB; 0 = default (10)
		DBName     string `json:"db_name,omitempty"`     // defaults to service name
		DBUser     string `json:"db_user,omitempty"`     // defaults to service name
		DBPassword string `json:"db_password,omitempty"` // auto-generated if empty
		// PullRegistryIntegrationID — credentials for pulling a private runtime image.
		// Set for image-source services; "" = clear (public image), nil = not set.
		PullRegistryIntegrationID *string `json:"pull_registry_integration_id,omitempty"`
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
		OperationID: "start-service",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/start",
		Summary:     "Start a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.StartService)

	huma.Register(api, huma.Operation{
		OperationID: "stop-service",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/stop",
		Summary:     "Stop a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.StopService)

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

	huma.Register(api, huma.Operation{
		OperationID: "get-database-config",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/database-config",
		Summary:     "Get database config for a database service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetDatabaseConfig)

	huma.Register(api, huma.Operation{
		OperationID: "reset-database",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/reset",
		Summary:     "Wipe and re-provision a database (destructive)",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ResetDatabase)

	huma.Register(api, huma.Operation{
		OperationID: "db-schema",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/db/schema",
		Summary:     "Introspect database schema",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DBSchema)

	huma.Register(api, huma.Operation{
		OperationID: "db-query",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/db/query",
		Summary:     "Execute a database query",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DBQuery)

	huma.Register(api, huma.Operation{
		OperationID: "list-service-pods",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/pods",
		Summary:     "List running pods for a service",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListServicePods)

	huma.Register(api, huma.Operation{
		OperationID: "get-pod-metrics",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/pods/metrics",
		Summary:     "Live CPU and memory usage per pod (requires metrics-server)",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetPodMetrics)
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
	var pullRegistryID *uuid.UUID
	if input.Body.PullRegistryIntegrationID != nil && *input.Body.PullRegistryIntegrationID != "" {
		id, err := parseUUID(*input.Body.PullRegistryIntegrationID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid pull_registry_integration_id")
		}
		pullRegistryID = &id
	}

	ports := make([]svc.PortInput, len(input.Body.Ports))
	for i, p := range input.Body.Ports {
		ports[i] = svc.PortInput{
			Name:      p.Name,
			Port:      p.Port,
			IsHTTP:    p.IsHTTP,
			IsPrimary: p.IsPrimary,
			IsPublic:  p.IsPublic,
		}
	}

	service, err := h.svc.Workloads.Create(ctx, projectID, svc.CreateWorkloadInput{
		Name:                      input.Body.Name,
		Image:                     input.Body.Image,
		PullRegistryIntegrationID: pullRegistryID,
		NodeID:                    nodeID,
		EnvVars:                   input.Body.EnvVars,
		Ports:                     ports,
		Replicas:                  input.Body.Replicas,
		CPURequest:                input.Body.CPURequest,
		CPULimit:                  input.Body.CPULimit,
		MemoryRequest:             input.Body.MemoryRequest,
		MemoryLimit:               input.Body.MemoryLimit,
		GitIntegrationID:          gitIntegrationID,
		GitRepo:                   input.Body.GitRepo,
		Branch:                    input.Body.Branch,
		Builder:                   db.BuilderType(input.Body.Builder),
		DockerfilePath:            input.Body.DockerfilePath,
		RegistryIntegrationID:     registryID,
		BuilderNode:               input.Body.BuilderNode,
		BuilderCPURequest:         input.Body.BuilderCPURequest,
		BuilderMemoryRequest:      input.Body.BuilderMemoryRequest,
		Type:                      db.ServiceType(input.Body.Type),
		Engine:                    db.DatabaseEngine(input.Body.Engine),
		Version:                   input.Body.Version,
		StorageGB:                 input.Body.StorageGB,
		DBName:                    input.Body.DBName,
		DBUser:                    input.Body.DBUser,
		DBPassword:                input.Body.DBPassword,
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

func (h *Handler) StartService(ctx context.Context, input *WorkloadPathInput) (*GetWorkloadOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := uuid.Parse(input.ServiceID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid service id")
	}
	svc, err := h.svc.Workloads.Start(ctx, serviceID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetWorkloadOutput{Body: svc}, nil
}

func (h *Handler) StopService(ctx context.Context, input *WorkloadPathInput) (*GetWorkloadOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := uuid.Parse(input.ServiceID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid service id")
	}
	svc, err := h.svc.Workloads.Stop(ctx, serviceID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetWorkloadOutput{Body: svc}, nil
}

// ─── PATCH service ────────────────────────────────────────────────────────────

type PatchWorkloadInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
	Body      struct {
		Name          *string    `json:"name,omitempty"`
		Image         *string    `json:"image,omitempty"`
		// node_id: omit = no change, "" = auto-schedule, UUID = pin to node
		NodeID        *string    `json:"node_id,omitempty"`
		Replicas      *int       `json:"replicas,omitempty"`
		CPURequest    *string    `json:"cpu_request,omitempty"`
		CPULimit      *string    `json:"cpu_limit,omitempty"`
		MemoryRequest *string    `json:"memory_request,omitempty"`
		MemoryLimit   *string    `json:"memory_limit,omitempty"`
		EnvVars       *string    `json:"env_vars,omitempty"`
		Ports         *[]PortBody `json:"ports,omitempty"` // nil = no change; replaces all ports when set
		// pull_registry_integration_id: omit = no change, "" = clear (public image), UUID = set
		PullRegistryIntegrationID *string `json:"pull_registry_integration_id,omitempty"`
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
	if input.Body.PullRegistryIntegrationID != nil {
		in.UpdatePullRegistry = true
		if *input.Body.PullRegistryIntegrationID != "" {
			id, err := parseUUID(*input.Body.PullRegistryIntegrationID)
			if err != nil {
				return nil, huma.Error400BadRequest("invalid pull_registry_integration_id")
			}
			in.PullRegistryIntegrationID = &id
		}
		// empty string → PullRegistryIntegrationID stays nil → clears to public image
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
	if input.Body.Ports != nil {
		ports := make([]svc.PortInput, len(*input.Body.Ports))
		for i, p := range *input.Body.Ports {
			ports[i] = svc.PortInput{
				Name:      p.Name,
				Port:      p.Port,
				IsHTTP:    p.IsHTTP,
				IsPrimary: p.IsPrimary,
				IsPublic:  p.IsPublic,
			}
		}
		in.Ports = &ports
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
		RollbackEnabled       *bool   `json:"rollback_enabled,omitempty"`
		ImageRetention        *int    `json:"image_retention,omitempty"`
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
		RollbackEnabled:      input.Body.RollbackEnabled,
		ImageRetention:       input.Body.ImageRetention,
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

type GetDatabaseConfigOutput struct {
	Body *db.DatabaseConfig
}

func (h *Handler) GetDatabaseConfig(ctx context.Context, input *WorkloadPathInput) (*GetDatabaseConfigOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	dc, err := h.svc.Workloads.GetDatabaseConfig(ctx, serviceID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetDatabaseConfigOutput{Body: dc}, nil
}

type ResetDatabaseOutput struct {
	Body *db.Deployment
}

func (h *Handler) ResetDatabase(ctx context.Context, input *WorkloadPathInput) (*ResetDatabaseOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	dep, err := h.svc.Deployments.ResetDatabase(ctx, serviceID)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return &ResetDatabaseOutput{Body: dep}, nil
}

// ─── DB Explorer ──────────────────────────────────────────────────────────────

type DBSchemaOutput struct {
	Body []svc.SchemaTable
}

func (h *Handler) DBSchema(ctx context.Context, input *WorkloadPathInput) (*DBSchemaOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	tables, err := h.svc.DBExplorer.Schema(ctx, serviceID)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return &DBSchemaOutput{Body: tables}, nil
}

type DBQueryInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
	Body      struct {
		Query    string `json:"query" minLength:"1"`
		ReadOnly bool   `json:"read_only"`
	}
}

type DBQueryOutput struct {
	Body *svc.QueryResult
}

func (h *Handler) DBQuery(ctx context.Context, input *DBQueryInput) (*DBQueryOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.DBExplorer.Query(ctx, serviceID, input.Body.Query, input.Body.ReadOnly)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return &DBQueryOutput{Body: result}, nil
}

type ListServicePodsOutput struct {
	Body []appk8s.PodInfo
}

func (h *Handler) ListServicePods(ctx context.Context, input *WorkloadPathInput) (*ListServicePodsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	if h.svc.K8s == nil {
		return nil, huma.Error503ServiceUnavailable("kubernetes not available")
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	namespace, k8sName, err := h.svc.Workloads.GetK8sInfo(ctx, serviceID)
	if err != nil {
		return nil, huma.Error404NotFound("service not found")
	}
	pods, err := appk8s.ListServicePods(ctx, h.svc.K8s, namespace, k8sName)
	if err != nil {
		return nil, huma.Error500InternalServerError("list pods: " + err.Error())
	}
	if pods == nil {
		pods = []appk8s.PodInfo{}
	}
	return &ListServicePodsOutput{Body: pods}, nil
}

type GetPodMetricsOutput struct {
	Body []appk8s.PodMetrics
}

func (h *Handler) GetPodMetrics(ctx context.Context, input *WorkloadPathInput) (*GetPodMetricsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	if h.svc.K8s == nil || h.svc.K8sRestConfig == nil {
		return nil, huma.Error503ServiceUnavailable("kubernetes not available")
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	namespace, k8sName, err := h.svc.Workloads.GetK8sInfo(ctx, serviceID)
	if err != nil {
		return nil, huma.Error404NotFound("service not found")
	}
	labelSelector := "app=" + k8sName + ",managed-by=meshploy"
	metrics, err := appk8s.GetPodMetrics(ctx, h.svc.K8sRestConfig, namespace, labelSelector)
	if err != nil {
		return nil, huma.Error500InternalServerError("metrics: " + err.Error())
	}
	if metrics == nil {
		metrics = []appk8s.PodMetrics{}
	}
	return &GetPodMetricsOutput{Body: metrics}, nil
}
