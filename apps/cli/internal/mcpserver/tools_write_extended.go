package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"
	"github.com/meshploy/apps/cli/internal/client"
)

func (s *srv) registerWriteToolsExtended(ms *mcpsdk.MCPServer) {
	// ── Projects ──────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("update_project",
			mcp.WithDescription("Rename a project."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID or slug")),
			mcp.WithString("name", mcp.Required(), mcp.Description("New project name")),
		),
		s.handleUpdateProject,
	)

	// ── Org members & invitations ─────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("invite_member",
			mcp.WithDescription("Send an invitation email to add a new user to the organization."),
			mcp.WithString("email", mcp.Required(), mcp.Description("Email address of the person to invite")),
			mcp.WithString("role", mcp.Required(), mcp.Description("Role to assign: admin or member"),
				mcp.Enum("admin", "member")),
		),
		s.handleInviteMember,
	)

	ms.AddTool(
		mcp.NewTool("update_member_role",
			mcp.WithDescription("DESTRUCTIVE — change a member's role. Confirm with user before calling."),
			mcp.WithString("user_id", mcp.Required(), mcp.Description("User ID from list_org_members")),
			mcp.WithString("role", mcp.Required(), mcp.Description("New role: admin or member"),
				mcp.Enum("admin", "member")),
		),
		s.handleUpdateMemberRole,
	)

	ms.AddTool(
		mcp.NewTool("remove_org_member",
			mcp.WithDescription("DESTRUCTIVE — remove a member from the organization. Confirm with user before calling."),
			mcp.WithString("user_id", mcp.Required(), mcp.Description("User ID from list_org_members")),
		),
		s.handleRemoveOrgMember,
	)

	// ── Permissions ───────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("grant_permission",
			mcp.WithDescription("Grant a member explicit access to a specific resource."),
			mcp.WithString("user_id", mcp.Required(), mcp.Description("User ID")),
			mcp.WithString("resource_type", mcp.Required(), mcp.Description("Resource type"),
				mcp.Enum("project", "service", "stack", "job")),
			mcp.WithString("resource_id", mcp.Required(), mcp.Description("Resource ID")),
			mcp.WithString("action", mcp.Required(), mcp.Description("Permission action"),
				mcp.Enum("view", "deploy", "create", "update", "delete")),
		),
		s.handleGrantPermission,
	)

	ms.AddTool(
		mcp.NewTool("revoke_permission",
			mcp.WithDescription("DESTRUCTIVE — revoke a specific permission from a member. Confirm with user before calling."),
			mcp.WithString("user_id", mcp.Required(), mcp.Description("User ID")),
			mcp.WithString("resource_type", mcp.Required(), mcp.Description("Resource type"),
				mcp.Enum("project", "service", "stack", "job")),
			mcp.WithString("resource_id", mcp.Required(), mcp.Description("Resource ID")),
			mcp.WithString("action", mcp.Required(), mcp.Description("Permission action"),
				mcp.Enum("view", "deploy", "create", "update", "delete")),
		),
		s.handleRevokePermission,
	)

	// ── Backups ───────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("create_backup_config",
			mcp.WithDescription("Configure automated backups for a database service."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("storage_integration_id", mcp.Required(), mcp.Description("Storage integration ID (from list_storage_integrations)")),
			mcp.WithString("schedule", mcp.Required(), mcp.Description("Cron schedule, e.g. 0 2 * * *")),
			mcp.WithString("retention_days", mcp.Description("Days to retain backups (default: 30)")),
			mcp.WithString("path_prefix", mcp.Description("S3 path prefix for backup files")),
		),
		s.handleCreateBackupConfig,
	)

	ms.AddTool(
		mcp.NewTool("delete_backup_config",
			mcp.WithDescription("DESTRUCTIVE — delete a backup configuration. Confirm with user before calling."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("backup_config_id", mcp.Required(), mcp.Description("Backup config ID")),
		),
		s.handleDeleteBackupConfig,
	)

	ms.AddTool(
		mcp.NewTool("trigger_backup",
			mcp.WithDescription("Trigger an immediate backup for a service."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("backup_config_id", mcp.Required(), mcp.Description("Backup config ID")),
		),
		s.handleTriggerBackup,
	)

	ms.AddTool(
		mcp.NewTool("restore_backup",
			mcp.WithDescription("DESTRUCTIVE — restore a service from a backup object. Confirm with user before calling. Use list_backup_objects to find the key."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("backup_config_id", mcp.Required(), mcp.Description("Backup config ID")),
			mcp.WithString("key", mcp.Required(), mcp.Description("Backup object key from list_backup_objects")),
		),
		s.handleRestoreBackup,
	)

	ms.AddTool(
		mcp.NewTool("upsert_system_backup",
			mcp.WithDescription("Configure the system-wide backup (entire database + volumes)."),
			mcp.WithString("storage_integration_id", mcp.Required(), mcp.Description("Storage integration ID")),
			mcp.WithString("schedule", mcp.Required(), mcp.Description("Cron schedule, e.g. 0 3 * * *")),
			mcp.WithBoolean("enabled", mcp.Description("Enable the backup schedule")),
			mcp.WithString("retention_days", mcp.Description("Days to retain backups")),
		),
		s.handleUpsertSystemBackup,
	)

	ms.AddTool(
		mcp.NewTool("trigger_system_backup",
			mcp.WithDescription("Trigger an immediate system-wide backup."),
		),
		s.handleTriggerSystemBackup,
	)

	ms.AddTool(
		mcp.NewTool("restore_system_backup",
			mcp.WithDescription("DESTRUCTIVE — restore the entire system from a system backup. Confirm with user before calling. Use list_system_backup_objects to find the key."),
			mcp.WithString("key", mcp.Required(), mcp.Description("Backup object key from list_system_backup_objects")),
		),
		s.handleRestoreSystemBackup,
	)

	// ── Notifications ─────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("create_notification_channel",
			mcp.WithDescription("Create a notification channel. Type is one of: slack, discord, email, webhook. Config keys depend on the type (e.g. {url} for slack/discord, {webhook_url,secret} for webhook, {to,smtp_host,...} for email)."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Channel name")),
			mcp.WithString("type", mcp.Required(), mcp.Description("Channel type"),
				mcp.Enum("slack", "discord", "email", "webhook")),
			mcp.WithString("webhook_url", mcp.Description("Webhook URL (for slack, discord, webhook types)")),
			mcp.WithString("events", mcp.Description("Comma-separated event names to subscribe to, e.g. deploy.success,deploy.failure")),
		),
		s.handleCreateNotificationChannel,
	)

	ms.AddTool(
		mcp.NewTool("delete_notification_channel",
			mcp.WithDescription("DESTRUCTIVE — delete a notification channel. Confirm with user before calling."),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID from list_notification_channels")),
		),
		s.handleDeleteNotificationChannel,
	)

	// ── Node tokens ───────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("generate_node_registration_token",
			mcp.WithDescription("Rotate the node registration token (mreg-…). The old token is immediately invalidated."),
		),
		s.handleGenerateNodeRegistrationToken,
	)

	ms.AddTool(
		mcp.NewTool("create_provisioning_token",
			mcp.WithDescription("Create a single-use node provisioning token (mprov-…) for automated node setup."),
			mcp.WithString("label", mcp.Required(), mcp.Description("Human-readable label for this token")),
		),
		s.handleCreateProvisioningToken,
	)

	// ── Jobs ──────────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("update_job",
			mcp.WithDescription("Update a job's name, image, schedule, or resource limits."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("job_id", mcp.Required(), mcp.Description("Job ID or name")),
			mcp.WithString("name", mcp.Description("New job name")),
			mcp.WithString("image", mcp.Description("New container image")),
			mcp.WithString("schedule", mcp.Description("New cron schedule, e.g. 0 4 * * *")),
			mcp.WithString("command", mcp.Description("New command to run")),
			mcp.WithString("env_vars", mcp.Description("Job env vars as KEY=VALUE lines")),
		),
		s.handleUpdateJob,
	)

	// ── Stacks ────────────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("sync_stack",
			mcp.WithDescription("Sync a stack from its git source, pulling the latest spec and applying changes."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("stack_id", mcp.Required(), mcp.Description("Stack ID or name")),
		),
		s.handleSyncStack,
	)

	// ── DB Explorer ───────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("db_query",
			mcp.WithDescription("Run a SQL query against a database-type service. Set read_only=true for SELECT queries (default). Set read_only=false for mutations — use with care."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("query", mcp.Required(), mcp.Description("SQL query to execute")),
			mcp.WithBoolean("read_only", mcp.Description("Set to false to allow INSERT/UPDATE/DELETE (default: true)")),
		),
		s.handleDBQuery,
	)

	// ── Deployments ───────────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("cancel_deployment",
			mcp.WithDescription("Cancel an in-progress deployment."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("deployment_id", mcp.Required(), mcp.Description("Deployment ID")),
		),
		s.handleCancelDeployment,
	)

	ms.AddTool(
		mcp.NewTool("retry_deployment",
			mcp.WithDescription("Retry a failed deployment."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("deployment_id", mcp.Required(), mcp.Description("Deployment ID")),
		),
		s.handleRetryDeployment,
	)

	// ── Git integrations ──────────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("create_git_integration",
			mcp.WithDescription("Connect a git provider using a Personal Access Token (PAT). Provider is one of: github, gitlab, gitea, bitbucket."),
			mcp.WithString("provider", mcp.Required(), mcp.Description("Git provider"),
				mcp.Enum("github", "gitlab", "gitea", "bitbucket")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Display name for this integration")),
			mcp.WithString("token", mcp.Required(), mcp.Description("Personal access token")),
			mcp.WithString("base_url", mcp.Description("Base URL for self-hosted providers (e.g. https://gitlab.example.com)")),
		),
		s.handleCreateGitIntegration,
	)

	ms.AddTool(
		mcp.NewTool("delete_git_integration",
			mcp.WithDescription("DESTRUCTIVE — delete a git integration. Services using it for builds will lose auto-deploy. Confirm with user before calling."),
			mcp.WithString("integration_id", mcp.Required(), mcp.Description("Git integration ID")),
		),
		s.handleDeleteGitIntegration,
	)

	// ── Registry integrations ─────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("create_registry_integration",
			mcp.WithDescription("Connect a container registry (Docker Hub, GHCR, ECR, or custom). Used for pushing built images and pulling private images."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Display name")),
			mcp.WithString("provider", mcp.Required(), mcp.Description("Registry provider"),
				mcp.Enum("dockerhub", "ghcr", "ecr", "custom")),
			mcp.WithString("username", mcp.Required(), mcp.Description("Registry username")),
			mcp.WithString("password", mcp.Required(), mcp.Description("Registry password or token")),
			mcp.WithString("namespace", mcp.Description("Registry namespace (org or username)")),
			mcp.WithString("endpoint", mcp.Description("Registry endpoint URL (for custom providers)")),
		),
		s.handleCreateRegistryIntegration,
	)

	ms.AddTool(
		mcp.NewTool("delete_registry_integration",
			mcp.WithDescription("DESTRUCTIVE — delete a registry integration. Confirm with user before calling."),
			mcp.WithString("integration_id", mcp.Required(), mcp.Description("Registry integration ID")),
		),
		s.handleDeleteRegistryIntegration,
	)

	// ── Storage integrations ──────────────────────────────────────────────────

	ms.AddTool(
		mcp.NewTool("create_storage_integration",
			mcp.WithDescription("Connect an S3-compatible storage provider for backups (AWS S3, Cloudflare R2, MinIO)."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Display name")),
			mcp.WithString("provider", mcp.Required(), mcp.Description("Storage provider"),
				mcp.Enum("s3", "r2", "minio", "custom")),
			mcp.WithString("bucket", mcp.Required(), mcp.Description("Bucket name")),
			mcp.WithString("access_key_id", mcp.Required(), mcp.Description("Access key ID")),
			mcp.WithString("secret_access_key", mcp.Required(), mcp.Description("Secret access key")),
			mcp.WithString("region", mcp.Description("Region (e.g. us-east-1)")),
			mcp.WithString("endpoint", mcp.Description("Endpoint URL (for R2 / MinIO / custom)")),
		),
		s.handleCreateStorageIntegration,
	)

	ms.AddTool(
		mcp.NewTool("delete_storage_integration",
			mcp.WithDescription("DESTRUCTIVE — delete a storage integration. Backup configs using it will stop working. Confirm with user before calling."),
			mcp.WithString("integration_id", mcp.Required(), mcp.Description("Storage integration ID")),
		),
		s.handleDeleteStorageIntegration,
	)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *srv) handleUpdateProject(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectRef := mcp.ParseString(req, "project_id", "")
	name := mcp.ParseString(req, "name", "")
	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}
	p, err := s.c.GetProjectBySlugOrID(s.orgID, projectRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	updated, err := s.c.UpdateProject(s.orgID, p.ID, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPProject{ID: updated.ID, Name: updated.Name, Slug: updated.Slug})
}

