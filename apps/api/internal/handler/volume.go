package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/service"
	"github.com/meshploy/packages/db"
)

// ─── Input / output types ─────────────────────────────────────────────────────

type VolumeProjectPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
}

type VolumePathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	VolumeID  string `path:"volumeId"`
}

type VolumeMountPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	VolumeID  string `path:"volumeId"`
	MountID   string `path:"mountId"`
}

type ServiceMountsPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
}

type ListVolumesOutput struct {
	Body []db.Volume
}

type GetVolumeOutput struct {
	Body *db.Volume
}

type CreateVolumeBody struct {
	Name      string `json:"name"`
	StorageGB int    `json:"storage_gb"`
}

type CreateVolumeInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      CreateVolumeBody
}

type AttachVolumeBody struct {
	ServiceID string `json:"service_id"`
	MountPath string `json:"mount_path"`
}

type AttachVolumeInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	VolumeID  string `path:"volumeId"`
	Body      AttachVolumeBody
}

type GetVolumeMountOutput struct {
	Body *db.VolumeMount
}

type ListVolumeMountsOutput struct {
	Body []db.VolumeMount
}

type VolumeBackupConfigOutput struct {
	Body *db.VolumeBackupConfig
}

type UpsertVolumeBackupBody struct {
	StorageIntegrationID string `json:"storage_integration_id"`
	Schedule             string `json:"schedule"`
	RetentionDays        int    `json:"retention_days"`
	PathPrefix           string `json:"path_prefix,omitempty"`
	Enabled              bool   `json:"enabled"`
}

type UpsertVolumeBackupInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	VolumeID  string `path:"volumeId"`
	Body      UpsertVolumeBackupBody
}

// ─── Route registration ───────────────────────────────────────────────────────

func (h *Handler) registerVolumeRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-volumes",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/volumes",
		Tags:        []string{"volumes"},
	}, h.ListVolumes)

	huma.Register(api, huma.Operation{
		OperationID:   "create-volume",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/volumes",
		Tags:          []string{"volumes"},
		DefaultStatus: 201,
	}, h.CreateVolume)

	huma.Register(api, huma.Operation{
		OperationID: "get-volume",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}",
		Tags:        []string{"volumes"},
	}, h.GetVolume)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-volume",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}",
		Tags:          []string{"volumes"},
		DefaultStatus: 204,
	}, h.DeleteVolume)

	huma.Register(api, huma.Operation{
		OperationID:   "attach-volume",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/mounts",
		Tags:          []string{"volumes"},
		DefaultStatus: 201,
	}, h.AttachVolume)

	huma.Register(api, huma.Operation{
		OperationID:   "detach-volume",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/mounts/{mountId}",
		Tags:          []string{"volumes"},
		DefaultStatus: 204,
	}, h.DetachVolume)

	huma.Register(api, huma.Operation{
		OperationID: "list-service-mounts",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/mounts",
		Tags:        []string{"volumes"},
	}, h.ListServiceMounts)

	huma.Register(api, huma.Operation{
		OperationID: "get-volume-backup",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/backup",
		Tags:        []string{"volumes"},
	}, h.GetVolumeBackup)

	huma.Register(api, huma.Operation{
		OperationID: "upsert-volume-backup",
		Method:      "PUT",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/backup",
		Tags:        []string{"volumes"},
	}, h.UpsertVolumeBackup)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-volume-backup",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/backup",
		Tags:          []string{"volumes"},
		DefaultStatus: 204,
	}, h.DeleteVolumeBackup)
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

func (h *Handler) ListVolumes(ctx context.Context, input *VolumeProjectPathInput) (*ListVolumesOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid project ID")
	}
	volumes, err := h.svc.Volumes.List(ctx, projectID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list volumes: " + err.Error())
	}
	return &ListVolumesOutput{Body: volumes}, nil
}

func (h *Handler) CreateVolume(ctx context.Context, input *CreateVolumeInput) (*GetVolumeOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid project ID")
	}
	volume, err := h.svc.Volumes.Create(ctx, projectID, input.Body.Name, input.Body.StorageGB)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &GetVolumeOutput{Body: volume}, nil
}

func (h *Handler) GetVolume(ctx context.Context, input *VolumePathInput) (*GetVolumeOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	volumeID, err := uuid.Parse(input.VolumeID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid volume ID")
	}
	volume, err := h.svc.Volumes.Get(ctx, volumeID)
	if err != nil {
		return nil, huma.Error404NotFound("volume not found")
	}
	return &GetVolumeOutput{Body: volume}, nil
}

func (h *Handler) DeleteVolume(ctx context.Context, input *VolumePathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	volumeID, err := uuid.Parse(input.VolumeID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid volume ID")
	}
	if err := h.svc.Volumes.Delete(ctx, volumeID); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return nil, nil
}

func (h *Handler) AttachVolume(ctx context.Context, input *AttachVolumeInput) (*GetVolumeMountOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	volumeID, err := uuid.Parse(input.VolumeID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid volume ID")
	}
	serviceID, err := uuid.Parse(input.Body.ServiceID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid service_id")
	}
	mount, err := h.svc.Volumes.Attach(ctx, volumeID, serviceID, input.Body.MountPath)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &GetVolumeMountOutput{Body: mount}, nil
}

func (h *Handler) DetachVolume(ctx context.Context, input *VolumeMountPathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	mountID, err := uuid.Parse(input.MountID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid mount ID")
	}
	if err := h.svc.Volumes.Detach(ctx, mountID); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return nil, nil
}

func (h *Handler) ListServiceMounts(ctx context.Context, input *ServiceMountsPathInput) (*ListVolumeMountsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := uuid.Parse(input.ServiceID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid service ID")
	}
	mounts, err := h.svc.Volumes.ListServiceMounts(ctx, serviceID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list mounts: " + err.Error())
	}
	return &ListVolumeMountsOutput{Body: mounts}, nil
}

func (h *Handler) GetVolumeBackup(ctx context.Context, input *VolumePathInput) (*VolumeBackupConfigOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	volumeID, err := uuid.Parse(input.VolumeID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid volume ID")
	}
	cfg, err := h.svc.Volumes.GetBackupConfig(ctx, volumeID)
	if err != nil {
		return nil, huma.Error404NotFound("no backup config")
	}
	return &VolumeBackupConfigOutput{Body: cfg}, nil
}

func (h *Handler) UpsertVolumeBackup(ctx context.Context, input *UpsertVolumeBackupInput) (*VolumeBackupConfigOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	volumeID, err := uuid.Parse(input.VolumeID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid volume ID")
	}
	storageID, err := uuid.Parse(input.Body.StorageIntegrationID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid storage_integration_id")
	}
	retention := input.Body.RetentionDays
	if retention <= 0 {
		retention = 30
	}
	cfg, err := h.svc.Volumes.UpsertBackupConfig(ctx, volumeID, service.VolumeBackupConfigInput{
		StorageIntegrationID: storageID,
		Schedule:             input.Body.Schedule,
		RetentionDays:        retention,
		PathPrefix:           input.Body.PathPrefix,
		Enabled:              input.Body.Enabled,
	})
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &VolumeBackupConfigOutput{Body: cfg}, nil
}

func (h *Handler) DeleteVolumeBackup(ctx context.Context, input *VolumePathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	volumeID, err := uuid.Parse(input.VolumeID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid volume ID")
	}
	return nil, h.svc.Volumes.DeleteBackupConfig(ctx, volumeID)
}
