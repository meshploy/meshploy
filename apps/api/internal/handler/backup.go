package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/service"
	db "github.com/meshploy/packages/db"
)

// ─── Per-service backup configs ───────────────────────────────────────────────

type ListBackupsOutput struct {
	Body []db.BackupConfig
}

type CreateBackupInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
	Body      struct {
		StorageIntegrationID string `json:"storage_integration_id" minLength:"1"`
		Schedule             string `json:"schedule"               minLength:"1"`
		RetentionDays        int    `json:"retention_days,omitempty"`
		PathPrefix           string `json:"path_prefix,omitempty"`
	}
}

type CreateBackupOutput struct {
	Body *db.BackupConfig
}

type UpdateBackupBody struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
	ID        string `path:"id"`
	Body      struct {
		Schedule      *string `json:"schedule,omitempty"`
		RetentionDays *int    `json:"retention_days,omitempty"`
		PathPrefix    *string `json:"path_prefix,omitempty"`
		Enabled       *bool   `json:"enabled,omitempty"`
	}
}

type BackupPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
	ID        string `path:"id"`
}

// ─── System backup ────────────────────────────────────────────────────────────

type SystemBackupOutput struct {
	Body *db.SystemBackupConfig
}

type UpsertSystemBackupInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		StorageIntegrationID string `json:"storage_integration_id" minLength:"1"`
		Schedule             string `json:"schedule"               minLength:"1"`
		RetentionDays        int    `json:"retention_days,omitempty"`
		PathPrefix           string `json:"path_prefix,omitempty"`
		Enabled              bool   `json:"enabled"`
	}
}

// ─── Routes ───────────────────────────────────────────────────────────────────

