package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

type WorkloadService struct {
	db        *gorm.DB
	k8s       kubernetes.Interface // nil when K8s is not configured
	varGroups *VariableGroupService
	// deployment re-applies a service's K8s Deployment from its current DB
	// config. Assigned after construction in service.New because the two
	// services reference each other.
	deployment *DeploymentService
}

// PortInput describes one port the caller wants to expose.
type PortInput struct {
	Name      string // e.g. "http", "grpc" — must be unique per service
	Port      int    // container port
	IsHTTP    bool   // speaks HTTP/1.1 (proxy-routable)
	IsPrimary bool   // exactly one per service should be true
	IsPublic  bool   // gets a K8s NodePort
}

type CreateWorkloadInput struct {
	Name    string
	Image   string
	NodeID  *uuid.UUID  // nil = let K3s schedule
	StackID *uuid.UUID  // nil = not part of a stack
	EnvVars string      // raw .env block, stored as EncryptedString
	Ports   []PortInput // at least one required; first is used as primary if none flagged

	// PullRegistryIntegrationID — credentials for pulling a private runtime image.
	// Set at create time for image-source services; nil = public image.
	PullRegistryIntegrationID *uuid.UUID

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
	err := s.db.WithContext(ctx).Preload("Ports").Where("project_id = ?", projectID).Find(&services).Error
	return services, err
}

// Get fetches a service scoped to its project — use this in handler layer.
func (s *WorkloadService) Get(ctx context.Context, serviceID, projectID uuid.UUID) (*db.Service, error) {
	var service db.Service
	err := s.db.WithContext(ctx).Preload("Ports").
		First(&service, "id = ? AND project_id = ?", serviceID, projectID).Error
	return &service, err
}

