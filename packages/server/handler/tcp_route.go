package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
	svc "github.com/meshploy/packages/server/service"
)

// ── I/O types ─────────────────────────────────────────────────────────────────

type TCPRoutePathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	RouteID   string `path:"routeId"`
}

type ListTCPRoutesInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
}

type ListOrgTCPRoutesInput struct {
	OrgID string `path:"orgId"`
}

type ListTCPRoutesOutput struct{ Body []db.TCPRoute }

// OrgTCPRoutesOutput carries the ports the gateway already uses alongside the
// routes, so a form can say a port is taken before it is submitted.
type OrgTCPRoutesOutput struct {
	Body struct {
		Routes   []db.TCPRoute `json:"routes"`
		Reserved []int         `json:"reserved" doc:"Ports the gateway uses for itself, which a route may never take"`
	}
}
type GetTCPRouteOutput struct{ Body *db.TCPRoute }

type CreateTCPRouteInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		GatewayPort  int      `json:"gateway_port" minimum:"1" maximum:"65535" doc:"The port the gateway listens on"`
		ServiceID    *string  `json:"service_id,omitempty"   doc:"Route to this service's published port"`
		ServicePort  *int     `json:"service_port,omitempty" doc:"Which of its ports; left out, the one it publishes"`
		NodeID       *string  `json:"node_id,omitempty"      doc:"Route to a port on this node instead, for something running outside Meshploy"`
		NodePort     *int     `json:"node_port,omitempty"    doc:"The port on that node"`
		AllowedCIDRs []string `json:"allowed_cidrs,omitempty" doc:"Addresses or ranges allowed to connect. Empty means anyone who can reach the gateway"`
	}
}

// UpdateTCPRouteBody changes only the fields it carries. Retargeting is a
// delete and a create: a route is its gateway port plus what it points at, and
// silently moving the target under a port people are connecting to is worse
// than making them say so.
type UpdateTCPRouteInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	RouteID   string `path:"routeId"`
	Body      struct {
		GatewayPort  *int     `json:"gateway_port,omitempty" minimum:"1" maximum:"65535"`
		AllowedCIDRs []string `json:"allowed_cidrs,omitempty"`
	}
}

// ── Registration ──────────────────────────────────────────────────────────────

func (h *Handler) registerTCPRouteRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-tcp-routes",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/tcp-routes",
		Summary:     "List TCP routes in a project",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListTCPRoutes)

	huma.Register(api, huma.Operation{
		OperationID: "list-org-tcp-routes",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/tcp-routes",
		Summary:     "List every published TCP port in an organization, with the gateway's own",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListOrgTCPRoutes)

	huma.Register(api, huma.Operation{
		OperationID:   "create-tcp-route",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/tcp-routes",
		Summary:       "Publish a TCP port on the gateway",
		Tags:          []string{"Routes"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 201,
	}, h.CreateTCPRoute)

	huma.Register(api, huma.Operation{
		OperationID: "get-tcp-route",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/tcp-routes/{routeId}",
		Summary:     "Get a TCP route",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetTCPRoute)

	huma.Register(api, huma.Operation{
		OperationID: "update-tcp-route",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/tcp-routes/{routeId}",
		Summary:     "Change a TCP route's port or who may connect",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateTCPRoute)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-tcp-route",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/tcp-routes/{routeId}",
		Summary:       "Stop publishing a TCP port",
		Tags:          []string{"Routes"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.DeleteTCPRoute)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (h *Handler) ListTCPRoutes(ctx context.Context, input *ListTCPRoutesInput) (*ListTCPRoutesOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionView, "")
	if err != nil {
		return nil, err
	}
	routes, err := h.svc.TCPRoutes.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &ListTCPRoutesOutput{Body: routes}, nil
}

func (h *Handler) ListOrgTCPRoutes(ctx context.Context, input *ListOrgTCPRoutesInput) (*OrgTCPRoutesOutput, error) {
	_, orgID, _, err := h.checkOrgMemberAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	routes, err := h.svc.TCPRoutes.ListForOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := &OrgTCPRoutesOutput{}
	out.Body.Routes, out.Body.Reserved = routes, svc.ReservedPorts()
	return out, nil
}

func (h *Handler) CreateTCPRoute(ctx context.Context, input *CreateTCPRouteInput) (*GetTCPRouteOutput, error) {
	_, orgID, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionCreate, "")
	if err != nil {
		return nil, err
	}
	in := svc.CreateTCPRouteInput{
		OrgID:        orgID,
		ProjectID:    projectID,
		GatewayPort:  input.Body.GatewayPort,
		AllowedCIDRs: input.Body.AllowedCIDRs,
	}
	if input.Body.ServiceID != nil {
		id, err := parseUUID(*input.Body.ServiceID)
		if err != nil {
			return nil, err
		}
		in.ServiceID = &id
	}
	if input.Body.NodeID != nil {
		id, err := parseUUID(*input.Body.NodeID)
		if err != nil {
			return nil, err
		}
		in.NodeID = &id
	}
	if input.Body.ServicePort != nil {
		in.ServicePort = *input.Body.ServicePort
	}
	if input.Body.NodePort != nil {
		in.NodePort = *input.Body.NodePort
	}

	route, err := h.svc.TCPRoutes.Create(ctx, in)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return &GetTCPRouteOutput{Body: route}, nil
}

func (h *Handler) GetTCPRoute(ctx context.Context, input *TCPRoutePathInput) (*GetTCPRouteOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionView, "")
	if err != nil {
		return nil, err
	}
	routeID, err := parseUUID(input.RouteID)
	if err != nil {
		return nil, err
	}
	route, err := h.svc.TCPRoutes.Get(ctx, routeID, projectID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetTCPRouteOutput{Body: route}, nil
}

func (h *Handler) UpdateTCPRoute(ctx context.Context, input *UpdateTCPRouteInput) (*GetTCPRouteOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
	if err != nil {
		return nil, err
	}
	routeID, err := parseUUID(input.RouteID)
	if err != nil {
		return nil, err
	}
	route, err := h.svc.TCPRoutes.Update(ctx, routeID, projectID, svc.UpdateTCPRouteInput{
		GatewayPort:  input.Body.GatewayPort,
		AllowedCIDRs: input.Body.AllowedCIDRs,
	})
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return &GetTCPRouteOutput{Body: route}, nil
}

func (h *Handler) DeleteTCPRoute(ctx context.Context, input *TCPRoutePathInput) (*struct{}, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionDelete, "")
	if err != nil {
		return nil, err
	}
	routeID, err := parseUUID(input.RouteID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.TCPRoutes.Delete(ctx, routeID, projectID); err != nil {
		return nil, err
	}
	return nil, nil
}
