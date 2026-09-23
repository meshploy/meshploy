package client

type Domain struct {
	ID    string `json:"id"`
	OrgID string `json:"organization_id"`
	// The API sends this as base_domain. It was read as "domain" here, a key
	// nothing sends, so every domain came back nameless.
	Domain     string  `json:"base_domain"`
	Verified   bool    `json:"verified"`
	IsPrimary  bool    `json:"is_primary"`
	DNSMode    string  `json:"dns_mode"`
	RetiringAt *string `json:"retiring_at,omitempty"`
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