// getByID fetches a service by ID only — for internal use within the service layer.
func (s *WorkloadService) getByID(ctx context.Context, serviceID uuid.UUID) (*db.Service, error) {
	var service db.Service
	err := s.db.WithContext(ctx).Preload("Ports").First(&service, "id = ?", serviceID).Error
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
	// Quota is metered per org; resolve it from the project. Runs before the
	// database branch so both service kinds are metered.
	var proj db.Project
	if err := s.db.WithContext(ctx).Select("organization_id").First(&proj, "id = ?", projectID).Error; err != nil {
		return nil, err
	}
	if err := checkQuota(ctx, proj.OrganizationID, QuotaService); err != nil {
		return nil, err
	}

	if in.Type == db.ServiceTypeDatabase {
		return s.createDatabase(ctx, projectID, in)
	}

	replicas := in.Replicas
	if replicas == 0 {
		replicas = 1
	}

	// Default to a single HTTP port 3000 when the caller provides none.
	ports := in.Ports
	if len(ports) == 0 {
		ports = []PortInput{{Name: "http", Port: 3000, IsHTTP: true, IsPrimary: true, IsPublic: true}}
	}
	// Ensure exactly one primary is marked.
	hasPrimary := false
	for _, p := range ports {
		if p.IsPrimary {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary {
		ports[0].IsPrimary = true
	}

	service := &db.Service{
		ProjectID:                  projectID,
		NodeID:                     in.NodeID,
		StackID:                    in.StackID,
		Name:                       in.Name,
		Slug:                       s.allocateSlug(ctx, projectID, in.Name),
		Type:                       db.ServiceTypeApplication,
		Image:                      in.Image,
		PullRegistryIntegrationID:  in.PullRegistryIntegrationID,
		Status:                     db.ServiceStopped,
		Replicas:                   replicas,
		EnvVars:                    db.EncryptedString(in.EnvVars),
		HealthcheckCmd:             in.HealthcheckCmd,
		HealthcheckIntervalSecs:    in.HealthcheckIntervalSecs,
		HealthcheckTimeoutSecs:     in.HealthcheckTimeoutSecs,
		HealthcheckRetries:         in.HealthcheckRetries,
		HealthcheckStartPeriodSecs: in.HealthcheckStartPeriodSecs,
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(service).Error; err != nil {
			return err
		}
		for _, pi := range ports {
			sp := &db.ServicePort{
				ServiceID: service.ID,
				Name:      pi.Name,
				Port:      pi.Port,
				IsHTTP:    pi.IsHTTP,
				IsPrimary: pi.IsPrimary,
				IsPublic:  pi.IsPublic,
			}
			if err := tx.Create(sp).Error; err != nil {
				return err
			}
			service.Ports = append(service.Ports, *sp)
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
		deployToken, err := generateDeployToken()
		if err != nil {
			return err
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
			DeployToken:           db.EncryptedString(deployToken),
		}
		return tx.Create(bc).Error
	}); err != nil {
		return nil, err
	}
	// Create system-managed variable group (best-effort; don't fail the service creation)
	if s.varGroups != nil {
		var proj db.Project
		if err := s.db.WithContext(ctx).Select("slug").First(&proj, "id = ?", projectID).Error; err == nil {
			_ = s.varGroups.UpsertSystemGroup(ctx, service, proj.Slug)
		}
	}
	return service, nil
}

// dbSlug generates a stable K8s resource name: {slugify(name)}-{6 random hex chars}.
func dbSlug(name string) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	base := strings.ToLower(name)
	base = strings.NewReplacer(" ", "-", "_", "-").Replace(base)
	return base + "-" + hex.EncodeToString(b)
}

// allocateSlug returns the Kubernetes object name for a new application
// service: the plain slug when it is free in the project, and the slug plus a
// random suffix when it is not.
//
// The namespace is the project, so two services sharing a name resolve to one
// Deployment -- deploying the same template twice would have had the second
// silently overwrite the first, and adopt its volume. Suffixing only on
// collision keeps the ordinary single-instance case addressable as plain "zot"
// rather than making every workload carry a suffix nobody needs.
//
// The check is a read-then-write and so races with a concurrent create of the
// same name. Losing that race costs a suffix, not correctness: both rows get
// distinct slugs on the retry, and the caller sees no error.
func (s *WorkloadService) allocateSlug(ctx context.Context, projectID uuid.UUID, name string) string {
	base := slugify(name)
	if base == "" {
		base = "service"
	}
	for attempt := 0; attempt < 5; attempt++ {
		candidate := base
		if attempt > 0 {
			b := make([]byte, 3)
			_, _ = rand.Read(b)
			candidate = base + "-" + hex.EncodeToString(b)
		}
		if !s.slugTaken(ctx, projectID, candidate) {
			return candidate
		}
	}
	// Five collisions on a 24-bit suffix is not chance; fall through with one
	// more rather than looping forever.
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return base + "-" + hex.EncodeToString(b)
}

// slugTaken reports whether a project already uses a Kubernetes name, checking
// both the stored slug and the legacy fallback so a pre-slug service still
// blocks the name it actually occupies in the cluster.
func (s *WorkloadService) slugTaken(ctx context.Context, projectID uuid.UUID, candidate string) bool {
	var rows []db.Service
	if err := s.db.WithContext(ctx).
		Select("name", "slug").
		Where("project_id = ?", projectID).Find(&rows).Error; err != nil {
		// Unknown is not free: a suffix costs nothing, a collision costs a
		// running workload.
		return true
	}
	for i := range rows {
		existing := rows[i].Slug
		if existing == "" {
			existing = slugify(rows[i].Name)
		}
		if existing == candidate {
			return true
		}
	}
	return false
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
		// Carried through like every other field the caller sets. Dropping it
		// left a stack-created database owned by nothing: it did not appear in
		// its stack's service list, and destroy could not find it to remove.
		StackID:  in.StackID,
		NodeID:   in.NodeID,
		Name:     in.Name,
		Type:     db.ServiceTypeDatabase,
		Image:    image,
		Status:   db.ServiceStopped,
		Replicas: 1,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(service).Error; err != nil {
			return err
		}
		// Database gets a single internal-only port (no NodePort, no HTTP routing).
		sp := &db.ServicePort{
			ServiceID: service.ID,
			Name:      "db",
			Port:      port,
			IsHTTP:    false,
			IsPrimary: true,
			IsPublic:  false,
		}
		if err := tx.Create(sp).Error; err != nil {
			return err
		}
		service.Ports = []db.ServicePort{*sp}
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
	}); err != nil {
		return nil, err
	}
	if s.varGroups != nil {
		var proj db.Project
		if err := s.db.WithContext(ctx).Select("slug").First(&proj, "id = ?", projectID).Error; err == nil {
			_ = s.varGroups.UpsertSystemGroup(ctx, service, proj.Slug)
		}
	}
	return service, nil
}

