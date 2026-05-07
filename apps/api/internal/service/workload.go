package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
	appk8s "github.com/meshploy/apps/api/internal/k8s"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

type WorkloadService struct {
	db  *gorm.DB
	k8s kubernetes.Interface // nil when K8s is not configured
}

type CreateWorkloadInput struct {
	Name    string
	Image   string
	NodeID  *uuid.UUID // nil = let K3s schedule
	StackID *uuid.UUID // nil = not part of a stack
	EnvVars string     // raw .env block, stored as EncryptedString
	Port    int        // container listen port; 0 = default (3000)

	// K8s resource spec — optional, defaults applied by the model
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
	Replicas      int

	// Optional build config — when GitRepo is set, a BuildConfig row is
	// created alongside the Service in the same transaction.
	GitIntegrationID      *uuid.UUID
	GitRepo               string
	Branch                string
	Builder               db.BuilderType
	DockerfilePath        string
	RegistryIntegrationID *uuid.UUID
	BuilderNode           string // "" = auto-schedule on any builder node
	BuilderCPURequest     string // "" = default (1000m)
	BuilderMemoryRequest  string // "" = default (1Gi)

	// Database-specific fields — used when Type == "database"
	Type       db.ServiceType
	Engine     db.DatabaseEngine
	Version    string
	StorageGB  int
	DBName     string
	DBUser     string
	DBPassword string

	// Healthcheck probe fields (from compose healthcheck: or API input)
	HealthcheckCmd             string
	HealthcheckIntervalSecs    int32
	HealthcheckTimeoutSecs     int32
	HealthcheckRetries         int32
	HealthcheckStartPeriodSecs int32
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

// dbDefaults returns the default image, port, and storage for a managed-database engine.
func dbDefaults(engine db.DatabaseEngine, version string) (image string, port int) {
	if version == "" {
		switch engine {
		case db.DatabasePostgres:
			version = "16"
		case db.DatabaseMySQL:
			version = "8.0"
		case db.DatabaseRedis:
			version = "7"
		case db.DatabaseMongoDB:
			version = "7"
		case db.DatabaseDragonfly:
			version = "latest"
		case db.DatabaseClickHouse:
			version = "24"
		}
	}
	switch engine {
	case db.DatabasePostgres:
		return "postgres:" + version, 5432
	case db.DatabaseMySQL:
		return "mysql:" + version, 3306
	case db.DatabaseRedis:
		return "redis:" + version, 6379
	case db.DatabaseMongoDB:
		return "mongo:" + version, 27017
	case db.DatabaseDragonfly:
		return "docker.dragonflydb.io/dragonflydb/dragonfly:" + version, 6379
	case db.DatabaseClickHouse:
		return "clickhouse/clickhouse-server:" + version, 9000
	default:
		return "postgres:" + version, 5432
	}
}

func (s *WorkloadService) Create(ctx context.Context, projectID uuid.UUID, in CreateWorkloadInput) (*db.Service, error) {
	if in.Type == db.ServiceTypeDatabase {
		return s.createDatabase(ctx, projectID, in)
	}

	replicas := in.Replicas
	if replicas == 0 {
		replicas = 1
	}
	port := in.Port
	if port == 0 {
		port = 3000
	}
	service := &db.Service{
		ProjectID:                  projectID,
		NodeID:                     in.NodeID,
		StackID:                    in.StackID,
		Name:                       in.Name,
		Type:                       db.ServiceTypeApplication,
		Image:                      in.Image,
		Status:                     db.ServiceStopped,
		Replicas:                   replicas,
		Port:                       port,
		EnvVars:                    db.EncryptedString(in.EnvVars),
		HealthcheckCmd:             in.HealthcheckCmd,
		HealthcheckIntervalSecs:    in.HealthcheckIntervalSecs,
		HealthcheckTimeoutSecs:     in.HealthcheckTimeoutSecs,
		HealthcheckRetries:         in.HealthcheckRetries,
		HealthcheckStartPeriodSecs: in.HealthcheckStartPeriodSecs,
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
			GitIntegrationID:      in.GitIntegrationID,
			Builder:               builder,
			GitRepo:               in.GitRepo,
			Branch:                branch,
			DockerfilePath:        dockerfilePath,
			RegistryIntegrationID: in.RegistryIntegrationID,
			BuilderNode:           in.BuilderNode,
			BuilderCPURequest:     in.BuilderCPURequest,
			BuilderMemoryRequest:  in.BuilderMemoryRequest,
		}
		return tx.Create(bc).Error
	})
}

