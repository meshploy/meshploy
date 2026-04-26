package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
)

// ─── Types ────────────────────────────────────────────────────────────────────

type SecretPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	SecretID  string `path:"secretId"`
}

type ListSecretsInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
}

// SecretDTO exposes a secret without its encrypted value. Callers see a masked
// placeholder so they know a value is set without receiving the plaintext.
type SecretDTO struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
	HasValue  bool      `json:"has_value"`
	CreatedAt string    `json:"created_at"`
	UpdatedAt string    `json:"updated_at"`
}

func toSecretDTO(s db.Secret) SecretDTO {
	return SecretDTO{
		ID:        s.ID,
		ProjectID: s.ProjectID,
		Name:      s.Name,
		HasValue:  string(s.Value) != "",
		CreatedAt: s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: s.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

type ListSecretsOutput struct {
	Body []SecretDTO
}

type CreateSecretInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		Name  string `json:"name"  minLength:"1" maxLength:"200"`
		Value string `json:"value" minLength:"1"`
	}
}

type CreateSecretOutput struct {
	Body SecretDTO
}

type UpdateSecretInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	SecretID  string `path:"secretId"`
	Body      struct {
		Value string `json:"value" minLength:"1"`
	}
}

type UpdateSecretOutput struct {
	Body SecretDTO
}

// ─── Attachments ──────────────────────────────────────────────────────────────

type AttachmentPathInput struct {
	OrgID        string `path:"orgId"`
	ProjectID    string `path:"projectId"`
	ServiceID    string `path:"serviceId"`
	AttachmentID string `path:"attachmentId"`
}

type ListAttachmentsInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
}

type AttachmentDTO struct {
	ID        uuid.UUID `json:"id"`
	ServiceID uuid.UUID `json:"service_id"`
	SecretID  uuid.UUID `json:"secret_id"`
	SecretName string   `json:"secret_name"`
	EnvKey    string    `json:"env_key"`
}

func toAttachmentDTO(a db.ServiceSecret) AttachmentDTO {
	return AttachmentDTO{
		ID:         a.ID,
		ServiceID:  a.ServiceID,
		SecretID:   a.SecretID,
		SecretName: a.Secret.Name,
		EnvKey:     a.EnvKey,
	}
}

type ListAttachmentsOutput struct {
	Body []AttachmentDTO
}

type AttachSecretInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
	Body      struct {
		SecretID string `json:"secret_id" minLength:"1"`
		EnvKey   string `json:"env_key"   minLength:"1" maxLength:"200"`
	}
}

type AttachSecretOutput struct {
	Body AttachmentDTO
}

// ─── Registration ─────────────────────────────────────────────────────────────

func (h *Handler) registerSecretRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-secrets",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/secrets",
		Summary:     "List project secrets",
		Tags:        []string{"Secrets"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListSecrets)

	huma.Register(api, huma.Operation{
		OperationID:   "create-secret",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/secrets",
		Summary:       "Create a project secret",
		Tags:          []string{"Secrets"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 201,
	}, h.CreateSecret)

	huma.Register(api, huma.Operation{
		OperationID: "update-secret",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/secrets/{secretId}",
		Summary:     "Update a secret's value",
		Tags:        []string{"Secrets"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateSecret)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-secret",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/secrets/{secretId}",
		Summary:       "Delete a project secret",
		Tags:          []string{"Secrets"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.DeleteSecret)

	huma.Register(api, huma.Operation{
		OperationID: "list-secret-attachments",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/secret-attachments",
		Summary:     "List secrets attached to a service",
		Tags:        []string{"Secrets"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListAttachments)

	huma.Register(api, huma.Operation{
		OperationID:   "attach-secret",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/secret-attachments",
		Summary:       "Attach a secret to a service",
		Tags:          []string{"Secrets"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 201,
	}, h.AttachSecret)

	huma.Register(api, huma.Operation{
		OperationID:   "detach-secret",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/secret-attachments/{attachmentId}",
		Summary:       "Detach a secret from a service",
		Tags:          []string{"Secrets"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.DetachSecret)
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

func (h *Handler) ListSecrets(ctx context.Context, input *ListSecretsInput) (*ListSecretsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := h.svc.Secrets.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	dtos := make([]SecretDTO, len(rows))
	for i, r := range rows {
		dtos[i] = toSecretDTO(r)
	}
	return &ListSecretsOutput{Body: dtos}, nil
}

func (h *Handler) CreateSecret(ctx context.Context, input *CreateSecretInput) (*CreateSecretOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	row, err := h.svc.Secrets.Create(ctx, projectID, input.Body.Name, input.Body.Value)
	if err != nil {
		return nil, huma.Error409Conflict("secret name already exists in this project")
	}
	return &CreateSecretOutput{Body: toSecretDTO(*row)}, nil
}

func (h *Handler) UpdateSecret(ctx context.Context, input *UpdateSecretInput) (*UpdateSecretOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	secretID, err := parseUUID(input.SecretID)
	if err != nil {
		return nil, err
	}
	row, err := h.svc.Secrets.Update(ctx, secretID, input.Body.Value)
	if err != nil {
		return nil, notFound(err)
	}
	return &UpdateSecretOutput{Body: toSecretDTO(*row)}, nil
}

func (h *Handler) DeleteSecret(ctx context.Context, input *SecretPathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	secretID, err := parseUUID(input.SecretID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.Secrets.Delete(ctx, secretID)
}

func (h *Handler) ListAttachments(ctx context.Context, input *ListAttachmentsInput) (*ListAttachmentsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	rows, err := h.svc.Secrets.ListAttachments(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	dtos := make([]AttachmentDTO, len(rows))
	for i, r := range rows {
		dtos[i] = toAttachmentDTO(r)
	}
	return &ListAttachmentsOutput{Body: dtos}, nil
}

func (h *Handler) AttachSecret(ctx context.Context, input *AttachSecretInput) (*AttachSecretOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	secretID, err := parseUUID(input.Body.SecretID)
	if err != nil {
		return nil, err
	}
	row, err := h.svc.Secrets.Attach(ctx, serviceID, secretID, input.Body.EnvKey)
	if err != nil {
		return nil, huma.Error409Conflict("env key already used on this service")
	}
	return &AttachSecretOutput{Body: toAttachmentDTO(*row)}, nil
}

func (h *Handler) DetachSecret(ctx context.Context, input *AttachmentPathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	attachmentID, err := parseUUID(input.AttachmentID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.Secrets.Detach(ctx, attachmentID)
}
