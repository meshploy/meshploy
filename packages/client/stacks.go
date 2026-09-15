package client

type Stack struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Spec          string  `json:"spec"`
	Status        string  `json:"status"`
	LastAppliedAt *string `json:"last_applied_at"`
}

type ApplyResult struct {
	Stack    *Stack   `json:"stack"`
	Created  []string `json:"created"`
	Updated  []string `json:"updated"`
	Deleted  []string `json:"deleted"`
	Deployed []string `json:"deployed"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// ApplyOptions carries what an apply does beyond reconciling the records.
type ApplyOptions struct {
	// NoDeploy writes the records and rolls nothing out.
	NoDeploy bool
}

type CreateStackBody struct {
	Name string `json:"name"`
	Spec string `json:"spec"`
	// Variables interpolate into the spec as ${NAME}. Write-only - no read
	// endpoint returns them, so a caller cannot read a secret back out.
	Variables map[string]string `json:"variables,omitempty"`

	// Git source. All empty means the stack holds the inline spec above.
	GitMode          string  `json:"git_mode,omitempty"` // "" | "file" | "repo"
	GitRepo          string  `json:"git_repo,omitempty"`
	GitBranch        string  `json:"git_branch,omitempty"`
	GitPath          string  `json:"git_path,omitempty"`
	GitIntegrationID *string `json:"git_integration_id,omitempty"` // nil = public repo
}

// UpdateStackBody changes only the fields it carries: a nil pointer, an empty
// name and nil variables keep what the stack has.
type UpdateStackBody struct {
	Name      string            `json:"name,omitempty"`
	Spec      *string           `json:"spec,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`

	// Git source
	GitMode          *string `json:"git_mode,omitempty"`
	GitRepo          *string `json:"git_repo,omitempty"`
	GitBranch        *string `json:"git_branch,omitempty"`
	GitPath          *string `json:"git_path,omitempty"`
	GitIntegrationID *string `json:"git_integration_id,omitempty"` // "" = clear, UUID = set
}

func (c *Client) ListStacks(orgID, projectID string) ([]Stack, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/stacks", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Stack](resp)
}

func (c *Client) CreateStack(orgID, projectID string, body CreateStackBody) (*Stack, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/stacks", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[Stack](resp)
}

func (c *Client) GetStack(orgID, projectID, stackID string) (*Stack, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/stacks/"+stackID, nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[Stack](resp)
}

func (c *Client) UpdateStack(orgID, projectID, stackID string, body UpdateStackBody) (*Stack, error) {
	resp, err := c.do("PUT", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/stacks/"+stackID, body)
	if err != nil {
		return nil, err
	}
	return decodePtr[Stack](resp)
}

func (c *Client) DeleteStack(orgID, projectID, stackID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/stacks/"+stackID)
}

func (c *Client) ListStackServices(orgID, projectID, stackID string) ([]Service, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/stacks/"+stackID+"/services", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Service](resp)
}

func (c *Client) ApplyStack(orgID, projectID, stackID string) (*ApplyResult, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/stacks/"+stackID+"/apply", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[ApplyResult](resp)
}

// ManifestBody is one inline compose manifest: the spec plus what the server
// cannot read off the caller's machine.
type ManifestBody struct {
	Name string
	Spec string
	// Files carries what the manifest's configs and secrets name by file:,
	// keyed by the path as written.
	Files map[string]string
	// Variables are the ${NAME} values the spec interpolates. Nil keeps the
	// ones the stack already has. Write-only, like the ones on a create.
	Variables map[string]string
}

// ApplyManifest upserts a raw stack named m.Name from an inline compose spec
// and reconciles it in one call. Idempotent - re-applying converges in place.
func (c *Client) ApplyManifest(orgID, projectID string, m ManifestBody, opts ...ApplyOptions) (*ApplyResult, error) {
	body := map[string]any{"name": m.Name, "spec": m.Spec}
	if len(m.Files) > 0 {
		body["files"] = m.Files
	}
	if len(m.Variables) > 0 {
		body["variables"] = m.Variables
	}
	if len(opts) > 0 && opts[0].NoDeploy {
		body["deploy"] = false
	}
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/apply", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[ApplyResult](resp)
}

func (c *Client) SyncStack(orgID, projectID, stackID string) (*ApplyResult, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/stacks/"+stackID+"/sync", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[ApplyResult](resp)
}

func (c *Client) GetStackByName(orgID, projectID, ref string) (*Stack, error) {
	stacks, err := c.ListStacks(orgID, projectID)
	if err != nil {
		return nil, err
	}
	for i, s := range stacks {
		if s.ID == ref || s.Name == ref {
			return &stacks[i], nil
		}
	}
	return nil, ErrNotFound("stack", ref)
}
