package handler

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
	svc "github.com/meshploy/apps/api/internal/service"
)

// --- Input / Output types ---

type RegisterInput struct {
	Body struct {
		Username string `json:"username" minLength:"3" maxLength:"50"`
		Email    string `json:"email" format:"email"`
		Password string `json:"password" minLength:"8"`
	}
}

type RegisterOutput struct {
	Body *db.User
}

type LoginInput struct {
	Body struct {
		Email    string `json:"email" format:"email"`
		Password string `json:"password"`
	}
}

type LoginOutput struct {
	Body struct {
		Token string `json:"token"`
	}
}

// --- Route registration ---

func (h *Handler) registerAuthRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "register",
		Method:      "POST",
		Path:        "/api/v1/auth/register",
		Summary:     "Register a new user",
		Tags:        []string{"Auth"},
	}, h.RegisterUser)

	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      "POST",
		Path:        "/api/v1/auth/login",
		Summary:     "Login and receive a JWT",
		Tags:        []string{"Auth"},
	}, h.Login)
}

// --- Handlers ---

func (h *Handler) RegisterUser(ctx context.Context, input *RegisterInput) (*RegisterOutput, error) {
	user, err := h.svc.Auth.Register(ctx, svc.RegisterInput{
		Username: input.Body.Username,
		Email:    input.Body.Email,
		Password: input.Body.Password,
	})
	if err != nil {
		return nil, huma.Error409Conflict("username or email already exists")
	}
	return &RegisterOutput{Body: user}, nil
}

func (h *Handler) Login(ctx context.Context, input *LoginInput) (*LoginOutput, error) {
	token, err := h.svc.Auth.Login(ctx, svc.LoginInput{
		Email:    input.Body.Email,
		Password: input.Body.Password,
	}, h.cfg.JWTSecret)
	if err != nil {
		if errors.Is(err, errors.New("invalid credentials")) {
			return nil, huma.Error401Unauthorized("invalid credentials")
		}
		return nil, huma.Error401Unauthorized("invalid credentials")
	}
	out := &LoginOutput{}
	out.Body.Token = token
	return out, nil
}
