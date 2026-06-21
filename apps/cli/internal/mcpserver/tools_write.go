package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"
	"github.com/meshploy/apps/cli/internal/client"
)

func (s *srv) registerWriteTools(ms *mcpsdk.MCPServer) {
	// ── Projects ──────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("create_project",
			mcp.WithDescription("Create a new project (K8s namespace)."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Project name")),
		),
		s.handleCreateProject,
	)

	ms.AddTool(
		mcp.NewTool("delete_project",
			mcp.WithDescription("DESTRUCTIVE — delete a project and all its resources. Confirm with the user before calling."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID or slug")),
		),
		s.handleDeleteProject,
	)

	// ── Secrets ───────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("set_secret",
			mcp.WithDescription("Create or update a secret in a project."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Secret name / key")),
			mcp.WithString("value", mcp.Required(), mcp.Description("Secret value")),
		),
		s.handleSetSecret,
	)

	ms.AddTool(
		mcp.NewTool("delete_secret",
			mcp.WithDescription("DESTRUCTIVE — permanently delete a secret. Confirm with the user before calling."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Secret name / key")),
		),
		s.handleDeleteSecret,
	)

	// ── Services ──────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("create_service",
			mcp.WithDescription("Create a new service. Use type=database for managed databases (requires engine). Use git_repo+branch+builder for git-based builds."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Service name")),
			mcp.WithString("image", mcp.Description("Container image (for image-based services)")),
			mcp.WithString("type", mcp.Description("Service type: application (default) or database")),
			mcp.WithString("engine", mcp.Description("Database engine: postgres, mysql, redis, mongodb (required when type=database)")),
			mcp.WithString("version", mcp.Description("Database version, e.g. 16, 8.0, 7")),
			mcp.WithString("storage_gb", mcp.Description("Database storage in GiB (default: 10)")),
			mcp.WithString("git_repo", mcp.Description("Git repository URL for build-from-source")),
			mcp.WithString("branch", mcp.Description("Git branch (default: main)")),
			mcp.WithString("builder", mcp.Description("Builder: nixpacks, herokuish, dockerfile, or image")),
			mcp.WithString("env_vars", mcp.Description("Runtime env vars as KEY=VALUE lines")),
			mcp.WithString("replicas", mcp.Description("Number of replicas (default: 1)")),
			mcp.WithString("cpu_request", mcp.Description("CPU request, e.g. 100m")),
			mcp.WithString("cpu_limit", mcp.Description("CPU limit, e.g. 500m")),
			mcp.WithString("memory_request", mcp.Description("Memory request, e.g. 128Mi")),
			mcp.WithString("memory_limit", mcp.Description("Memory limit, e.g. 512Mi")),
		),
		s.handleCreateService,
	)

	ms.AddTool(
		mcp.NewTool("update_service",
			mcp.WithDescription("Update service settings (image, replicas, resource limits). Use set_env_vars to change env vars."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("name", mcp.Description("New service name")),
			mcp.WithString("image", mcp.Description("New container image")),
			mcp.WithString("replicas", mcp.Description("Number of replicas")),
			mcp.WithString("cpu_request", mcp.Description("CPU request, e.g. 100m")),
			mcp.WithString("cpu_limit", mcp.Description("CPU limit, e.g. 500m")),
			mcp.WithString("memory_request", mcp.Description("Memory request, e.g. 128Mi")),
			mcp.WithString("memory_limit", mcp.Description("Memory limit, e.g. 512Mi")),
		),
		s.handleUpdateService,
	)

	ms.AddTool(
		mcp.NewTool("set_env_vars",
			mcp.WithDescription("Set runtime env vars for a service. Provide all vars as KEY=VALUE lines — this replaces the existing set."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("env_vars", mcp.Required(), mcp.Description("Env vars as KEY=VALUE lines, e.g. PORT=3000\\nNODE_ENV=production")),
		),
		s.handleSetEnvVars,
	)

	ms.AddTool(
		mcp.NewTool("rollback_deployment",
			mcp.WithDescription("Roll back a service to a previous deployment. Use list_deployments to find the deployment_id."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("deployment_id", mcp.Required(), mcp.Description("Deployment ID to roll back to")),
		),
		s.handleRollbackDeployment,
	)

	ms.AddTool(
		mcp.NewTool("deploy_service",
			mcp.WithDescription("Trigger a new deployment for a service."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
		),
		s.handleDeployService,
	)

	ms.AddTool(
		mcp.NewTool("start_service",
			mcp.WithDescription("Start a stopped service."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
		),
		s.handleStartService,
	)

	ms.AddTool(
		mcp.NewTool("stop_service",
			mcp.WithDescription("Stop a running service."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
		),
		s.handleStopService,
	)

	ms.AddTool(
		mcp.NewTool("delete_service",
			mcp.WithDescription("DESTRUCTIVE — permanently delete a service and all deployment history. Confirm with the user before calling."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
		),
		s.handleDeleteService,
	)

	ms.AddTool(
		mcp.NewTool("update_build_config",
			mcp.WithDescription("Update the build configuration for a service (git repo, branch, builder, Dockerfile path, auto-deploy)."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("git_repo", mcp.Description("Git repository URL")),
			mcp.WithString("branch", mcp.Description("Git branch")),
			mcp.WithString("builder", mcp.Description("Builder: nixpacks, herokuish, dockerfile, or image")),
			mcp.WithString("dockerfile_path", mcp.Description("Path to Dockerfile (default: Dockerfile)")),
			mcp.WithBoolean("auto_deploy", mcp.Description("Auto-deploy on git push")),
		),
		s.handleUpdateBuildConfig,
	)

	ms.AddTool(
		mcp.NewTool("set_build_env_vars",
			mcp.WithDescription("Set build-time env vars for a service as KEY=VALUE lines. Replaces the existing set."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("build_env_vars", mcp.Required(), mcp.Description("Build env vars as KEY=VALUE lines")),
		),
		s.handleSetBuildEnvVars,
	)

	// ── Variable Groups ───────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("create_variable_group",
			mcp.WithDescription("Create a variable group in a project. Variable groups hold key/value pairs that can be attached to multiple services."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Group name")),
			mcp.WithString("description", mcp.Description("Optional description")),
		),
		s.handleCreateVariableGroup,
	)

	ms.AddTool(
		mcp.NewTool("update_variable_group",
			mcp.WithDescription("Rename or update the description of a variable group."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("group_id", mcp.Required(), mcp.Description("Variable group ID or name")),
			mcp.WithString("name", mcp.Description("New group name")),
			mcp.WithString("description", mcp.Description("New description")),
		),
		s.handleUpdateVariableGroup,
	)

	ms.AddTool(
		mcp.NewTool("delete_variable_group",
			mcp.WithDescription("DESTRUCTIVE — delete a variable group and all its items. Confirm with the user before calling."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("group_id", mcp.Required(), mcp.Description("Variable group ID or name")),
		),
		s.handleDeleteVariableGroup,
	)

	ms.AddTool(
		mcp.NewTool("set_variable",
			mcp.WithDescription("Create or update a variable (key/value pair) in a variable group. Set is_secret=true for sensitive values."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("group_id", mcp.Required(), mcp.Description("Variable group ID or name")),
			mcp.WithString("key", mcp.Required(), mcp.Description("Variable key")),
			mcp.WithString("value", mcp.Required(), mcp.Description("Variable value")),
			mcp.WithBoolean("is_secret", mcp.Description("Mark value as secret (masked in UI)")),
		),
		s.handleSetVariable,
	)

	ms.AddTool(
		mcp.NewTool("delete_variable",
			mcp.WithDescription("DESTRUCTIVE — delete a variable from a variable group. Use get_variable_group to find the item ID. Confirm with the user before calling."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("group_id", mcp.Required(), mcp.Description("Variable group ID or name")),
			mcp.WithString("item_id", mcp.Required(), mcp.Description("Variable item ID")),
		),
		s.handleDeleteVariable,
	)

	ms.AddTool(
		mcp.NewTool("attach_variable_group",
			mcp.WithDescription("Attach a variable group to a service so its variables are injected at runtime."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("group_id", mcp.Required(), mcp.Description("Variable group ID or name")),
		),
		s.handleAttachVariableGroup,
	)

	ms.AddTool(
		mcp.NewTool("detach_variable_group",
			mcp.WithDescription("Detach a variable group from a service."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("group_id", mcp.Required(), mcp.Description("Variable group ID or name")),
		),
		s.handleDetachVariableGroup,
	)

	// ── Stacks ────────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("create_stack",
			mcp.WithDescription("Create a new stack with a Docker Compose–style YAML spec."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Stack name")),
			mcp.WithString("spec", mcp.Description("Docker Compose–style YAML spec (optional, can apply later)")),
		),
		s.handleCreateStack,
	)

	ms.AddTool(
		mcp.NewTool("update_stack",
			mcp.WithDescription("Update a stack's YAML spec."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("stack_id", mcp.Required(), mcp.Description("Stack ID or name")),
			mcp.WithString("spec", mcp.Required(), mcp.Description("New Docker Compose–style YAML spec")),
		),
		s.handleUpdateStack,
	)

	ms.AddTool(
		mcp.NewTool("apply_stack",
			mcp.WithDescription("Apply a stack, creating or updating its services from the stored spec."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("stack_id", mcp.Required(), mcp.Description("Stack ID or name")),
		),
		s.handleApplyStack,
	)

	ms.AddTool(
		mcp.NewTool("delete_stack",
			mcp.WithDescription("DESTRUCTIVE — delete a stack and all its managed services. Confirm with the user before calling."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("stack_id", mcp.Required(), mcp.Description("Stack ID or name")),
		),
		s.handleDeleteStack,
	)

	// ── Jobs ──────────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("trigger_job",
			mcp.WithDescription("Manually trigger a job run now."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("job_id", mcp.Required(), mcp.Description("Job ID or name")),
		),
		s.handleTriggerJob,
	)

	// ── Volumes ───────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("create_volume",
			mcp.WithDescription("Create a persistent volume."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Volume name")),
			mcp.WithString("storage_gb", mcp.Description("Storage size in GB (default: 5)")),
		),
		s.handleCreateVolume,
	)

	ms.AddTool(
		mcp.NewTool("attach_volume",
			mcp.WithDescription("Attach a volume to a service at a mount path."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("volume_id", mcp.Required(), mcp.Description("Volume ID or name")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID")),
			mcp.WithString("mount_path", mcp.Required(), mcp.Description("Absolute path inside the container, e.g. /data")),
		),
		s.handleAttachVolume,
	)

	ms.AddTool(
		mcp.NewTool("detach_volume",
			mcp.WithDescription("Detach a volume from its service. Use get_resource(type=volumes) to find the mount_id."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("volume_id", mcp.Required(), mcp.Description("Volume ID or name")),
			mcp.WithString("mount_id", mcp.Required(), mcp.Description("Mount ID from get_resource(type=volumes).mounts[].id")),
		),
		s.handleDetachVolume,
	)

	ms.AddTool(
		mcp.NewTool("delete_volume",
			mcp.WithDescription("DESTRUCTIVE — permanently delete a volume. The volume must not be attached. Confirm with the user before calling."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("volume_id", mcp.Required(), mcp.Description("Volume ID or name")),
		),
		s.handleDeleteVolume,
	)

	// ── Routes ────────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("create_route",
			mcp.WithDescription("Map a hostname to a service for incoming traffic."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("hostname", mcp.Required(), mcp.Description("Full hostname, e.g. app.example.com")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID to route traffic to")),
		),
		s.handleCreateRoute,
	)

	ms.AddTool(
		mcp.NewTool("delete_route",
			mcp.WithDescription("DESTRUCTIVE — remove a route. Confirm with the user before calling."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("route_id", mcp.Required(), mcp.Description("Route ID")),
		),
		s.handleDeleteRoute,
	)

	// ── Jobs (extended) ───────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("create_job",
			mcp.WithDescription("Create a job. Set is_cron=true and schedule for recurring runs."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Job name")),
			mcp.WithString("image", mcp.Required(), mcp.Description("Container image")),
			mcp.WithString("command", mcp.Description("Command to run")),
			mcp.WithString("schedule", mcp.Description("Cron expression, e.g. 0 2 * * *")),
			mcp.WithBoolean("is_cron", mcp.Description("True for a cron job")),
		),
		s.handleCreateJob,
	)

	ms.AddTool(
		mcp.NewTool("delete_job",
			mcp.WithDescription("DESTRUCTIVE — permanently delete a job and its run history. Confirm with the user before calling."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("job_id", mcp.Required(), mcp.Description("Job ID or name")),
		),
		s.handleDeleteJob,
	)

	// ── Nodes ─────────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("delete_node",
			mcp.WithDescription("DESTRUCTIVE — remove a node from Headscale, K3s, and the database. Confirm with the user before calling."),
			mcp.WithString("node_id", mcp.Required(), mcp.Description("Node ID or name")),
		),
		s.handleDeleteNode,
	)
}

// ── Service handlers ──────────────────────────────────────────────────────────

func (s *srv) handleDeployService(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	d, err := s.c.Deploy(s.orgID, projectID, svc.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(map[string]string{"deployment_id": d.ID, "status": d.Status})
}

func (s *srv) handleStartService(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.StartService(s.orgID, projectID, svc.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("service %q starting", svc.Name)), nil
}

func (s *srv) handleStopService(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.StopService(s.orgID, projectID, svc.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("service %q stopped", svc.Name)), nil
}

func (s *srv) handleDeleteService(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.DeleteService(s.orgID, projectID, svc.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("service %q deleted", svc.Name)), nil
}

// ── Stack handlers ────────────────────────────────────────────────────────────

func (s *srv) handleCreateStack(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	name := mcp.ParseString(req, "name", "")
	spec := mcp.ParseString(req, "spec", "")

	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}
	st, err := s.c.CreateStack(s.orgID, projectID, client.CreateStackBody{Name: name, Spec: spec})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPStack{ID: st.ID, Name: st.Name, Status: st.Status, Spec: st.Spec})
}

func (s *srv) handleUpdateStack(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	stackRef := mcp.ParseString(req, "stack_id", "")
	spec := mcp.ParseString(req, "spec", "")

	st, err := s.c.GetStackByName(s.orgID, projectID, stackRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	updated, err := s.c.UpdateStack(s.orgID, projectID, st.ID, client.UpdateStackBody{Spec: spec})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPStack{ID: updated.ID, Name: updated.Name, Status: updated.Status, Spec: updated.Spec})
}

func (s *srv) handleApplyStack(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	stackRef := mcp.ParseString(req, "stack_id", "")

	st, err := s.c.GetStackByName(s.orgID, projectID, stackRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	result, err := s.c.ApplyStack(s.orgID, projectID, st.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(result)
}

func (s *srv) handleDeleteStack(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	stackRef := mcp.ParseString(req, "stack_id", "")

	st, err := s.c.GetStackByName(s.orgID, projectID, stackRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.DeleteStack(s.orgID, projectID, st.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("stack %q deleted", st.Name)), nil
}

// ── Job handlers ──────────────────────────────────────────────────────────────

func (s *srv) handleTriggerJob(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	jobRef := mcp.ParseString(req, "job_id", "")

	j, err := s.c.GetJobByName(s.orgID, projectID, jobRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	run, err := s.c.TriggerJob(s.orgID, projectID, j.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(map[string]string{"run_id": run.ID, "status": run.Status})
}

// ── Volume handlers ───────────────────────────────────────────────────────────

func (s *srv) handleCreateVolume(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	name := mcp.ParseString(req, "name", "")
	storageStr := mcp.ParseString(req, "storage_gb", "5")

	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}
	storageGB := 5
	fmt.Sscanf(storageStr, "%d", &storageGB)

	vol, err := s.c.CreateVolume(s.orgID, projectID, client.CreateVolumeBody{Name: name, StorageGB: storageGB})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPVolume{ID: vol.ID, Name: vol.Name, StorageGB: vol.StorageGB, Status: vol.Status})
}

func (s *srv) handleAttachVolume(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	volumeRef := mcp.ParseString(req, "volume_id", "")
	serviceID := mcp.ParseString(req, "service_id", "")
	mountPath := mcp.ParseString(req, "mount_path", "")

	vol, err := s.c.GetVolumeByName(s.orgID, projectID, volumeRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	mount, err := s.c.AttachVolume(s.orgID, projectID, vol.ID, client.AttachVolumeBody{
		ServiceID: serviceID,
		MountPath: mountPath,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPMount{ID: mount.ID, ServiceID: mount.ServiceID, MountPath: mount.MountPath})
}

func (s *srv) handleDetachVolume(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	volumeRef := mcp.ParseString(req, "volume_id", "")
	mountID := mcp.ParseString(req, "mount_id", "")

	vol, err := s.c.GetVolumeByName(s.orgID, projectID, volumeRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.DetachVolume(s.orgID, projectID, vol.ID, mountID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("volume detached"), nil
}

func (s *srv) handleDeleteVolume(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	volumeRef := mcp.ParseString(req, "volume_id", "")

	vol, err := s.c.GetVolumeByName(s.orgID, projectID, volumeRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.DeleteVolume(s.orgID, projectID, vol.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("volume %q deleted", vol.Name)), nil
}

// ── Route handlers ────────────────────────────────────────────────────────────

func (s *srv) handleCreateRoute(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	hostname := mcp.ParseString(req, "hostname", "")
	serviceID := mcp.ParseString(req, "service_id", "")

	if hostname == "" {
		return mcp.NewToolResultError("hostname is required"), nil
	}
	route, err := s.c.CreateRoute(s.orgID, projectID, client.CreateRouteBody{
		Hostname:  &hostname,
		ServiceID: &serviceID,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	svcID := ""
	if route.ServiceID != nil {
		svcID = *route.ServiceID
	}
	return jsonResult(MCPRoute{ID: route.ID, Hostname: route.Hostname, ServiceID: svcID, Port: route.TargetPort})
}

func (s *srv) handleDeleteRoute(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	routeID := mcp.ParseString(req, "route_id", "")

	if err := s.c.DeleteRoute(s.orgID, projectID, routeID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("route deleted"), nil
}

// ── Project handlers ──────────────────────────────────────────────────────────

func (s *srv) handleCreateProject(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := mcp.ParseString(req, "name", "")
	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}
	p, err := s.c.CreateProject(s.orgID, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPProject{ID: p.ID, Name: p.Name, Slug: p.Slug})
}

func (s *srv) handleDeleteProject(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectRef := mcp.ParseString(req, "project_id", "")
	p, err := s.c.GetProjectBySlugOrID(s.orgID, projectRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.DeleteProject(s.orgID, p.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("project %q deleted", p.Name)), nil
}

// ── Secret handlers ───────────────────────────────────────────────────────────

func (s *srv) handleSetSecret(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	name := mcp.ParseString(req, "name", "")
	value := mcp.ParseString(req, "value", "")

	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}
	sec, err := s.c.SetSecret(s.orgID, projectID, name, value)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPSecret{ID: sec.ID, Name: sec.Name})
}

func (s *srv) handleDeleteSecret(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	name := mcp.ParseString(req, "name", "")

	secrets, err := s.c.ListSecrets(s.orgID, projectID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	for _, sec := range secrets {
		if sec.Name == name || sec.ID == name {
			if err := s.c.DeleteSecret(s.orgID, projectID, sec.ID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("secret %q deleted", sec.Name)), nil
		}
	}
	return mcp.NewToolResultError("secret " + name + " not found"), nil
}

// ── Service env var + deployment handlers ─────────────────────────────────────

func (s *srv) handleSetEnvVars(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	envVars := mcp.ParseString(req, "env_vars", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.SetEnvVars(s.orgID, projectID, svc.ID, envVars); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("env vars updated for service %q", svc.Name)), nil
}

func (s *srv) handleRollbackDeployment(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	deploymentID := mcp.ParseString(req, "deployment_id", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	d, err := s.c.RollbackDeployment(s.orgID, projectID, svc.ID, deploymentID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(map[string]string{"deployment_id": d.ID, "status": d.Status})
}

// ── Job handlers (extended) ───────────────────────────────────────────────────

func (s *srv) handleCreateJob(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	name := mcp.ParseString(req, "name", "")
	image := mcp.ParseString(req, "image", "")
	command := mcp.ParseString(req, "command", "")
	schedule := mcp.ParseString(req, "schedule", "")
	isCron := mcp.ParseBoolean(req, "is_cron", false)

	if name == "" || image == "" {
		return mcp.NewToolResultError("name and image are required"), nil
	}
	j, err := s.c.CreateJob(s.orgID, projectID, client.CreateJobBody{
		Name:     name,
		Image:    image,
		Command:  command,
		Schedule: schedule,
		IsCron:   isCron,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	lastRun := ""
	if j.LastRunAt != nil {
		lastRun = *j.LastRunAt
	}
	return jsonResult(MCPJob{ID: j.ID, Name: j.Name, Schedule: j.Schedule, Status: j.Status, LastRunAt: lastRun})
}

func (s *srv) handleDeleteJob(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	jobRef := mcp.ParseString(req, "job_id", "")

	j, err := s.c.GetJobByName(s.orgID, projectID, jobRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.DeleteJob(s.orgID, projectID, j.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("job %q deleted", j.Name)), nil
}

// ── Service create/update handlers ────────────────────────────────────────────

func (s *srv) handleCreateService(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	name := mcp.ParseString(req, "name", "")

	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}

	body := client.CreateServiceBody{
		Name:          name,
		Image:         mcp.ParseString(req, "image", ""),
		Type:          mcp.ParseString(req, "type", ""),
		Engine:        mcp.ParseString(req, "engine", ""),
		Version:       mcp.ParseString(req, "version", ""),
		GitRepo:       mcp.ParseString(req, "git_repo", ""),
		Branch:        mcp.ParseString(req, "branch", ""),
		Builder:       mcp.ParseString(req, "builder", ""),
		EnvVars:       mcp.ParseString(req, "env_vars", ""),
		CPURequest:    mcp.ParseString(req, "cpu_request", ""),
		CPULimit:      mcp.ParseString(req, "cpu_limit", ""),
		MemoryRequest: mcp.ParseString(req, "memory_request", ""),
		MemoryLimit:   mcp.ParseString(req, "memory_limit", ""),
	}
	replicasStr := mcp.ParseString(req, "replicas", "")
	if replicasStr != "" {
		fmt.Sscanf(replicasStr, "%d", &body.Replicas)
	}
	storageStr := mcp.ParseString(req, "storage_gb", "")
	if storageStr != "" {
		fmt.Sscanf(storageStr, "%d", &body.StorageGB)
	}

	svc, err := s.c.CreateService(s.orgID, projectID, body)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPService{ID: svc.ID, Name: svc.Name, Type: svc.Type, Status: svc.Status, Image: svc.Image})
}

func (s *srv) handleUpdateService(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")

	existing, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	body := client.UpdateServiceBody{}
	if v := mcp.ParseString(req, "name", ""); v != "" {
		body.Name = &v
	}
	if v := mcp.ParseString(req, "image", ""); v != "" {
		body.Image = &v
	}
	if v := mcp.ParseString(req, "cpu_request", ""); v != "" {
		body.CPURequest = &v
	}
	if v := mcp.ParseString(req, "cpu_limit", ""); v != "" {
		body.CPULimit = &v
	}
	if v := mcp.ParseString(req, "memory_request", ""); v != "" {
		body.MemoryRequest = &v
	}
	if v := mcp.ParseString(req, "memory_limit", ""); v != "" {
		body.MemoryLimit = &v
	}
	if v := mcp.ParseString(req, "replicas", ""); v != "" {
		var r int
		fmt.Sscanf(v, "%d", &r)
		body.Replicas = &r
	}

	svc, err := s.c.UpdateService(s.orgID, projectID, existing.ID, body)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPService{ID: svc.ID, Name: svc.Name, Type: svc.Type, Status: svc.Status, Image: svc.Image})
}

func (s *srv) handleUpdateBuildConfig(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	body := client.UpdateBuildConfigBody{}
	if v := mcp.ParseString(req, "git_repo", ""); v != "" {
		body.GitRepo = &v
	}
	if v := mcp.ParseString(req, "branch", ""); v != "" {
		body.Branch = &v
	}
	if v := mcp.ParseString(req, "builder", ""); v != "" {
		body.Builder = &v
	}
	if v := mcp.ParseString(req, "dockerfile_path", ""); v != "" {
		body.DockerfilePath = &v
	}
	// auto_deploy is a bool; only set if explicitly provided (mcp-go returns false as default)
	// We check by parsing the raw argument
	autoDeploy := mcp.ParseBoolean(req, "auto_deploy", false)
	body.AutoDeploy = &autoDeploy

	bc, err := s.c.UpdateBuildConfig(s.orgID, projectID, svc.ID, body)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPBuildConfig{
		Builder:        bc.Builder,
		GitRepo:        bc.GitRepo,
		Branch:         bc.Branch,
		DockerfilePath: bc.DockerfilePath,
		AutoDeploy:     bc.AutoDeploy,
	})
}

func (s *srv) handleSetBuildEnvVars(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	buildEnvVars := mcp.ParseString(req, "build_env_vars", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.SetBuildEnvVars(s.orgID, projectID, svc.ID, buildEnvVars); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("build env vars updated for service %q", svc.Name)), nil
}

// ── Variable group handlers ────────────────────────────────────────────────────

func (s *srv) handleCreateVariableGroup(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	name := mcp.ParseString(req, "name", "")
	description := mcp.ParseString(req, "description", "")

	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}
	g, err := s.c.CreateVariableGroup(s.orgID, projectID, client.CreateVariableGroupBody{Name: name, Description: description})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPVariableGroup{ID: g.ID, Name: g.Name, Description: g.Description})
}

func (s *srv) handleUpdateVariableGroup(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	groupRef := mcp.ParseString(req, "group_id", "")

	existing, err := s.c.GetVariableGroupByName(s.orgID, projectID, groupRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	body := client.UpdateVariableGroupBody{}
	if v := mcp.ParseString(req, "name", ""); v != "" {
		body.Name = &v
	}
	if v := mcp.ParseString(req, "description", ""); v != "" {
		body.Description = &v
	}

	g, err := s.c.UpdateVariableGroup(s.orgID, projectID, existing.ID, body)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPVariableGroup{ID: g.ID, Name: g.Name, Description: g.Description})
}

func (s *srv) handleDeleteVariableGroup(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	groupRef := mcp.ParseString(req, "group_id", "")

	g, err := s.c.GetVariableGroupByName(s.orgID, projectID, groupRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.DeleteVariableGroup(s.orgID, projectID, g.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("variable group %q deleted", g.Name)), nil
}

func (s *srv) handleSetVariable(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	groupRef := mcp.ParseString(req, "group_id", "")
	key := mcp.ParseString(req, "key", "")
	value := mcp.ParseString(req, "value", "")
	isSecret := mcp.ParseBoolean(req, "is_secret", false)

	if key == "" {
		return mcp.NewToolResultError("key is required"), nil
	}
	g, err := s.c.GetVariableGroupByName(s.orgID, projectID, groupRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	item, err := s.c.UpsertVariableItem(s.orgID, projectID, g.ID, client.UpsertVariableItemBody{
		Key:      key,
		Value:    value,
		IsSecret: isSecret,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPVariableGroupItem{ID: item.ID, Key: item.Key, IsSecret: item.IsSecret})
}

func (s *srv) handleDeleteVariable(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	groupRef := mcp.ParseString(req, "group_id", "")
	itemID := mcp.ParseString(req, "item_id", "")

	g, err := s.c.GetVariableGroupByName(s.orgID, projectID, groupRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.DeleteVariableItem(s.orgID, projectID, g.ID, itemID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("variable deleted"), nil
}

func (s *srv) handleAttachVariableGroup(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	groupRef := mcp.ParseString(req, "group_id", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	g, err := s.c.GetVariableGroupByName(s.orgID, projectID, groupRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.AttachVariableGroup(s.orgID, projectID, svc.ID, g.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("variable group %q attached to service %q", g.Name, svc.Name)), nil
}

func (s *srv) handleDetachVariableGroup(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	groupRef := mcp.ParseString(req, "group_id", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	g, err := s.c.GetVariableGroupByName(s.orgID, projectID, groupRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.DetachVariableGroup(s.orgID, projectID, svc.ID, g.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("variable group %q detached from service %q", g.Name, svc.Name)), nil
}

// ── Node handlers ─────────────────────────────────────────────────────────────

func (s *srv) handleDeleteNode(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeRef := mcp.ParseString(req, "node_id", "")

	nodes, err := s.c.ListNodes(s.orgID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	for _, n := range nodes {
		if n.ID == nodeRef || n.Name == nodeRef {
			if err := s.c.DeleteNode(s.orgID, n.ID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("node %q deleted", n.Name)), nil
		}
	}
	return mcp.NewToolResultError("node " + nodeRef + " not found"), nil
}
