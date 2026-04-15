package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type WorkloadService struct {
	db *gorm.DB
}

type CreateWorkloadInput struct {
	Name    string
	Image   string
	NodeID  *uuid.UUID // nil = let K3s schedule
	EnvVars string     // raw .env block, stored as EncryptedString

	// K8s resource spec — optional, defaults applied by the model
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
	Replicas      int

	// Optional build config — when GitRepo is set, a BuildConfig row is
	// created alongside the Service in the same transaction.
	GitRepo               string
	Branch                string
	Builder               db.BuilderType
	DockerfilePath        string
	RegistryIntegrationID *uuid.UUID
	BuilderNode           string // "" = auto-schedule on any builder node
}

func (s *WorkloadService) List(ctx context.Context, projectID uuid.UUID) ([]db.Service, error) {
	services := make([]db.Service, 0)
	err := s.db.WithContext(ctx).Where("project_id = ?", projectID).Find(&services).Error
	return services, err
}

func (s *WorkloadService) Get(ctx context.Context, serviceID uuid.UUID) (*db.Service, error) {
	var service db.Service
	err := s.db.WithContext(ctx).First(&service, "id = ?", serviceID).Error
	return &service, err
}

func (s *WorkloadService) Create(ctx context.Context, projectID uuid.UUID, in CreateWorkloadInput) (*db.Service, error) {
	replicas := in.Replicas
	if replicas == 0 {
		replicas = 1
	}
	service := &db.Service{
		ProjectID: projectID,
		NodeID:    in.NodeID,
		Name:      in.Name,
		Type:      db.ServiceTypeApplication,
		Image:     in.Image,
		Status:    db.ServiceStopped,
		Replicas:  replicas,
		EnvVars:   db.EncryptedString(in.EnvVars),
	}

	return service, s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(service).Error; err != nil {
			return err
		}
		if in.GitRepo == "" {
			return nil
		}
		builder := in.Builder
		if builder == "" {
			builder = db.BuilderNixpacks
		}
		branch := in.Branch
		if branch == "" {
			branch = "main"
		}
		dockerfilePath := in.DockerfilePath
		if dockerfilePath == "" {
			dockerfilePath = "Dockerfile"
		}
		bc := &db.BuildConfig{
			ServiceID:             service.ID,
			Builder:               builder,
			GitRepo:               in.GitRepo,
			Branch:                branch,
			DockerfilePath:        dockerfilePath,
			RegistryIntegrationID: in.RegistryIntegrationID,
			BuilderNode:           in.BuilderNode,
		}
		return tx.Create(bc).Error
	})
}

func (s *WorkloadService) Delete(ctx context.Context, serviceID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.Service{}, "id = ?", serviceID).Error
}

// ─── Update ───────────────────────────────────────────────────────────────────

type UpdateWorkloadInput struct {
	Name          *string
	Image         *string
	UpdateNode    bool       // when true, NodeID is applied (nil = auto-schedule)
	NodeID        *uuid.UUID
	Replicas      *int
	CPURequest    *string
	CPULimit      *string
	MemoryRequest *string
	MemoryLimit   *string
	EnvVars       *string // nil = no change
}

func (s *WorkloadService) Update(ctx context.Context, serviceID uuid.UUID, in UpdateWorkloadInput) (*db.Service, error) {
	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Image != nil {
		updates["image"] = *in.Image
	}
	if in.UpdateNode {
		updates["node_id"] = in.NodeID // nil → NULL, uuid → pin
	}
	if in.Replicas != nil {
		updates["replicas"] = *in.Replicas
	}
	if in.CPURequest != nil {
		updates["cpu_request"] = *in.CPURequest
	}
	if in.CPULimit != nil {
		updates["cpu_limit"] = *in.CPULimit
	}
	if in.MemoryRequest != nil {
		updates["memory_request"] = *in.MemoryRequest
	}
	if in.MemoryLimit != nil {
		updates["memory_limit"] = *in.MemoryLimit
	}
	if in.EnvVars != nil {
		updates["env_vars"] = db.EncryptedString(*in.EnvVars)
	}
	if len(updates) == 0 {
		return s.Get(ctx, serviceID)
	}
	if err := s.db.WithContext(ctx).Model(&db.Service{}).Where("id = ?", serviceID).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, serviceID)
}

func (s *WorkloadService) GetEnvVars(ctx context.Context, serviceID uuid.UUID) (string, error) {
	var svc db.Service
	if err := s.db.WithContext(ctx).Select("env_vars").First(&svc, "id = ?", serviceID).Error; err != nil {
		return "", err
	}
	return string(svc.EnvVars), nil
}

// ─── Build config ─────────────────────────────────────────────────────────────

func (s *WorkloadService) GetBuildConfig(ctx context.Context, serviceID uuid.UUID) (*db.BuildConfig, error) {
	var bc db.BuildConfig
	if err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).First(&bc).Error; err != nil {
		return nil, err
	}
	return &bc, nil
}

// GetBuildEnvVars returns the decrypted build-time env vars for a service.
func (s *WorkloadService) GetBuildEnvVars(ctx context.Context, serviceID uuid.UUID) (string, error) {
	bc, err := s.GetBuildConfig(ctx, serviceID)
	if err != nil {
		return "", err
	}
	return string(bc.BuildEnvVars), nil
}

type UpdateBuildConfigInput struct {
	GitRepo               *string
	Branch                *string
	Builder               *db.BuilderType
	DockerfilePath        *string
	RegistryIntegrationID *uuid.UUID
	ClearRegistry         bool    // when true, set registry_integration_id to NULL
	BuildEnvVars          *string // nil = no change; "" = clear
	BuilderNode           *string // nil = no change; "" = auto-schedule
	BuilderCPURequest     *string // nil = no change; "" = use default (1000m)
	BuilderMemoryRequest  *string // nil = no change; "" = use default (1Gi)
}

// UpsertBuildConfig creates or updates the BuildConfig for a service.
func (s *WorkloadService) UpsertBuildConfig(ctx context.Context, serviceID uuid.UUID, in UpdateBuildConfigInput) (*db.BuildConfig, error) {
	var bc db.BuildConfig
	err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).First(&bc).Error
	isNew := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !isNew {
		return nil, err
	}
	if isNew {
		bc = db.BuildConfig{
			ServiceID:      serviceID,
			Builder:        db.BuilderNixpacks,
			DockerfilePath: "Dockerfile",
		}
	}
	if in.GitRepo != nil {
		bc.GitRepo = *in.GitRepo
	}
	if in.Branch != nil {
		bc.Branch = *in.Branch
	}
	if in.Builder != nil {
		bc.Builder = *in.Builder
	}
	if in.DockerfilePath != nil {
		bc.DockerfilePath = *in.DockerfilePath
	}
	if in.ClearRegistry {
		bc.RegistryIntegrationID = nil
	} else if in.RegistryIntegrationID != nil {
		bc.RegistryIntegrationID = in.RegistryIntegrationID
	}
	if in.BuildEnvVars != nil {
		bc.BuildEnvVars = db.EncryptedString(*in.BuildEnvVars)
	}
	if in.BuilderNode != nil {
		bc.BuilderNode = *in.BuilderNode
	}
	if in.BuilderCPURequest != nil {
		bc.BuilderCPURequest = *in.BuilderCPURequest
	}
	if in.BuilderMemoryRequest != nil {
		bc.BuilderMemoryRequest = *in.BuilderMemoryRequest
	}
	if isNew {
		err = s.db.WithContext(ctx).Create(&bc).Error
	} else {
		err = s.db.WithContext(ctx).Save(&bc).Error
	}
	return &bc, err
}
