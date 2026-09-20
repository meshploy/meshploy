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

	// TargetTLS makes the hop to an address target HTTPS.
	TargetTLS bool `json:"-"`
	// Path and StripPath route one prefix of the hostname; empty means "/".
	Path      string `json:"-"`
	StripPath bool   `json:"-"`

	// Published false creates the route paused: it exists, serves nothing and
	// gets no certificate. A migration creates every route this way and
	// publishes each as its group moves.
	Published *bool `json:"published,omitempty"`
}

// routeWire is what the API actually takes: a zone, and the targets as a list.
//
// The friendly body above names one target inline, because that is what every
// caller wants; this is the translation. Sending the friendly shape straight to
// the API is what `meshploy route create` and the migration were both doing,
// and the API refused both with "expected required property targets".
type routeWire struct {
	DomainID  *string           `json:"domain_id,omitempty"`
	Zone      string            `json:"zone"`
	Subdomain string            `json:"subdomain,omitempty"`
	Hostname  *string           `json:"hostname,omitempty"`
	Published *bool             `json:"published,omitempty"`
	Targets   []routeWireTarget `json:"targets"`
}

type routeWireTarget struct {
	Path      string  `json:"path"`
	StripPath bool    `json:"strip_path"`
	ServiceID *string `json:"service_id,omitempty"`
	NodeID    *string `json:"node_id,omitempty"`
	TargetIP  *string `json:"target_ip,omitempty"`
	TargetTLS *bool   `json:"target_tls,omitempty"`
	Port      *int    `json:"port,omitempty"`
}

func (c *Client) ListRoutes(orgID, projectID string) ([]Route, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/routes", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Route](resp)
}

func (c *Client) CreateRoute(orgID, projectID string, body CreateRouteBody) (*Route, error) {
	zone := body.Zone
	if zone == "" {
		zone = "public"
	}
	path := body.Path
	if path == "" {
		path = "/"
	}
	target := routeWireTarget{
		Path: path, StripPath: body.StripPath,
		ServiceID: body.ServiceID, NodeID: body.NodeID, TargetIP: body.TargetIP,
	}
	// A service target resolves its own port; a node or address target is the
	// one that needs the number.
	switch {
	case body.TargetPort != nil:
		target.Port = body.TargetPort
	case body.ServiceID == nil && body.Port != nil:
		target.Port = body.Port
	}
	if body.TargetTLS {
		tls := true
		target.TargetTLS = &tls
	}
	wire := routeWire{
		DomainID: body.DomainID, Zone: zone, Subdomain: body.Subdomain,
		Hostname: body.Hostname, Published: body.Published,
		Targets: []routeWireTarget{target},
	}
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/routes", wire)
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
