package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	svc "github.com/meshploy/apps/api/internal/service"
	"github.com/meshploy/packages/db"
)

type EmailConfigOrgInput struct {
	OrgID string `path:"orgId"`
}

type EmailConfigOutput struct {
	Body *db.OrgEmailConfig
}

type SaveEmailConfigInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Host        string `json:"host"         minLength:"1"`
		Port        int    `json:"port"         minimum:"1" maximum:"65535"`
		Username    string `json:"username"`
		Password    string `json:"password"`     // empty = keep existing on update
		FromAddress string `json:"from_address" minLength:"3"`
		FromName    string `json:"from_name"`
		UseTLS      bool   `json:"use_tls"`
	}
}

func (h *Handler) registerEmailConfigRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-email-config",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/email-config",
		Summary:     "Get the org SMTP configuration",
		Tags:        []string{"Email"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetEmailConfig)

	huma.Register(api, huma.Operation{
		OperationID: "save-email-config",
		Method:      "PUT",
		Path:        "/api/v1/orgs/{orgId}/email-config",
		Summary:     "Create or update the org SMTP configuration",
		Tags:        []string{"Email"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.SaveEmailConfig)

	huma.Register(api, huma.Operation{
		OperationID: "delete-email-config",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/email-config",
		Summary:     "Remove the org SMTP configuration",
		Tags:        []string{"Email"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteEmailConfig)
}

func (h *Handler) GetEmailConfig(ctx context.Context, input *EmailConfigOrgInput) (*EmailConfigOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	cfg, err := h.svc.EmailConfig.Get(ctx, orgID)
	if err != nil {
		return nil, notFound(err)
	}
	return &EmailConfigOutput{Body: cfg}, nil
}

func (h *Handler) SaveEmailConfig(ctx context.Context, input *SaveEmailConfigInput) (*EmailConfigOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	cfg, err := h.svc.EmailConfig.Save(ctx, orgID, svc.SaveEmailConfigInput{
		Host:        input.Body.Host,
		Port:        input.Body.Port,
		Username:    input.Body.Username,
		Password:    input.Body.Password,
		FromAddress: input.Body.FromAddress,
		FromName:    input.Body.FromName,
		UseTLS:      input.Body.UseTLS,
	})
	if err != nil {
		return nil, err
	}
	return &EmailConfigOutput{Body: cfg}, nil
}

func (h *Handler) DeleteEmailConfig(ctx context.Context, input *EmailConfigOrgInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.EmailConfig.Delete(ctx, orgID)
}
