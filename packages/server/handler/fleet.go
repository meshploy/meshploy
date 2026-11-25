package handler

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/server/service"
)

// The fleet link is the server's, not one workspace's: only its owner (the
// owner of its first organization) may read, make, pause or end it. It decides
// what leaves the server.

type FleetStatusOutput struct {
	Body *service.FleetStatus
}

type FleetEnrollInput struct {
	Body struct {
		Token string `json:"token" minLength:"1" doc:"The mflt- link token from the account plane"`
	}
}

type FleetReportInput struct {
	Body struct {
		Report bool `json:"report" doc:"Whether this server reports its state to the account plane"`
	}
}

func (h *Handler) registerFleetRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-fleet-link",
		Method:      "GET",
		Path:        "/api/v1/system/fleet",
		Summary:     "This server's link to Meshploy Cloud",
		Description: "Server owner only.",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetFleetLink)

	huma.Register(api, huma.Operation{
		OperationID: "link-fleet",
		Method:      "POST",
		Path:        "/api/v1/system/fleet",
		Summary:     "Link this server to Meshploy Cloud with a link token",
		Description: "Server owner only.",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.LinkFleet)

	huma.Register(api, huma.Operation{
		OperationID: "set-fleet-report",
		Method:      "PATCH",
		Path:        "/api/v1/system/fleet",
		Summary:     "Turn reporting to Meshploy Cloud on or off",
		Description: "Server owner only. Off, the link stands and nothing about the server is sent.",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.SetFleetReport)

	huma.Register(api, huma.Operation{
		OperationID:   "unlink-fleet",
		Method:        "DELETE",
		Path:          "/api/v1/system/fleet",
		Summary:       "Unlink this server from Meshploy Cloud",
		Description:   "Server owner only.",
		Tags:          []string{"System"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.UnlinkFleet)
}

func (h *Handler) requireServerOwner(ctx context.Context) error {
	userID, err := requireUser(ctx)
	if err != nil {
		return err
	}
	owner, err := h.svc.System.IsInstanceOwner(ctx, userID)
	if err != nil {
		return err
	}
	if !owner {
		return huma.Error403Forbidden("only the server's owner can manage its link to Meshploy Cloud")
	}
	return nil
}

func fleetError(err error) error {
	switch {
	case errors.Is(err, service.ErrFleetLinked):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, service.ErrFleetNotLinked):
		return huma.Error404NotFound(err.Error())
	}
	return huma.Error400BadRequest(err.Error())
}

func (h *Handler) GetFleetLink(ctx context.Context, _ *struct{}) (*FleetStatusOutput, error) {
	if err := h.requireServerOwner(ctx); err != nil {
		return nil, err
	}
	st, err := h.svc.Fleet.Status(ctx)
	if err != nil {
		return nil, err
	}
	return &FleetStatusOutput{Body: st}, nil
}

func (h *Handler) LinkFleet(ctx context.Context, in *FleetEnrollInput) (*FleetStatusOutput, error) {
	if err := h.requireServerOwner(ctx); err != nil {
		return nil, err
	}
	userID, _ := requireUser(ctx)
	st, err := h.svc.Fleet.Enroll(ctx, in.Body.Token, userID)
	if err != nil {
		return nil, fleetError(err)
	}
	return &FleetStatusOutput{Body: st}, nil
}

func (h *Handler) SetFleetReport(ctx context.Context, in *FleetReportInput) (*FleetStatusOutput, error) {
	if err := h.requireServerOwner(ctx); err != nil {
		return nil, err
	}
	st, err := h.svc.Fleet.SetReport(ctx, in.Body.Report)
	if err != nil {
		return nil, fleetError(err)
	}
	return &FleetStatusOutput{Body: st}, nil
}

func (h *Handler) UnlinkFleet(ctx context.Context, _ *struct{}) (*struct{}, error) {
	if err := h.requireServerOwner(ctx); err != nil {
		return nil, err
	}
	if err := h.svc.Fleet.Unlink(ctx); err != nil {
		return nil, fleetError(err)
	}
	return nil, nil
}