func (s *WorkloadService) Start(ctx context.Context, serviceID uuid.UUID) (*db.Service, error) {
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	if svc.Image == "" {
		return nil, errors.New("service has never been deployed — trigger a deployment first")
	}
	// Replicas must be at least one for a start to mean anything.
	if svc.Replicas == 0 {
		if err := s.db.WithContext(ctx).Model(&svc).Update("replicas", 1).Error; err != nil {
			return nil, err
		}
		svc.Replicas = 1
	}
	if s.k8s != nil {
		// Re-apply the whole spec rather than scaling what is already in the
		// cluster. Scaling resurrects whatever K8s happens to hold, which may be
		// a Deployment left behind by a deleted service of the same name, or a
		// spec from before the image, env, node or volumes changed — starting a
		// service must not silently run stale configuration.
		//
		// ReapplyService only acts on a running service, so the status is written
		// first; a failure below rolls it back rather than leaving the row
		// claiming the service runs.
		if err := s.db.WithContext(ctx).Model(&svc).Update("status", db.ServiceRunning).Error; err != nil {
			return nil, err
		}
		if s.deployment == nil {
			return nil, errors.New("deployment service unavailable")
		}
		if err := s.deployment.ReapplyService(ctx, serviceID); err != nil {
			_ = s.db.WithContext(ctx).Model(&svc).Update("status", db.ServiceStopped).Error
			return nil, err
		}
	}
	if err := s.db.WithContext(ctx).Model(&svc).Update("status", db.ServiceRunning).Error; err != nil {
		return nil, err
	}
	return s.getByID(ctx, serviceID)
}

func (s *WorkloadService) Stop(ctx context.Context, serviceID uuid.UUID) (*db.Service, error) {
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	if s.k8s != nil {
		if err := appk8s.ScaleDeployment(ctx, s.k8s, s.k8sName(ctx, &svc), svc.Project.Slug, 0); err != nil {
			return nil, err
		}
	}
	if err := s.db.WithContext(ctx).Model(&svc).Update("status", db.ServiceStopped).Error; err != nil {
		return nil, err
	}
	return s.getByID(ctx, serviceID)
}

func (s *WorkloadService) GetDatabaseConfig(ctx context.Context, serviceID uuid.UUID) (*db.DatabaseConfig, error) {
	var dc db.DatabaseConfig
	err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).First(&dc).Error
	return &dc, err
}

// k8sName returns the Deployment name backing a service.
//
// A database's Deployment carries a random suffix — "umami-db" is deployed as
// "umami-db-5e9871" — so its name cannot be derived from the service name. Any
// cluster call that assumes it can silently addresses a workload that does not
// exist: stopping a database reported success while it kept serving, deleting
// one left its Deployment, Service and PVC orphaned, and the status reconciler
// read the absence as failure and marked every database failed within thirty
// seconds of creation.
//
// The preloaded config is used when present, and looked up otherwise, so this
// is correct regardless of how the caller loaded the service.
func (s *WorkloadService) k8sName(ctx context.Context, svc *db.Service) string {
	if svc.Type != db.ServiceTypeDatabase {
		// Stored at creation. Empty on rows that predate the column, which fall
		// back to the name they were already deployed under -- so nothing that
		// is running gets renamed and no backfill is needed.
		if svc.Slug != "" {
			return svc.Slug
		}
		return slugify(svc.Name)
	}
	if svc.DatabaseConfig != nil && svc.DatabaseConfig.Slug != "" {
		return svc.DatabaseConfig.Slug
	}
	var dc db.DatabaseConfig
	if err := s.db.WithContext(ctx).Where("service_id = ?", svc.ID).First(&dc).Error; err == nil && dc.Slug != "" {
		return dc.Slug
	}
	return slugify(svc.Name)
}

// GetK8sInfo returns the K8s namespace (= project slug) and deployment name
// (= app label) for a service, needed for pod listing and exec.
func (s *WorkloadService) GetK8sInfo(ctx context.Context, serviceID uuid.UUID) (namespace, k8sName string, err error) {
	var svc db.Service
	if err = s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", serviceID).Error; err != nil {
		return
	}
	namespace = svc.Project.Slug
	if svc.Type == db.ServiceTypeDatabase {
		var dc db.DatabaseConfig
		if err = s.db.WithContext(ctx).Where("service_id = ?", serviceID).First(&dc).Error; err != nil {
			return
		}
		k8sName = dc.Slug
	} else {
		k8sName = appK8sName(&svc)
	}
	return
}

