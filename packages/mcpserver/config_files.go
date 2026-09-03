package mcpserver

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"
	"github.com/meshploy/packages/client"
)

// resolveConfigFile accepts an ID or a name, matching how every other resource
// reference in this server behaves. There is no by-name endpoint, so it filters
// the project listing -- cheap, and it keeps the API surface smaller.
func (s *srv) resolveConfigFile(projectID, ref string) (*client.ConfigFile, error) {
	files, err := s.c.ListConfigFiles(s.orgID, projectID)
	if err != nil {
		return nil, err
	}
	for i := range files {
		if files[i].ID == ref || files[i].Name == ref {
			return &files[i], nil
		}
	}
	return nil, fmt.Errorf("config file %q not found in project", ref)
}

func toMCPConfigFile(f client.ConfigFile) MCPConfigFile {
	out := MCPConfigFile{ID: f.ID, Name: f.Name, Path: f.Path, Size: f.Size, Services: f.Services}
	if out.Services == nil {
		out.Services = []string{}
	}
	if f.StackID != nil {
		out.StackID = *f.StackID
	}
	return out
}

func (s *srv) registerConfigFileTools(ms *mcpsdk.MCPServer) {
	ms.AddTool(
		mcp.NewTool("list_config_files",
			mcp.WithDescription("List config files in a project, or those attached to a specific service when service_id is given. File contents are never returned."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Description("Service ID or name — if set, returns only files attached to that service")),
		),
		s.handleListConfigFiles,
	)

	ms.AddTool(
		mcp.NewTool("create_config_file",
			mcp.WithDescription("Create a config file in a project. The content is stored encrypted and projected into each attached service at the given absolute path. Create the file first, then attach it with attach_config_file."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Display name, e.g. \"zot config\"")),
			mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path inside the container, e.g. /etc/zot/config.json")),
			mcp.WithString("content", mcp.Required(), mcp.Description("File contents (max 256KB)")),
		),
		s.handleCreateConfigFile,
	)

	ms.AddTool(
		mcp.NewTool("update_config_file",
			mcp.WithDescription("Update a config file's name, path or content. Editing content affects EVERY attached service — all of them are redeployed. Call list_config_files first to see who is attached."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("file_id", mcp.Required(), mcp.Description("Config file ID or name")),
			mcp.WithString("name", mcp.Description("New display name")),
			mcp.WithString("path", mcp.Description("New absolute path inside the container")),
			mcp.WithString("content", mcp.Description("New file contents — replaces the existing body entirely")),
		),
		s.handleUpdateConfigFile,
	)

	ms.AddTool(
		mcp.NewTool("delete_config_file",
			mcp.WithDescription("DESTRUCTIVE — delete a config file. Refused while any service is still attached; detach first. Confirm with the user before calling."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("file_id", mcp.Required(), mcp.Description("Config file ID or name")),
		),
		s.handleDeleteConfigFile,
	)

	ms.AddTool(
		mcp.NewTool("attach_config_file",
			mcp.WithDescription("Attach a config file to a service so it is mounted at its path. The service is redeployed."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("file_id", mcp.Required(), mcp.Description("Config file ID or name")),
		),
		s.handleAttachConfigFile,
	)

	ms.AddTool(
		mcp.NewTool("detach_config_file",
			mcp.WithDescription("Detach a config file from a service. The service is redeployed so the file stops being mounted — a running pod keeps its copy until then."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("service_id", mcp.Required(), mcp.Description("Service ID or name")),
			mcp.WithString("file_id", mcp.Required(), mcp.Description("Config file ID or name")),
		),
		s.handleDetachConfigFile,
	)
}

func (s *srv) handleListConfigFiles(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")

	files, err := s.c.ListConfigFiles(s.orgID, projectID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// The listing carries attached service names, so filtering by service needs
	// no second call -- but the caller may have passed an ID, and the names are
	// what came back, so resolve to a name first.
	var wantName string
	if serviceRef != "" {
		svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		wantName = svc.Name
	}

	out := make([]MCPConfigFile, 0, len(files))
	for _, f := range files {
		if wantName != "" && !slices.Contains(f.Services, wantName) {
			continue
		}
		out = append(out, toMCPConfigFile(f))
	}
	return jsonResult(out)
}

func (s *srv) handleCreateConfigFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	name := mcp.ParseString(req, "name", "")
	path := mcp.ParseString(req, "path", "")
	content := mcp.ParseString(req, "content", "")

	if !strings.HasPrefix(path, "/") {
		return mcp.NewToolResultError("path must be absolute, e.g. /etc/zot/config.json"), nil
	}
	f, err := s.c.CreateConfigFile(s.orgID, projectID, name, path, content)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(toMCPConfigFile(*f))
}

func (s *srv) handleUpdateConfigFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	fileRef := mcp.ParseString(req, "file_id", "")

	f, err := s.resolveConfigFile(projectID, fileRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	name := mcp.ParseString(req, "name", f.Name)
	path := mcp.ParseString(req, "path", f.Path)
	content := mcp.ParseString(req, "content", "")

	if !strings.HasPrefix(path, "/") {
		return mcp.NewToolResultError("path must be absolute, e.g. /etc/zot/config.json"), nil
	}
	updated, err := s.c.UpdateConfigFile(s.orgID, projectID, f.ID, name, path, content)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(f.Services) > 0 && content != "" {
		return mcp.NewToolResultText(fmt.Sprintf(
			"config file %q updated; %d attached service(s) redeployed: %s",
			updated.Name, len(f.Services), strings.Join(f.Services, ", "))), nil
	}
	return jsonResult(toMCPConfigFile(*updated))
}

func (s *srv) handleDeleteConfigFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	fileRef := mcp.ParseString(req, "file_id", "")

	f, err := s.resolveConfigFile(projectID, fileRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.DeleteConfigFile(s.orgID, projectID, f.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("config file %q deleted", f.Name)), nil
}

func (s *srv) handleAttachConfigFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	fileRef := mcp.ParseString(req, "file_id", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	f, err := s.resolveConfigFile(projectID, fileRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.AttachConfigFile(s.orgID, projectID, f.ID, svc.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"config file %q attached to service %q at %s", f.Name, svc.Name, f.Path)), nil
}

func (s *srv) handleDetachConfigFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")
	fileRef := mcp.ParseString(req, "file_id", "")

	svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	f, err := s.resolveConfigFile(projectID, fileRef)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.c.DetachConfigFile(s.orgID, projectID, f.ID, svc.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"config file %q detached from service %q", f.Name, svc.Name)), nil
}
