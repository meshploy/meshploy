package client

type Route struct {
	ID         string  `json:"id"`
	Hostname   string  `json:"hostname"`
	TargetIP   string  `json:"target_ip"`
	TargetPort int     `json:"target_port"`
	ServiceID  *string `json:"service_id"`
	Zone       string  `json:"zone"`
	CreatedAt  string  `json:"created_at"`
}

type CreateRouteBody struct {
	// Domain-based (preferred)
	DomainID  *string `json:"domain_id,omitempty"`
	Zone      string  `json:"zone,omitempty"`
	Subdomain string  `json:"subdomain,omitempty"`

	// Raw hostname fallback
	Hostname *string `json:"hostname,omitempty"`

	// Target: either service_id OR node_id+port
	ServiceID *string `json:"service_id,omitempty"`
	NodeID    *string `json:"node_id,omitempty"`
	Port      *int    `json:"port,omitempty"`

	// Direct override (bypass resolution)
	TargetIP   *string `json:"target_ip,omitempty"`
	TargetPort *int    `json:"target_port,omitempty"`
}

func (c *Client) ListRoutes(orgID, projectID string) ([]Route, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/routes", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Route](resp)
}

func (c *Client) CreateRoute(orgID, projectID string, body CreateRouteBody) (*Route, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/routes", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[Route](resp)
}

func (c *Client) DeleteRoute(orgID, projectID, routeID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/routes/"+routeID)
}
