package handler

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/server/middleware"
	"github.com/meshploy/packages/server/service"
	"github.com/meshploy/packages/server/version"
)

// CLI login through a browser. The CLI asks login-info which server it has
// found, starts a login, prints the approval link and polls; the person
// approves on the console's /cli-login page; the CLI collects its token.

// --- Inputs / Outputs ---

// hostInput carries the Host a request arrived through, which decides the
// addresses handed back: a platform name on that domain when it serves the
// platform, else the primary's.
type hostInput struct {
	host string
}

func (in *hostInput) Resolve(ctx huma.Context) []error {
	in.host = ctx.Host()
	return nil
}

type LoginInfoInput struct {
	hostInput
}

type LoginInfoOutput struct {
	Body struct {
		ConsoleURL string `json:"console_url"`
		APIURL     string `json:"api_url"`
		Version    string `json:"version"`
	}
}

type StartCLILoginInput struct {
	hostInput
	Body struct {
		Host string `json:"host" maxLength:"255" doc:"The machine's name, shown on the approval page"`
		Door string `json:"door,omitempty" doc:"The console to approve at; the console itself when empty"`
	}
}

type StartCLILoginOutput struct {
	Body struct {
		DeviceCode      string    `json:"device_code"`
		UserCode        string    `json:"user_code"`
		VerificationURL string    `json:"verification_url"`
		APIURL          string    `json:"api_url"`
		Interval        int       `json:"interval" doc:"Seconds between polls"`
		ExpiresAt       time.Time `json:"expires_at"`
	}
}

type PollCLILoginInput struct {
	Body struct {
		DeviceCode string `json:"device_code" minLength:"1"`
	}
}

type PollCLILoginOutput struct {
	Body struct {
		Status string `json:"status" enum:"pending,approved,denied,collected,expired"`
		Token  string `json:"token,omitempty" doc:"Given once, when approved"`
	}
}

type CLILoginCodeInput struct {
	Code string `path:"code"`
}

type CLILoginDTO struct {
	UserCode  string    `json:"user_code"`
	Host      string    `json:"host"`
	ExpiresAt time.Time `json:"expires_at"`
}

type CLILoginOutput struct {
	Body CLILoginDTO
}

