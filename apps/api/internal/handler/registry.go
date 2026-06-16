package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/service"
	db "github.com/meshploy/packages/db"
)

// ─── I/O types ────────────────────────────────────────────────────────────────

type ListRegistriesOutput struct {
	Body []db.RegistryIntegration
}

type CreateRegistryInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Name      string `json:"name"      minLength:"1" maxLength:"100"`
		Provider  string `json:"provider"  enum:"ghcr,dockerhub,ecr,gcr,custom"`
		Endpoint  string `json:"endpoint,omitempty"`
		Namespace string `json:"namespace,omitempty"`
		Username  string `json:"username"  minLength:"1"`
		Password  string `json:"password"  minLength:"1"`
	}
}

type CreateRegistryOutput struct {
	Body *db.RegistryIntegration
}

type RegistryPathInput struct {
	OrgID string `path:"orgId"`
	ID    string `path:"id"`
}

// ─── Routes ───────────────────────────────────────────────────────────────────

func (h *Handler) registerRegistryRoutes(api huma.API) {
	const tag = "Registry Integrations"

	huma.Register(api, huma.Operation{
		OperationID: "list-registry-integrations",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/registry-integrations",
		Summary:     "List container registry integrations",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
	}) (*ListRegistriesOutput, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
		}
		items, err := h.svc.Registries.List(ctx, orgID)
		if err != nil {
			return nil, err
		}
		return &ListRegistriesOutput{Body: items}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-registry-integration",
		Method:      http.MethodPost,
		Path:        "/api/v1/orgs/{orgId}/registry-integrations",
		Summary:     "Add a container registry integration",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *CreateRegistryInput) (*CreateRegistryOutput, error) {
		callerID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
		}
		if err := h.enforceAdminRole(ctx, orgID, callerID); err != nil {
			return nil, err
		}
		item, err := h.svc.Registries.Create(ctx, orgID, service.CreateRegistryInput{
			Name:      in.Body.Name,
			Provider:  db.RegistryProvider(in.Body.Provider),
			Endpoint:  in.Body.Endpoint,
			Namespace: in.Body.Namespace,
			Username:  in.Body.Username,
			Password:  in.Body.Password,
		})
		if err != nil {
			return nil, err
		}
		return &CreateRegistryOutput{Body: item}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-registry-integration",
		Method:        http.MethodDelete,
		Path:          "/api/v1/orgs/{orgId}/registry-integrations/{id}",
		Summary:       "Remove a container registry integration",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *RegistryPathInput) (*struct{}, error) {
		callerID, err := requireUser(ctx)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid ID")
		}
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
		}
		if err := h.enforceAdminRole(ctx, orgID, callerID); err != nil {
			return nil, err
		}
		return nil, h.svc.Registries.Delete(ctx, id, orgID)
	})
}
