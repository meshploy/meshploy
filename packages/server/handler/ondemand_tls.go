package handler

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

type OnDemandTLSCheckInput struct {
	Domain string `query:"domain" required:"true"`
}

// registerOnDemandTLSRoutes wires the ask endpoint used ONLY by the on-demand
// TLS Caddyfile (DNS_MODE=ondemand). It is intentionally separate from
// domain-check so the NS-delegation path and its handler stay untouched.
func (h *Handler) registerOnDemandTLSRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "ondemand-tls-check",
		Method:      "GET",
		Path:        "/api/v1/internal/ondemand-tls-check",
		Summary:     "Caddy ask endpoint for On-Demand TLS (self-managed DNS mode)",
		Tags:        []string{"Internal"},
	}, h.OnDemandTLSCheck)
}

// OnDemandTLSCheck authorizes on-demand cert issuance when the install uses
// self-managed DNS (wildcard A record) instead of NS delegation. Caddy asks
// before issuing a cert for any hostname matched by the wildcard or :443
// catchall blocks. It approves:
//
//   - active workload subdomains under the base domain (e.g. app.<DOMAIN>)
//   - verified custom domains (reuses the existing read-only check)
//
// Meshploy's own named subdomains (api/console/headscale) are explicit site
// blocks in Caddyfile.ondemand and obtain HTTP-01 certs without asking, so they
// are not handled here.
func (h *Handler) OnDemandTLSCheck(ctx context.Context, input *OnDemandTLSCheckInput) (*struct{}, error) {
	host := strings.ToLower(strings.TrimSpace(input.Domain))

	// Active workload route under the base domain.
	if base := h.cfg.Domain; base != "" && strings.HasSuffix(host, "."+base) {
		if h.svc.Routes.HasRoute(ctx, host) {
			return &struct{}{}, nil
		}
	}

	// Verified custom domain — same gate the delegation path uses.
	if h.svc.Routes.IsCustomDomainVerified(ctx, host) {
		return &struct{}{}, nil
	}

	return nil, huma.Error403Forbidden("hostname not authorized for on-demand TLS")
}
