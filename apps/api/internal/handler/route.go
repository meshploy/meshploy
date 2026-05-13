package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	svc "github.com/meshploy/apps/api/internal/service"
)

// ── Path params ───────────────────────────────────────────────────────────────

type RoutePathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	RouteID   string `path:"routeId"`
}

type RouteTargetPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	RouteID   string `path:"routeId"`
	TargetID  string `path:"targetId"`
}

type ListRoutesInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
}

type ListOrgRoutesInput struct {
	OrgID string `path:"orgId"`
}

// ── Output types ──────────────────────────────────────────────────────────────

type ListRoutesOutput struct{ Body []db.Route }
type GetRouteOutput struct{ Body *db.Route }
type GetRouteTargetOutput struct{ Body *db.RouteTarget }

// ── Create route ──────────────────────────────────────────────────────────────

type CreateRouteInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		DomainID  *string `json:"domain_id,omitempty"`
		Zone      string  `json:"zone"`
		Subdomain string  `json:"subdomain,omitempty"`
		Hostname  *string `json:"hostname,omitempty"`
		Targets   []struct {
			Path      string  `json:"path"`
			StripPath bool    `json:"strip_path"`
			ServiceID *string `json:"service_id,omitempty"`
			NodeID    *string `json:"node_id,omitempty"`
			Port      *int    `json:"port,omitempty"`
		} `json:"targets"`
	}
}

type CreateRouteOutput struct{ Body *db.Route }

// ── Add / update / delete target ─────────────────────────────────────────────

type UpsertTargetInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	RouteID   string `path:"routeId"`
	Body      struct {
		Path      string  `json:"path"`
		StripPath bool    `json:"strip_path"`
		ServiceID *string `json:"service_id,omitempty"`
		NodeID    *string `json:"node_id,omitempty"`
		Port      *int    `json:"port,omitempty"`
	}
}

type AddTargetInput = UpsertTargetInput

type UpdateTargetInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	RouteID   string `path:"routeId"`
	TargetID  string `path:"targetId"`
	Body      struct {
		Path      string  `json:"path"`
		StripPath bool    `json:"strip_path"`
		ServiceID *string `json:"service_id,omitempty"`
		NodeID    *string `json:"node_id,omitempty"`
		Port      *int    `json:"port,omitempty"`
	}
}

