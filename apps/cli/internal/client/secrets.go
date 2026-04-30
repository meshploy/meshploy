package client

type Secret struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type secretsListBody struct {
	Secrets []Secret `json:"secrets"`
}

func (c *Client) ListSecrets(orgID, projectID string) ([]Secret, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/secrets", nil)
	if err != nil {
		return nil, err
	}
	out, err := decode[secretsListBody](resp)
	return out.Secrets, err
}

func (c *Client) SetSecret(orgID, projectID, name, value string) (*Secret, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/secrets",
		map[string]string{"name": name, "value": value})
	if err != nil {
		return nil, err
	}
	return decodePtr[Secret](resp)
}

func (c *Client) DeleteSecret(orgID, projectID, secretID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/secrets/"+secretID)
}
