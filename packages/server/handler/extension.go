package handler

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
)

// Extension point: additional HTTP routes.
//
// The CE binary never imports the EE module, so routeHooks stays empty in CE
// builds — the same open-core pattern packages/db uses for RegisterMigration.
// An EE package registers from its init():
//
//	func init() { handler.RegisterRoutes(routes) }
//	func routes(api huma.API, h *handler.Handler) { huma.Register(api, ..., h.audit) }
var routeHooks []func(huma.API, *Handler)

// RegisterRoutes adds a callback that mounts additional routes. Callbacks run
// in registration order, after all CE routes are mounted, so an extension can
// rely on the CE surface already existing.
func RegisterRoutes(fn func(huma.API, *Handler)) {
	routeHooks = append(routeHooks, fn)
}

// Services exposes the service aggregate to registered extensions. Extensions
// live in a separate module and cannot reach the unexported field.
func (h *Handler) Services() *service.Services { return h.svc }

// Config exposes the server config to registered extensions. It may be nil in
// tests that construct a Handler without one.
func (h *Handler) Config() *config.Config { return h.cfg }
