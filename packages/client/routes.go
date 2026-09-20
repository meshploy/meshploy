package client

type Route struct {
	ID         string  `json:"id"`
	Hostname   string  `json:"hostname"`
	TargetIP   string  `json:"target_ip"`
	TargetPort int     `json:"target_port"`
	ServiceID  *string `json:"service_id"`
	Zone       string  `json:"zone"`
	// Published is false for a paused route: kept, not served.
	Published bool   `json:"published"`
	CreatedAt string `json:"created_at"`
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

	// Published false creates the route paused: it exists, serves nothing and
	// gets no certificate. A migration creates every route this way and
	// publishes each as its group moves.
	Published *bool `json:"published,omitempty"`
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

// PublishRoute serves a paused route's hostname again.
func (c *Client) PublishRoute(orgID, projectID, routeID string) (*Route, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/routes/"+routeID+"/publish", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[Route](resp)
}

// PauseRoute keeps a route and stops serving it: 404, no certificate.
func (c *Client) PauseRoute(orgID, projectID, routeID string) (*Route, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/routes/"+routeID+"/pause", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[Route](resp)
}

func (c *Client) DeleteRoute(orgID, projectID, routeID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/routes/"+routeID)
}
