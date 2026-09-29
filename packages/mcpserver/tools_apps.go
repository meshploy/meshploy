package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"

	"github.com/meshploy/packages/client"
)

// The two tools that take an app an agent just wrote to people: deploy the
// folder it is in, then share it. deploy_folder reads the agent's own disk, so
// it only makes sense on the local server (meshploy mcp); the remote /mcp
// strips it, since a path there would be the gateway's. share_app is offered
// everywhere, Community included, where it answers that sharing needs
// Enterprise or Meshploy Cloud: the agent tells its user, and nothing of
// sharing is in Community beyond this call.

// deployWait bounds how long deploy_folder follows a deployment: a first
// Railpack build without cache can take several minutes.
var (
	deployWait = 20 * time.Minute
	deployPoll = 3 * time.Second
)

// enterpriseURL is where the console sends someone asking about Enterprise.
const enterpriseURL = "https://meshploy.com/contact?topic=enterprise&src=mcp"

func (s *srv) registerAppTools(ms *mcpsdk.MCPServer) {
	ms.AddTool(
		mcp.NewTool("deploy_folder",
			mcp.WithDescription("Deploy a folder on this machine as a service: packs it (leaving out .git, node_modules, .env files and what .gitignore and .meshployignore name), uploads it, builds it with Railpack (or its Dockerfile when builder=dockerfile) and waits until it is up. "+
				"Makes the service when it does not exist, named after the folder. Returns the service, the deployment and its links, or the build's error with the end of its log. "+
				"The service has no public link until a route points at it: use create_route after, if the user wants one."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID or slug")),
			mcp.WithString("path", mcp.Required(), mcp.Description("Folder to deploy, absolute or relative to where the MCP server runs")),
			mcp.WithString("service", mcp.Description("Service name or ID (default: the folder's name)")),
			mcp.WithString("port", mcp.Description("Port the app listens on, for a service made now (default 3000); it is also given as $PORT")),
			mcp.WithString("builder", mcp.Description("railpack (default) or dockerfile, for a service made now")),
			mcp.WithString("wait", mcp.Description("true (default): return once it is up or has failed; false: once the deploy has started")),
		),
		s.handleDeployFolder,
	)

	ms.AddTool(
		mcp.NewTool("share_app",
			mcp.WithDescription("Share a service's app with people by email: each gets a sign-in link, and only they (and the workspace's members) can open it. "+
				"Needs Meshploy Enterprise or Meshploy Cloud; on Community it says so, which is worth telling the user as it is."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Project ID or slug")),
			mcp.WithString("service", mcp.Required(), mcp.Description("Service name or ID")),
			mcp.WithString("emails", mcp.Required(), mcp.Description("Email addresses, comma-separated")),
			mcp.WithString("role", mcp.Description("viewer (default) or editor")),
		),
		s.handleShareApp,
	)
}

func (s *srv) handleDeployFolder(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := mcp.ParseString(req, "path", "")
	if path == "" {
		return mcp.NewToolResultError("path is required"), nil
	}
	p, err := s.c.GetProjectBySlugOrID(s.orgID, mcp.ParseString(req, "project_id", ""))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	folder, err := client.PackFolder(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	name := mcp.ParseString(req, "service", "")
	if name == "" {
		name = client.ServiceNameFor(folder.Name)
	}
	created := false
	svc, err := s.c.GetServiceByName(s.orgID, p.ID, name)
	if err != nil {
		port := 3000
		if v := mcp.ParseString(req, "port", ""); v != "" {
			if port, err = strconv.Atoi(v); err != nil || port < 1 || port > 65535 {
				return mcp.NewToolResultError("port must be a number from 1 to 65535"), nil
			}
		}
		builder := mcp.ParseString(req, "builder", "")
		if builder != "" && builder != "railpack" && builder != "dockerfile" {
			return mcp.NewToolResultError("builder must be railpack or dockerfile"), nil
		}
		svc, err = s.c.CreateService(s.orgID, p.ID, client.CreateServiceBody{
			Name:       name,
			FromUpload: true,
			Builder:    builder,
			Ports:      []client.ServicePortBody{{Name: "http", Port: port, IsHTTP: true, IsPrimary: true, IsPublic: true}},
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("create service %q: %v", name, err)), nil
		}
		created = true
	}

	res, err := s.c.UploadSource(s.orgID, p.ID, svc.ID, folder, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := map[string]any{
		"project_id":      p.ID,
		"service":         svc.Name,
		"service_id":      svc.ID,
		"service_created": created,
		"files":           folder.Files,
		"left_out":        folder.Skipped,
		"digest":          res.Source.Digest,
	}
	if res.Deployment == nil {
		return mcp.NewToolResultError("the folder was stored, but no deployment started"), nil
	}
	out["deployment_id"] = res.Deployment.ID
	out["status"] = res.Deployment.Status
	if mcp.ParseString(req, "wait", "true") == "false" {
		return jsonResult(out)
	}

	d, err := s.followDeployment(ctx, p.ID, svc.ID, res.Deployment.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out["status"] = d.Status
	if d.Status != "success" {
		return mcp.NewToolResultError(fmt.Sprintf("the deployment of %s %s. The end of its log:\n%s", svc.Name, d.Status, logTail(d.Log, 40))), nil
	}
	links := s.serviceLinks(p.ID, svc.ID)
	out["links"] = links
	if len(links) == 0 {
		out["note"] = "It is running, with no link yet: create_route gives it one."
	}
	return jsonResult(out)
}

// followDeployment waits until a deployment has succeeded or failed.
func (s *srv) followDeployment(ctx context.Context, projectID, serviceID, deploymentID string) (*client.Deployment, error) {
	deadline := time.Now().Add(deployWait)
	for {
		d, err := s.c.GetDeployment(s.orgID, projectID, serviceID, deploymentID)
		if err != nil {
			return nil, err
		}
		switch d.Status {
		case "success", "failed", "cancelled":
			return d, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("still %s after %s: follow it with get_deployment %s", d.Status, deployWait, deploymentID)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(deployPoll):
		}
	}
}

// serviceLinks are the https links of the published routes to a service.
func (s *srv) serviceLinks(projectID, serviceID string) []string {
	routes, err := s.c.ListRoutes(s.orgID, projectID)
	if err != nil {
		return nil
	}
	var links []string
	for _, r := range routes {
		if r.Published && r.ServiceID != nil && *r.ServiceID == serviceID && r.Hostname != "" {
			links = append(links, "https://"+r.Hostname)
		}
	}
	return links
}

func (s *srv) handleShareApp(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p, err := s.c.GetProjectBySlugOrID(s.orgID, mcp.ParseString(req, "project_id", ""))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var emails []string
	for _, e := range strings.FieldsFunc(mcp.ParseString(req, "emails", ""), func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' }) {
		if e = strings.TrimSpace(e); e != "" {
			if !strings.Contains(e, "@") {
				return mcp.NewToolResultError(fmt.Sprintf("%q is not an email address", e)), nil
			}
			emails = append(emails, e)
		}
	}
	if len(emails) == 0 {
		return mcp.NewToolResultError("emails is required: one or more addresses, comma-separated"), nil
	}
	role := mcp.ParseString(req, "role", "viewer")
	if role != "viewer" && role != "editor" {
		return mcp.NewToolResultError("role must be viewer or editor"), nil
	}
	svc, err := s.c.GetServiceByName(s.orgID, p.ID, mcp.ParseString(req, "service", ""))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out, err := s.c.ShareService(s.orgID, p.ID, svc.ID, client.ShareBody{Emails: emails, Role: role})
	if errors.Is(err, client.ErrSharingUnavailable) {
		// Not a failure of the call: the answer, to pass on as it is.
		return mcp.NewToolResultText(fmt.Sprintf(
			"This Meshploy is the Community edition, which does not share apps with people outside the workspace. "+
				"Sharing %s with %s needs Meshploy Enterprise (on this server) or Meshploy Cloud: %s. "+
				"Until then, the workspace's own members can open it, and anyone can if its route is public.",
			svc.Name, strings.Join(emails, ", "), enterpriseURL)), nil
	}
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(out)
}

// logTail is the last n lines of a log.
func logTail(log string, n int) string {
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
