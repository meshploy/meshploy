package client

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

func (c *Client) CreateProject(orgID, name string) (*Project, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects", map[string]string{"name": name})
	if err != nil {
		return nil, err
	}
	return decodePtr[Project](resp)
}

func (c *Client) DeleteProject(orgID, projectID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID)
}

// GetProjectBySlugOrID resolves a project by ID or slug.
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
