package handler

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
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
		Report    *bool `json:"report,omitempty" doc:"Whether this server reports its state to the account plane"`
		AgentApps *bool `json:"agent_apps,omitempty" doc:"Whether agent apps may act, through the account plane, as people here who linked their accounts"`
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
		Summary:     "Turn reporting, or agent apps, on or off",
		Description: "Server owner only. Reporting off, the link stands and nothing about the server is sent. Agent apps off, no call runs, whoever linked their account.",
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

// A person's own Meshploy account: whether agent apps may act as them here.
// Every signed-in person has these for themselves; agent apps never do (the
// fleet connector refuses /api/v1/me).

type MyAccountOutput struct {
	Body *service.MyAccount
}

type StartPersonLinkOutput struct {
	Body struct {
		URL string `json:"url" doc:"Where to sign in to the account plane and approve; works once, for 15 minutes"`
	}
}

type MyAgentCallsOutput struct {
	Body struct {
		Calls []db.FleetCall `json:"calls"`
	}
}

func (h *Handler) registerMyAccountRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-my-meshploy-account", Method: "GET", Path: "/api/v1/me/meshploy-account",
		Summary: "Whether your Meshploy account may act as you here", Tags: []string{"Account"},
		Security: []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, _ *struct{}) (*MyAccountOutput, error) {
		userID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		out, err := h.svc.Fleet.MyAccount(ctx, userID)
		if err != nil {
			return nil, err
		}
		return &MyAccountOutput{Body: out}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "link-my-meshploy-account", Method: "POST", Path: "/api/v1/me/meshploy-account",
		Summary: "Start linking your Meshploy account: returns where to approve it", Tags: []string{"Account"},
		Security: []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, _ *struct{}) (*StartPersonLinkOutput, error) {
		userID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		u, err := h.svc.Fleet.StartPersonLink(ctx, userID)
		if err != nil {
			return nil, fleetError(err)
		}
		out := &StartPersonLinkOutput{}
		out.Body.URL = u
		return out, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "unlink-my-meshploy-account", Method: "DELETE", Path: "/api/v1/me/meshploy-account",
		Summary: "Stop agent apps acting as you here", Tags: []string{"Account"},
		Security: []map[string][]string{{"bearer": {}}}, DefaultStatus: 204,
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		userID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		if err := h.svc.Fleet.UnlinkPerson(ctx, userID); err != nil {
			return nil, fleetError(err)
		}
		return nil, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "list-my-agent-calls", Method: "GET", Path: "/api/v1/me/meshploy-account/calls",
		Summary: "What agent apps did here acting as you, newest first", Tags: []string{"Account"},
		Security: []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, _ *struct{}) (*MyAgentCallsOutput, error) {
		userID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		calls, err := h.svc.Fleet.CallsFor(ctx, userID, 100)
		if err != nil {
			return nil, err
		}
		out := &MyAgentCallsOutput{}
		out.Body.Calls = calls
		return out, nil
	})
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
	case errors.Is(err, service.ErrFleetNotLinked), errors.Is(err, service.ErrPersonNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, service.ErrAgentAppsOff):
		return huma.Error403Forbidden(err.Error())
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
	var st *service.FleetStatus
	var err error
	if in.Body.Report != nil {
		if st, err = h.svc.Fleet.SetReport(ctx, *in.Body.Report); err != nil {
			return nil, fleetError(err)
		}
	}
	if in.Body.AgentApps != nil {
		userID, _ := requireUser(ctx)
		if st, err = h.svc.Fleet.SetAgentApps(ctx, *in.Body.AgentApps, userID); err != nil {
			return nil, fleetError(err)
		}
	}
	if st == nil {
		if st, err = h.svc.Fleet.Status(ctx); err != nil {
			return nil, err
		}
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
