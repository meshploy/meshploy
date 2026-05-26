package handler

import (
	"context"
	"net/http"
	"strings"

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
	Cookie string `header:"Cookie"`
	Body   struct {
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
	UserAgent string `header:"User-Agent"`
	Body      struct {
		MFAToken    string `json:"mfa_token" minLength:"1"`
		Code        string `json:"code" minLength:"6" maxLength:"6"`
		TrustDevice bool   `json:"trust_device,omitempty"`
	}
}

type CompleteTOTPLoginOutput struct {
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		Token string `json:"token"`
	}
}

type GetMeOutput struct {
	Body *db.User
}

type ChangePasswordInput struct {
	Body struct {
		CurrentPassword string `json:"current_password" minLength:"1"`
		NewPassword     string `json:"new_password" minLength:"8"`
	}
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

type EnableTOTPOutput struct {
	Body struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
}

type DisableTOTPInput struct {
	Body struct {
		Code string `json:"code" minLength:"6" maxLength:"6"`
	}
}

type CompleteRecoveryLoginInput struct {
	Body struct {
		MFAToken     string `json:"mfa_token" minLength:"1"`
		RecoveryCode string `json:"recovery_code" minLength:"1"`
	}
}

type RegenerateRecoveryCodesInput struct {
	Body struct {
		Code string `json:"code" minLength:"6" maxLength:"6"`
	}
}

type RegenerateRecoveryCodesOutput struct {
	Body struct {
		RecoveryCodes []string `json:"recovery_codes"`
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
		deviceName := truncate(in.UserAgent, 200)
		result, err := h.svc.Auth.CompleteTOTPLogin(ctx, in.Body.MFAToken, in.Body.Code, h.cfg.JWTSecret, in.Body.TrustDevice, deviceName)
		if err != nil {
			return nil, huma.Error401Unauthorized(err.Error())
		}
		out := &CompleteTOTPLoginOutput{}
		out.Body.Token = result.Token
		if result.DeviceToken != "" {
			out.SetCookie = deviceCookie(result.DeviceToken, strings.HasPrefix(h.cfg.APIBaseURL, "https://"))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "complete-recovery-login",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/recovery",
		Summary:     "Complete login with a one-time recovery code",
		Tags:        []string{tag},
	}, func(ctx context.Context, in *CompleteRecoveryLoginInput) (*CompleteTOTPLoginOutput, error) {
		result, err := h.svc.Auth.CompleteRecoveryLogin(ctx, in.Body.MFAToken, in.Body.RecoveryCode, h.cfg.JWTSecret)
		if err != nil {
			return nil, huma.Error401Unauthorized(err.Error())
		}
		out := &CompleteTOTPLoginOutput{}
		out.Body.Token = result.Token
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
		OperationID: "change-password",
		Method:      http.MethodPatch,
		Path:        "/api/v1/me/password",
		Summary:     "Change current user password",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *ChangePasswordInput) (*struct{}, error) {
		userID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		if err := h.svc.Auth.ChangePassword(ctx, userID, in.Body.CurrentPassword, in.Body.NewPassword); err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		return nil, nil
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
	}, func(ctx context.Context, in *EnableTOTPInput) (*EnableTOTPOutput, error) {
		userID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		codes, err := h.svc.Auth.VerifyAndEnableTOTP(ctx, userID, in.Body.Code)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		out := &EnableTOTPOutput{}
		out.Body.RecoveryCodes = codes
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "regenerate-recovery-codes",
		Method:      http.MethodPost,
		Path:        "/api/v1/me/recovery-codes/regenerate",
		Summary:     "Regenerate 2FA recovery codes (requires current TOTP code)",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *RegenerateRecoveryCodesInput) (*RegenerateRecoveryCodesOutput, error) {
		userID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		codes, err := h.svc.Auth.RegenerateRecoveryCodes(ctx, userID, in.Body.Code)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		out := &RegenerateRecoveryCodesOutput{}
		out.Body.RecoveryCodes = codes
		return out, nil
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
		Email:       input.Body.Email,
		Password:    input.Body.Password,
		DeviceToken: extractCookie(input.Cookie, "meshploy_device_token"),
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

// extractCookie parses a raw Cookie header string and returns the named cookie value.
func extractCookie(rawCookie, name string) string {
	h := http.Header{"Cookie": []string{rawCookie}}
	r := &http.Request{Header: h}
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// deviceCookie builds the Set-Cookie header value for a trusted device token.
func deviceCookie(token string, secure bool) string {
	c := &http.Cookie{
		Name:     "meshploy_device_token",
		Value:    token,
		Path:     "/api/v1/auth/login",
		MaxAge:   30 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
	}
	return c.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

