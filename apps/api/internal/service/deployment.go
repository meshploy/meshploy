package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/config"
	appk8s "github.com/meshploy/apps/api/internal/k8s"
	db "github.com/meshploy/packages/db"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

type DeploymentService struct {
	db  *gorm.DB
	cfg *config.Config
	k8s kubernetes.Interface // nil when K8s is not configured
	git *GitIntegrationService
}

// ─── Read ─────────────────────────────────────────────────────────────────────

func (s *DeploymentService) List(ctx context.Context, serviceID uuid.UUID) ([]db.Deployment, error) {
	deployments := make([]db.Deployment, 0)
	err := s.db.WithContext(ctx).
		Where("service_id = ?", serviceID).
		Order("created_at DESC").
		Find(&deployments).Error
	return deployments, err
}

func (s *DeploymentService) Get(ctx context.Context, deploymentID uuid.UUID) (*db.Deployment, error) {
	var d db.Deployment
	err := s.db.WithContext(ctx).First(&d, "id = ?", deploymentID).Error
	return &d, err
}

// ─── Trigger ──────────────────────────────────────────────────────────────────

// TriggerInput carries everything the caller must supply.
type TriggerInput struct {
	ServiceID uuid.UUID
	// TriggeredBy is the user ID (stored in deployment log).
	TriggeredBy uuid.UUID
}

// Trigger creates a Deployment record, then fires off the build+deploy pipeline
// in a background goroutine. Returns the pending Deployment immediately.
//
// Returns an error if K8s is not configured or the service is missing a
// BuildConfig. The goroutine handles all subsequent status updates.
func (s *DeploymentService) Trigger(ctx context.Context, in TriggerInput) (*db.Deployment, error) {
	if s.k8s == nil {
		return nil, fmt.Errorf("kubernetes is not configured on this instance (set KUBECONFIG)")
	}

	// Load the service + its project (for namespace) + build config.
	var svc db.Service
	if err := s.db.WithContext(ctx).
		Preload("Project").
		First(&svc, "id = ?", in.ServiceID).Error; err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	if svc.Type != db.ServiceTypeApplication {
		return nil, fmt.Errorf("only application services can be deployed via build pipeline")
	}

	var bc db.BuildConfig
	if err := s.db.WithContext(ctx).
		Where("service_id = ?", in.ServiceID).
		Preload("RegistryIntegration").
		First(&bc).Error; err != nil {
		return nil, fmt.Errorf("build config not found: configure a git source first")
	}

	if bc.GitRepo == "" {
		return nil, fmt.Errorf("build config has no git repository configured")
	}

	// Load git integration for the org (need installation token).
	var gitIntegration db.GitIntegration
	if err := s.db.WithContext(ctx).
		Joins("JOIN projects ON projects.id = ?", svc.ProjectID).
		Where("git_integrations.organization_id = projects.organization_id AND git_integrations.provider = 'github'").
		First(&gitIntegration).Error; err != nil {
		return nil, fmt.Errorf("no GitHub integration found — connect one in Integrations")
	}

	// Resolve registry credentials.
	registryHost, registryUser, registryPass, err := s.resolveRegistry(ctx, &bc)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}

	// Derive image name: registry/project-service:deploymentid[:8]
	deploymentID := uuid.New()
	tag := deploymentID.String()[:8]
	imageName := fmt.Sprintf("%s/%s-%s:%s",
		strings.TrimSuffix(registryHost, "/"),
		svc.Project.Slug,
		slugify(svc.Name),
		tag,
	)

	// Job name: build-{service-slug}-{tag}
	jobName := fmt.Sprintf("build-%s-%s", slugify(svc.Name), tag)
	namespace := svc.Project.Slug

	deployment := db.Deployment{
		Base:         db.Base{ID: deploymentID},
		ServiceID:    in.ServiceID,
		Status:       db.DeploymentBuilding,
		Image:        imageName,
		BuildJobName: jobName,
		Log:          fmt.Sprintf("Build triggered by user %s\n", in.TriggeredBy),
	}
	if err := s.db.WithContext(ctx).Create(&deployment).Error; err != nil {
		return nil, fmt.Errorf("create deployment record: %w", err)
	}

	// Get a short-lived GitHub installation token.
	appCfg, err := s.git.GetAppConfig(ctx)
	if err != nil || appCfg == nil {
		s.failDeployment(deployment.ID, "GitHub App not configured")
		return &deployment, nil
	}
	gitToken, err := getInstallationToken(appCfg.AppID, string(appCfg.PrivateKey), string(gitIntegration.InstallationID))
	if err != nil {
		s.failDeployment(deployment.ID, "failed to get GitHub token: "+err.Error())
		return &deployment, nil
	}

	// Launch the pipeline in the background.
	go s.runPipeline(context.Background(), runPipelineArgs{
		deployment:    deployment,
		svc:           svc,
		bc:            bc,
		namespace:     namespace,
		jobName:       jobName,
		imageName:     imageName,
		gitToken:      gitToken,
		registryHost:  registryHost,
		registryUser:  registryUser,
		registryPass:  registryPass,
	})

	return &deployment, nil
}

// ─── Pipeline goroutine ───────────────────────────────────────────────────────