func (s *srv) handleInviteMember(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	email := mcp.ParseString(req, "email", "")
	role := mcp.ParseString(req, "role", "member")
	if email == "" {
		return mcp.NewToolResultError("email is required"), nil
	}
	inv, err := s.c.CreateInvitation(s.orgID, email, role)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPInvitation{ID: inv.ID, Email: inv.Email, Role: inv.Role, ExpiresAt: inv.ExpiresAt})
}

func (s *srv) handleUpdateMemberRole(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := mcp.ParseString(req, "user_id", "")
	role := mcp.ParseString(req, "role", "")
	if err := s.c.UpdateMemberRole(s.orgID, userID, role); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("member %s role updated to %s", userID, role)), nil
}

func (s *srv) handleRemoveOrgMember(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := mcp.ParseString(req, "user_id", "")
	if err := s.c.RemoveMember(s.orgID, userID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("member %s removed from org", userID)), nil
}

func (s *srv) handleGrantPermission(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := mcp.ParseString(req, "user_id", "")
	body := client.GrantPermissionBody{
		ResourceType: mcp.ParseString(req, "resource_type", ""),
		ResourceID:   mcp.ParseString(req, "resource_id", ""),
		Action:       mcp.ParseString(req, "action", ""),
	}
	if err := s.c.GrantPermission(s.orgID, userID, body); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("permission %s on %s %s granted to %s", body.Action, body.ResourceType, body.ResourceID, userID)), nil
}