func (s *WorkloadService) Delete(ctx context.Context, serviceID uuid.UUID) error {
	// Remove the cluster workload before the row. Deleting only the record
	// leaves the Deployment and Service running under the same name, and a later
	// service with that name silently adopts the orphan — inheriting its image,
	// env and volume claims.
	//
	// A failure here is returned rather than ignored: dropping the row while the
	// workload keeps running is the bug this prevents, so the caller should see
	// that the cluster could not be cleaned up.
	if s.k8s != nil {
		var svc db.Service
		if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", serviceID).Error; err != nil {
			return err
		}
		if err := appk8s.DeleteWorkload(ctx, s.k8s, s.k8sName(ctx, &svc), svc.Project.Slug); err != nil {
			return fmt.Errorf("remove workload from the cluster: %w", err)
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx.Where("resource_type = ? AND resource_id = ?", db.ResourceService, serviceID).
			Delete(&db.ResourcePermission{})
		return tx.Delete(&db.Service{}, "id = ?", serviceID).Error
	})
}

// ─── Update ───────────────────────────────────────────────────────────────────

type UpdateWorkloadInput struct {
	Name          *string
	Image         *string
	UpdateNode    bool // when true, NodeID is applied (nil = auto-schedule)
	NodeID        *uuid.UUID
	Replicas      *int
	CPURequest    *string
	CPULimit      *string
	MemoryRequest *string
	MemoryLimit   *string
	EnvVars       *string      // nil = no change
	Ports         *[]PortInput // nil = no change; replaces all ports when set

	// UpdatePullRegistry controls whether PullRegistryIntegrationID is written.
	// When true, PullRegistryIntegrationID is applied (nil = clear / public image).
	UpdatePullRegistry        bool
	PullRegistryIntegrationID *uuid.UUID
}