type CLISessionDTO struct {
	ID          string     `json:"id"`
	Host        string     `json:"host"`
	TokenPrefix string     `json:"token_prefix"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	Current     bool       `json:"current" doc:"The session this request was made with"`
}

type ListCLISessionsInput struct {
	Authorization string `header:"Authorization"`
}

type ListCLISessionsOutput struct {
	Body []CLISessionDTO
}

type CLISessionInput struct {
	ID string `path:"id"`
}

type LogoutCLIInput struct {
	Authorization string `header:"Authorization"`
}

// --- Routes ---

func (h *Handler) registerCLILoginRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "login-info",
		Method:      "GET",
		Path:        "/api/v1/system/login-info",
		Summary:     "The server's console and API addresses, for a CLI finding it",
		Tags:        []string{"Auth"},
	}, h.LoginInfo)

	huma.Register(api, huma.Operation{
		OperationID:   "start-cli-login",
		Method:        "POST",
		Path:          "/api/v1/cli/logins",
		Summary:       "Start a CLI login, to be approved in a browser",
		Tags:          []string{"Auth"},
		DefaultStatus: 201,
	}, h.StartCLILogin)

	huma.Register(api, huma.Operation{
		OperationID: "poll-cli-login",
		Method:      "POST",
		Path:        "/api/v1/cli/logins/token",
		Summary:     "Ask whether a CLI login was approved, and collect its token once",
		Tags:        []string{"Auth"},
	}, h.PollCLILogin)

	huma.Register(api, huma.Operation{
		OperationID: "get-cli-login",
		Method:      "GET",
		Path:        "/api/v1/cli/logins/{code}",
		Summary:     "A CLI login waiting for approval, by the code its terminal shows",
		Tags:        []string{"Auth"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetCLILogin)

	huma.Register(api, huma.Operation{
		OperationID:   "approve-cli-login",
		Method:        "POST",
		Path:          "/api/v1/cli/logins/{code}/approve",
		Summary:       "Approve a CLI login: the CLI acts as you",
		Tags:          []string{"Auth"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.ApproveCLILogin)

	huma.Register(api, huma.Operation{
		OperationID:   "deny-cli-login",
		Method:        "POST",
		Path:          "/api/v1/cli/logins/{code}/deny",
		Summary:       "Deny a CLI login",
		Tags:          []string{"Auth"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.DenyCLILogin)

	huma.Register(api, huma.Operation{
		OperationID: "list-cli-sessions",
		Method:      "GET",
		Path:        "/api/v1/me/cli-sessions",
		Summary:     "Your logged-in CLIs",
		Tags:        []string{"Auth"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListCLISessions)

	huma.Register(api, huma.Operation{
		OperationID:   "revoke-cli-session",
		Method:        "DELETE",
		Path:          "/api/v1/me/cli-sessions/{id}",
		Summary:       "Log one of your CLIs out",
		Tags:          []string{"Auth"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.RevokeCLISession)

	huma.Register(api, huma.Operation{
		OperationID:   "logout-cli",
		Method:        "DELETE",
		Path:          "/api/v1/cli/session",
		Summary:       "Log out the CLI making this request",
		Tags:          []string{"Auth"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.LogoutCLI)
}

// --- Handlers ---

// platformURL is a platform name's address for a caller that arrived through
// host, falling back to the configured address on a machine with no domain.
func (h *Handler) platformURL(ctx context.Context, host, name, fallback string) string {
	if u := h.svc.Domains.PlatformURLFor(ctx, host, name); u != "" {
		return u
	}
	return strings.TrimRight(fallback, "/")
}

func (h *Handler) LoginInfo(ctx context.Context, in *LoginInfoInput) (*LoginInfoOutput, error) {
	out := &LoginInfoOutput{}
	out.Body.ConsoleURL = h.platformURL(ctx, in.host, "console", h.cfg.FrontendURL)
	out.Body.APIURL = h.platformURL(ctx, in.host, "api", h.cfg.APIBaseURL)
	out.Body.Version = version.Current
	return out, nil
}

func (h *Handler) StartCLILogin(ctx context.Context, in *StartCLILoginInput) (*StartCLILoginOutput, error) {
	start, err := h.svc.CLILogins.Start(ctx, in.Body.Host, in.Body.Door)
	if errors.Is(err, service.ErrCLILoginDoor) {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err != nil {
		return nil, err
	}
	// The console's own address stands in for any door on a machine with no
	// domain, as there is nothing else to serve it from.
	door := h.platformURL(ctx, in.host, start.Door, h.cfg.FrontendURL)
	out := &StartCLILoginOutput{}
	out.Body.DeviceCode = start.DeviceCode
	out.Body.UserCode = start.UserCode
	out.Body.VerificationURL = door + "/cli-login?code=" + url.QueryEscape(start.UserCode)
	out.Body.APIURL = h.platformURL(ctx, in.host, "api", h.cfg.APIBaseURL)
	out.Body.Interval = int(start.Interval / time.Second)
	out.Body.ExpiresAt = start.ExpiresAt
	return out, nil
}

func (h *Handler) PollCLILogin(ctx context.Context, in *PollCLILoginInput) (*PollCLILoginOutput, error) {
	poll, err := h.svc.CLILogins.Poll(ctx, in.Body.DeviceCode)
	if errors.Is(err, service.ErrCLILoginNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err != nil {
		return nil, err
	}
	out := &PollCLILoginOutput{}
	out.Body.Status = poll.Status
	out.Body.Token = poll.Token
	return out, nil
}

func cliLoginError(err error) error {
	switch {
	case errors.Is(err, service.ErrCLILoginNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, service.ErrCLILoginSettled):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, service.ErrCLIApprover):
		return huma.Error403Forbidden(err.Error())
	}
	return err
}

func (h *Handler) GetCLILogin(ctx context.Context, in *CLILoginCodeInput) (*CLILoginOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	login, err := h.svc.CLILogins.Waiting(ctx, in.Code)
	if err != nil {
		return nil, cliLoginError(err)
	}
	return &CLILoginOutput{Body: CLILoginDTO{UserCode: login.UserCode, Host: login.Host, ExpiresAt: login.ExpiresAt}}, nil
}

// decideCLILogin approves or denies, from a browser session only: two-factor
// was asked for there, and a token cannot hand itself on to another machine.
func (h *Handler) decideCLILogin(ctx context.Context, code string, approve bool) (*struct{}, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	if middleware.CredentialFromContext(ctx) != middleware.CredentialSession {
		return nil, cliLoginError(service.ErrCLIApprover)
	}
	if err := h.svc.CLILogins.Decide(ctx, code, userID, approve); err != nil {
		return nil, cliLoginError(err)
	}
	return nil, nil
}

func (h *Handler) ApproveCLILogin(ctx context.Context, in *CLILoginCodeInput) (*struct{}, error) {
	return h.decideCLILogin(ctx, in.Code, true)
}

func (h *Handler) DenyCLILogin(ctx context.Context, in *CLILoginCodeInput) (*struct{}, error) {
	return h.decideCLILogin(ctx, in.Code, false)
}

func (h *Handler) ListCLISessions(ctx context.Context, in *ListCLISessionsInput) (*ListCLISessionsOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := h.svc.CLILogins.Sessions(ctx, userID, strings.TrimPrefix(in.Authorization, "Bearer "))
	if err != nil {
		return nil, err
	}
	out := &ListCLISessionsOutput{Body: make([]CLISessionDTO, len(rows))}
	for i, r := range rows {
		out.Body[i] = CLISessionDTO{ID: r.ID.String(), Host: r.Host, TokenPrefix: r.TokenPrefix,
			CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt, Current: r.Current}
	}
	return out, nil
}

func (h *Handler) RevokeCLISession(ctx context.Context, in *CLISessionInput) (*struct{}, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(in.ID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid session id")
	}
	if err := h.svc.CLILogins.Revoke(ctx, userID, id); errors.Is(err, service.ErrTokenNotFound) {
		return nil, huma.Error404NotFound("session not found")
	} else if err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *Handler) LogoutCLI(ctx context.Context, in *LogoutCLIInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	if middleware.CredentialFromContext(ctx) != middleware.CredentialCLI {
		return nil, huma.Error400BadRequest("this request was not made with a CLI login")
	}
	if err := h.svc.CLILogins.RevokeToken(ctx, strings.TrimPrefix(in.Authorization, "Bearer ")); err != nil {
		return nil, err
	}
	return nil, nil
}
