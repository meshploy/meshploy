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

// ApplyStack reconciles a stack and rolls out what it changed.
//
// The empty body is not optional: the API takes an object of options - env
// overrides, whether to roll out - and refuses a request without one. Sending
// nil got "request body is required", which is a confusing thing to read when
// the call takes no arguments.
func (c *Client) ApplyStack(orgID, projectID, stackID string) (*ApplyResult, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/stacks/"+stackID+"/apply",
		struct{}{})
	if err != nil {
		return nil, err
	}
	return decodePtr[ApplyResult](resp)
}

// ApplyStackRecordsOnly reconciles a stack's records and starts no rollout:
// services, ports, files and routes are written, and nothing is built or
// deployed. For a caller that decides what each service runs - the
// migration, which starts services on the images their old platform ran.
func (c *Client) ApplyStackRecordsOnly(orgID, projectID, stackID string, files ...map[string]string) (*ApplyResult, error) {
	body := map[string]any{"deploy": false}
	// The files its spec names by path, for a stack whose repository cannot
	// be read yet.
	if len(files) > 0 && len(files[0]) > 0 {
		body["files"] = files[0]
	}
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/stacks/"+stackID+"/apply", body)
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

// SetEdgeFallback sends the hostnames the old platform still serves to its
// edge on a side port, during a migration that took the edge first.
func (c *Client) SetEdgeFallback(orgID, upstream string, hostnames []string) error {
	resp, err := c.do("PUT", "/api/v1/orgs/"+orgID+"/edge-fallback",
		map[string]any{"upstream": upstream, "hostnames": hostnames})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ClearEdgeFallback stops sending anything to the old platform's edge.
func (c *Client) ClearEdgeFallback(orgID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/edge-fallback")
}
