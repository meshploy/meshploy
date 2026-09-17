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
		OperationID: "get-host-agent",
		Method:      "GET",
		Path:        "/api/v1/system/host-agent",
		Summary:     "Whether the gateway's host agent is reporting",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetHostAgent)

	huma.Register(api, huma.Operation{
		OperationID: "get-dokploy-migration",
		Method:      "GET",
		Path:        "/api/v1/system/migrate/dokploy",
		Summary:     "The latest Dokploy detection and plan from the host agent, and their requests",
		Description: "Instance owner only: the plan names every app, domain and path on the server.",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetDokployMigration)

	huma.Register(api, huma.Operation{
		OperationID:   "request-dokploy-migration",
		Method:        "POST",
		Path:          "/api/v1/system/migrate/dokploy/{kind}",
		Summary:       "Ask the host agent to detect Dokploy or plan moving it",
		Description:   "Read-only on the host. Queues the request; follow it with GET /system/migrate/dokploy.",
		Tags:          []string{"System"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 202,
	}, h.RequestDokployMigration)

	huma.Register(api, huma.Operation{
		OperationID: "check-for-updates",
		Method:      "POST",
		Path:        "/api/v1/system/check-updates",
		Summary:     "Ask GitHub for the newest release or build now, past the caches",
		Description: "Forced checks are limited to one every 30 seconds; a sooner one returns the last answer.",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.CheckForUpdates)

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
		Summary:       "Queue an upgrade of this server to the latest build on its channel, or a switch to the other channel",
		Tags:          []string{"System"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusAccepted,
	}, h.RequestUpgrade)

	huma.Register(api, huma.Operation{
		OperationID: "get-channels",
		Method:      "GET",
		Path:        "/api/v1/system/channels",
		Summary:     "Describe the stable and edge channels, where this server is, and whether it may switch",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetChannels)
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

func (h *Handler) GetDokployMigration(ctx context.Context, _ *struct{}) (*struct{ Body service.MigrationState }, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.svc.System.GetMigrationState(ctx, userID)
	if errors.Is(err, service.ErrMigrationNotOwner) {
		return nil, huma.Error403Forbidden(err.Error())
	}
	if err != nil {
		return nil, err
	}
	return &struct{ Body service.MigrationState }{Body: st}, nil
}

func (h *Handler) RequestDokployMigration(ctx context.Context, in *struct {
	Kind string `path:"kind" enum:"detect,plan"`
}) (*struct{ Body service.HostRequestState }, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.svc.System.RequestMigration(ctx, userID, in.Kind)
	switch {
	case errors.Is(err, service.ErrMigrationNotOwner):
		return nil, huma.Error403Forbidden(err.Error())
	case errors.Is(err, service.ErrHostAgentNotReporting):
		return nil, huma.Error409Conflict(err.Error())
	case errors.Is(err, service.ErrUnknownHostRequest):
		return nil, huma.Error404NotFound(err.Error())
	case err != nil:
		return nil, err
	}
	return &struct{ Body service.HostRequestState }{Body: st}, nil
}

func (h *Handler) GetHostAgent(ctx context.Context, _ *struct{}) (*struct{ Body service.HostAgentStatus }, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	return &struct{ Body service.HostAgentStatus }{Body: h.svc.System.HostAgentStatus()}, nil
}

func (h *Handler) CheckForUpdates(ctx context.Context, _ *struct{}) (*VersionInfoOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	info := h.svc.System.CheckForUpdates(ctx)
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

type RequestUpgradeInput struct {
	// Optional: a bare POST upgrades on the server's own channel.
	Body *struct {
		Channel string `json:"channel,omitempty" enum:"stable,edge" doc:"Switch to this channel. Omit to stay on the server's own."`
		Edition string `json:"edition,omitempty" enum:"enterprise" doc:"Switch to the Enterprise images the active licence grants. Omit to keep the images the server runs."`
	}
}

// RequestUpgrade upgrades on the server's own channel, or switches channel or
// edition. What reaches the host is the channel, which the runner checks again,
// and for a switch to Enterprise the image the verified licence names, never
// one the client sent.
func (h *Handler) RequestUpgrade(ctx context.Context, input *RequestUpgradeInput) (*UpgradeStatusOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	var opts service.UpgradeOptions
	if input.Body != nil {
		opts.Channel, opts.Edition = input.Body.Channel, input.Body.Edition
	}
	st, err := h.svc.System.RequestUpgrade(ctx, userID, opts)
	switch {
	case errors.Is(err, service.ErrNotInstanceOwner):
		return nil, huma.Error403Forbidden(err.Error())
	case errors.Is(err, service.ErrUnknownChannel), errors.Is(err, service.ErrUnknownEdition):
		return nil, huma.Error400BadRequest(err.Error())
	case errors.Is(err, service.ErrUpgradeNotEnabled),
		errors.Is(err, service.ErrUpgradeRunning),
		errors.Is(err, service.ErrUpgradeDevBuild),
		errors.Is(err, service.ErrChannelSwitchRefused),
		errors.Is(err, service.ErrEditionSwitchRefused):
		return nil, huma.Error409Conflict(err.Error())
	case err != nil:
		return nil, huma.Error500InternalServerError("failed to queue the upgrade", err)
	}
	return &UpgradeStatusOutput{Body: &st}, nil
}

type ChannelsOutput struct {
	Body *service.Channels
}

// GetChannels is readable by any member, like the version; switching is the
// instance owner's, and RequestUpgrade checks that.
func (h *Handler) GetChannels(ctx context.Context, _ *struct{}) (*ChannelsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	out := h.svc.System.GetChannels(ctx)
	return &ChannelsOutput{Body: &out}, nil
}
