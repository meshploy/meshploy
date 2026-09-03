package service

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// maxConfigFileBytes is the ceiling on a single file's content.
//
// A Kubernetes Secret holds at most 1MiB across all its keys, and a service may
// mount several files. Rejecting at the API boundary gives a clear message
// instead of an apply that fails much later with a Kubernetes error.
const maxConfigFileBytes = 256 * 1024

type ConfigFileService struct {
	db         *gorm.DB
	deployment *DeploymentService
}

type CreateConfigFileInput struct {
	Name    string
	Path    string
	Content string
	StackID *uuid.UUID
}

// validate checks what the cluster would otherwise reject much later.
func (in CreateConfigFileInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !strings.HasPrefix(in.Path, "/") {
		return fmt.Errorf("path must be absolute, e.g. /etc/zot/config.json")
	}
	if strings.HasSuffix(in.Path, "/") {
		return fmt.Errorf("path must name a file, not a directory")
	}
	if len(in.Content) > maxConfigFileBytes {
		return fmt.Errorf("content is %d bytes; the limit is %d", len(in.Content), maxConfigFileBytes)
	}
	return nil
}

func (s *ConfigFileService) List(ctx context.Context, projectID uuid.UUID) ([]db.ConfigFile, error) {
	files := make([]db.ConfigFile, 0)
	err := s.db.WithContext(ctx).Where("project_id = ?", projectID).Order("name").Find(&files).Error
	return files, err
}

func (s *ConfigFileService) Get(ctx context.Context, fileID uuid.UUID) (*db.ConfigFile, error) {
	var f db.ConfigFile
	err := s.db.WithContext(ctx).First(&f, "id = ?", fileID).Error
	return &f, err
}

func (s *ConfigFileService) Create(ctx context.Context, projectID uuid.UUID, in CreateConfigFileInput) (*db.ConfigFile, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	f := &db.ConfigFile{
		ProjectID: projectID,
		StackID:   in.StackID,
		Name:      strings.TrimSpace(in.Name),
		Path:      in.Path,
		Content:   db.EncryptedString(in.Content),
	}
	if err := s.db.WithContext(ctx).Create(f).Error; err != nil {
		return nil, err
	}
	return f, nil
}

// Update replaces a file's content or path and re-applies every service using
// it.
//
// Editing propagates, which is where this differs from a volume: changing a
// volume affects nothing else, changing a config file changes it for every
// attached service. They are all re-applied so the running files match what the
// UI shows — a subPath mount is a one-time copy, so without this the change is
// invisible until each service happens to restart.
func (s *ConfigFileService) Update(ctx context.Context, fileID uuid.UUID, in CreateConfigFileInput) (*db.ConfigFile, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	var f db.ConfigFile
	if err := s.db.WithContext(ctx).First(&f, "id = ?", fileID).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&f).Updates(map[string]any{
		"name":    strings.TrimSpace(in.Name),
		"path":    in.Path,
		"content": db.EncryptedString(in.Content),
	}).Error; err != nil {
		return nil, err
	}
	s.reapplyAttached(ctx, fileID)
	return s.Get(ctx, fileID)
}

// Delete removes a file, refusing while any service still mounts it.
//
// Same rule as a volume: the resource is still referenced, and deleting it is a
// request that cannot be satisfied coherently. Detaching first is an explicit
// step per service, which is the point.
func (s *ConfigFileService) Delete(ctx context.Context, fileID uuid.UUID) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&db.ServiceConfigFile{}).
		Where("config_file_id = ?", fileID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("config file is still attached to %d service(s) — detach it first", count)
	}
	return s.db.WithContext(ctx).Delete(&db.ConfigFile{}, "id = ?", fileID).Error
}

// Attach mounts a file into a service and re-applies it.
func (s *ConfigFileService) Attach(ctx context.Context, fileID, serviceID uuid.UUID) error {
	var existing int64
	if err := s.db.WithContext(ctx).Model(&db.ServiceConfigFile{}).
		Where("config_file_id = ? AND service_id = ?", fileID, serviceID).
		Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return fmt.Errorf("already attached to this service")
	}
	if err := s.db.WithContext(ctx).Create(&db.ServiceConfigFile{
		ServiceID: serviceID, ConfigFileID: fileID,
	}).Error; err != nil {
		return err
	}
	s.reapply(ctx, serviceID)
	return nil
}

// Detach removes a file from a service and re-applies it immediately.
//
// Not blocked, even while the service runs: refusing would mean a service could
// not be reconfigured without deleting it. The re-apply is what makes it safe —
// a subPath mount is a copy, so the running pod keeps the file after detach, and
// without re-applying the change would surface only at some later restart,
// failing for a reason nobody connects to this action.
func (s *ConfigFileService) Detach(ctx context.Context, fileID, serviceID uuid.UUID) error {
	if err := s.db.WithContext(ctx).
		Where("config_file_id = ? AND service_id = ?", fileID, serviceID).
		Delete(&db.ServiceConfigFile{}).Error; err != nil {
		return err
	}
	s.reapply(ctx, serviceID)
	return nil
}

// AttachedServices lists the services mounting a file, so callers can say what
// an edit or a delete would affect.
func (s *ConfigFileService) AttachedServices(ctx context.Context, fileID uuid.UUID) ([]db.Service, error) {
	services := make([]db.Service, 0)
	err := s.db.WithContext(ctx).
		Joins("JOIN service_config_files scf ON scf.service_id = services.id").
		Where("scf.config_file_id = ?", fileID).
		Find(&services).Error
	return services, err
}

// ForService returns the files a service mounts, in path order so the rendered
// Secret is stable between deploys.
func (s *ConfigFileService) ForService(ctx context.Context, serviceID uuid.UUID) ([]db.ConfigFile, error) {
	files := make([]db.ConfigFile, 0)
	err := s.db.WithContext(ctx).
		Joins("JOIN service_config_files scf ON scf.config_file_id = config_files.id").
		Where("scf.service_id = ?", serviceID).
		Order("config_files.path").
		Find(&files).Error
	return files, err
}

func (s *ConfigFileService) reapplyAttached(ctx context.Context, fileID uuid.UUID) {
	services, err := s.AttachedServices(ctx, fileID)
	if err != nil {
		log.Printf("config file %s: list attached services: %v", fileID, err)
		return
	}
	for i := range services {
		s.reapply(ctx, services[i].ID)
	}
}

// reapply pushes the change to the cluster. Failures are logged rather than
// returned: the record is already correct, and the caller cannot usefully undo a
// committed edit. The orphan report and the status reconciler surface a service
// whose cluster state fell behind.
func (s *ConfigFileService) reapply(ctx context.Context, serviceID uuid.UUID) {
	if s.deployment == nil {
		return
	}
	if err := s.deployment.ReapplyService(ctx, serviceID); err != nil {
		log.Printf("config file: re-apply service %s: %v", serviceID, err)
	}
}
