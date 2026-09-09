package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/server/service"
)

type VersionInfoOutput struct {
	Body *service.VersionInfo
}

func (h *Handler) registerSystemRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-version",
		Method:      "GET",
		Path:        "/api/v1/system/version",
		Summary:     "Get current and latest platform version",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetVersion)

	huma.Register(api, huma.Operation{
		OperationID: "get-exposure",
		Method:      "GET",
		Path:        "/api/v1/system/exposure",
		Summary:     "Report whether this gateway runs without a host firewall",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetExposure)

	huma.Register(api, huma.Operation{
		OperationID: "dismiss-notice",
		Method:      "POST",
		Path:        "/api/v1/system/notices/{key}/dismiss",
		Summary:     "Dismiss a console advisory for the current user",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DismissNotice)
}

type ExposureOutput struct {
	Body *service.Exposure
}

// GetExposure is deliberately readable by any authenticated member rather than
// admins only: it reports nothing an operator could act on maliciously -- the
// ports are Meshploy's own published defaults, documented in the compose file --
// and hiding it from the person who happens to be signed in is how it goes
// unnoticed.
func (h *Handler) GetExposure(ctx context.Context, _ *struct{}) (*ExposureOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	out, err := h.svc.System.GetExposure(ctx, userID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read exposure state", err)
	}
	return &ExposureOutput{Body: &out}, nil
}

type DismissNoticeInput struct {
	Key string `path:"key" doc:"Notice key, e.g. host-exposure"`
}

func (h *Handler) DismissNotice(ctx context.Context, input *DismissNoticeInput) (*struct{}, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.System.DismissNotice(ctx, userID, input.Key); err != nil {
		return nil, huma.Error400BadRequest("could not dismiss notice", err)
	}
	return nil, nil
}

func (h *Handler) GetVersion(ctx context.Context, _ *struct{}) (*VersionInfoOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	info := h.svc.System.GetVersionInfo(ctx)
	return &VersionInfoOutput{Body: &info}, nil
}
