package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
)

// The edge fallback: where the proxy sends hostnames it has no route for while
// a migration serves them from the old platform's edge. Set and cleared by the
// migration; read by the console. Admin only - it decides where traffic goes.

type EdgeFallbackOutput struct {
	Body *db.EdgeFallback
}

type SetEdgeFallbackInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Upstream  string   `json:"upstream" doc:"host:port of the old edge's HTTP entrypoint, e.g. 127.0.0.1:18080"`
		Hostnames []string `json:"hostnames" doc:"Domains the old edge still serves"`
	}
}

func (h *Handler) registerEdgeFallbackRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-edge-fallback",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/edge-fallback",
		Summary:     "Where the proxy sends hostnames it has no route for, during a migration; null when nothing",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
	}) (*EdgeFallbackOutput, error) {
		if _, _, _, err := h.checkOrgMemberAccess(ctx, in.OrgID, ""); err != nil {
			return nil, err
		}
		f, err := h.svc.EdgeFallback.Get(ctx)
		if err != nil {
			return nil, err
		}
		return &EdgeFallbackOutput{Body: f}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-edge-fallback",
		Method:      http.MethodPut,
		Path:        "/api/v1/orgs/{orgId}/edge-fallback",
		Summary:     "Send the hostnames the old platform still serves to its edge, on a side port",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *SetEdgeFallbackInput) (*EdgeFallbackOutput, error) {
		if _, _, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, ""); err != nil {
			return nil, err
		}
		f, err := h.svc.EdgeFallback.Set(ctx, in.Body.Upstream, in.Body.Hostnames)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return &EdgeFallbackOutput{Body: f}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "clear-edge-fallback",
		Method:        http.MethodDelete,
		Path:          "/api/v1/orgs/{orgId}/edge-fallback",
		Summary:       "Stop sending anything to the old platform's edge",
		Tags:          []string{"System"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
	}) (*struct{}, error) {
		if _, _, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, ""); err != nil {
			return nil, err
		}
		return nil, h.svc.EdgeFallback.Clear(ctx)
	})
}
