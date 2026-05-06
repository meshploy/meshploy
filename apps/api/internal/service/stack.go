package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

type StackService struct {
	db       *gorm.DB
	workload *WorkloadService
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

func (s *StackService) List(ctx context.Context, projectID uuid.UUID) ([]meshdb.Stack, error) {
	var stacks []meshdb.Stack
	err := s.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Find(&stacks).Error
	return stacks, err
}

func (s *StackService) Get(ctx context.Context, stackID uuid.UUID) (*meshdb.Stack, error) {
	var stack meshdb.Stack
	err := s.db.WithContext(ctx).First(&stack, "id = ?", stackID).Error
	return &stack, err
}

func (s *StackService) Create(ctx context.Context, projectID uuid.UUID, name, spec string) (*meshdb.Stack, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	stack := &meshdb.Stack{
		ProjectID: projectID,
		Name:      name,
		Spec:      spec,
		Status:    meshdb.StackIdle,
	}
	err := s.db.WithContext(ctx).Create(stack).Error
	return stack, err
}

func (s *StackService) Update(ctx context.Context, stackID uuid.UUID, name, spec string) (*meshdb.Stack, error) {
	var stack meshdb.Stack
	if err := s.db.WithContext(ctx).First(&stack, "id = ?", stackID).Error; err != nil {
		return nil, err
	}
	updates := map[string]any{}
	if name != "" {
		updates["name"] = name
	}
	updates["spec"] = spec
	err := s.db.WithContext(ctx).Model(&stack).Updates(updates).Error
	return &stack, err
}

func (s *StackService) Delete(ctx context.Context, stackID uuid.UUID) error {
	// Unlink services (SET NULL on StackID) — handled by the FK constraint.
	return s.db.WithContext(ctx).Delete(&meshdb.Stack{}, "id = ?", stackID).Error
}

// ListServices returns all services that belong to a stack.
func (s *StackService) ListServices(ctx context.Context, stackID uuid.UUID) ([]meshdb.Service, error) {
	var services []meshdb.Service
	err := s.db.WithContext(ctx).
		Where("stack_id = ?", stackID).
		Order("name ASC").
		Find(&services).Error
	return services, err
}

// ---------------------------------------------------------------------------
// Apply — reconcile DB records from spec
// ---------------------------------------------------------------------------

// composeSpec mirrors the Docker Compose YAML format with x-meshploy extensions.
type composeSpec struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image       string            `yaml:"image"`
	Command     []string          `yaml:"command"`
	Environment map[string]string `yaml:"environment"`
	Ports       []string          `yaml:"ports"`
	MeshployExt *meshployExt      `yaml:"x-meshploy"`
}

type meshployExt struct {
	Type     string             `yaml:"type"`
	Source   *meshploySource    `yaml:"source"`
	Build    *meshployBuild     `yaml:"build"`
	Deploy   *meshployDeploy    `yaml:"deploy"`
	Rollback *meshployRollback  `yaml:"rollback"`
	Database *meshployDatabase  `yaml:"database"`
}

type meshployDatabase struct {
	Engine    string `yaml:"engine"`
	Version   string `yaml:"version"`
	StorageGB int    `yaml:"storage_gb"`
	DBName    string `yaml:"db_name"`
	DBUser    string `yaml:"db_user"`
	DBPassword string `yaml:"db_password"`
}

type meshploySource struct {
	Git       string `yaml:"git"`
	Branch    string `yaml:"branch"`
	RootDir   string `yaml:"root_dir"`
	IntegrationID string `yaml:"integration_id"`
}

type meshployBuild struct {
	Builder             string `yaml:"builder"`
	DockerfilePath      string `yaml:"dockerfile_path"`
	BuilderNode         string `yaml:"builder_node"`
	BuilderCPURequest   string `yaml:"builder_cpu_request"`
	BuilderMemoryRequest string `yaml:"builder_memory_request"`
}

type meshployDeploy struct {
	Replicas      int    `yaml:"replicas"`
	Port          int    `yaml:"port"`
	Node          string `yaml:"node"`
	CPURequest    string `yaml:"cpu_request"`
	CPULimit      string `yaml:"cpu_limit"`
	MemoryRequest string `yaml:"memory_request"`
	MemoryLimit   string `yaml:"memory_limit"`
}

