package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	db "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/dnsprovider"
	"github.com/meshploy/packages/server/service"
	"gorm.io/gorm"
)

// ─── I/O types ────────────────────────────────────────────────────────────────

type ListDNSProvidersOutput struct {
	Body []db.DNSProvider
}

type CreateDNSProviderInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Name  string `json:"name"  minLength:"1" maxLength:"100"`
		Kind  string `json:"kind"  enum:"cloudflare"`
		Token string `json:"token" minLength:"1" maxLength:"500"`
	}
}

type DNSProviderCheckOutput struct {
	Body *service.DNSProviderCheck
}

type DNSProviderPathInput struct {
	OrgID string `path:"orgId"`
	ID    string `path:"id"`
}

// ─── Routes ───────────────────────────────────────────────────────────────────

func (h *Handler) registerDNSProviderRoutes(api huma.API) {
	const tag = "DNS Providers"

	huma.Register(api, huma.Operation{
		OperationID: "list-dns-providers",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/dns-providers",
		Summary:     "List connected DNS providers",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
	}) (*ListDNSProvidersOutput, error) {
		_, orgID, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
		if err != nil {
			return nil, err
		}
		items, err := h.svc.DNSProviders.List(ctx, orgID)
		if err != nil {
			return nil, err
		}
		return &ListDNSProvidersOutput{Body: items}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-dns-provider",
		Method:      http.MethodPost,
		Path:        "/api/v1/orgs/{orgId}/dns-providers",
		Summary:     "Connect a DNS provider",
		Description: "Checks the token first: it must be able to write records in at least one zone. " +
			"The check writes a TXT record under _meshploy-check in each zone the token sees and removes it again.",
		Tags:     []string{tag},
		Security: []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *CreateDNSProviderInput) (*DNSProviderCheckOutput, error) {
		_, orgID, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
		if err != nil {
			return nil, err
		}
		out, err := h.svc.DNSProviders.Create(ctx, orgID, service.CreateDNSProviderInput{
			Name:  in.Body.Name,
			Kind:  dnsprovider.Kind(in.Body.Kind),
			Token: in.Body.Token,
		})
		if err != nil {
			return nil, dnsProviderError(err)
		}
		return &DNSProviderCheckOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "check-dns-provider",
		Method:      http.MethodPost,
		Path:        "/api/v1/orgs/{orgId}/dns-providers/{id}/check",
		Summary:     "Check a DNS provider's token again",
		Description: "Lists the zones the token sees and which of them it may write in, and keeps the answer.",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *DNSProviderPathInput) (*DNSProviderCheckOutput, error) {
		_, orgID, id, err := h.checkOrgAdminAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		out, err := h.svc.DNSProviders.Check(ctx, orgID, id)
		if err != nil {
			return nil, dnsProviderError(err)
		}
		return &DNSProviderCheckOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-dns-provider",
		Method:        http.MethodDelete,
		Path:          "/api/v1/orgs/{orgId}/dns-providers/{id}",
		Summary:       "Disconnect a DNS provider",
		Description:   "Refused while a domain still has its records written through it.",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *DNSProviderPathInput) (*struct{}, error) {
		_, orgID, id, err := h.checkOrgAdminAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		if err := h.svc.DNSProviders.Delete(ctx, orgID, id); err != nil {
			return nil, dnsProviderError(err)
		}
		return nil, nil
	})
}

func dnsProviderError(err error) error {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return huma.Error404NotFound("DNS provider not found")
	case errors.Is(err, service.ErrDNSProviderCheck):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, service.ErrDNSProviderInUse):
		return huma.Error409Conflict(err.Error())
	}
	return err
}
