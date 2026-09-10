package handler

import (
	"context"
	"errors"
	"net/http"

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

	huma.Register(api, huma.Operation{
		OperationID: "get-upgrade",
		Method:      "GET",
		Path:        "/api/v1/system/upgrade",
		Summary:     "Report whether this server can be upgraded from the console, and the last upgrade",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetUpgrade)

	huma.Register(api, huma.Operation{
		OperationID:   "request-upgrade",
		Method:        "POST",
		Path:          "/api/v1/system/upgrade",
		Summary:       "Queue an upgrade of this server to the latest build on its channel",
		Tags:          []string{"System"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusAccepted,
	}, h.RequestUpgrade)
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

type UpgradeStatusOutput struct {
	Body *service.UpgradeStatus
}

// GetUpgrade is readable by any member, so everyone signed in can see that an
// upgrade is under way; the service gives the log only to the instance owner.
func (h *Handler) GetUpgrade(ctx context.Context, _ *struct{}) (*UpgradeStatusOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.svc.System.GetUpgradeStatus(ctx, userID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read the upgrade status", err)
	}
	return &UpgradeStatusOutput{Body: &st}, nil
}

// RequestUpgrade takes no body. The channel is the running build's, so a
// request cannot move a stable server onto edge, and nothing the client sends
// reaches the host.
func (h *Handler) RequestUpgrade(ctx context.Context, _ *struct{}) (*UpgradeStatusOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.svc.System.RequestUpgrade(ctx, userID)
	switch {
	case errors.Is(err, service.ErrNotInstanceOwner):
		return nil, huma.Error403Forbidden(err.Error())
	case errors.Is(err, service.ErrUpgradeNotEnabled),
		errors.Is(err, service.ErrUpgradeRunning),
		errors.Is(err, service.ErrUpgradeDevBuild):
		return nil, huma.Error409Conflict(err.Error())
	case err != nil:
		return nil, huma.Error500InternalServerError("failed to queue the upgrade", err)
	}
	return &UpgradeStatusOutput{Body: &st}, nil
}
