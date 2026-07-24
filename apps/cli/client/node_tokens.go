package client

type RegistrationToken struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type ProvisioningToken struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Token     string `json:"token"` // plaintext — shown once on creation
	ExpiresAt string `json:"expires_at,omitempty"`
	Used      bool   `json:"used"`
}

func (c *Client) GetNodeRegistrationToken(orgID string) (*RegistrationToken, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/nodes/registration-token", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[RegistrationToken](resp)
}

func (c *Client) GenerateNodeRegistrationToken(orgID string) (*RegistrationToken, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/nodes/registration-token", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[RegistrationToken](resp)
}

func (c *Client) CreateProvisioningToken(orgID, label string) (*ProvisioningToken, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/nodes/provisioning-tokens", map[string]string{"label": label})
	if err != nil {
		return nil, err
	}
	return decodePtr[ProvisioningToken](resp)
}
