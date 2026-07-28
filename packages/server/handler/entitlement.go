package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/server/service"
)

// The entitlement endpoints exist in CE, not just EE.
//
// CE returns an empty feature list, so the web app has one code path instead of
// special-casing a 404. CE also verifies a pasted token — a valid license on a
// CE binary unlocks nothing, because EE features are not compiled in, but
// verifying lets the UI tell the admin the token is good and what to do next
// (switch to the EE image). That guidance is only possible if CE can check the
// signature.

type EntitlementsOutput struct {
	Body service.Status
}

type ActivateLicenseInput struct {
	Body struct {
		Token string `json:"token" minLength:"1" doc:"The mlic- license token, shown once at issuance"`
	}
}

func (h *Handler) registerEntitlementRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-entitlements",
		Method:      "GET",
		Path:        "/api/v1/entitlements",
		Summary:     "Current license entitlements for this install",
		Tags:        []string{"Entitlements"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetEntitlements)

	huma.Register(api, huma.Operation{
		OperationID: "activate-license",
		Method:      "POST",
		Path:        "/api/v1/entitlements/license",
		Summary:     "Install a license token",
		Tags:        []string{"Entitlements"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ActivateLicense)
}

// GetEntitlements is readable by any authenticated user: the UI needs it on
// every page load to decide what to render, and it exposes no secret — the
// token itself is never returned, only the tier and feature list.
func (h *Handler) GetEntitlements(ctx context.Context, _ *struct{}) (*EntitlementsOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.svc.Entitlements.Describe(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &EntitlementsOutput{Body: st}, nil
}

// ActivateLicense installs a token. Restricted to org admins — it changes what
// the whole install is entitled to.
func (h *Handler) ActivateLicense(ctx context.Context, input *ActivateLicenseInput) (*EntitlementsOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgs, err := h.svc.Orgs.ListForUser(ctx, userID)
	if err != nil || len(orgs) == 0 {
		return nil, huma.Error403Forbidden("no organization found")
	}
	if err := h.enforceAdminRole(ctx, orgs[0].ID, userID); err != nil {
		return nil, err
	}

	st, err := h.svc.Entitlements.Activate(ctx, input.Body.Token, userID)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &EntitlementsOutput{Body: st}, nil
}