func (h *Handler) registerBackupRoutes(api huma.API) {
	const tag = "Backups"

	// ── Per-service backup configs ─────────────────────────────────────────

	huma.Register(api, huma.Operation{
		OperationID: "list-backup-configs",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups",
		Summary:     "List backup configs for a service",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *struct {
		OrgID     string `path:"orgId"`
		ProjectID string `path:"projectId"`
		ServiceID string `path:"serviceId"`
	}) (*ListBackupsOutput, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		serviceID, err := uuid.Parse(in.ServiceID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid service ID")
		}
		items, err := h.svc.Backups.List(ctx, serviceID)
		if err != nil {
			return nil, err
		}
		return &ListBackupsOutput{Body: items}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-backup-config",
		Method:        http.MethodPost,
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups",
		Summary:       "Add a backup config for a service",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *CreateBackupInput) (*CreateBackupOutput, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
		}
		serviceID, err := uuid.Parse(in.ServiceID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid service ID")
		}
		storageID, err := uuid.Parse(in.Body.StorageIntegrationID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid storage integration ID")
		}
		item, err := h.svc.Backups.Create(ctx, orgID, serviceID, service.CreateBackupInput{
			StorageIntegrationID: storageID,
			Schedule:             in.Body.Schedule,
			RetentionDays:        in.Body.RetentionDays,
			PathPrefix:           in.Body.PathPrefix,
		})
		if err != nil {
			return nil, err
		}
		return &CreateBackupOutput{Body: item}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-backup-config",
		Method:      http.MethodPatch,
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}",
		Summary:     "Update a backup config",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *UpdateBackupBody) (*CreateBackupOutput, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid ID")
		}
		serviceID, err := uuid.Parse(in.ServiceID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid service ID")
		}
		item, err := h.svc.Backups.Update(ctx, id, serviceID, service.UpdateBackupInput{
			Schedule:      in.Body.Schedule,
			RetentionDays: in.Body.RetentionDays,
			PathPrefix:    in.Body.PathPrefix,
			Enabled:       in.Body.Enabled,
		})
		if err != nil {
			return nil, err
		}
		return &CreateBackupOutput{Body: item}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-backup-config",
		Method:        http.MethodDelete,
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}",
		Summary:       "Delete a backup config",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *BackupPathInput) (*struct{}, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid ID")
		}
		serviceID, err := uuid.Parse(in.ServiceID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid service ID")
		}
		return nil, h.svc.Backups.Delete(ctx, id, serviceID)
	})

	huma.Register(api, huma.Operation{
		OperationID:   "trigger-backup-config",
		Method:        http.MethodPost,
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}/trigger",
		Summary:       "Manually trigger a backup",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, in *BackupPathInput) (*CreateBackupOutput, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid ID")
		}
		serviceID, err := uuid.Parse(in.ServiceID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid service ID")
		}
		item, err := h.svc.Backups.Trigger(ctx, id, serviceID)
		if err != nil {
			return nil, err
		}
		return &CreateBackupOutput{Body: item}, nil
	})

	// ── System backup ──────────────────────────────────────────────────────

	huma.Register(api, huma.Operation{
		OperationID: "get-system-backup",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/system-backup",
		Summary:     "Get system backup config for an org",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
	}) (*SystemBackupOutput, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
		}
		item, err := h.svc.Backups.GetSystem(ctx, orgID)
		if err != nil {
			return nil, err
		}
		return &SystemBackupOutput{Body: item}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "upsert-system-backup",
		Method:      http.MethodPut,
		Path:        "/api/v1/orgs/{orgId}/system-backup",
		Summary:     "Create or update system backup config",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *UpsertSystemBackupInput) (*SystemBackupOutput, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
		}
		storageID, err := uuid.Parse(in.Body.StorageIntegrationID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid storage integration ID")
		}
		item, err := h.svc.Backups.UpsertSystem(ctx, orgID, service.UpsertSystemBackupInput{
			StorageIntegrationID: storageID,
			Schedule:             in.Body.Schedule,
			RetentionDays:        in.Body.RetentionDays,
			PathPrefix:           in.Body.PathPrefix,
			Enabled:              in.Body.Enabled,
		})
		if err != nil {
			return nil, err
		}
		return &SystemBackupOutput{Body: item}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "trigger-system-backup",
		Method:        http.MethodPost,
		Path:          "/api/v1/orgs/{orgId}/system-backup/trigger",
		Summary:       "Manually trigger the system backup",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
	}) (*SystemBackupOutput, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
		}
		item, err := h.svc.Backups.TriggerSystem(ctx, orgID)
		if err != nil {
			return nil, err
		}
		return &SystemBackupOutput{Body: item}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-backup-objects",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}/objects",
		Summary:     "List restore points for a backup config",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *BackupPathInput) (*struct {
		Body []service.BackupObject
	}, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid ID")
		}
		serviceID, err := uuid.Parse(in.ServiceID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid service ID")
		}
		items, err := h.svc.Backups.ListObjects(ctx, id, serviceID)
		if err != nil {
			return nil, err
		}
		return &struct{ Body []service.BackupObject }{Body: items}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "restore-backup",
		Method:        http.MethodPost,
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}/restore",
		Summary:       "Restore a database from a backup object",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, in *struct {
		OrgID     string `path:"orgId"`
		ProjectID string `path:"projectId"`
		ServiceID string `path:"serviceId"`
		ID        string `path:"id"`
		Body      struct {
			Key string `json:"key" minLength:"1"`
		}
	}) (*struct{}, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid ID")
		}
		serviceID, err := uuid.Parse(in.ServiceID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid service ID")
		}
		return nil, h.svc.Backups.Restore(ctx, id, serviceID, in.Body.Key)
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-system-backup-objects",
		Method:      http.MethodGet,
		Path:        "/api/v1/orgs/{orgId}/system-backup/objects",
		Summary:     "List restore points for the system backup",
		Tags:        []string{tag},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
	}) (*struct {
		Body []service.BackupObject
	}, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
		}
		items, err := h.svc.Backups.ListSystemObjects(ctx, orgID)
		if err != nil {
			return nil, err
		}
		return &struct{ Body []service.BackupObject }{Body: items}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "restore-system-backup",
		Method:        http.MethodPost,
		Path:          "/api/v1/orgs/{orgId}/system-backup/restore",
		Summary:       "Restore system database from a backup object",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
		Body  struct {
			Key string `json:"key" minLength:"1"`
		}
	}) (*struct{}, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
		}
		return nil, h.svc.Backups.RestoreSystem(ctx, orgID, in.Body.Key)
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-system-backup",
		Method:        http.MethodDelete,
		Path:          "/api/v1/orgs/{orgId}/system-backup",
		Summary:       "Delete system backup config",
		Tags:          []string{tag},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
	}) (*struct{}, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		orgID, err := uuid.Parse(in.OrgID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org ID")
		}
		return nil, h.svc.Backups.DeleteSystem(ctx, orgID)
	})
}
