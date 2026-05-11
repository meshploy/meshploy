package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"
)

func (s *srv) registerReadTools(ms *mcpsdk.MCPServer) {
	ms.AddTool(
		mcp.NewTool("list_resources",
			mcp.WithDescription("List resources. type=projects|nodes need no project_id; all others require project_id."),
			mcp.WithString("type",
				mcp.Required(),
				mcp.Description("Resource type"),
				mcp.Enum("services", "jobs", "volumes", "stacks", "routes", "projects", "nodes"),
			),
			mcp.WithString("project_id",
				mcp.Description("Project ID — required for services, jobs, volumes, stacks, routes"),
			),
		),
		s.handleListResources,
	)

	ms.AddTool(
		mcp.NewTool("get_resource",
			mcp.WithDescription("Get a single resource by ID or name. Volumes include their mount list."),
			mcp.WithString("type",
				mcp.Required(),
				mcp.Description("Resource type"),
				mcp.Enum("services", "jobs", "volumes", "stacks", "routes", "projects", "nodes"),
			),
			mcp.WithString("id",
				mcp.Required(),
				mcp.Description("Resource ID or name"),
			),
			mcp.WithString("project_id",
				mcp.Description("Project ID — required for services, jobs, volumes, stacks, routes"),
			),
		),
		s.handleGetResource,
	)
}

