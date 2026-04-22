package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	svc "github.com/meshploy/apps/api/internal/service"
)

type RoutePathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	RouteID   string `path:"routeId"`
}

type ListRoutesInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
}

type ListOrgRoutesInput struct {
	OrgID string `path:"orgId"`
}

type ListRoutesOutput struct {
	Body []db.Route
}

type GetRouteOutput struct {
	Body *db.Route
}

type CreateRouteInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		// Domain-based (preferred): supply domain_id + zone + subdomain.
		// Meshploy derives the full hostname and enforces reserved-subdomain rules.
		DomainID  *string `json:"domain_id"`  // UUID of a verified Domain
		Zone      string  `json:"zone"`       // "public" | "internal" | "preview"
		Subdomain string  `json:"subdomain"`  // prefix only, e.g. "keeper"
		// Legacy / manual: supply a raw hostname when domain_id is omitted.
		Hostname   string  `json:"hostname"`
		TargetIP   string  `json:"target_ip"`
		TargetPort int     `json:"target_port" minimum:"1" maximum:"65535"`
		ServiceID  *string `json:"service_id"`
		NodeID     *string `json:"node_id"`
		Port       int     `json:"port" minimum:"1" maximum:"65535"`
	}
}

type CreateRouteOutput struct {
	Body *db.Route
}

type UpdateRouteInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	RouteID   string `path:"routeId"`
	Body      struct {
		TargetIP   string `json:"target_ip"`
		TargetPort int    `json:"target_port" minimum:"1" maximum:"65535"`
	}
}

type UpdateRouteOutput struct {
	Body *db.Route
}

func (h *Handler) registerRouteRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-org-routes",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/routes",
		Summary:     "List all routes in an organization",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListOrgRoutes)

	huma.Register(api, huma.Operation{
		OperationID: "list-routes",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/routes",
		Summary:     "List routes in a project",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListRoutes)

	huma.Register(api, huma.Operation{
		OperationID: "create-route",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/routes",
		Summary:     "Create a route",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.CreateRoute)

	huma.Register(api, huma.Operation{
		OperationID: "get-route",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/routes/{routeId}",
		Summary:     "Get a route",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetRoute)

	huma.Register(api, huma.Operation{
		OperationID: "update-route",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/routes/{routeId}",
		Summary:     "Update a route target",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateRoute)

	huma.Register(api, huma.Operation{
		OperationID: "delete-route",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/routes/{routeId}",
		Summary:     "Delete a route",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteRoute)
}

func (h *Handler) ListOrgRoutes(ctx context.Context, input *ListOrgRoutesInput) (*ListRoutesOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	routes, err := h.svc.Routes.ListByOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return &ListRoutesOutput{Body: routes}, nil
}

func (h *Handler) ListRoutes(ctx context.Context, input *ListRoutesInput) (*ListRoutesOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	routes, err := h.svc.Routes.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &ListRoutesOutput{Body: routes}, nil
}

func (h *Handler) CreateRoute(ctx context.Context, input *CreateRouteInput) (*CreateRouteOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	// service_id is optional
	var parsedServiceID *uuid.UUID
	if input.Body.ServiceID != nil {
		id, err := parseUUID(*input.Body.ServiceID)
		if err != nil {
			return nil, err
		}
		parsedServiceID = &id
	}

	// domain_id is optional — when provided, zone + subdomain are used to derive the hostname
	var parsedDomainID *uuid.UUID
	if input.Body.DomainID != nil {
		id, err := parseUUID(*input.Body.DomainID)
		if err != nil {
			return nil, err
		}
		parsedDomainID = &id
	}

	var parsedNodeID *uuid.UUID
	if input.Body.NodeID != nil {
		id, err := parseUUID(*input.Body.NodeID)
		if err != nil {
			return nil, err
		}
		parsedNodeID = &id
	}

	route, err := h.svc.Routes.Create(ctx, svc.CreateRouteInput{
		OrgID:      orgID,
		ProjectID:  projectID,
		ServiceID:  parsedServiceID,
		NodeID:     parsedNodeID,
		Port:       input.Body.Port,
		DomainID:   parsedDomainID,
		Zone:       db.RouteZone(input.Body.Zone),
		Subdomain:  input.Body.Subdomain,
		Hostname:   input.Body.Hostname,
		TargetIP:   input.Body.TargetIP,
		TargetPort: input.Body.TargetPort,
	})
	if err != nil {
		return nil, err
	}
	return &CreateRouteOutput{Body: route}, nil
}

func (h *Handler) GetRoute(ctx context.Context, input *RoutePathInput) (*GetRouteOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	routeID, err := parseUUID(input.RouteID)
	if err != nil {
		return nil, err
	}
	route, err := h.svc.Routes.Get(ctx, routeID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetRouteOutput{Body: route}, nil
}

func (h *Handler) UpdateRoute(ctx context.Context, input *UpdateRouteInput) (*UpdateRouteOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	routeID, err := parseUUID(input.RouteID)
	if err != nil {
		return nil, err
	}
	route, err := h.svc.Routes.Update(ctx, routeID, input.Body.TargetIP, input.Body.TargetPort)
	if err != nil {
		return nil, notFound(err)
	}
	return &UpdateRouteOutput{Body: route}, nil
}

func (h *Handler) DeleteRoute(ctx context.Context, input *RoutePathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	routeID, err := parseUUID(input.RouteID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.Routes.Delete(ctx, routeID)
}
