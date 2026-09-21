package client

// TCPRoute is one port the gateway publishes and forwards over the mesh, for
// what does not speak HTTP. A domain route is the HTTP equivalent.
type TCPRoute struct {
	ID          string  `json:"id"`
	GatewayPort int     `json:"gateway_port"`
	ServiceID   *string `json:"service_id"`
	ServicePort int     `json:"service_port"`
	NodeID      *string `json:"node_id"`
	TargetIP    string  `json:"target_ip"`
	TargetPort  int     `json:"target_port"`
	// AllowedCIDRs is empty when anyone who can reach the gateway may connect.
	AllowedCIDRs []string `json:"allowed_cidrs"`
	// Status is what the gateway managed to do: pending, open, failed or paused.
	Status    string `json:"status"`
	LastError string `json:"last_error"`
	// Published is false for a paused route: the port stays reserved, closed.
	Published bool   `json:"published"`
	CreatedAt string `json:"created_at"`
}

type CreateTCPRouteBody struct {
	GatewayPort int `json:"gateway_port"`
	// Zone is where the port is reachable from: public, mesh or local. Empty
	// is public, which is what the API defaults to.
	Zone string `json:"zone,omitempty"`
	// Published false creates the route with its port closed, for a route that
	// exists before it should serve - a migration creates every one this way.
	Published *bool `json:"published,omitempty"`
	// The target: a service's published port, or a port on a node.
	ServiceID   *string `json:"service_id,omitempty"`
	ServicePort *int    `json:"service_port,omitempty"`
	NodeID      *string `json:"node_id,omitempty"`
	NodePort    *int    `json:"node_port,omitempty"`

	AllowedCIDRs []string `json:"allowed_cidrs,omitempty"`
}

type UpdateTCPRouteBody struct {
	GatewayPort  *int     `json:"gateway_port,omitempty"`
	AllowedCIDRs []string `json:"allowed_cidrs,omitempty"`
}

func (c *Client) ListTCPRoutes(orgID, projectID string) ([]TCPRoute, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/tcp-routes", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]TCPRoute](resp)
}

func (c *Client) CreateTCPRoute(orgID, projectID string, body CreateTCPRouteBody) (*TCPRoute, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/tcp-routes", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[TCPRoute](resp)
}

func (c *Client) UpdateTCPRoute(orgID, projectID, routeID string, body UpdateTCPRouteBody) (*TCPRoute, error) {
	resp, err := c.do("PATCH", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/tcp-routes/"+routeID, body)
	if err != nil {
		return nil, err
	}
	return decodePtr[TCPRoute](resp)
}

// PublishTCPRoute opens a paused route's port again.
func (c *Client) PublishTCPRoute(orgID, projectID, routeID string) (*TCPRoute, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/tcp-routes/"+routeID+"/publish", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[TCPRoute](resp)
}

// PauseTCPRoute closes a route's port and keeps the route.
func (c *Client) PauseTCPRoute(orgID, projectID, routeID string) (*TCPRoute, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/tcp-routes/"+routeID+"/pause", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[TCPRoute](resp)
}

func (c *Client) DeleteTCPRoute(orgID, projectID, routeID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/tcp-routes/"+routeID)
}