func (s *WorkloadService) Update(ctx context.Context, serviceID uuid.UUID, in UpdateWorkloadInput) (*db.Service, error) {
	// A rename changes the K8s object's name, and K8s names are immutable — the
	// old Deployment and Service cannot follow, they have to be replaced.
	//
	// Nothing did that before: the row was renamed and the cluster was left
	// alone, so the workload kept running under the old name owned by nothing,
	// and the next deploy created a second one under the new name. That is how a
	// deployment ends up running for months with no service behind it.
	//
	// Only applications are affected. A database's K8s name is its stored slug,
	// which is deliberately stable across renames.
	var before db.Service
	if err := s.db.WithContext(ctx).Preload("Project").First(&before, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	// A rename only moves the cluster workload for services with no stored slug
	// -- rows from before the slug existed, whose Kubernetes name is still
	// derived from their display name.
	//
	// Once a slug is stored the two are independent: renaming changes what the
	// UI shows and nothing in the cluster, so there is no old Deployment to
	// clean up. That removes the delete-and-recreate a rename used to trigger,
	// and with it the orphan left behind whenever it failed partway.
	renamedFrom := ""
	if in.Name != nil && before.Type != db.ServiceTypeDatabase && before.Slug == "" {
		if old, want := slugify(before.Name), slugify(*in.Name); old != want {
			renamedFrom = old
		}
	}

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
	if in.UpdatePullRegistry {
		updates["pull_registry_integration_id"] = in.PullRegistryIntegrationID // nil → NULL, uuid → set
	}

	if in.Ports != nil {
		// Replace all ports in a transaction
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("service_id = ?", serviceID).Delete(&db.ServicePort{}).Error; err != nil {
				return err
			}
			hasPrimary := false
			for _, p := range *in.Ports {
				if p.IsPrimary {
					hasPrimary = true
				}
			}
			for i, p := range *in.Ports {
				primary := p.IsPrimary
				if !hasPrimary && i == 0 {
					primary = true
				}
				sp := db.ServicePort{
					ServiceID: serviceID,
					Name:      p.Name,
					Port:      p.Port,
					IsHTTP:    p.IsHTTP,
					IsPrimary: primary,
					IsPublic:  p.IsPublic,
				}
				if err := tx.Create(&sp).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&db.Service{}).Where("id = ?", serviceID).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	// A target-node change has to reach the running workload, not just the row.
	// Previously the value was saved and silently ignored until the next deploy,
	// so the UI reported a placement the cluster was not honouring.
	if in.UpdateNode {
		if err := s.applyNodePin(ctx, serviceID, in.NodeID); err != nil {
			return nil, err
		}
	}

	// Replace the workload under its new name: remove the old objects, then
	// re-apply from the freshly renamed row. Order matters — the old Deployment
	// goes first so the two never coexist, which is what produced a duplicate.
	//
	// Best-effort by design: the rename itself has already been committed, and
	// failing the whole call would leave the caller unsure which half applied.
	// The orphan report surfaces anything left behind.
	if renamedFrom != "" && s.k8s != nil && before.Project.Slug != "" {
		if err := appk8s.DeleteWorkload(ctx, s.k8s, renamedFrom, before.Project.Slug); err != nil {
			log.Printf("warning: rename %s: remove old workload: %v", renamedFrom, err)
		}
		if s.deployment != nil {
			if err := s.deployment.ReapplyService(ctx, serviceID); err != nil {
				log.Printf("warning: rename %s: re-apply under the new name: %v", renamedFrom, err)
			}
		}
	}
	return s.getByID(ctx, serviceID)
}

// applyNodePin pushes the service's target node onto its live Deployment.
//
// It refuses a move that the storage cannot follow: a service holding a bound
// node-local volume can only run where that volume lives, and pinning it
// elsewhere would leave the pod Pending forever with the reason buried in
// kubectl describe.
func (s *WorkloadService) applyNodePin(ctx context.Context, serviceID uuid.UUID, nodeID *uuid.UUID) error {
	if s.k8s == nil {
		return nil
	}
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", serviceID).Error; err != nil {
		return err
	}

	nodeName := ""
	if nodeID != nil {
		var node db.Node
		if err := s.db.WithContext(ctx).First(&node, "id = ?", *nodeID).Error; err != nil {
			return fmt.Errorf("target node not found")
		}
		nodeName = node.Name
	}

	// Guard: bound volumes anchor the workload to their own node.
	var mounts []db.VolumeMount
	if err := s.db.WithContext(ctx).Preload("Volume").
		Where("service_id = ?", serviceID).Find(&mounts).Error; err == nil {
		for _, m := range mounts {
			if m.Volume.Slug == "" {
				continue
			}
			st, err := appk8s.GetVolumePVCStatus(ctx, s.k8s, m.Volume.Slug, svc.Project.Slug)
			if err != nil || !st.Bound || st.Node == "" {
				continue
			}
			if nodeName == "" {
				// Auto-schedule with a bound volume still resolves to the volume's
				// node; pin explicitly so the intent is visible in the cluster.
				nodeName = st.Node
				continue
			}
			if st.Node != nodeName {
				return fmt.Errorf(
					"volume %q is provisioned on %q — the service cannot run on %q; move it back or recreate the volume",
					m.Volume.Name, st.Node, nodeName)
			}
		}
	}

	return appk8s.SetDeploymentNode(ctx, s.k8s, s.k8sName(ctx, &svc), svc.Project.Slug, nodeName)
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
	RootDir               *string // subdirectory within the repo to build from (e.g. "frontend")
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
	AutoDeploy            *bool
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
	if in.RootDir != nil {
		bc.RootDir = *in.RootDir
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
	if in.AutoDeploy != nil {
		bc.AutoDeploy = *in.AutoDeploy
	}
	if isNew {
		// Generate a per-service deploy token so the user can set up a manual webhook
		// without needing a GitHub App integration.
		deployToken, err := generateDeployToken()
		if err != nil {
			return nil, err
		}
		bc.DeployToken = db.EncryptedString(deployToken)
		err = s.db.WithContext(ctx).Create(&bc).Error
	} else {
		err = s.db.WithContext(ctx).Save(&bc).Error
	}
	return &bc, err
}

// RegenerateDeployToken creates a new deploy token for the build config of the
// given service, replacing any existing token.
func (s *WorkloadService) RegenerateDeployToken(ctx context.Context, serviceID uuid.UUID) (string, error) {
	var bc db.BuildConfig
	if err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).First(&bc).Error; err != nil {
		return "", err
	}
	token, err := generateDeployToken()
	if err != nil {
		return "", err
	}
	if err := s.db.WithContext(ctx).Model(&bc).Update("deploy_token", db.EncryptedString(token)).Error; err != nil {
		return "", err
	}
	return token, nil
}

// generateDeployToken returns a cryptographically random 32-byte hex string
// used as a per-service webhook deploy token.
func generateDeployToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate deploy token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// toPortSpecs converts loaded ServicePort rows into k8s PortSpec values,
// preserving any already-assigned NodePorts so they survive re-deployments.
func toPortSpecs(ports []db.ServicePort) []appk8s.PortSpec {
	specs := make([]appk8s.PortSpec, len(ports))
	for i, p := range ports {
		specs[i] = appk8s.PortSpec{
			Name:     p.Name,
			Port:     int32(p.Port),
			IsPublic: p.IsPublic,
			NodePort: int32(p.NodePort),
		}
	}
	return specs
}

// primaryPort returns the container port of the primary ServicePort, or 3000.
func primaryPort(ports []db.ServicePort) int32 {
	for _, p := range ports {
		if p.IsPrimary {
			return int32(p.Port)
		}
	}
	if len(ports) > 0 {
		return int32(ports[0].Port)
	}
	return 3000
}
