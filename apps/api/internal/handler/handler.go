package handler

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
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

// Register wires all Huma (OpenAPI) routes onto the router.
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
	h.registerStorageRoutes(api)
	h.registerBackupRoutes(api)
	h.registerNotificationRoutes(api)
	h.registerEmailConfigRoutes(api)
	h.registerSecretRoutes(api)
	h.registerVariableGroupRoutes(api)
	h.registerJobRoutes(api)
	h.registerStackRoutes(api)
	h.registerVolumeRoutes(api)
}

// RegisterRaw wires routes that need raw http.HandlerFunc access:
// OAuth redirects, SSE log streams, and WebSocket connections.
func (h *Handler) RegisterRaw(r chi.Router) {
	// Install/uninstall scripts — handlers call requireUser internally to enforce auth.
	r.Get("/install.sh", h.ServeInstallScript)
	r.Get("/uninstall.sh", h.ServeUninstallScript)

	// Git OAuth / App callbacks
	r.Get("/api/v1/github/app-callback", h.GitHubAppCallback)
	r.Get("/api/v1/github/callback", h.GitHubCallback)
	r.Get("/api/v1/gitlab/callback", h.GitLabOAuthCallback)
	r.Get("/api/v1/gitea/callback", h.GiteaOAuthCallback)

	// SSE log streams
	r.Get("/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}/logs/stream",
		h.StreamDeploymentLogs)
	r.Get("/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/logs/stream",
		h.StreamServiceLogs)
	r.Get("/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/logs",
		h.GetServiceLogs)

	// WebSocket: node terminal
	r.Get("/api/v1/orgs/{orgId}/nodes/{nodeId}/terminal", h.NodeTerminal)

	// WebSocket: service pod terminal
	r.Get("/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/pods/{podName}/terminal",
		h.ServiceTerminal)
}
