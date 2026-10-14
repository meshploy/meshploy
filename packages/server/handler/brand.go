package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
)

// An organisation's brand: the name, logos and accent colour people see in
// the console. Community always answers Meshploy's.

type BrandOutput struct {
	Body db.Brand
}

type HostBrandInput struct {
	hostInput
}

type OrgBrandInput struct {
	OrgID string `path:"orgId"`
}

func (h *Handler) registerBrandRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-host-brand",
		Method:      "GET",
		Path:        "/api/v1/brand",
		Summary:     "The brand of the organisation this host belongs to, for the sign-in page",
		Tags:        []string{"Brand"},
	}, h.GetHostBrand)

	huma.Register(api, huma.Operation{
		OperationID: "get-org-brand",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/brand",
		Summary:     "An organisation's brand",
		Tags:        []string{"Brand"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetOrgBrand)
}

// GetHostBrand is public: the sign-in page shows it before anyone signs in,
// and a brand is what an organisation chose to show.
func (h *Handler) GetHostBrand(ctx context.Context, in *HostBrandInput) (*BrandOutput, error) {
	return &BrandOutput{Body: h.svc.Brands.ForHost(ctx, in.host)}, nil
}

func (h *Handler) GetOrgBrand(ctx context.Context, in *OrgBrandInput) (*BrandOutput, error) {
	_, orgID, _, err := h.checkOrgMemberAccess(ctx, in.OrgID, "")
	if err != nil {
		return nil, err
	}
	return &BrandOutput{Body: h.svc.Brands.ForOrg(ctx, orgID)}, nil
}
