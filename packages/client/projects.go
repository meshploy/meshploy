package client

import "strings"

type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"created_at"`
}

func (c *Client) ListProjects(orgID string) ([]Project, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Project](resp)
}

// CreateProject makes a project, deriving its slug from the name: the slug is
// the Kubernetes namespace, which the API requires, constrained to [a-z0-9-].
func (c *Client) CreateProject(orgID, name string) (*Project, error) {
	return c.CreateProjectWithSlug(orgID, name, ProjectSlug(name))
}

// CreateProjectWithSlug makes a project with a slug the caller chooses.
func (c *Client) CreateProjectWithSlug(orgID, name, slug string) (*Project, error) {
	return c.CreateProjectSandboxed(orgID, name, slug, nil)
}

// CreateProjectSandboxed makes a project, saying whether it starts sandboxed;
// nil leaves it to the organization's setting.
func (c *Client) CreateProjectSandboxed(orgID, name, slug string, sandboxed *bool) (*Project, error) {
	body := map[string]any{"name": name, "slug": slug}
	if sandboxed != nil {
		body["sandboxed"] = *sandboxed
	}
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[Project](resp)
}

// ProjectSlug turns a project name into a name Kubernetes accepts: lower case,
// spaces and underscores as hyphens, and nothing else but letters, digits and
// hyphens. The same rule the server applies to a service name.
func ProjectSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-' || r == '.':
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if slug == "" {
		return "project"
	}
	if len(slug) > 50 {
		slug = strings.Trim(slug[:50], "-")
	}
	return slug
}

func (c *Client) DeleteProject(orgID, projectID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID)
}

// GetProjectBySlugOrID resolves a project by ID or slug.
func (c *Client) UpdateProject(orgID, projectID, name string) (*Project, error) {
	resp, err := c.do("PATCH", "/api/v1/orgs/"+orgID+"/projects/"+projectID, map[string]string{"name": name})
	if err != nil {
		return nil, err
	}
	return decodePtr[Project](resp)
}

func (c *Client) GetProjectBySlugOrID(orgID, ref string) (*Project, error) {
	projects, err := c.ListProjects(orgID)
	if err != nil {
		return nil, err
	}
	for i, p := range projects {
		if p.ID == ref || p.Slug == ref {
			return &projects[i], nil
		}
	}
	return nil, ErrNotFound("project", ref)
}

// SandboxState is whether a project's, or a level's, pods run on gVisor, and
// whether a rule requires it.
type SandboxState struct {
	On       bool   `json:"on"`
	Required bool   `json:"required"`
	Why      string `json:"why,omitempty"`
}

// SandboxOverview is the organization's prepared nodes and sandboxed projects.
type SandboxOverview struct {
	Nodes    []string `json:"nodes"`
	Projects []struct {
		ID        string `json:"id"`
		ProjectID string `json:"project_id"`
		Name      string `json:"name"`
		Level     string `json:"level,omitempty"`
		Required  bool   `json:"required"`
	} `json:"projects"`
}

func (c *Client) GetSandboxOverview(orgID string) (*SandboxOverview, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/sandbox", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[SandboxOverview](resp)
}

func (c *Client) GetProjectSandbox(orgID, projectID string) (*SandboxState, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/sandbox", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[SandboxState](resp)
}

// SetProjectSandbox switches a project, or a level, in or out of the sandbox.
// Its running workloads move at once: services roll, databases restart.
func (c *Client) SetProjectSandbox(orgID, projectID string, on bool) (*SandboxState, error) {
	resp, err := c.do("PUT", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/sandbox", map[string]bool{"on": on})
	if err != nil {
		return nil, err
	}
	return decodePtr[SandboxState](resp)
}
