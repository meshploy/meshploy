package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
)

// Discovery: what runs on this org's nodes that Meshploy does not route.
//
// Read-only. The actions it leads to - serve a hostname, publish a port, import
// a container - are the route and request endpoints that already exist, called
// with what a row prefills.
func (h *Handler) registerDiscoveryRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-discovery",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/discovery",
		Summary:     "List endpoints and containers on this org's nodes that Meshploy does not route",
		Tags:        []string{"Discovery"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetDiscovery)

	// Recording that an endpoint is known and correct as it is. Nothing in the
	// console calls these yet: Discovery is a view of what a server runs, not a
	// list to be cleared. They exist for the case that earns them - a "new
	// endpoint appeared" notification, which needs a baseline to compare with.
	huma.Register(api, huma.Operation{
		OperationID: "ignore-endpoint",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/discovery/ignores",
		Summary:     "Record that a discovered endpoint is known and correct as it is",
		Tags:        []string{"Discovery"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.IgnoreEndpoint)

	huma.Register(api, huma.Operation{
		OperationID:   "unignore-endpoint",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/discovery/ignores/{ignoreId}",
		Summary:       "Remove that record",
		Tags:          []string{"Discovery"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.UnignoreEndpoint)
}

type IgnoreEndpointInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		NodeID  string `json:"node_id"`
		Address string `json:"address"`
		Port    int    `json:"port"`
		Note    string `json:"note,omitempty" maxLength:"500"`
	}
}

type IgnoreEndpointOutput struct {
	Body *db.IgnoredEndpoint
}

func (h *Handler) IgnoreEndpoint(ctx context.Context, input *IgnoreEndpointInput) (*IgnoreEndpointOutput, error) {
	userID, orgID, nodeID, err := h.checkOrgMemberAccess(ctx, input.OrgID, input.Body.NodeID)
	if err != nil {
		return nil, err
	}
	row, err := h.svc.System.IgnoreEndpoint(ctx, orgID, nodeID, userID, input.Body.Address, input.Body.Port, input.Body.Note)
	if err != nil {
		return nil, err
	}
	return &IgnoreEndpointOutput{Body: row}, nil
}

type UnignoreEndpointInput struct {
	OrgID    string `path:"orgId"`
	IgnoreID string `path:"ignoreId"`
}

func (h *Handler) UnignoreEndpoint(ctx context.Context, input *UnignoreEndpointInput) (*struct{}, error) {
	_, orgID, ignoreID, err := h.checkOrgMemberAccess(ctx, input.OrgID, input.IgnoreID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.System.UnignoreEndpoint(ctx, orgID, ignoreID)
}

type DiscoveryInput struct {
	OrgID string `path:"orgId"`
}

type DiscoveryOutput struct {
	Body service.Discovery
}

func (h *Handler) GetDiscovery(ctx context.Context, input *DiscoveryInput) (*DiscoveryOutput, error) {
	_, orgID, _, err := h.checkOrgMemberAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	out, err := h.svc.System.GetDiscovery(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return &DiscoveryOutput{Body: out}, nil
}
