package client

type Stack struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Spec          string  `json:"spec"`
	Status        string  `json:"status"`
	LastAppliedAt *string `json:"last_applied_at"`
}

type ApplyResult struct {
	Stack   *Stack   `json:"stack"`
	Created []string `json:"created"`
	Updated []string `json:"updated"`
	Deleted []string `json:"deleted"`
	Errors  []string `json:"errors"`
}

type CreateStackBody struct {
	Name string `json:"name"`
	Spec string `json:"spec"`
}

type UpdateStackBody struct {
	Name string `json:"name,omitempty"`
	Spec string `json:"spec"`
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
