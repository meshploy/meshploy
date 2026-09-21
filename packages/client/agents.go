package client

// Agent principals: a machine that acts on an organisation, with its own
// membership and permissions, and a token instead of a password.

type Agent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

func (c *Client) ListAgents(orgID string) ([]Agent, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/agents", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Agent](resp)
}

// DeleteAgent removes the principal and, with it, every token it holds.
func (c *Client) DeleteAgent(orgID, agentID string) error {
	_, err := c.do("DELETE", "/api/v1/orgs/"+orgID+"/agents/"+agentID, nil)
	return err
}
