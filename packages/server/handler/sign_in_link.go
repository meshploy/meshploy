package handler

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	svc "github.com/meshploy/packages/server/service"
)

// --- Input / Output types ---

type CreateSignInLinkInput struct {
	// Key is the installation's sign-in link key (SIGN_IN_LINK_KEY). It is
	// sent in a header of its own, not as a bearer token, because it is no
	// one's session.
	Key  string `header:"X-Sign-In-Link-Key"`
	Body struct {
		Email string `json:"email" doc:"The address the person signs in with"`
		// Next is where the console goes once signed in: a path on it.
		Next string `json:"next,omitempty" doc:"A path on the console to land on, e.g. /projects"`
	}
}

type CreateSignInLinkOutput struct {
	Body struct {
		URL       string    `json:"url" doc:"The link, to send the person's browser to at once"`
		ExpiresAt time.Time `json:"expires_at"`
	}
}

type SpendSignInLinkInput struct {
	ClientHeaders
	Body struct {
		Token string `json:"token"`
	}
}

type SpendSignInLinkOutput struct {
	Body struct {
		Token string `json:"token" doc:"The console session's token"`
	}
}

// --- Routes ---

func (h *Handler) registerSignInLinkRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-sign-in-link",
		Method:        "POST",
		Path:          "/api/v1/system/sign-in-links",
		Summary:       "Make a one-time link that signs a person in to the console (operator key)",
		Tags:          []string{"Auth"},
		DefaultStatus: 201,
	}, h.CreateSignInLink)

	huma.Register(api, huma.Operation{
		OperationID: "spend-sign-in-link",
		Method:      "POST",
		Path:        "/api/v1/auth/sign-in-link",
		Summary:     "Use a one-time sign-in link, starting a console session",
		Tags:        []string{"Auth"},
	}, h.SpendSignInLink)
}

// --- Handlers ---

func (h *Handler) CreateSignInLink(ctx context.Context, in *CreateSignInLinkInput) (*CreateSignInLinkOutput, error) {
	// Off unless the installation set a key: then the route is not there.
	if h.cfg.SignInLinkKey == "" {
		return nil, huma.Error404NotFound("sign-in links are not enabled on this server")
	}
	if subtle.ConstantTimeCompare([]byte(in.Key), []byte(h.cfg.SignInLinkKey)) != 1 {
		return nil, huma.Error401Unauthorized("a sign-in link takes this server's sign-in link key")
	}
	next := strings.TrimSpace(in.Body.Next)
	if next != "" && !isConsolePath(next) {
		return nil, huma.Error422UnprocessableEntity("next must be a path on the console, such as /projects")
	}
	token, expires, err := h.svc.SignInLinks.Create(ctx, in.Body.Email)
	if errors.Is(err, svc.ErrSignInLinkNoPerson) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err != nil {
		return nil, err
	}
	console := h.svc.Domains.GatewayPlatformURL(ctx, "console")
	if console == "" {
		console = strings.TrimRight(h.cfg.FrontendURL, "/")
	}
	link := console + "/link"
	if next != "" {
		link += "?next=" + url.QueryEscape(next)
	}
	// The token rides in the fragment, which a browser never sends to a
	// server, so no proxy or access log in between records it.
	out := &CreateSignInLinkOutput{}
	out.Body.URL = link + "#" + token
	out.Body.ExpiresAt = expires
	return out, nil
}

func (h *Handler) SpendSignInLink(ctx context.Context, in *SpendSignInLinkInput) (*SpendSignInLinkOutput, error) {
	userID, err := h.svc.SignInLinks.Spend(ctx, strings.TrimSpace(in.Body.Token))
	if errors.Is(err, svc.ErrSignInLinkSpent) {
		return nil, huma.Error401Unauthorized(err.Error())
	}
	if err != nil {
		return nil, err
	}
	token, err := h.SignInAs(ctx, userID, in.ClientHeaders)
	if err != nil {
		return nil, err
	}
	out := &SpendSignInLinkOutput{}
	out.Body.Token = token
	return out, nil
}

// isConsolePath is a path on this console and nowhere else: not another
// site's address, not one a browser reads as another host (//host, /\host).
func isConsolePath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "/\\") &&
		!strings.ContainsAny(p, "\r\n#")
}
