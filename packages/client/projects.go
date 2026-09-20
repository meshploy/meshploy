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

// CreateProject makes a project, deriving its slug from the name.
//
// The slug is the Kubernetes namespace, so the API requires one and constrains
// it to [a-z0-9-]. This used to send only the name, which the API refused with
// "expected required property slug to be present" - so `meshploy project
// create`, the MCP tool and anything else on this client could not create a
// project at all.
func (c *Client) CreateProject(orgID, name string) (*Project, error) {
	return c.CreateProjectWithSlug(orgID, name, ProjectSlug(name))
}

// CreateProjectWithSlug makes a project with a slug the caller chooses.
func (c *Client) CreateProjectWithSlug(orgID, name, slug string) (*Project, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects",
		map[string]string{"name": name, "slug": slug})
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
