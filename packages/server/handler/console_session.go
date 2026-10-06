package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/server/middleware"
	"github.com/meshploy/packages/server/service"
)

// Console sessions: each sign-in to the console, which its person, or an
// organisation's admin, can see and end.

type ConsoleSessionsOutput struct {
	Body []service.ConsoleSessionView
}

type ConsoleSessionInput struct {
	ID string `path:"id"`
}

type OrgConsoleSessionsOutput struct {
	Body []service.OrgConsoleSession
}

type OrgConsoleSessionInput struct {
	OrgID     string `path:"orgId"`
	SessionID string `path:"sessionId"`
}

type EndedSessionsOutput struct {
	Body struct {
		Ended int64 `json:"ended"`
	}
}

func (h *Handler) registerConsoleSessionRoutes(api huma.API) {
	sec := []map[string][]string{{"bearer": {}}}
	huma.Register(api, huma.Operation{OperationID: "logout", Method: http.MethodPost, Path: "/api/v1/auth/logout",
		Summary: "End the console session making this request", Tags: []string{"Auth"}, Security: sec,
		DefaultStatus: http.StatusNoContent}, h.Logout)
	huma.Register(api, huma.Operation{OperationID: "list-console-sessions", Method: http.MethodGet, Path: "/api/v1/me/sessions",
		Summary: "Where you are signed in to the console", Tags: []string{"Auth"}, Security: sec}, h.ListConsoleSessions)
	huma.Register(api, huma.Operation{OperationID: "revoke-console-session", Method: http.MethodDelete, Path: "/api/v1/me/sessions/{id}",
		Summary: "Sign one of your console sessions out", Tags: []string{"Auth"}, Security: sec,
		DefaultStatus: http.StatusNoContent}, h.RevokeConsoleSession)
	huma.Register(api, huma.Operation{OperationID: "revoke-other-console-sessions", Method: http.MethodDelete, Path: "/api/v1/me/sessions",
		Summary: "Sign out everywhere else: every console session but this one", Tags: []string{"Auth"}, Security: sec},
		h.RevokeOtherConsoleSessions)
	huma.Register(api, huma.Operation{OperationID: "org-console-sessions", Method: http.MethodGet, Path: "/api/v1/orgs/{orgId}/console-sessions",
		Summary: "Console sign-ins of the organisation's members (owners and admins)", Tags: []string{"Auth"}, Security: sec},
		h.OrgConsoleSessions)
	huma.Register(api, huma.Operation{OperationID: "revoke-org-console-session", Method: http.MethodDelete, Path: "/api/v1/orgs/{orgId}/console-sessions/{sessionId}",
		Summary: "Sign a member out of the console (owners and admins), while they belong to this organisation alone", Tags: []string{"Auth"},
		Security: sec, DefaultStatus: http.StatusNoContent}, h.RevokeOrgConsoleSession)
}

func (h *Handler) Logout(ctx context.Context, _ *struct{}) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	sid := middleware.SessionFromContext(ctx)
	if sid == uuid.Nil {
		return nil, huma.Error400BadRequest("this request was not made with a console sign-in")
	}
	return nil, h.svc.Sessions.End(ctx, sid)
}

func (h *Handler) ListConsoleSessions(ctx context.Context, _ *struct{}) (*ConsoleSessionsOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := h.svc.Sessions.List(ctx, userID, middleware.SessionFromContext(ctx))
	if err != nil {
		return nil, err
	}
	return &ConsoleSessionsOutput{Body: rows}, nil
}

func (h *Handler) RevokeConsoleSession(ctx context.Context, in *ConsoleSessionInput) (*struct{}, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(in.ID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid session id")
	}
	if err := h.svc.Sessions.Revoke(ctx, userID, id); errors.Is(err, service.ErrSessionNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	} else if err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *Handler) RevokeOtherConsoleSessions(ctx context.Context, _ *struct{}) (*EndedSessionsOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	n, err := h.svc.Sessions.RevokeOthers(ctx, userID, middleware.SessionFromContext(ctx))
	if err != nil {
		return nil, err
	}
	out := &EndedSessionsOutput{}
	out.Body.Ended = n
	return out, nil
}

func (h *Handler) OrgConsoleSessions(ctx context.Context, in *OrgSessionsInput) (*OrgConsoleSessionsOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
	if err != nil {
		return nil, err
	}
	var user *uuid.UUID
	if in.UserID != "" {
		id, err := parseUUID(in.UserID)
		if err != nil {
			return nil, err
		}
		user = &id
	}
	rows, err := h.svc.Sessions.OrgSessions(ctx, orgID, user)
	if err != nil {
		return nil, err
	}
	return &OrgConsoleSessionsOutput{Body: rows}, nil
}

func (h *Handler) RevokeOrgConsoleSession(ctx context.Context, in *OrgConsoleSessionInput) (*struct{}, error) {
	_, orgID, id, err := h.checkOrgAdminAccess(ctx, in.OrgID, in.SessionID)
	if err != nil {
		return nil, err
	}
	switch err := h.svc.Sessions.RevokeInOrg(ctx, orgID, id); {
	case errors.Is(err, service.ErrSessionNotFound):
		return nil, huma.Error404NotFound("no such console session in this organisation")
	case errors.Is(err, service.ErrSessionElsewhere):
		return nil, huma.Error409Conflict(err.Error())
	case err != nil:
		return nil, err
	}
	return nil, nil
}
