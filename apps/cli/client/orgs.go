package client

type OrgMember struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	UserName  string `json:"user_name"`
	UserEmail string `json:"user_email"`
}

type Invitation struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	ExpiresAt string `json:"expires_at"`
	Token     string `json:"token,omitempty"`
}

func (c *Client) ListMembers(orgID string) ([]OrgMember, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/members", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]OrgMember](resp)
}

func (c *Client) AddMember(orgID, email, role string) (*OrgMember, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/members", map[string]string{"email": email, "role": role})
	if err != nil {
		return nil, err
	}
	return decodePtr[OrgMember](resp)
}

func (c *Client) UpdateMember(orgID, userID, role string) error {
	return c.doNoContent("PATCH", "/api/v1/orgs/"+orgID+"/members/"+userID)
}

func (c *Client) UpdateMemberRole(orgID, userID, role string) error {
	resp, err := c.do("PATCH", "/api/v1/orgs/"+orgID+"/members/"+userID, map[string]string{"role": role})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) RemoveMember(orgID, userID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/members/"+userID)
}

func (c *Client) CreateInvitation(orgID, email, role string) (*Invitation, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/invitations", map[string]string{"email": email, "role": role})
	if err != nil {
		return nil, err
	}
	return decodePtr[Invitation](resp)
}

func (c *Client) ListInvitations(orgID string) ([]Invitation, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/invitations", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Invitation](resp)
}
