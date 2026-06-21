package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"
)

func (s *srv) registerReadToolsExtended(ms *mcpsdk.MCPServer) {
	// ── Org members & invitations ─────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("list_org_members",
			mcp.WithDescription("List all members of the organization with their roles."),
		),
		s.handleListOrgMembers,
	)

	ms.AddTool(
		mcp.NewTool("list_invitations",
			mcp.WithDescription("List pending invitations for the organization."),
		),
		s.handleListInvitations,
	)

	// ── Permissions ───────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("list_member_permissions",
			mcp.WithDescription("List all resource permission grants for a specific member."),
			mcp.WithString("user_id", mcp.Required(), mcp.Description("User ID")),
		),
		s.handleListMemberPermissions,
	)

	ms.AddTool(
		mcp.NewTool("list_resource_permissions",
			mcp.WithDescription("List all users who have explicit permissions on a resource."),
			mcp.WithString("resource_type", mcp.Required(), mcp.Description("Resource type: project, service, stack, or job"),
				mcp.Enum("project", "service", "stack", "job")),
			mcp.WithString("resource_id", mcp.Required(), mcp.Description("Resource ID")),
		),
		s.handleListResourcePermissions,
	)

	// ── Backups ───────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("list_backup_configs",
			mcp.WithDescription("List backup configurations for a service."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
		),
		s.handleListBackupConfigs,
	)

	ms.AddTool(
		mcp.NewTool("list_backup_objects",
			mcp.WithDescription("List available backup objects (files) for a backup config."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("backup_config_id", mcp.Required(), mcp.Description("Backup config ID")),
		),
		s.handleListBackupObjects,
	)

	ms.AddTool(
		mcp.NewTool("get_system_backup",
			mcp.WithDescription("Get the system-wide (database + volumes) backup configuration."),
		),
		s.handleGetSystemBackup,
	)

	ms.AddTool(
		mcp.NewTool("list_system_backup_objects",
			mcp.WithDescription("List available system backup objects."),
		),
		s.handleListSystemBackupObjects,
	)

	// ── Notifications ─────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("list_notification_channels",
			mcp.WithDescription("List all notification channels (Slack, Discord, email, webhook)."),
		),
		s.handleListNotificationChannels,
	)

	// ── Domains ───────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("list_domains",
			mcp.WithDescription("List custom domains registered to the organization."),
		),
		s.handleListDomains,
	)

	// ── Node tokens ───────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("get_node_registration_token",
			mcp.WithDescription("Get the current node registration token (mreg-…) used when adding worker nodes."),
		),
		s.handleGetNodeRegistrationToken,
	)

	// ── Service details ───────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("list_service_pods",
			mcp.WithDescription("List running pods for a service. Requires K8s connectivity."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
		),
		s.handleListServicePods,
	)

	ms.AddTool(
		mcp.NewTool("get_database_config",
			mcp.WithDescription("Get the database configuration for a database-type service (engine, version, credentials)."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
		),
		s.handleGetDatabaseConfig,
	)

	ms.AddTool(
		mcp.NewTool("db_schema",
			mcp.WithDescription("Get the database schema (tables and columns) for a database-type service."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
		),
		s.handleDBSchema,
	)

	// ── Deployments ───────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("get_deployment",
			mcp.WithDescription("Get details of a specific deployment. Use list_deployments to find the deployment_id."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("deployment_id", mcp.Required(), mcp.Description("Deployment ID")),
		),
		s.handleGetDeployment,
	)

	// ── Git integrations ──────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("list_git_integrations",
			mcp.WithDescription("List configured git integrations (GitHub, GitLab, Gitea, Bitbucket)."),
		),
		s.handleListGitIntegrations,
	)

	ms.AddTool(
		mcp.NewTool("list_git_repos",
			mcp.WithDescription("List repositories accessible via a git integration."),
			mcp.WithString("integration_id", mcp.Required(), mcp.Description("Git integration ID")),
		),
		s.handleListGitRepos,
	)

	ms.AddTool(
		mcp.NewTool("list_git_branches",
			mcp.WithDescription("List branches for a repository via a git integration."),
			mcp.WithString("integration_id", mcp.Required(), mcp.Description("Git integration ID")),
			mcp.WithString("repo_url", mcp.Required(), mcp.Description("Repository URL or full_name from list_git_repos")),
		),
		s.handleListGitBranches,
	)

	// ── Registry & storage integrations ──────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("list_registry_integrations",
			mcp.WithDescription("List container registry integrations."),
		),
		s.handleListRegistryIntegrations,
	)

	ms.AddTool(
		mcp.NewTool("list_storage_integrations",
			mcp.WithDescription("List S3-compatible storage integrations used for backups."),
		),
		s.handleListStorageIntegrations,
	)

	// ── Stacks ────────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("list_stack_services",
			mcp.WithDescription("List all services managed by a stack."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("stack_id", mcp.Required(), mcp.Description("Stack ID or name")),
		),
		s.handleListStackServices,
	)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *srv) handleListOrgMembers(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	members, err := s.c.ListMembers(s.orgID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPMember, 0, len(members))
	for _, m := range members {
		out = append(out, MCPMember{UserID: m.UserID, Role: m.Role, UserName: m.UserName, UserEmail: m.UserEmail})
	}
	return jsonResult(out)
}

