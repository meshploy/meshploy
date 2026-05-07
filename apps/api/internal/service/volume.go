package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	appk8s "github.com/meshploy/apps/api/internal/k8s"
	db "github.com/meshploy/packages/db"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

type VolumeService struct {
	db         *gorm.DB
	k8s        kubernetes.Interface
	deployment *DeploymentService
}

// volumeSlug generates a stable K8s PVC name: vol-<6 random hex chars>.
func volumeSlug() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return "vol-" + hex.EncodeToString(b)
}

func (s *VolumeService) List(ctx context.Context, projectID uuid.UUID) ([]db.Volume, error) {
	var volumes []db.Volume
	err := s.db.WithContext(ctx).
		Preload("Mounts").
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Find(&volumes).Error
	return volumes, err
}

func (s *VolumeService) Get(ctx context.Context, volumeID uuid.UUID) (*db.Volume, error) {
	var volume db.Volume
	err := s.db.WithContext(ctx).
		Preload("Mounts.Volume").
		First(&volume, "id = ?", volumeID).Error
	return &volume, err
}

func (s *VolumeService) Create(ctx context.Context, projectID uuid.UUID, name string, storageGB int) (*db.Volume, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if storageGB <= 0 {
		storageGB = 5
	}
	slug := volumeSlug()
	volume := &db.Volume{
		ProjectID: projectID,
		Name:      name,
		Slug:      slug,
		StorageGB: storageGB,
		Status:    db.VolumePending,
	}
	if err := s.db.WithContext(ctx).Create(volume).Error; err != nil {
		return nil, err
	}

	// Provision the PVC if K8s is available. We need the project namespace.
	if s.k8s != nil {
		var project db.Project
		if err := s.db.WithContext(ctx).First(&project, "id = ?", projectID).Error; err == nil {
			if err := appk8s.EnsureNamespace(ctx, s.k8s, project.Slug); err == nil {
				if err := appk8s.EnsureVolumePVC(ctx, s.k8s, slug, project.Slug, storageGB); err == nil {
					s.db.WithContext(ctx).Model(volume).Update("status", db.VolumeReady)
					volume.Status = db.VolumeReady
				}
			}
		}
	}
	return volume, nil
}

func (s *VolumeService) Delete(ctx context.Context, volumeID uuid.UUID) error {
	var volume db.Volume
	if err := s.db.WithContext(ctx).Preload("Mounts").First(&volume, "id = ?", volumeID).Error; err != nil {
		return err
	}
	if len(volume.Mounts) > 0 {
		return fmt.Errorf("volume is still attached to %d service(s) — detach before deleting", len(volume.Mounts))
	}

	// Delete K8s PVC if available.
	if s.k8s != nil {
		var project db.Project
		if err := s.db.WithContext(ctx).First(&project, "id = ?", volume.ProjectID).Error; err == nil {
			_ = appk8s.DeleteVolumePVC(ctx, s.k8s, volume.Slug, project.Slug)
		}
	}
	return s.db.WithContext(ctx).Delete(&db.Volume{}, "id = ?", volumeID).Error
}

// Attach mounts a volume to a service at the given path.
// Enforces: app service only, not already attached, and forces replicas to 1.
func (s *VolumeService) Attach(ctx context.Context, volumeID, serviceID uuid.UUID, mountPath string) (*db.VolumeMount, error) {
	if mountPath == "" {
		return nil, fmt.Errorf("mount_path is required")
	}
	if !strings.HasPrefix(mountPath, "/") {
		return nil, fmt.Errorf("mount_path must be an absolute path")
	}

	var volume db.Volume
	if err := s.db.WithContext(ctx).Preload("Mounts").First(&volume, "id = ?", volumeID).Error; err != nil {
		return nil, err
	}
	if len(volume.Mounts) > 0 {
		return nil, fmt.Errorf("volume is already attached to another service — detach it first")
	}

	var svc db.Service
	if err := s.db.WithContext(ctx).First(&svc, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	if svc.Type != db.ServiceTypeApplication {
		return nil, fmt.Errorf("volumes can only be attached to application services")
	}

	mount := &db.VolumeMount{
		VolumeID:  volumeID,
		ServiceID: serviceID,
		MountPath: mountPath,
	}
	if err := s.db.WithContext(ctx).Create(mount).Error; err != nil {
		return nil, err
	}

	// Enforce single replica when a volume is attached (RWO constraint).
	if svc.Replicas > 1 {
		s.db.WithContext(ctx).Model(&db.Service{}).Where("id = ?", serviceID).Update("replicas", 1)
	}

	// Re-apply the K8s deployment to mount the PVC immediately.
	if s.deployment != nil {
		_ = s.deployment.ReapplyService(ctx, serviceID)
	}
	return mount, nil
}

// Detach removes a volume mount. K8s deployment is re-applied immediately.
func (s *VolumeService) Detach(ctx context.Context, mountID uuid.UUID) error {
	var mount db.VolumeMount
	if err := s.db.WithContext(ctx).First(&mount, "id = ?", mountID).Error; err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&db.VolumeMount{}, "id = ?", mountID).Error; err != nil {
		return err
	}
	if s.deployment != nil {
		_ = s.deployment.ReapplyService(ctx, mount.ServiceID)
	}
	return nil
}

func (s *VolumeService) ListMounts(ctx context.Context, volumeID uuid.UUID) ([]db.VolumeMount, error) {
	var mounts []db.VolumeMount
	err := s.db.WithContext(ctx).Where("volume_id = ?", volumeID).Find(&mounts).Error
	return mounts, err
}

func (s *VolumeService) ListServiceMounts(ctx context.Context, serviceID uuid.UUID) ([]db.VolumeMount, error) {
	var mounts []db.VolumeMount
	err := s.db.WithContext(ctx).
		Preload("Volume").
		Where("service_id = ?", serviceID).
		Find(&mounts).Error
	return mounts, err
}

// resolveServiceVolumeMounts is used by DeploymentService to load volume mounts during deploy.
func resolveServiceVolumeMounts(ctx context.Context, gdb *gorm.DB, serviceID uuid.UUID) []appk8s.VolumeAttachment {
	var mounts []db.VolumeMount
	gdb.WithContext(ctx).Preload("Volume").Where("service_id = ?", serviceID).Find(&mounts)
	attachments := make([]appk8s.VolumeAttachment, 0, len(mounts))
	for _, m := range mounts {
		if m.Volume.Slug != "" {
			attachments = append(attachments, appk8s.VolumeAttachment{
				PVCName:   m.Volume.Slug,
				MountPath: m.MountPath,
			})
		}
	}
	return attachments
}

// ErrVolumeNotFound is returned when a volume is not found.
var ErrVolumeNotFound = errors.New("volume not found")