// dbSlug generates a stable K8s resource name: {slugify(name)}-{6 random hex chars}.
func dbSlug(name string) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	base := strings.ToLower(name)
	base = strings.NewReplacer(" ", "-", "_", "-").Replace(base)
	return base + "-" + hex.EncodeToString(b)
}

func (s *WorkloadService) createDatabase(ctx context.Context, projectID uuid.UUID, in CreateWorkloadInput) (*db.Service, error) {
	if in.Engine == "" {
		in.Engine = db.DatabasePostgres
	}
	image, port := dbDefaults(in.Engine, in.Version)
	version := in.Version
	if version == "" {
		switch in.Engine {
		case db.DatabasePostgres:
			version = "16"
		case db.DatabaseMySQL:
			version = "8.0"
		case db.DatabaseRedis:
			version = "7"
		case db.DatabaseMongoDB:
			version = "7"
		}
	}
	storageGB := in.StorageGB
	if storageGB == 0 {
		storageGB = 10
	}
	dbName := in.DBName
	if dbName == "" {
		dbName = strings.ToLower(in.Name)
	}
	dbUser := in.DBUser
	if dbUser == "" {
		dbUser = strings.ToLower(in.Name)
	}
	dbPassword := in.DBPassword
	if dbPassword == "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		dbPassword = hex.EncodeToString(b)
	}
	service := &db.Service{
		ProjectID: projectID,
		NodeID:    in.NodeID,
		Name:      in.Name,
		Type:      db.ServiceTypeDatabase,
		Image:     image,
		Status:    db.ServiceStopped,
		Replicas:  1,
		Port:      port,
	}
	return service, s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(service).Error; err != nil {
			return err
		}
		dc := &db.DatabaseConfig{
			ServiceID:  service.ID,
			Engine:     in.Engine,
			Version:    version,
			StorageGB:  storageGB,
			Slug:       dbSlug(in.Name),
			DBName:     dbName,
			DBUser:     dbUser,
			DBPassword: db.EncryptedString(dbPassword),
		}
		return tx.Create(dc).Error
	})
}

func (s *WorkloadService) Start(ctx context.Context, serviceID uuid.UUID) (*db.Service, error) {
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	if svc.Image == "" {
		return nil, errors.New("service has never been deployed — trigger a deployment first")
	}
	if s.k8s != nil {
		replicas := int32(svc.Replicas)
		if replicas == 0 {
			replicas = 1
		}
		if err := appk8s.ScaleDeployment(ctx, s.k8s, slugify(svc.Name), svc.Project.Slug, replicas); err != nil {
			return nil, err
		}
	}
	if err := s.db.WithContext(ctx).Model(&svc).Update("status", db.ServiceRunning).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, serviceID)
}

func (s *WorkloadService) Stop(ctx context.Context, serviceID uuid.UUID) (*db.Service, error) {
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	if s.k8s != nil {
		if err := appk8s.ScaleDeployment(ctx, s.k8s, slugify(svc.Name), svc.Project.Slug, 0); err != nil {
			return nil, err
		}
	}
	if err := s.db.WithContext(ctx).Model(&svc).Update("status", db.ServiceStopped).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, serviceID)
}

func (s *WorkloadService) GetDatabaseConfig(ctx context.Context, serviceID uuid.UUID) (*db.DatabaseConfig, error) {
	var dc db.DatabaseConfig
	err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).First(&dc).Error
	return &dc, err
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
	Port          *int
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
	if in.Port != nil {
		updates["port"] = *in.Port
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
	GitIntegrationID      *uuid.UUID
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
	RollbackEnabled       *bool
	ImageRetention        *int
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
	if in.GitIntegrationID != nil {
		bc.GitIntegrationID = in.GitIntegrationID
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
	if in.RollbackEnabled != nil {
		bc.RollbackEnabled = *in.RollbackEnabled
	}
	if in.ImageRetention != nil {
		bc.ImageRetention = *in.ImageRetention
	}
	if isNew {
		err = s.db.WithContext(ctx).Create(&bc).Error
	} else {
		err = s.db.WithContext(ctx).Save(&bc).Error
	}
	return &bc, err
}
