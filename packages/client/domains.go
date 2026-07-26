package client

type Domain struct {
	ID       string `json:"id"`
	OrgID    string `json:"org_id"`
	Domain   string `json:"domain"`
	Verified bool   `json:"verified"`
}

func (c *Client) ListDomains(orgID string) ([]Domain, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/domains", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Domain](resp)
}

func (c *Client) GetDomain(orgID, domainID string) (*Domain, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/domains/"+domainID, nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[Domain](resp)
}
