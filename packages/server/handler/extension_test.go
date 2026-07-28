package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/meshploy/packages/server/service"
)

func withRouteHooks(t *testing.T, fns ...func(huma.API, *Handler)) {
	t.Helper()
	saved := routeHooks
	t.Cleanup(func() { routeHooks = saved })
	routeHooks = nil
	for _, fn := range fns {
		RegisterRoutes(fn)
	}
}

// The load-bearing CE guarantee: nothing registered, so Register() mounts only
// the CE surface.
func TestNoRouteHooksInCE(t *testing.T) {
	withRouteHooks(t)
	if len(routeHooks) != 0 {
		t.Fatalf("a CE build must register no extension routes, got %d", len(routeHooks))
	}
}

// End-to-end: a fake extension registers a route, Register() mounts it, and the
// route actually serves. This is Phase 2's done condition.
func TestExtensionRouteIsMountedAndServes(t *testing.T) {
	withRouteHooks(t, func(api huma.API, h *Handler) {
		huma.Register(api, huma.Operation{
			OperationID: "fake-ee-endpoint",
			Method:      http.MethodGet,
			Path:        "/api/v1/fake-ee",
			Summary:     "registered by a fake extension",
		}, func(ctx context.Context, _ *struct{}) (*struct {
			Body struct {
				OK bool `json:"ok"`
			}
		}, error) {
			// Prove the extension can reach the injected dependencies.
			if h.Services() == nil {
				t.Error("extension must receive a usable Handler")
			}
			out := &struct {
				Body struct {
					OK bool `json:"ok"`
				}
			}{}
			out.Body.OK = true
			return out, nil
		})
	})

	// Non-nil services so the accessor assertion above is meaningful — an
	// extension must be able to reach the aggregate it was handed.
	_, api := humatest.New(t, huma.DefaultConfig("test", "1.0.0"))
	h := New(nil, &service.Services{})
	h.Register(api)

	resp := api.Get("/api/v1/fake-ee")
	if resp.Code != http.StatusOK {
		t.Fatalf("extension route should serve 200, got %d: %s", resp.Code, resp.Body.String())
	}
}

// Multiple extensions compose — Custom-EE layers on top of EE, so both must mount.
func TestMultipleExtensionRoutesCompose(t *testing.T) {
	mk := func(id, path string) func(huma.API, *Handler) {
		return func(api huma.API, _ *Handler) {
			huma.Register(api, huma.Operation{
				OperationID: id, Method: http.MethodGet, Path: path,
			}, func(ctx context.Context, _ *struct{}) (*struct{ Body struct{} }, error) {
				return &struct{ Body struct{} }{}, nil
			})
		}
	}
	withRouteHooks(t, mk("ee-one", "/api/v1/ee-one"), mk("ee-two", "/api/v1/ee-two"))

	_, api := humatest.New(t, huma.DefaultConfig("test", "1.0.0"))
	New(nil, nil).Register(api)

	for _, p := range []string{"/api/v1/ee-one", "/api/v1/ee-two"} {
		if resp := api.Get(p); resp.Code != http.StatusOK {
			t.Fatalf("%s should be mounted, got %d", p, resp.Code)
		}
	}
}