func (s *srv) handleListInvitations(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	invs, err := s.c.ListInvitations(s.orgID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPInvitation, 0, len(invs))
	for _, inv := range invs {
		out = append(out, MCPInvitation{ID: inv.ID, Email: inv.Email, Role: inv.Role, ExpiresAt: inv.ExpiresAt})
	}
	return jsonResult(out)
}

func (s *srv) handleListMemberPermissions(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := mcp.ParseString(req, "user_id", "")
	perms, err := s.c.ListMemberPermissions(s.orgID, userID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPPermission, 0, len(perms))
	for _, p := range perms {
		out = append(out, MCPPermission{
			ID:           p.ID,
			ResourceType: p.ResourceType,
			ResourceID:   p.ResourceID,
			Action:       p.Action,
			ResourceName: p.ResourceName,
		})
	}
	return jsonResult(out)
}

func (s *srv) handleListResourcePermissions(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resourceType := mcp.ParseString(req, "resource_type", "")
	resourceID := mcp.ParseString(req, "resource_id", "")
	perms, err := s.c.ListResourcePermissions(s.orgID, resourceType, resourceID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPPermissionsWithUser, 0, len(perms))
	for _, p := range perms {
		out = append(out, MCPPermissionsWithUser{UserID: p.UserID, UserName: p.UserName, UserEmail: p.UserEmail, Action: p.Action})
	}
	return jsonResult(out)
}

func (s *srv) handleListBackupConfigs(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	configs, err := s.c.ListBackupConfigs(s.orgID, projectID, svc.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPBackupConfig, 0, len(configs))
	for _, bc := range configs {
		out = append(out, MCPBackupConfig{
			ID:                   bc.ID,
			StorageIntegrationID: bc.StorageIntegrationID,
			Schedule:             bc.Schedule,
			RetentionDays:        bc.RetentionDays,
			Enabled:              bc.Enabled,
		})
	}
	return jsonResult(out)
}

func (s *srv) handleListBackupObjects(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	configID := mcp.ParseString(req, "backup_config_id", "")
	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	objects, err := s.c.ListBackupObjects(s.orgID, projectID, svc.ID, configID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPBackupObject, 0, len(objects))
	for _, o := range objects {
		out = append(out, MCPBackupObject{Key: o.Key, Size: o.Size, LastModified: o.LastModified})
	}
	return jsonResult(out)
}