func (s *srv) handleListResources(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resType := mcp.ParseString(req, "type", "")
	projectID := mcp.ParseString(req, "project_id", "")

	switch resType {
	case "projects":
		items, err := s.c.ListProjects(s.orgID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out := make([]MCPProject, 0, len(items))
		for _, p := range items {
			out = append(out, MCPProject{ID: p.ID, Name: p.Name, Slug: p.Slug})
		}
		return jsonResult(out)

	case "nodes":
		items, err := s.c.ListNodes(s.orgID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out := make([]MCPNode, 0, len(items))
		for _, n := range items {
			out = append(out, MCPNode{ID: n.ID, Name: n.Name, IP: n.TailscaleIP, Status: n.Status, Role: n.K3sRole})
		}
		return jsonResult(out)

	case "services":
		if projectID == "" {
			return mcp.NewToolResultError("project_id is required for type=services"), nil
		}
		items, err := s.c.ListServices(s.orgID, projectID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out := make([]MCPService, 0, len(items))
		for _, svc := range items {
			out = append(out, MCPService{ID: svc.ID, Name: svc.Name, Type: svc.Type, Status: svc.Status, Image: svc.Image})
		}
		return jsonResult(out)

	case "jobs":
		if projectID == "" {
			return mcp.NewToolResultError("project_id is required for type=jobs"), nil
		}
		items, err := s.c.ListJobs(s.orgID, projectID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out := make([]MCPJob, 0, len(items))
		for _, j := range items {
			lastRun := ""
			if j.LastRunAt != nil {
				lastRun = *j.LastRunAt
			}
			out = append(out, MCPJob{ID: j.ID, Name: j.Name, Schedule: j.Schedule, Status: j.Status, LastRunAt: lastRun})
		}
		return jsonResult(out)

	case "volumes":
		if projectID == "" {
			return mcp.NewToolResultError("project_id is required for type=volumes"), nil
		}
		items, err := s.c.ListVolumes(s.orgID, projectID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out := make([]MCPVolume, 0, len(items))
		for _, v := range items {
			out = append(out, MCPVolume{ID: v.ID, Name: v.Name, StorageGB: v.StorageGB, Status: v.Status})
		}
		return jsonResult(out)

	case "stacks":
		if projectID == "" {
			return mcp.NewToolResultError("project_id is required for type=stacks"), nil
		}
		items, err := s.c.ListStacks(s.orgID, projectID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out := make([]MCPStack, 0, len(items))
		for _, st := range items {
			out = append(out, MCPStack{ID: st.ID, Name: st.Name, Status: st.Status})
		}
		return jsonResult(out)

	case "routes":
		if projectID == "" {
			return mcp.NewToolResultError("project_id is required for type=routes"), nil
		}
		items, err := s.c.ListRoutes(s.orgID, projectID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out := make([]MCPRoute, 0, len(items))
		for _, r := range items {
			svcID := ""
			if r.ServiceID != nil {
				svcID = *r.ServiceID
			}
			out = append(out, MCPRoute{ID: r.ID, Hostname: r.Hostname, ServiceID: svcID, Port: r.TargetPort})
		}
		return jsonResult(out)

	default:
		return mcp.NewToolResultError("unknown type: " + resType), nil
	}
}

func (s *srv) handleGetResource(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resType := mcp.ParseString(req, "type", "")
	id := mcp.ParseString(req, "id", "")
	projectID := mcp.ParseString(req, "project_id", "")

	if id == "" {
		return mcp.NewToolResultError("id is required"), nil
	}

	switch resType {
	case "projects":
		p, err := s.c.GetProjectBySlugOrID(s.orgID, id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(MCPProject{ID: p.ID, Name: p.Name, Slug: p.Slug})

	case "nodes":
		nodes, err := s.c.ListNodes(s.orgID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		for _, n := range nodes {
			if n.ID == id || n.Name == id {
				return jsonResult(MCPNode{ID: n.ID, Name: n.Name, IP: n.TailscaleIP, Status: n.Status, Role: n.K3sRole})
			}
		}
		return mcp.NewToolResultError("node " + id + " not found"), nil

	case "services":
		if projectID == "" {
			return mcp.NewToolResultError("project_id is required for type=services"), nil
		}
		svc, err := s.c.GetServiceByName(s.orgID, projectID, id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(MCPService{ID: svc.ID, Name: svc.Name, Type: svc.Type, Status: svc.Status, Image: svc.Image})

	case "jobs":
		if projectID == "" {
			return mcp.NewToolResultError("project_id is required for type=jobs"), nil
		}
		j, err := s.c.GetJobByName(s.orgID, projectID, id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		lastRun := ""
		if j.LastRunAt != nil {
			lastRun = *j.LastRunAt
		}
		return jsonResult(MCPJob{ID: j.ID, Name: j.Name, Schedule: j.Schedule, Status: j.Status, LastRunAt: lastRun})

	case "volumes":
		if projectID == "" {
			return mcp.NewToolResultError("project_id is required for type=volumes"), nil
		}
		vol, err := s.c.GetVolumeByName(s.orgID, projectID, id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		mounts, err := s.c.ListVolumeMounts(s.orgID, projectID, vol.ID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		mcpMounts := make([]MCPMount, 0, len(mounts))
		for _, m := range mounts {
			mcpMounts = append(mcpMounts, MCPMount{ID: m.ID, ServiceID: m.ServiceID, MountPath: m.MountPath})
		}
		return jsonResult(MCPVolume{ID: vol.ID, Name: vol.Name, StorageGB: vol.StorageGB, Status: vol.Status, Mounts: mcpMounts})

	case "stacks":
		if projectID == "" {
			return mcp.NewToolResultError("project_id is required for type=stacks"), nil
		}
		st, err := s.c.GetStackByName(s.orgID, projectID, id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(MCPStack{ID: st.ID, Name: st.Name, Status: st.Status, Spec: st.Spec})

	case "routes":
		if projectID == "" {
			return mcp.NewToolResultError("project_id is required for type=routes"), nil
		}
		routes, err := s.c.ListRoutes(s.orgID, projectID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		for _, r := range routes {
			if r.ID == id || r.Hostname == id {
				svcID := ""
				if r.ServiceID != nil {
					svcID = *r.ServiceID
				}
				return jsonResult(MCPRoute{ID: r.ID, Hostname: r.Hostname, ServiceID: svcID, Port: r.TargetPort})
			}
		}
		return mcp.NewToolResultError("route " + id + " not found"), nil

	default:
		return mcp.NewToolResultError("unknown type: " + resType), nil
	}
}
