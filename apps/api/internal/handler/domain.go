package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	svc "github.com/meshploy/apps/api/internal/service"
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

type CreateDomainInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		BaseDomain        string `json:"base_domain" minLength:"3"`
		InternalSubdomain string `json:"internal_subdomain"` // optional, defaults to "internal"
		PreviewSubdomain  string `json:"preview_subdomain"`  // optional, defaults to "preview"
	}
}

type CreateDomainOutput struct {
	Body *db.Domain
}

type UpdateDomainInput struct {
	OrgID    string `path:"orgId"`
	DomainID string `path:"domainId"`
	Body     struct {
		InternalSubdomain string `json:"internal_subdomain"`
		PreviewSubdomain  string `json:"preview_subdomain"`
	}
}

type UpdateDomainOutput struct {
	Body *db.Domain
}

func (h *Handler) registerDomainRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-domains",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/domains",
		Summary:     "List domains for an organization",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListDomains)

	huma.Register(api, huma.Operation{
		OperationID: "create-domain",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/domains",
		Summary:     "Register a new domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.CreateDomain)

	huma.Register(api, huma.Operation{
		OperationID: "get-domain",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}",
		Summary:     "Get a domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetDomain)

	huma.Register(api, huma.Operation{
		OperationID: "update-domain",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}",
		Summary:     "Update reserved subdomains",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateDomain)

	huma.Register(api, huma.Operation{
		OperationID: "delete-domain",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}",
		Summary:     "Delete a domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteDomain)

	huma.Register(api, huma.Operation{
		OperationID: "verify-domain",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}/verify",
		Summary:     "Verify domain ownership via DNS TXT record",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.VerifyDomain)
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

func (h *Handler) CreateDomain(ctx context.Context, input *CreateDomainInput) (*CreateDomainOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Create(ctx, orgID, svc.CreateDomainInput{
		BaseDomain:        input.Body.BaseDomain,
		InternalSubdomain: input.Body.InternalSubdomain,
		PreviewSubdomain:  input.Body.PreviewSubdomain,
	})
	if err != nil {
		return nil, err
	}
	return &CreateDomainOutput{Body: domain}, nil
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

func (h *Handler) UpdateDomain(ctx context.Context, input *UpdateDomainInput) (*UpdateDomainOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	domainID, err := parseUUID(input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Update(ctx, domainID, svc.UpdateDomainInput{
		InternalSubdomain: input.Body.InternalSubdomain,
		PreviewSubdomain:  input.Body.PreviewSubdomain,
	})
	if err != nil {
		return nil, notFound(err)
	}
	return &UpdateDomainOutput{Body: domain}, nil
}

func (h *Handler) DeleteDomain(ctx context.Context, input *DomainPathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	domainID, err := parseUUID(input.DomainID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.Domains.Delete(ctx, domainID)
}

func (h *Handler) VerifyDomain(ctx context.Context, input *DomainPathInput) (*GetDomainOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	domainID, err := parseUUID(input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Verify(ctx, domainID)
	if err != nil {
		return nil, err
	}
	return &GetDomainOutput{Body: domain}, nil
}
