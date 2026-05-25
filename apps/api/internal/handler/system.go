package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/apps/api/internal/service"
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
}

func (h *Handler) GetVersion(ctx context.Context, _ *struct{}) (*VersionInfoOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	info := h.svc.System.GetVersionInfo(ctx)
	return &VersionInfoOutput{Body: &info}, nil
}