func (s *srv) handleRevokePermission(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := mcp.ParseString(req, "user_id", "")
	body := client.GrantPermissionBody{
		ResourceType: mcp.ParseString(req, "resource_type", ""),
		ResourceID:   mcp.ParseString(req, "resource_id", ""),
		Action:       mcp.ParseString(req, "action", ""),
	}
	if err := s.c.RevokePermission(s.orgID, userID, body); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("permission %s on %s %s revoked from %s", body.Action, body.ResourceType, body.ResourceID, userID)), nil
}

func (s *srv) handleCreateBackupConfig(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	body := client.CreateBackupConfigBody{
		StorageIntegrationID: mcp.ParseString(req, "storage_integration_id", ""),
		Schedule:             mcp.ParseString(req, "schedule", ""),
		PathPrefix:           mcp.ParseString(req, "path_prefix", ""),
	}
	if v := mcp.ParseString(req, "retention_days", ""); v != "" {
		fmt.Sscanf(v, "%d", &body.RetentionDays)
	}
	bc, err := s.c.CreateBackupConfig(s.orgID, projectID, svc.ID, body)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPBackupConfig{ID: bc.ID, StorageIntegrationID: bc.StorageIntegrationID, Schedule: bc.Schedule, RetentionDays: bc.RetentionDays, Enabled: bc.Enabled})
}