// ── Registration ──────────────────────────────────────────────────────────────

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
		OperationID: "delete-route",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/routes/{routeId}",
		Summary:     "Delete a route",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteRoute)

	huma.Register(api, huma.Operation{
		OperationID: "verify-custom-hostname",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/routes/{routeId}/verify-hostname",
		Summary:     "Verify DNS ownership of a custom-domain route via TXT record",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.VerifyCustomHostname)

	// Target CRUD
	huma.Register(api, huma.Operation{
		OperationID: "add-route-target",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/routes/{routeId}/targets",
		Summary:     "Add a path target to a route",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.AddRouteTarget)

	huma.Register(api, huma.Operation{
		OperationID: "update-route-target",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/routes/{routeId}/targets/{targetId}",
		Summary:     "Update a route target",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateRouteTarget)

	huma.Register(api, huma.Operation{
		OperationID: "delete-route-target",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/routes/{routeId}/targets/{targetId}",
		Summary:     "Delete a route target",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteRouteTarget)

	huma.Register(api, huma.Operation{
		OperationID: "sync-route-target",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/routes/{routeId}/targets/{targetId}/sync",
		Summary:     "Re-resolve target IP from current service node",
		Tags:        []string{"Routes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.SyncRouteTarget)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

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

	var domainID *uuid.UUID
	if input.Body.DomainID != nil {
		id, err := parseUUID(*input.Body.DomainID)
		if err != nil {
			return nil, err
		}
		domainID = &id
	}

	targets := make([]svc.TargetInput, 0, len(input.Body.Targets))
	for _, t := range input.Body.Targets {
		ti := svc.TargetInput{
			Path:      t.Path,
			StripPath: t.StripPath,
		}
		if t.ServiceID != nil {
			id, err := parseUUID(*t.ServiceID)
			if err != nil {
				return nil, huma.Error400BadRequest("invalid service_id")
			}
			ti.ServiceID = &id
		}
		if t.NodeID != nil {
			id, err := parseUUID(*t.NodeID)
			if err != nil {
				return nil, huma.Error400BadRequest("invalid node_id")
			}
			ti.NodeID = &id
		}
		if t.Port != nil {
			ti.Port = *t.Port
		}
		targets = append(targets, ti)
	}

	hostname := ""
	if input.Body.Hostname != nil {
		hostname = *input.Body.Hostname
	}

	route, err := h.svc.Routes.Create(ctx, svc.CreateRouteInput{
		OrgID:     orgID,
		ProjectID: projectID,
		DomainID:  domainID,
		Zone:      db.RouteZone(input.Body.Zone),
		Subdomain: input.Body.Subdomain,
		Hostname:  hostname,
		Targets:   targets,
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

func (h *Handler) VerifyCustomHostname(ctx context.Context, input *RoutePathInput) (*GetRouteOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	routeID, err := parseUUID(input.RouteID)
	if err != nil {
		return nil, err
	}
	route, err := h.svc.Routes.VerifyCustomHostname(ctx, routeID)
	if err != nil {
		return nil, err
	}
	return &GetRouteOutput{Body: route}, nil
}

func (h *Handler) AddRouteTarget(ctx context.Context, input *AddTargetInput) (*GetRouteTargetOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	routeID, err := parseUUID(input.RouteID)
	if err != nil {
		return nil, err
	}
	ti, err := parseTargetBody(input.Body.Path, input.Body.StripPath, input.Body.ServiceID, input.Body.NodeID, input.Body.Port)
	if err != nil {
		return nil, err
	}
	target, err := h.svc.Routes.AddTarget(ctx, routeID, ti)
	if err != nil {
		return nil, err
	}
	return &GetRouteTargetOutput{Body: target}, nil
}

func (h *Handler) UpdateRouteTarget(ctx context.Context, input *UpdateTargetInput) (*GetRouteTargetOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	targetID, err := parseUUID(input.TargetID)
	if err != nil {
		return nil, err
	}
	ti, err := parseTargetBody(input.Body.Path, input.Body.StripPath, input.Body.ServiceID, input.Body.NodeID, input.Body.Port)
	if err != nil {
		return nil, err
	}
	target, err := h.svc.Routes.UpdateTarget(ctx, targetID, ti)
	if err != nil {
		return nil, err
	}
	return &GetRouteTargetOutput{Body: target}, nil
}

func (h *Handler) DeleteRouteTarget(ctx context.Context, input *RouteTargetPathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	targetID, err := parseUUID(input.TargetID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.Routes.DeleteTarget(ctx, targetID)
}

func (h *Handler) SyncRouteTarget(ctx context.Context, input *RouteTargetPathInput) (*GetRouteTargetOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	targetID, err := parseUUID(input.TargetID)
	if err != nil {
		return nil, err
	}
	target, err := h.svc.Routes.SyncTargetIP(ctx, targetID)
	if err != nil {
		return nil, err
	}
	return &GetRouteTargetOutput{Body: target}, nil
}

// ── Shared helper ─────────────────────────────────────────────────────────────

func parseTargetBody(path string, stripPath bool, serviceID, nodeID *string, port *int) (svc.TargetInput, error) {
	ti := svc.TargetInput{Path: path, StripPath: stripPath}
	if serviceID != nil {
		id, err := parseUUID(*serviceID)
		if err != nil {
			return ti, huma.Error400BadRequest("invalid service_id")
		}
		ti.ServiceID = &id
	}
	if nodeID != nil {
		id, err := parseUUID(*nodeID)
		if err != nil {
			return ti, huma.Error400BadRequest("invalid node_id")
		}
		ti.NodeID = &id
	}
	if port != nil {
		ti.Port = *port
	}
	return ti, nil
}