type meshployRollback struct {
	Enabled   bool `yaml:"enabled"`
	Retention int  `yaml:"retention"`
}

type ApplyResult struct {
	Stack    *meshdb.Stack
	Created  []string
	Updated  []string
	Deleted  []string
	Errors   []string
}

// Apply parses the stack's spec and reconciles Service + BuildConfig rows.
// Services no longer present in the spec are unlinked from the stack (not deleted).
func (s *StackService) Apply(ctx context.Context, stackID uuid.UUID, triggerBy uuid.UUID) (*ApplyResult, error) {
	var stack meshdb.Stack
	if err := s.db.WithContext(ctx).First(&stack, "id = ?", stackID).Error; err != nil {
		return nil, err
	}

	if err := s.db.WithContext(ctx).Model(&stack).Updates(map[string]any{
		"status": meshdb.StackApplying,
	}).Error; err != nil {
		return nil, err
	}

	result := &ApplyResult{Stack: &stack}

	var spec composeSpec
	if err := yaml.Unmarshal([]byte(stack.Spec), &spec); err != nil {
		_ = s.db.WithContext(ctx).Model(&stack).Updates(map[string]any{"status": meshdb.StackFailed}).Error
		return nil, fmt.Errorf("invalid spec YAML: %w", err)
	}

	// Current services in this stack.
	var existing []meshdb.Service
	s.db.WithContext(ctx).Where("stack_id = ?", stackID).Find(&existing)
	existingByName := make(map[string]meshdb.Service, len(existing))
	for _, svc := range existing {
		existingByName[svc.Name] = svc
	}

	specNames := make(map[string]struct{}, len(spec.Services))

	for svcName, svcDef := range spec.Services {
		specNames[svcName] = struct{}{}
		ext := svcDef.MeshployExt

		// Build env vars string (KEY=VALUE lines).
		var envParts []string
		for k, v := range svcDef.Environment {
			envParts = append(envParts, k+"="+v)
		}
		envVarsStr := strings.Join(envParts, "\n")

		port := 3000
		if ext != nil && ext.Deploy != nil && ext.Deploy.Port > 0 {
			port = ext.Deploy.Port
		}
		replicas := 1
		if ext != nil && ext.Deploy != nil && ext.Deploy.Replicas > 0 {
			replicas = ext.Deploy.Replicas
		}
		cpuRequest, cpuLimit := "100m", "500m"
		memRequest, memLimit := "128Mi", "512Mi"
		if ext != nil && ext.Deploy != nil {
			if ext.Deploy.CPURequest != "" {
				cpuRequest = ext.Deploy.CPURequest
			}
			if ext.Deploy.CPULimit != "" {
				cpuLimit = ext.Deploy.CPULimit
			}
			if ext.Deploy.MemoryRequest != "" {
				memRequest = ext.Deploy.MemoryRequest
			}
			if ext.Deploy.MemoryLimit != "" {
				memLimit = ext.Deploy.MemoryLimit
			}
		}

		isDatabase := ext != nil && ext.Type == "database"

		existingSvc, exists := existingByName[svcName]
		if !exists {
			var svc *meshdb.Service
			var createErr error

			if isDatabase && ext.Database != nil {
				dbInput := CreateWorkloadInput{
					StackID:   &stackID,
					Name:      svcName,
					Type:      meshdb.ServiceTypeDatabase,
					Port:      port,
					Replicas:  replicas,
					Engine:    meshdb.DatabaseEngine(ext.Database.Engine),
					Version:   ext.Database.Version,
					StorageGB: ext.Database.StorageGB,
					DBName:    ext.Database.DBName,
					DBUser:    ext.Database.DBUser,
					DBPassword: ext.Database.DBPassword,
				}
				if dbInput.StorageGB == 0 {
					dbInput.StorageGB = 10
				}
				svc, createErr = s.workload.Create(ctx, stack.ProjectID, dbInput)
			} else {
				input := CreateWorkloadInput{
					StackID:       &stackID,
					Name:          svcName,
					Type:          meshdb.ServiceTypeApplication,
					Image:         svcDef.Image,
					Port:          port,
					Replicas:      replicas,
					CPURequest:    cpuRequest,
					CPULimit:      cpuLimit,
					MemoryRequest: memRequest,
					MemoryLimit:   memLimit,
					EnvVars:       envVarsStr,
				}
				svc, createErr = s.workload.Create(ctx, stack.ProjectID, input)
			}
			if createErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: create failed: %v", svcName, createErr))
				continue
			}

			// Create build config if source or builder is specified (app only).
			if !isDatabase && ext != nil && (ext.Source != nil || ext.Build != nil) {
				if err := s.applyBuildConfig(ctx, svc.ID, svcDef); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: build config failed: %v", svcName, err))
				}
			}
			result.Created = append(result.Created, svcName)
		} else {
			// Update existing service.
			updates := map[string]any{
				"image":          svcDef.Image,
				"port":           port,
				"replicas":       replicas,
				"cpu_request":    cpuRequest,
				"cpu_limit":      cpuLimit,
				"memory_request": memRequest,
				"memory_limit":   memLimit,
			}
			if err := s.db.WithContext(ctx).Model(&existingSvc).Updates(updates).Error; err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: update failed: %v", svcName, err))
				continue
			}
			result.Updated = append(result.Updated, svcName)
		}
	}

	// Unlink services that are no longer in spec.
	for name, svc := range existingByName {
		if _, ok := specNames[name]; !ok {
			s.db.WithContext(ctx).Model(&svc).Update("stack_id", nil)
			result.Deleted = append(result.Deleted, name)
		}
	}

	now := time.Now()
	finalStatus := meshdb.StackIdle
	if len(result.Errors) > 0 {
		finalStatus = meshdb.StackFailed
	}
	s.db.WithContext(ctx).Model(&stack).Updates(map[string]any{
		"status":          finalStatus,
		"last_applied_at": now,
	})
	stack.Status = finalStatus
	stack.LastAppliedAt = &now
	result.Stack = &stack

	return result, nil
}