func (s *srv) handleGetSystemBackup(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sb, err := s.c.GetSystemBackup(s.orgID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(map[string]any{
		"id":                     sb.ID,
		"storage_integration_id": sb.StorageIntegrationID,
		"schedule":               sb.Schedule,
		"retention_days":         sb.RetentionDays,
		"enabled":                sb.Enabled,
	})
}

func (s *srv) handleListSystemBackupObjects(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	objects, err := s.c.ListSystemBackupObjects(s.orgID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPBackupObject, 0, len(objects))
	for _, o := range objects {
		out = append(out, MCPBackupObject{Key: o.Key, Size: o.Size, LastModified: o.LastModified})
	}
	return jsonResult(out)
}

func (s *srv) handleListNotificationChannels(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	channels, err := s.c.ListNotificationChannels(s.orgID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPNotificationChannel, 0, len(channels))
	for _, ch := range channels {
		out = append(out, MCPNotificationChannel{ID: ch.ID, Name: ch.Name, Type: ch.Type, Enabled: ch.Enabled, Events: ch.Events})
	}
	return jsonResult(out)
}

func (s *srv) handleListDomains(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	domains, err := s.c.ListDomains(s.orgID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPDomain, 0, len(domains))
	for _, d := range domains {
		out = append(out, MCPDomain{ID: d.ID, Domain: d.Domain, Verified: d.Verified})
	}
	return jsonResult(out)
}

func (s *srv) handleGetNodeRegistrationToken(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	tok, err := s.c.GetNodeRegistrationToken(s.orgID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPRegistrationToken{Token: tok.Token, ExpiresAt: tok.ExpiresAt})
}

func (s *srv) handleListServicePods(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	pods, err := s.c.ListPods(s.orgID, projectID, svc.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPPod, 0, len(pods))
	for _, p := range pods {
		out = append(out, MCPPod{Name: p.Name, Phase: p.Phase, Ready: p.Ready, Restarts: p.Restarts, NodeName: p.NodeName, StartedAt: p.StartedAt})
	}
	return jsonResult(out)
}

func (s *srv) handleGetDatabaseConfig(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	dc, err := s.c.GetDatabaseConfig(s.orgID, projectID, svc.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPDatabaseConfig{Engine: dc.Engine, Version: dc.Version, StorageGB: dc.StorageGB, DBName: dc.DBName, DBUser: dc.DBUser})
}

func (s *srv) handleDBSchema(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	tables, err := s.c.DBSchema(s.orgID, projectID, svc.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(tables)
}

func (s *srv) handleGetDeployment(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	deploymentID := mcp.ParseString(req, "deployment_id", "")
	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	d, err := s.c.GetDeployment(s.orgID, projectID, svc.ID, deploymentID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	deployedAt := ""
	if d.DeployedAt != nil {
		deployedAt = *d.DeployedAt
	}
	return jsonResult(MCPDeployment{ID: d.ID, Status: d.Status, Image: d.Image, DeployedAt: deployedAt})
}

func (s *srv) handleListGitIntegrations(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	items, err := s.c.ListGitIntegrations(s.orgID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPGitIntegration, 0, len(items))
	for _, g := range items {
		out = append(out, MCPGitIntegration{ID: g.ID, Name: g.Name, Provider: g.Provider, AuthMethod: g.AuthMethod, Connected: g.Connected})
	}
	return jsonResult(out)
}

func (s *srv) handleListGitRepos(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	integrationID := mcp.ParseString(req, "integration_id", "")
	repos, err := s.c.ListRepos(s.orgID, integrationID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(repos)
}

func (s *srv) handleListGitBranches(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	integrationID := mcp.ParseString(req, "integration_id", "")
	repoURL := mcp.ParseString(req, "repo_url", "")
	branches, err := s.c.ListBranches(s.orgID, integrationID, repoURL)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(branches)
}

func (s *srv) handleListRegistryIntegrations(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	items, err := s.c.ListRegistryIntegrations(s.orgID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPRegistryIntegration, 0, len(items))
	for _, r := range items {
		out = append(out, MCPRegistryIntegration{ID: r.ID, Name: r.Name, Provider: r.Provider, Namespace: r.Namespace, Endpoint: r.Endpoint})
	}
	return jsonResult(out)
}

func (s *srv) handleListStorageIntegrations(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	items, err := s.c.ListStorageIntegrations(s.orgID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPStorageIntegration, 0, len(items))
	for _, st := range items {
		out = append(out, MCPStorageIntegration{ID: st.ID, Name: st.Name, Provider: st.Provider, Endpoint: st.Endpoint, Region: st.Region, Bucket: st.Bucket})
	}
	return jsonResult(out)
}

func (s *srv) handleListStackServices(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	stackRef := mcp.ParseString(req, "stack_id", "")
	st, err := s.c.GetStackByName(s.orgID, projectID, stackRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	services, err := s.c.ListStackServices(s.orgID, projectID, st.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]MCPService, 0, len(services))
	for _, svc := range services {
		out = append(out, MCPService{ID: svc.ID, Name: svc.Name, Type: svc.Type, Status: svc.Status, Image: svc.Image})
	}
	return jsonResult(out)
}

// suppress unused import warning
var _ = fmt.Sprintf
