package handler

import (
	"context"
	"net/http"

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
		Token        string `json:"token,omitempty"`
		TOTPRequired bool   `json:"totp_required,omitempty"`
		MFAToken     string `json:"mfa_token,omitempty"`
	}
}

type CompleteTOTPLoginInput struct {
	Body struct {
		MFAToken string `json:"mfa_token" minLength:"1"`
		Code     string `json:"code" minLength:"6" maxLength:"6"`
	}
}

type CompleteTOTPLoginOutput struct {
	Body struct {
		Token string `json:"token"`
	}
}

type GetMeOutput struct {
	Body *db.User
}

type SetupTOTPOutput struct {
	Body struct {
		OTPURL string `json:"otp_url"`
		Secret string `json:"secret"`
	}
}

type EnableTOTPInput struct {
	Body struct {
		Code string `json:"code" minLength:"6" maxLength:"6"`
	}
}

type DisableTOTPInput struct {
	Body struct {
		Code string `json:"code" minLength:"6" maxLength:"6"`
	}
}

// --- Route registration ---

func (h *Handler) registerAuthRoutes(api huma.API) {
	const tag = "Auth"

	huma.Register(api, huma.Operation{
		OperationID: "register",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/register",
		Summary:     "Register a new user",
		Tags:        []string{tag},
	}, h.RegisterUser)

	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/login",
		Summary:     "Login and receive a JWT",
		Tags:        []string{tag},
	}, h.Login)

	huma.Register(api, huma.Operation{
		OperationID: "complete-totp-login",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/totp",
		Summary:     "Complete login with TOTP code",
		Tags:        []string{tag},
	}, func(ctx context.Context, in *CompleteTOTPLoginInput) (*CompleteTOTPLoginOutput, error) {
		token, err := h.svc.Auth.CompleteTOTPLogin(ctx, in.Body.MFAToken, in.Body.Code, h.cfg.JWTSecret)
		if err != nil {
			return nil, huma.Error401Unauthorized(err.Error())
		}
		out := &CompleteTOTPLoginOutput{}
		out.Body.Token = token
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-me",
		Method:      http.MethodGet,
		Path:        "/api/v1/me",
		Summary:     "Get current user",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, _ *struct{}) (*GetMeOutput, error) {
		userID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		me, err := h.svc.Auth.GetMe(ctx, userID)
		if err != nil {
			return nil, huma.Error404NotFound("user not found")
		}
		return &GetMeOutput{Body: me}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "setup-totp",
		Method:        http.MethodPost,
		Path:          "/api/v1/me/totp/setup",
		Summary:       "Generate a new TOTP secret (not yet enabled)",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, _ *struct{}) (*SetupTOTPOutput, error) {
		userID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		otpURL, secret, err := h.svc.Auth.SetupTOTP(ctx, userID)
		if err != nil {
			return nil, err
		}
		out := &SetupTOTPOutput{}
		out.Body.OTPURL = otpURL
		out.Body.Secret = secret
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "enable-totp",
		Method:      http.MethodPost,
		Path:        "/api/v1/me/totp/enable",
		Summary:     "Verify TOTP code and enable 2FA",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *EnableTOTPInput) (*struct{}, error) {
		userID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		if err := h.svc.Auth.VerifyAndEnableTOTP(ctx, userID, in.Body.Code); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return &struct{}{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "disable-totp",
		Method:        http.MethodDelete,
		Path:          "/api/v1/me/totp",
		Summary:       "Disable 2FA (requires current TOTP code)",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *DisableTOTPInput) (*struct{}, error) {
		userID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		if err := h.svc.Auth.DisableTOTP(ctx, userID, in.Body.Code); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return &struct{}{}, nil
	})
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
	result, err := h.svc.Auth.Login(ctx, svc.LoginInput{
		Email:    input.Body.Email,
		Password: input.Body.Password,
	}, h.cfg.JWTSecret)
	if err != nil {
		return nil, huma.Error401Unauthorized("invalid credentials")
	}
	out := &LoginOutput{}
	out.Body.Token = result.Token
	out.Body.TOTPRequired = result.TOTPRequired
	out.Body.MFAToken = result.MFAToken
	return out, nil
}