func (s *StackService) applyBuildConfig(ctx context.Context, serviceID uuid.UUID, svcDef composeService) error {
	ext := svcDef.MeshployExt
	if ext == nil {
		return nil
	}

	builder := meshdb.BuilderNixpacks
	if ext.Build != nil && ext.Build.Builder != "" {
		switch ext.Build.Builder {
		case "railpack":
			builder = meshdb.BuilderRailpack
		case "dockerfile":
			builder = meshdb.BuilderDockerfile
		case "image":
			builder = meshdb.BuilderImage
		}
	}
	if svcDef.Image != "" && (ext.Source == nil || ext.Source.Git == "") {
		builder = meshdb.BuilderImage
	}

	input := UpdateBuildConfigInput{
		Builder: &builder,
	}
	if ext.Source != nil {
		gitRepo := ext.Source.Git
		input.GitRepo = &gitRepo
		if ext.Source.Branch != "" {
			branch := ext.Source.Branch
			input.Branch = &branch
		}
		if ext.Source.IntegrationID != "" {
			id, err := uuid.Parse(ext.Source.IntegrationID)
			if err == nil {
				input.GitIntegrationID = &id
			}
		}
	}
	if ext.Build != nil {
		if ext.Build.DockerfilePath != "" {
			input.DockerfilePath = &ext.Build.DockerfilePath
		}
		if ext.Build.BuilderNode != "" {
			input.BuilderNode = &ext.Build.BuilderNode
		}
		if ext.Build.BuilderCPURequest != "" {
			input.BuilderCPURequest = &ext.Build.BuilderCPURequest
		}
		if ext.Build.BuilderMemoryRequest != "" {
			input.BuilderMemoryRequest = &ext.Build.BuilderMemoryRequest
		}
	}
	if ext.Rollback != nil {
		input.RollbackEnabled = &ext.Rollback.Enabled
		if ext.Rollback.Retention > 0 {
			input.ImageRetention = &ext.Rollback.Retention
		}
	}

	_, err := s.workload.UpsertBuildConfig(ctx, serviceID, input)
	return err
}
