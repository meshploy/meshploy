package handler

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/apps/api/internal/config"
	"github.com/meshploy/apps/api/internal/service"
)

type Handler struct {
	cfg *config.Config
	svc *service.Services
}

func New(cfg *config.Config, svc *service.Services) *Handler {
	return &Handler{cfg: cfg, svc: svc}
}

// Register wires all route groups onto the Huma API instance.
func (h *Handler) Register(api huma.API) {
	h.registerAuthRoutes(api)
	h.registerOrgRoutes(api)
	h.registerProjectRoutes(api)
	h.registerNodeRoutes(api)
	h.registerWorkloadRoutes(api)
	h.registerDomainRoutes(api)
	h.registerRouteRoutes(api)
	h.registerDeploymentRoutes(api)
	h.registerGitIntegrationRoutes(api)
	h.registerRegistryRoutes(api)
}
