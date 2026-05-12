package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
)

type DomainPathInput struct {
	OrgID    string `path:"orgId"`
	DomainID string `path:"domainId"`
}

type ListDomainsInput struct {
	OrgID string `path:"orgId"`
}

type ListDomainsOutput struct {
	Body []db.Domain
}

type GetDomainOutput struct {
	Body *db.Domain
}

type DomainCheckInput struct {
	Domain string `query:"domain" required:"true"`
}

func (h *Handler) registerDomainRoutes(api huma.API) {
	// Internal endpoint — called by Caddy before issuing an On-Demand TLS cert.
	// No auth: returns 200 if the hostname belongs to a verified custom-domain route.
	huma.Register(api, huma.Operation{
		OperationID: "domain-check",
		Method:      "GET",
		Path:        "/api/v1/internal/domain-check",
		Summary:     "Caddy ask endpoint for On-Demand TLS",
		Tags:        []string{"Internal"},
	}, h.DomainCheck)

	huma.Register(api, huma.Operation{
		OperationID: "list-domains",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/domains",
		Summary:     "List domains for an organization",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListDomains)

	huma.Register(api, huma.Operation{
		OperationID: "get-domain",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}",
		Summary:     "Get a domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetDomain)
}

func (h *Handler) ListDomains(ctx context.Context, input *ListDomainsInput) (*ListDomainsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	domains, err := h.svc.Domains.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return &ListDomainsOutput{Body: domains}, nil
}

func (h *Handler) GetDomain(ctx context.Context, input *DomainPathInput) (*GetDomainOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	domainID, err := parseUUID(input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Get(ctx, domainID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetDomainOutput{Body: domain}, nil
}

// DomainCheck is called by Caddy's on_demand_tls ask mechanism before it issues
// a TLS cert for an unknown hostname. Returns 200 only for verified custom domains.
func (h *Handler) DomainCheck(ctx context.Context, input *DomainCheckInput) (*struct{}, error) {
	if !h.svc.Routes.IsCustomDomainVerified(ctx, input.Domain) {
		return nil, huma.Error403Forbidden("domain not verified")
	}
	return &struct{}{}, nil
}
