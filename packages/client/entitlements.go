package client

// Entitlements is the licence snapshot the API reports for this install.
//
// Mirrors service.Status. Only the fields a client acts on are listed; the
// server may send more.
type Entitlements struct {
	Licensed  bool     `json:"licensed"`
	Tier      string   `json:"tier,omitempty"`
	Customer  string   `json:"customer,omitempty"`
	Features  []string `json:"features"`
	ExpiresAt string   `json:"expires_at,omitempty"`
	Expired   bool     `json:"expired"`
	NodeLimit int      `json:"node_limit,omitempty"`
	NodeCount int      `json:"node_count"`
	OverLimit bool     `json:"over_limit"`
	Problem   string   `json:"problem,omitempty"`

	// RegistryScope names the private image this licence grants, e.g.
	// "ghcr.io/meshploy/api-ee". Empty on an unlicensed install, and on a
	// licence that grants no image beyond the stock one.
	RegistryScope string `json:"registry_scope,omitempty"`

	// CanActivate is false in a build that trusts no signing key and therefore
	// cannot store a licence at all.
	CanActivate bool `json:"can_activate"`

	// Edition is "community" or "enterprise": which binary is answering,
	// whatever licence it holds. Empty from an API older than the field; use
	// IsEnterprise rather than reading it directly.
	Edition string `json:"edition,omitempty"`
}

// IsEnterprise reports whether the Enterprise binary is answering. An API older
// than the Edition field could only verify a licence on the Enterprise image,
// so for those, being able to activate is what Enterprise meant.
func (e *Entitlements) IsEnterprise() bool {
	if e.Edition != "" {
		return e.Edition == "enterprise"
	}
	return e.CanActivate
}

// GetEntitlements reads the install's licence status. Readable by any
// authenticated user; it never returns the licence token itself.
func (c *Client) GetEntitlements() (*Entitlements, error) {
	resp, err := c.do("GET", "/api/v1/entitlements", nil)
	if err != nil {
		return nil, err
	}
	return decode[*Entitlements](resp)
}

// ActivateLicense installs a licence token and returns the resulting
// entitlements. The server verifies the signature before storing, so a bad
// token fails here rather than silently persisting.
//
// A build that trusts no signing key rejects every token; check
// Entitlements.CanActivate first to tell "wrong token" from "wrong build".
func (c *Client) ActivateLicense(token string) (*Entitlements, error) {
	resp, err := c.do("POST", "/api/v1/entitlements/license", map[string]string{"token": token})
	if err != nil {
		return nil, err
	}
	return decode[*Entitlements](resp)
}
