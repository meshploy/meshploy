package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/apps/api/internal/service"
	db "github.com/meshploy/packages/db"
)

type ListStorageOutput struct {
	Body []db.StorageIntegration
}

type CreateStorageInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Name            string `json:"name"              minLength:"1" maxLength:"100"`
		Provider        string `json:"provider"          enum:"s3,r2,minio,b2"`
		Endpoint        string `json:"endpoint,omitempty"`
		Region          string `json:"region,omitempty"`
		Bucket          string `json:"bucket"            minLength:"1"`
		AccessKeyID     string `json:"access_key_id"     minLength:"1"`
		SecretAccessKey string `json:"secret_access_key" minLength:"1"`
	}
}

type CreateStorageOutput struct {
	Body *db.StorageIntegration
}

type StoragePathInput struct {
	OrgID string `path:"orgId"`
	ID    string `path:"id"`
}

func (h *Handler) registerStorageRoutes(api huma.API) {
	const tag = "Storage Integrations"

	huma.Register(api, huma.Operation{
		OperationID: "list-storage-integrations",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/storage-integrations",
		Summary:     "List object storage integrations",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
	}) (*ListStorageOutput, error) {
		_, orgID, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
		if err != nil {
			return nil, err
		}
		items, err := h.svc.Storage.List(ctx, orgID)
		if err != nil {
			return nil, err
		}
		return &ListStorageOutput{Body: items}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-storage-integration",
		Method:        http.MethodPost,
		Path:          "/api/v1/orgs/{orgId}/storage-integrations",
		Summary:       "Add an object storage integration",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *CreateStorageInput) (*CreateStorageOutput, error) {
		_, orgID, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
		if err != nil {
			return nil, err
		}
		item, err := h.svc.Storage.Create(ctx, orgID, service.CreateStorageInput{
			Name:            in.Body.Name,
			Provider:        db.StorageProvider(in.Body.Provider),
			Endpoint:        in.Body.Endpoint,
			Region:          in.Body.Region,
			Bucket:          in.Body.Bucket,
			AccessKeyID:     in.Body.AccessKeyID,
			SecretAccessKey: in.Body.SecretAccessKey,
		})
		if err != nil {
			return nil, err
		}
		return &CreateStorageOutput{Body: item}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-storage-integration",
		Method:        http.MethodDelete,
		Path:          "/api/v1/orgs/{orgId}/storage-integrations/{id}",
		Summary:       "Remove an object storage integration",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *StoragePathInput) (*struct{}, error) {
		_, orgID, id, err := h.checkOrgAdminAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		return nil, h.svc.Storage.Delete(ctx, id, orgID)
	})
}