func (s *srv) handleDeleteBackupConfig(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	configID := mcp.ParseString(req, "backup_config_id", "")
	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.DeleteBackupConfig(s.orgID, projectID, svc.ID, configID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("backup config deleted"), nil
}

func (s *srv) handleTriggerBackup(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	configID := mcp.ParseString(req, "backup_config_id", "")
	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.TriggerBackup(s.orgID, projectID, svc.ID, configID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("backup triggered"), nil
}

func (s *srv) handleRestoreBackup(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	configID := mcp.ParseString(req, "backup_config_id", "")
	key := mcp.ParseString(req, "key", "")
	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.RestoreBackup(s.orgID, projectID, svc.ID, configID, key); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("restore started"), nil
}

func (s *srv) handleUpsertSystemBackup(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	body := client.UpsertSystemBackupBody{
		StorageIntegrationID: mcp.ParseString(req, "storage_integration_id", ""),
		Schedule:             mcp.ParseString(req, "schedule", ""),
		Enabled:              mcp.ParseBoolean(req, "enabled", false),
	}
	if v := mcp.ParseString(req, "retention_days", ""); v != "" {
		fmt.Sscanf(v, "%d", &body.RetentionDays)
	}
	sb, err := s.c.UpsertSystemBackup(s.orgID, body)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(map[string]any{
		"id":      sb.ID,
		"enabled": sb.Enabled,
		"schedule": sb.Schedule,
	})
}

func (s *srv) handleTriggerSystemBackup(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.c.TriggerSystemBackup(s.orgID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("system backup triggered"), nil
}

func (s *srv) handleRestoreSystemBackup(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := mcp.ParseString(req, "key", "")
	if err := s.c.RestoreSystemBackup(s.orgID, key); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("system restore started"), nil
}

func (s *srv) handleCreateNotificationChannel(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := mcp.ParseString(req, "name", "")
	channelType := mcp.ParseString(req, "type", "")
	webhookURL := mcp.ParseString(req, "webhook_url", "")
	eventsStr := mcp.ParseString(req, "events", "")

	config := map[string]string{}
	if webhookURL != "" {
		config["url"] = webhookURL
	}

	var events []string
	if eventsStr != "" {
		for _, e := range splitComma(eventsStr) {
			if e != "" {
				events = append(events, e)
			}
		}
	}

	ch, err := s.c.CreateNotificationChannel(s.orgID, client.CreateNotificationBody{
		Name:   name,
		Type:   channelType,
		Config: config,
		Events: events,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPNotificationChannel{ID: ch.ID, Name: ch.Name, Type: ch.Type, Enabled: ch.Enabled, Events: ch.Events})
}

func (s *srv) handleDeleteNotificationChannel(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	channelID := mcp.ParseString(req, "channel_id", "")
	if err := s.c.DeleteNotificationChannel(s.orgID, channelID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("notification channel deleted"), nil
}

func (s *srv) handleGenerateNodeRegistrationToken(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	tok, err := s.c.GenerateNodeRegistrationToken(s.orgID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPRegistrationToken{Token: tok.Token, ExpiresAt: tok.ExpiresAt})
}

func (s *srv) handleCreateProvisioningToken(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	label := mcp.ParseString(req, "label", "")
	if label == "" {
		return mcp.NewToolResultError("label is required"), nil
	}
	tok, err := s.c.CreateProvisioningToken(s.orgID, label)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPProvisioningToken{ID: tok.ID, Label: tok.Label, Token: tok.Token, ExpiresAt: tok.ExpiresAt})
}

func (s *srv) handleUpdateJob(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	jobRef := mcp.ParseString(req, "job_id", "")

	j, err := s.c.GetJobByName(s.orgID, projectID, jobRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	body := client.UpdateJobBody{}
	if v := mcp.ParseString(req, "name", ""); v != "" {
		body.Name = &v
	}
	if v := mcp.ParseString(req, "image", ""); v != "" {
		body.Image = &v
	}
	if v := mcp.ParseString(req, "schedule", ""); v != "" {
		body.Schedule = &v
	}
	if v := mcp.ParseString(req, "command", ""); v != "" {
		body.Command = &v
	}
	if v := mcp.ParseString(req, "env_vars", ""); v != "" {
		body.EnvVars = &v
	}

	updated, err := s.c.UpdateJob(s.orgID, projectID, j.ID, body)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	lastRun := ""
	if updated.LastRunAt != nil {
		lastRun = *updated.LastRunAt
	}
	return jsonResult(MCPJob{ID: updated.ID, Name: updated.Name, Schedule: updated.Schedule, Status: updated.Status, LastRunAt: lastRun})
}

func (s *srv) handleSyncStack(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	stackRef := mcp.ParseString(req, "stack_id", "")

	st, err := s.c.GetStackByName(s.orgID, projectID, stackRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	result, err := s.c.SyncStack(s.orgID, projectID, st.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(result)
}

func (s *srv) handleDBQuery(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	query := mcp.ParseString(req, "query", "")
	readOnly := mcp.ParseBoolean(req, "read_only", true)

	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}
	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	result, err := s.c.DBQuery(s.orgID, projectID, svc.ID, query, readOnly)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(result)
}

func (s *srv) handleCancelDeployment(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	deploymentID := mcp.ParseString(req, "deployment_id", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.CancelDeployment(s.orgID, projectID, svc.ID, deploymentID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("deployment cancelled"), nil
}

func (s *srv) handleRetryDeployment(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	deploymentID := mcp.ParseString(req, "deployment_id", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	d, err := s.c.RetryDeployment(s.orgID, projectID, svc.ID, deploymentID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(map[string]string{"deployment_id": d.ID, "status": d.Status})
}

func (s *srv) handleCreateGitIntegration(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	provider := mcp.ParseString(req, "provider", "")
	name := mcp.ParseString(req, "name", "")
	token := mcp.ParseString(req, "token", "")
	baseURL := mcp.ParseString(req, "base_url", "")

	gi, err := s.c.CreatePATIntegration(s.orgID, provider, name, baseURL, "", token)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPGitIntegration{ID: gi.ID, Name: gi.Name, Provider: gi.Provider, AuthMethod: gi.AuthMethod, Connected: gi.Connected})
}

func (s *srv) handleDeleteGitIntegration(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := mcp.ParseString(req, "integration_id", "")
	if err := s.c.DeleteGitIntegration(s.orgID, id); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("git integration deleted"), nil
}

func (s *srv) handleCreateRegistryIntegration(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	body := client.CreateRegistryBody{
		Name:      mcp.ParseString(req, "name", ""),
		Provider:  mcp.ParseString(req, "provider", ""),
		Username:  mcp.ParseString(req, "username", ""),
		Password:  mcp.ParseString(req, "password", ""),
		Namespace: mcp.ParseString(req, "namespace", ""),
		Endpoint:  mcp.ParseString(req, "endpoint", ""),
	}
	ri, err := s.c.CreateRegistryIntegration(s.orgID, body)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPRegistryIntegration{ID: ri.ID, Name: ri.Name, Provider: ri.Provider, Namespace: ri.Namespace, Endpoint: ri.Endpoint})
}

func (s *srv) handleDeleteRegistryIntegration(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := mcp.ParseString(req, "integration_id", "")
	if err := s.c.DeleteRegistryIntegration(s.orgID, id); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("registry integration deleted"), nil
}

func (s *srv) handleCreateStorageIntegration(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	body := client.CreateStorageBody{
		Name:            mcp.ParseString(req, "name", ""),
		Provider:        mcp.ParseString(req, "provider", ""),
		Bucket:          mcp.ParseString(req, "bucket", ""),
		AccessKeyID:     mcp.ParseString(req, "access_key_id", ""),
		SecretAccessKey: mcp.ParseString(req, "secret_access_key", ""),
		Region:          mcp.ParseString(req, "region", ""),
		Endpoint:        mcp.ParseString(req, "endpoint", ""),
	}
	si, err := s.c.CreateStorageIntegration(s.orgID, body)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(MCPStorageIntegration{ID: si.ID, Name: si.Name, Provider: si.Provider, Endpoint: si.Endpoint, Region: si.Region, Bucket: si.Bucket})
}

func (s *srv) handleDeleteStorageIntegration(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := mcp.ParseString(req, "integration_id", "")
	if err := s.c.DeleteStorageIntegration(s.orgID, id); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText("storage integration deleted"), nil
}

// splitComma splits a comma-separated string, trimming whitespace.
func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			v := trimSpace(s[start:i])
			out = append(out, v)
			start = i + 1
		}
	}
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
