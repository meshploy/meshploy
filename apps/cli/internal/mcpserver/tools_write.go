package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"
	"github.com/meshploy/apps/cli/internal/client"
)

func (s *srv) registerWriteTools(ms *mcpsdk.MCPServer) {
	// ── Services ──────────────────────────────────────────────────────────────

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