type runPipelineArgs struct {
	deployment   db.Deployment
	svc          db.Service
	bc           db.BuildConfig
	namespace    string
	jobName      string
	imageName    string
	gitToken     string
	registryHost string
	registryUser string
	registryPass string
}

func (s *DeploymentService) runPipeline(ctx context.Context, a runPipelineArgs) {
	builderImage := s.cfg.BuilderImage
	if builderImage == "" {
		builderImage = appk8s.BuilderImage
	}

	// Ensure the project namespace exists.
	if err := appk8s.EnsureNamespace(ctx, s.k8s, a.namespace); err != nil {
		s.failDeployment(a.deployment.ID, "failed to ensure namespace: "+err.Error())
		return
	}

	// Create the build Job.
	err := appk8s.CreateBuildJob(ctx, s.k8s, appk8s.BuildJobParams{
		JobName:      a.jobName,
		Namespace:    a.namespace,
		GitRepo:      a.bc.GitRepo,
		GitBranch:    a.bc.Branch,
		GitToken:     a.gitToken,
		Builder:      string(a.bc.Builder),
		ImageDest:    a.imageName,
		RegistryHost: a.registryHost,
		RegistryUser: a.registryUser,
		RegistryPass: a.registryPass,
	})
	if err != nil {
		s.failDeployment(a.deployment.ID, "failed to create build job: "+err.Error())
		return
	}

	s.setStatus(a.deployment.ID, db.DeploymentBuilding, "Build job created: "+a.jobName)

	// Wait for the job to finish (up to 30 minutes).
	result := appk8s.WaitForJob(ctx, s.k8s, a.namespace, a.jobName, 30*time.Minute)

	if !result.Success {
		s.failDeployment(a.deployment.ID, result.Log)
		return
	}

	s.setStatus(a.deployment.ID, db.DeploymentDeploying, result.Log)

	// Determine the container port (default 8080 if not set).
	port := int32(8080)

	// Resolve node name if a specific node is pinned.
	nodeName := ""
	if a.svc.NodeID != nil {
		var node db.Node
		if err := s.db.WithContext(ctx).First(&node, a.svc.NodeID).Error; err == nil {
			nodeName = node.Name
		}
	}

	// Apply K8s Deployment + Service.
	wp := appk8s.WorkloadParams{
		Name:          slugify(a.svc.Name),
		Namespace:     a.namespace,
		Image:         a.imageName,
		Port:          port,
		Replicas:      int32(a.svc.Replicas),
		CPURequest:    a.svc.CPURequest,
		CPULimit:      a.svc.CPULimit,
		MemoryRequest: a.svc.MemoryRequest,
		MemoryLimit:   a.svc.MemoryLimit,
		NodeName:      nodeName,
	}
	if err := appk8s.ApplyDeployment(ctx, s.k8s, wp); err != nil {
		s.failDeployment(a.deployment.ID, "failed to apply K8s deployment: "+err.Error())
		return
	}
	if err := appk8s.ApplyService(ctx, s.k8s, slugify(a.svc.Name), a.namespace, port); err != nil {
		s.failDeployment(a.deployment.ID, "failed to apply K8s service: "+err.Error())
		return
	}

	// Update service image + status.
	now := time.Now()
	s.db.Model(&db.Deployment{}).Where("id = ?", a.deployment.ID).Updates(map[string]any{
		"status":      db.DeploymentRunning,
		"log":         result.Log + "\nDeployment applied successfully.",
		"deployed_at": &now,
	})
	s.db.Model(&db.Service{}).Where("id = ?", a.svc.ID).Updates(map[string]any{
		"image":  a.imageName,
		"status": db.ServiceRunning,
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (s *DeploymentService) setStatus(id uuid.UUID, status db.DeploymentStatus, log string) {
	s.db.Model(&db.Deployment{}).Where("id = ?", id).Updates(map[string]any{
		"status": status,
		"log":    log,
	})
}

func (s *DeploymentService) failDeployment(id uuid.UUID, reason string) {
	s.db.Model(&db.Deployment{}).Where("id = ?", id).Updates(map[string]any{
		"status": db.DeploymentFailed,
		"log":    reason,
	})
	// Also mark the service as stopped.
	s.db.Model(&db.Service{}).
		Joins("JOIN deployments ON deployments.service_id = services.id").
		Where("deployments.id = ?", id).
		Update("status", db.ServiceStopped)
}

// resolveRegistry returns registry credentials from the build config or
// returns an error if no registry is configured.
func (s *DeploymentService) resolveRegistry(ctx context.Context, bc *db.BuildConfig) (host, user, pass string, err error) {
	if bc.RegistryIntegrationID == nil {
		return "", "", "", fmt.Errorf("no container registry configured — add one in Integrations and link it to this service")
	}
	var reg db.RegistryIntegration
	if err := s.db.WithContext(ctx).First(&reg, bc.RegistryIntegrationID).Error; err != nil {
		return "", "", "", fmt.Errorf("registry integration not found")
	}
	return reg.Endpoint, string(reg.Username), string(reg.Password), nil
}

// slugify converts a name to a K8s-safe lowercase slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}
