package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/config"
	appk8s "github.com/meshploy/apps/api/internal/k8s"
	db "github.com/meshploy/packages/db"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type DeploymentService struct {
	db        *gorm.DB
	cfg       *config.Config
	k8s       kubernetes.Interface // nil when K8s is not configured
	git       *GitIntegrationService
	varGroups *VariableGroupService
	notif     *NotificationService
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
		Preload("Ports").
		First(&svc, "id = ?", in.ServiceID).Error; err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	if svc.Type == db.ServiceTypeDatabase {
		return s.provisionDatabase(ctx, &svc)
	}
	if svc.Type != db.ServiceTypeApplication {
		return nil, fmt.Errorf("only application services can be deployed via build pipeline")
	}

	// Try to load the build config. Services with a pre-built image have no build config.
	var bc db.BuildConfig
	hasBc := s.db.WithContext(ctx).
		Where("service_id = ?", in.ServiceID).
		Preload("RegistryIntegration").
		First(&bc).Error == nil

	if !hasBc || bc.GitRepo == "" {
		// No git source — deploy the pre-configured image directly (no build step).
		if svc.Image == "" {
			return nil, fmt.Errorf("no image configured and no git source — set an image or configure a git source")
		}
		return s.triggerDirectDeploy(ctx, &svc, in.TriggeredBy)
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

	// Resolve git token. Public repos (no GitIntegrationID) use an empty token.
	gitToken := ""
	if bc.GitIntegrationID != nil {
		var gitIntegration db.GitIntegration
		if err := s.db.WithContext(ctx).First(&gitIntegration, "id = ?", bc.GitIntegrationID).Error; err != nil {
			s.failDeployment(deployment.ID, "git integration not found — it may have been deleted")
			return &deployment, nil
		}
		if gitIntegration.GHAppID == "" || string(gitIntegration.InstallationID) == "" {
			s.failDeployment(deployment.ID, "GitHub App not fully configured — complete setup and installation in Integrations")
			return &deployment, nil
		}
		tok, err := getInstallationToken(gitIntegration.GHAppID, string(gitIntegration.GHPrivateKey), string(gitIntegration.InstallationID))
		if err != nil {
			s.failDeployment(deployment.ID, "failed to get GitHub token: "+err.Error())
			return &deployment, nil
		}
		gitToken = tok
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

// triggerDirectDeploy deploys a service that already has a pre-configured image
// (no build step). Handles both public images and private images with a
// PullRegistryIntegrationID set.
func (s *DeploymentService) triggerDirectDeploy(ctx context.Context, svc *db.Service, triggeredBy uuid.UUID) (*db.Deployment, error) {
	if s.k8s == nil {
		return nil, fmt.Errorf("kubernetes is not configured on this instance (set KUBECONFIG)")
	}
	deploymentID := uuid.New()
	deployment := db.Deployment{
		Base:      db.Base{ID: deploymentID},
		ServiceID: svc.ID,
		Status:    db.DeploymentDeploying,
		Image:     svc.Image,
		Log:       fmt.Sprintf("Direct image deploy triggered by user %s\n", triggeredBy),
	}
	if err := s.db.WithContext(ctx).Create(&deployment).Error; err != nil {
		return nil, fmt.Errorf("create deployment record: %w", err)
	}

	go func() {
		bgCtx := context.Background()
		namespace := svc.Project.Slug

		if err := appk8s.EnsureNamespace(bgCtx, s.k8s, namespace); err != nil {
			s.failDeployment(deploymentID, "failed to ensure namespace: "+err.Error())
			return
		}

		// Resolve imagePullSecret for private images.
		pullSecretName := ""
		if svc.PullRegistryIntegrationID != nil {
			var reg db.RegistryIntegration
			if err := s.db.WithContext(bgCtx).First(&reg, svc.PullRegistryIntegrationID).Error; err != nil {
				s.failDeployment(deploymentID, "pull registry integration not found")
				return
			}
			pullSecretName = "meshploy-pull-" + svc.PullRegistryIntegrationID.String()[:8]
			if err := appk8s.EnsureRegistryPullSecret(bgCtx, s.k8s, namespace, pullSecretName, reg.Endpoint, string(reg.Username), string(reg.Password)); err != nil {
				s.failDeployment(deploymentID, "failed to create pull secret: "+err.Error())
				return
			}
		}

		var svcPorts []db.ServicePort
		s.db.WithContext(bgCtx).Where("service_id = ?", svc.ID).Find(&svcPorts)
		portSpecs := toPortSpecs(svcPorts)
		port := primaryPort(svcPorts)

		nodeName := ""
		if svc.NodeID != nil {
			var node db.Node
			if s.db.WithContext(bgCtx).First(&node, svc.NodeID).Error == nil {
				nodeName = node.Name
			}
		}

		groupEnvs, _ := s.varGroups.CollectEnvVars(bgCtx, svc.ID)
		envVars := mergeSecretEnvs(runtimeEnvVars(string(svc.EnvVars), port), groupEnvs)
		volMounts := resolveServiceVolumeMounts(bgCtx, s.db, svc.ID)
		probe := buildProbeFromService(svc)

		wp := appk8s.WorkloadParams{
			Name:                slugify(svc.Name),
			Namespace:           namespace,
			Image:               svc.Image,
			Ports:               portSpecs,
			Replicas:            int32(svc.Replicas),
			Env:                 envVars,
			CPURequest:          svc.CPURequest,
			CPULimit:            svc.CPULimit,
			MemoryRequest:       svc.MemoryRequest,
			MemoryLimit:         svc.MemoryLimit,
			NodeName:            nodeName,
			VolumeMounts:        volMounts,
			LivenessProbe:       probe,
			ReadinessProbe:      probe,
			ImagePullSecretName: pullSecretName,
		}
		if err := appk8s.ApplyDeployment(bgCtx, s.k8s, wp); err != nil {
			s.failDeployment(deploymentID, "failed to apply K8s deployment: "+err.Error())
			return
		}
		if err := appk8s.ApplyService(bgCtx, s.k8s, slugify(svc.Name), namespace, portSpecs); err != nil {
			s.failDeployment(deploymentID, "failed to apply K8s service: "+err.Error())
			return
		}
		assignedNPs, err := appk8s.ApplyNodePortService(bgCtx, s.k8s, slugify(svc.Name), namespace, portSpecs)
		if err != nil {
			s.failDeployment(deploymentID, "failed to apply K8s NodePort service: "+err.Error())
			return
		}
		for _, sp := range svcPorts {
			if np, ok := assignedNPs[sp.Name]; ok && np != 0 {
				s.db.Model(&db.ServicePort{}).Where("id = ?", sp.ID).Update("node_port", np)
			}
		}

		now := time.Now()
		s.db.Model(&db.Deployment{}).Where("id = ?", deploymentID).Updates(map[string]any{
			"status":      db.DeploymentSuccess,
			"log":         deployment.Log + "Deployment applied successfully.",
			"deployed_at": &now,
		})
		s.db.Model(&db.Service{}).Where("id = ?", svc.ID).Updates(map[string]any{
			"status": db.ServiceRunning,
		})

		if s.varGroups != nil {
			var freshSvc db.Service
			if err := s.db.WithContext(bgCtx).Preload("Ports").First(&freshSvc, "id = ?", svc.ID).Error; err == nil {
				_ = s.varGroups.UpsertSystemGroup(bgCtx, &freshSvc, namespace)
			}
		}
	}()

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

	// Ensure the per-namespace build cache PVC exists (created once, reused
	// across all builds so nix/npm layers are not re-downloaded every time).
	if err := appk8s.EnsureBuildCachePVC(ctx, s.k8s, a.namespace); err != nil {
		s.failDeployment(a.deployment.ID, "failed to ensure build cache PVC: "+err.Error())
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
		BuildEnvVars:  string(a.bc.BuildEnvVars),
		BuilderNode:   a.bc.BuilderNode,
		CPURequest:    a.bc.BuilderCPURequest,
		MemoryRequest: a.bc.BuilderMemoryRequest,
	})
	if err != nil {
		s.failDeployment(a.deployment.ID, "failed to create build job: "+err.Error())
		return
	}

	s.setStatus(a.deployment.ID, db.DeploymentBuilding, "Build job created: "+a.jobName)

	// Wait for the job to finish (up to 60 minutes).
	// First-time builds without layer cache (railpack native snapshotter, large
	// repos) can easily exceed 30 minutes.
	result := appk8s.WaitForJob(ctx, s.k8s, a.namespace, a.jobName, 60*time.Minute)

	if !result.Success {
		s.failDeployment(a.deployment.ID, result.Log)
		return
	}

	s.setStatus(a.deployment.ID, db.DeploymentDeploying, result.Log)

	// Load ports fresh (may differ from snapshot in runPipelineArgs).
	var svcPorts []db.ServicePort
	s.db.WithContext(ctx).Where("service_id = ?", a.svc.ID).Find(&svcPorts)
	portSpecs := toPortSpecs(svcPorts)
	port := primaryPort(svcPorts)

	// Resolve node name if a specific node is pinned.
	nodeName := ""
	if a.svc.NodeID != nil {
		var node db.Node
		if err := s.db.WithContext(ctx).First(&node, a.svc.NodeID).Error; err == nil {
			nodeName = node.Name
		}
	}

	// Merge variable group items into env vars (explicit env wins on conflict).
	groupEnvs, _ := s.varGroups.CollectEnvVars(ctx, a.svc.ID)
	envVars := mergeSecretEnvs(runtimeEnvVars(string(a.svc.EnvVars), port), groupEnvs)

	// Ensure imagePullSecret when the built image is in a private registry.
	pullSecretName := ""
	if a.registryHost != "" && a.registryUser != "" {
		pullSecretName = "meshploy-reg-" + slugify(a.svc.Name)
		if err := appk8s.EnsureRegistryPullSecret(ctx, s.k8s, a.namespace, pullSecretName, a.registryHost, a.registryUser, a.registryPass); err != nil {
			s.failDeployment(a.deployment.ID, "failed to create pull secret: "+err.Error())
			return
		}
	}

	// Apply K8s Deployment + Service.
	probe := buildProbeFromService(&a.svc)
	volMounts := resolveServiceVolumeMounts(ctx, s.db, a.svc.ID)
	wp := appk8s.WorkloadParams{
		Name:                slugify(a.svc.Name),
		Namespace:           a.namespace,
		Image:               a.imageName,
		Ports:               portSpecs,
		Replicas:            int32(a.svc.Replicas),
		Env:                 envVars,
		CPURequest:          a.svc.CPURequest,
		CPULimit:            a.svc.CPULimit,
		MemoryRequest:       a.svc.MemoryRequest,
		MemoryLimit:         a.svc.MemoryLimit,
		NodeName:            nodeName,
		VolumeMounts:        volMounts,
		LivenessProbe:       probe,
		ReadinessProbe:      probe,
		ImagePullSecretName: pullSecretName,
	}
	if err := appk8s.ApplyDeployment(ctx, s.k8s, wp); err != nil {
		s.failDeployment(a.deployment.ID, "failed to apply K8s deployment: "+err.Error())
		return
	}
	if err := appk8s.ApplyService(ctx, s.k8s, slugify(a.svc.Name), a.namespace, portSpecs); err != nil {
		s.failDeployment(a.deployment.ID, "failed to apply K8s service: "+err.Error())
		return
	}
	assignedNPs, err := appk8s.ApplyNodePortService(ctx, s.k8s, slugify(a.svc.Name), a.namespace, portSpecs)
	if err != nil {
		s.failDeployment(a.deployment.ID, "failed to apply K8s NodePort service: "+err.Error())
		return
	}

	// Persist assigned NodePorts back to service_ports rows.
	for _, sp := range svcPorts {
		if np, ok := assignedNPs[sp.Name]; ok && np != 0 {
			s.db.Model(&db.ServicePort{}).Where("id = ?", sp.ID).Update("node_port", np)
		}
	}

	// Update service image and status.
	now := time.Now()
	s.db.Model(&db.Deployment{}).Where("id = ?", a.deployment.ID).Updates(map[string]any{
		"status":      db.DeploymentSuccess,
		"log":         result.Log + "\nDeployment applied successfully.",
		"deployed_at": &now,
	})
	s.db.Model(&db.Service{}).Where("id = ?", a.svc.ID).Updates(map[string]any{
		"image":  a.imageName,
		"status": db.ServiceRunning,
	})

	// Refresh system-managed variable group with updated NodePorts.
	if s.varGroups != nil {
		var freshSvc db.Service
		if err := s.db.WithContext(ctx).Preload("Ports").First(&freshSvc, "id = ?", a.svc.ID).Error; err == nil {
			_ = s.varGroups.UpsertSystemGroup(ctx, &freshSvc, a.namespace)
		}
	}

	if s.notif != nil {
		var proj db.Project
		if s.db.WithContext(ctx).First(&proj, "id = ?", a.svc.ProjectID).Error == nil {
			s.notif.Dispatch(ctx, proj.OrganizationID, "deploy.success", NotificationData{
				ServiceName: a.svc.Name,
				ProjectName: proj.Name,
			})
		}
	}

	// Prune old images from the registry (best-effort, non-blocking).
	go s.pruneOldImages(context.Background(), a.svc.ID, a.bc)
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
	if s.notif != nil {
		var dep db.Deployment
		if s.db.Preload("Service.Project").First(&dep, "id = ?", id).Error == nil {
			s.notif.Dispatch(context.Background(), dep.Service.Project.OrganizationID, "deploy.failed", NotificationData{
				ServiceName: dep.Service.Name,
				ProjectName: dep.Service.Project.Name,
			})
		}
	}
}

// ReapplyService re-applies the K8s Deployment for a running service using its
// current DB config (image, env, resources, volume mounts, probes). Called by
// VolumeService after attach/detach so changes take effect without a new build.
func (s *DeploymentService) ReapplyService(ctx context.Context, serviceID uuid.UUID) error {
	if s.k8s == nil {
		return nil
	}
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Project").Preload("Ports").First(&svc, "id = ?", serviceID).Error; err != nil {
		return err
	}
	if svc.Status != db.ServiceRunning || svc.Image == "" {
		return nil
	}
	portSpecs := toPortSpecs(svc.Ports)
	port := primaryPort(svc.Ports)
	nodeName := ""
	if svc.NodeID != nil {
		var node db.Node
		if s.db.WithContext(ctx).First(&node, svc.NodeID).Error == nil {
			nodeName = node.Name
		}
	}
	groupEnvs, _ := s.varGroups.CollectEnvVars(ctx, svc.ID)
	envVars := mergeSecretEnvs(runtimeEnvVars(string(svc.EnvVars), port), groupEnvs)
	volMounts := resolveServiceVolumeMounts(ctx, s.db, serviceID)
	probe := buildProbeFromService(&svc)

	pullSecretName := ""
	if svc.PullRegistryIntegrationID != nil {
		var reg db.RegistryIntegration
		if s.db.WithContext(ctx).First(&reg, svc.PullRegistryIntegrationID).Error == nil {
			pullSecretName = "meshploy-pull-" + svc.PullRegistryIntegrationID.String()[:8]
			_ = appk8s.EnsureRegistryPullSecret(ctx, s.k8s, svc.Project.Slug, pullSecretName, reg.Endpoint, string(reg.Username), string(reg.Password))
		}
	}

	return appk8s.ApplyDeployment(ctx, s.k8s, appk8s.WorkloadParams{
		Name:                slugify(svc.Name),
		Namespace:           svc.Project.Slug,
		Image:               svc.Image,
		Ports:               portSpecs,
		Replicas:            int32(svc.Replicas),
		Env:                 envVars,
		CPURequest:          svc.CPURequest,
		CPULimit:            svc.CPULimit,
		MemoryRequest:       svc.MemoryRequest,
		MemoryLimit:         svc.MemoryLimit,
		NodeName:            nodeName,
		VolumeMounts:        volMounts,
		LivenessProbe:       probe,
		ReadinessProbe:      probe,
		ImagePullSecretName: pullSecretName,
	})
}

// buildProbeFromService constructs a K8s exec probe from the healthcheck fields
// stored on the service row. Returns nil when no healthcheck is configured.
func buildProbeFromService(svc *db.Service) *corev1.Probe {
	if svc.HealthcheckCmd == "" {
		return nil
	}
	var cmd []string
	if err := json.Unmarshal([]byte(svc.HealthcheckCmd), &cmd); err != nil || len(cmd) == 0 {
		return nil
	}
	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{Command: cmd},
		},
	}
	if svc.HealthcheckIntervalSecs > 0 {
		probe.PeriodSeconds = svc.HealthcheckIntervalSecs
	}
	if svc.HealthcheckTimeoutSecs > 0 {
		probe.TimeoutSeconds = svc.HealthcheckTimeoutSecs
	}
	if svc.HealthcheckRetries > 0 {
		probe.FailureThreshold = svc.HealthcheckRetries
	}
	if svc.HealthcheckStartPeriodSecs > 0 {
		probe.InitialDelaySeconds = svc.HealthcheckStartPeriodSecs
	}
	return probe
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

// Cancel kills an active build job and marks the deployment as failed.
func (s *DeploymentService) Cancel(ctx context.Context, deploymentID uuid.UUID) error {
	var d db.Deployment
	if err := s.db.WithContext(ctx).Preload("Service.Project").First(&d, "id = ?", deploymentID).Error; err != nil {
		return err
	}
	if d.Status != db.DeploymentBuilding && d.Status != db.DeploymentDeploying && d.Status != db.DeploymentPending {
		return fmt.Errorf("deployment is not active")
	}
	// Delete the K8s Job — this terminates the build pod immediately.
	if s.k8s != nil && d.BuildJobName != "" {
		var svc db.Service
		if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", d.ServiceID).Error; err == nil {
			fg := metav1.DeletePropagationForeground
			_ = s.k8s.BatchV1().Jobs(svc.Project.Slug).Delete(ctx, d.BuildJobName, metav1.DeleteOptions{
				PropagationPolicy: &fg,
			})
		}
	}
	return s.db.WithContext(ctx).Model(&db.Deployment{}).Where("id = ?", deploymentID).Updates(map[string]any{
		"status": db.DeploymentFailed,
		"log":    "Cancelled by user.",
	}).Error
}

// DeleteRecord removes a deployment record from the database.
// Returns an error if the deployment is still active (cancel it first).
func (s *DeploymentService) DeleteRecord(ctx context.Context, deploymentID uuid.UUID) error {
	var d db.Deployment
	if err := s.db.WithContext(ctx).First(&d, "id = ?", deploymentID).Error; err != nil {
		return err
	}
	if d.Status == db.DeploymentBuilding || d.Status == db.DeploymentDeploying || d.Status == db.DeploymentPending {
		return fmt.Errorf("deployment is still active; cancel it before deleting")
	}
	return s.db.WithContext(ctx).Delete(&db.Deployment{}, "id = ?", deploymentID).Error
}

// ClearBuildCache deletes the buildah layer-cache PVC for the given project
// namespace. It is recreated automatically on the next build.
func (s *DeploymentService) ClearBuildCache(ctx context.Context, namespace string) error {
	if s.k8s == nil {
		return fmt.Errorf("kubernetes is not configured on this instance")
	}
	return appk8s.DeleteBuildCachePVC(ctx, s.k8s, namespace)
}

// ─── Rollback ─────────────────────────────────────────────────────────────────

// Rollback re-deploys the image from a previous successful deployment without
// triggering a new build. Returns a new Deployment record immediately.
func (s *DeploymentService) Rollback(ctx context.Context, deploymentID uuid.UUID) (*db.Deployment, error) {
	if s.k8s == nil {
		return nil, fmt.Errorf("kubernetes is not configured on this instance")
	}
	var target db.Deployment
	if err := s.db.WithContext(ctx).Preload("Service.Project").Preload("Service.Ports").First(&target, "id = ?", deploymentID).Error; err != nil {
		return nil, fmt.Errorf("deployment not found")
	}
	if target.Status != db.DeploymentSuccess {
		return nil, fmt.Errorf("can only roll back to a successful deployment")
	}
	if target.Image == "" {
		return nil, fmt.Errorf("deployment has no image reference stored")
	}

	now := time.Now()
	dep := &db.Deployment{
		ServiceID:  target.ServiceID,
		Status:     db.DeploymentDeploying,
		Image:      target.Image,
		Log:        fmt.Sprintf("Rolling back to deployment %s (%s)\n", target.ID.String()[:8], target.Image),
		DeployedAt: &now,
	}
	if err := s.db.WithContext(ctx).Create(dep).Error; err != nil {
		return nil, err
	}

	svc := target.Service
	namespace := svc.Project.Slug

	go func() {
		portSpecs := toPortSpecs(svc.Ports)
		port := primaryPort(svc.Ports)
		nodeName := ""
		if svc.NodeID != nil {
			var node db.Node
			if err := s.db.First(&node, svc.NodeID).Error; err == nil {
				nodeName = node.Name
			}
		}
		groupEnvs, _ := s.varGroups.CollectEnvVars(context.Background(), svc.ID)
		envVars := mergeSecretEnvs(runtimeEnvVars(string(svc.EnvVars), port), groupEnvs)
		wp := appk8s.WorkloadParams{
			Name:          slugify(svc.Name),
			Namespace:     namespace,
			Image:         target.Image,
			Ports:         portSpecs,
			Replicas:      int32(svc.Replicas),
			Env:           envVars,
			CPURequest:    svc.CPURequest,
			CPULimit:      svc.CPULimit,
			MemoryRequest: svc.MemoryRequest,
			MemoryLimit:   svc.MemoryLimit,
			NodeName:      nodeName,
		}
		if err := appk8s.ApplyDeployment(context.Background(), s.k8s, wp); err != nil {
			s.failDeployment(dep.ID, "rollback failed: "+err.Error())
			return
		}
		if err := appk8s.ApplyService(context.Background(), s.k8s, slugify(svc.Name), namespace, portSpecs); err != nil {
			s.failDeployment(dep.ID, "rollback failed to apply K8s service: "+err.Error())
			return
		}
		assignedNPs, err := appk8s.ApplyNodePortService(context.Background(), s.k8s, slugify(svc.Name), namespace, portSpecs)
		if err != nil {
			s.failDeployment(dep.ID, "rollback failed to apply K8s NodePort service: "+err.Error())
			return
		}
		for _, sp := range svc.Ports {
			if np, ok := assignedNPs[sp.Name]; ok && np != 0 {
				s.db.Model(&db.ServicePort{}).Where("id = ?", sp.ID).Update("node_port", np)
			}
		}
		finishedAt := time.Now()
		s.db.Model(&db.Deployment{}).Where("id = ?", dep.ID).Updates(map[string]any{
			"status":      db.DeploymentSuccess,
			"log":         dep.Log + "Rollback applied successfully.",
			"deployed_at": &finishedAt,
		})
		s.db.Model(&db.Service{}).Where("id = ?", svc.ID).Updates(map[string]any{
			"image":  target.Image,
			"status": db.ServiceRunning,
		})
	}()

	return dep, nil
}

// Retry re-triggers a fresh build+deploy for a failed deployment's service.
func (s *DeploymentService) Retry(ctx context.Context, deploymentID uuid.UUID) (*db.Deployment, error) {
	var dep db.Deployment
	if err := s.db.WithContext(ctx).First(&dep, "id = ?", deploymentID).Error; err != nil {
		return nil, fmt.Errorf("deployment not found")
	}
	if dep.Status != db.DeploymentFailed {
		return nil, fmt.Errorf("can only retry a failed deployment")
	}
	return s.Trigger(ctx, TriggerInput{ServiceID: dep.ServiceID})
}

// ─── Registry cleanup ─────────────────────────────────────────────────────────

// pruneOldImages deletes registry manifests for successful deployments beyond
// the retention count. Called in a background goroutine after each successful build.
func (s *DeploymentService) pruneOldImages(ctx context.Context, serviceID uuid.UUID, bc db.BuildConfig) {
	if !bc.RollbackEnabled || bc.ImageRetention <= 0 {
		return
	}
	var deployments []db.Deployment
	s.db.WithContext(ctx).
		Where("service_id = ? AND status = ? AND image != ''", serviceID, db.DeploymentSuccess).
		Order("created_at DESC").
		Find(&deployments)
	if len(deployments) <= bc.ImageRetention {
		return
	}
	host, user, pass, err := s.resolveRegistry(ctx, &bc)
	if err != nil {
		return
	}
	for _, dep := range deployments[bc.ImageRetention:] {
		s.deleteRegistryImage(host, user, pass, dep.Image)
	}
}

// deleteRegistryImage soft-deletes a manifest from the registry.
// image format: "registryHost/name:tag"
func (s *DeploymentService) deleteRegistryImage(host, user, pass, image string) {
	// Strip registry host prefix: "host/name:tag" → "name:tag"
	withoutHost := strings.TrimPrefix(image, strings.TrimSuffix(host, "/")+"/")
	// Split name and tag on last ":"
	lastColon := strings.LastIndex(withoutHost, ":")
	if lastColon < 0 {
		return
	}
	name := withoutHost[:lastColon]
	tag := withoutHost[lastColon+1:]

	scheme := "https"
	if !strings.HasPrefix(host, "https://") {
		scheme = "http"
	}
	registryBase := scheme + "://" + strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")

	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: resolve manifest digest.
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/v2/%s/manifests/%s", registryBase, name, tag), nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	resp.Body.Close()
	if digest == "" {
		return
	}

	// Step 2: delete the manifest.
	req2, err := http.NewRequest("DELETE", fmt.Sprintf("%s/v2/%s/manifests/%s", registryBase, name, digest), nil)
	if err != nil {
		return
	}
	if user != "" {
		req2.SetBasicAuth(user, pass)
	}
	resp2, err := client.Do(req2)
	if err == nil && resp2 != nil {
		resp2.Body.Close()
	}
}

// ─── Log streaming ────────────────────────────────────────────────────────────

// StreamBuildLogs streams the build log for a deployment as SSE events.
// Each log line is written as "data: <line>\n\n".
// A final "event: done\ndata: \n\n" signals the stream is complete.
// flush is called after each write so the HTTP layer can push data immediately.
func (s *DeploymentService) StreamBuildLogs(ctx context.Context, deploymentID uuid.UUID, w io.Writer, flush func()) error {
	var d db.Deployment
	if err := s.db.WithContext(ctx).First(&d, "id = ?", deploymentID).Error; err != nil {
		return fmt.Errorf("deployment not found")
	}

	sendLine := func(line string) {
		fmt.Fprintf(w, "data: %s\n\n", line)
		flush()
	}
	sendDone := func() {
		fmt.Fprintf(w, "event: done\ndata: \n\n")
		flush()
	}

	// Completed deployment — replay stored log then close.
	if d.Status != db.DeploymentBuilding && d.Status != db.DeploymentDeploying && d.Status != db.DeploymentPending {
		for _, line := range strings.Split(d.Log, "\n") {
			sendLine(line)
		}
		sendDone()
		return nil
	}

	if s.k8s == nil {
		sendLine("Kubernetes is not configured on this instance.")
		sendDone()
		return nil
	}

	// Get the namespace from the service's project.
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", d.ServiceID).Error; err != nil {
		return fmt.Errorf("service not found")
	}
	namespace := svc.Project.Slug

	// Wait up to 60 s for the build pod to be scheduled.
	sendLine(fmt.Sprintf("Waiting for build pod (job: %s)…", d.BuildJobName))
	flush()
	podName, err := s.waitForBuildPod(ctx, namespace, d.BuildJobName)
	if err != nil {
		sendLine("Error: " + err.Error())
		sendDone()
		return nil
	}

	// Stream pod logs — wait for the container to be running first (image pull
	// can take tens of seconds). Retry up to 3 minutes.
	sendLine(fmt.Sprintf("Streaming logs from pod %s", podName))
	flush()
	var stream io.ReadCloser
	streamDeadline := time.Now().Add(3 * time.Minute)
	attempt := 0
	for {
		req := s.k8s.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
			Follow: true,
			// No TailLines — replay all existing output then follow new lines.
			// Needed so reconnecting clients catch up on already-written log output.
		})
		var err error
		stream, err = req.Stream(ctx)
		if err == nil {
			break
		}
		if time.Now().After(streamDeadline) {
			// Give up — kubelet unreachable or container never started.
			sendLine("Warning: cannot stream live logs (" + err.Error() + ")")
			sendLine("Replaying stored log output:")
			var snap db.Deployment
			if snapErr := s.db.WithContext(ctx).First(&snap, "id = ?", d.ID).Error; snapErr == nil && snap.Log != "" {
				for _, line := range strings.Split(snap.Log, "\n") {
					sendLine(line)
				}
			}
			sendDone()
			return nil
		}
		attempt++
		if attempt%5 == 0 {
			sendLine(fmt.Sprintf("Waiting for container to start (attempt %d)…", attempt))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
		}
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
			sendLine(scanner.Text())
		}
	}

	sendDone()
	return nil
}

// LogOptions controls how runtime logs are fetched or streamed.
type LogOptions struct {
	TailLines int64  // 0 = default (200 for streaming, unlimited for fetch)
	Follow    bool   // stream new lines as they arrive
	Since     string // duration: "1h", "6h", "24h", "7d" — or empty for no filter
}

// parseSinceDuration converts a user-supplied duration string into seconds for
// the K8s SinceSeconds field. Supports Go durations ("1h", "30m") and "Nd" for days.
func parseSinceDuration(s string) *int64 {
	if s == "" {
		return nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		secs := int64(d.Seconds())
		return &secs
	}
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && n > 0 {
			secs := int64(n) * 86400
			return &secs
		}
	}
	return nil
}

// findRuntimePod resolves the running pod name and namespace for a service.
func (s *DeploymentService) findRuntimePod(ctx context.Context, serviceID uuid.UUID) (podName, namespace string, err error) {
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", serviceID).Error; err != nil {
		return "", "", fmt.Errorf("service not found")
	}
	namespace = svc.Project.Slug
	podSlug := slugify(svc.Name)
	if svc.Type == db.ServiceTypeDatabase {
		var dc db.DatabaseConfig
		if err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).First(&dc).Error; err == nil && dc.Slug != "" {
			podSlug = dc.Slug
		}
	}
	selector := fmt.Sprintf("app=%s,managed-by=meshploy", podSlug)
	pods, err := s.k8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil || len(pods.Items) == 0 {
		return "", "", fmt.Errorf("no running pod found for this service")
	}
	return pods.Items[0].Name, namespace, nil
}

// buildPodLogOptions converts LogOptions into corev1.PodLogOptions.
func buildPodLogOptions(opts LogOptions, defaultTail int64) *corev1.PodLogOptions {
	o := &corev1.PodLogOptions{Follow: opts.Follow, Timestamps: true}
	tail := opts.TailLines
	if tail == 0 && defaultTail > 0 {
		tail = defaultTail
	}
	if tail > 0 {
		o.TailLines = &tail
	}
	if secs := parseSinceDuration(opts.Since); secs != nil {
		o.SinceSeconds = secs
	}
	return o
}

// StreamRuntimeLogs streams live stdout/stderr from the running pod for a
// service. Each line is written as "data: <line>\n\n"; "event: done\ndata: \n\n"
// signals the end of stream.
func (s *DeploymentService) StreamRuntimeLogs(ctx context.Context, serviceID uuid.UUID, opts LogOptions, w io.Writer, flush func()) error {
	sendLine := func(line string) {
		fmt.Fprintf(w, "data: %s\n\n", line)
		flush()
	}
	sendDone := func() {
		fmt.Fprintf(w, "event: done\ndata: \n\n")
		flush()
	}

	if s.k8s == nil {
		sendLine("Kubernetes is not configured on this instance.")
		sendDone()
		return nil
	}

	podName, namespace, err := s.findRuntimePod(ctx, serviceID)
	if err != nil {
		sendLine("Error: " + err.Error())
		sendDone()
		return nil
	}

	sendLine(fmt.Sprintf("Streaming logs from pod %s", podName))
	flush()

	req := s.k8s.CoreV1().Pods(namespace).GetLogs(podName, buildPodLogOptions(opts, 200))
	stream, err := req.Stream(ctx)
	if err != nil {
		sendLine("Cannot stream live container logs: " + err.Error())
		sendLine("Hint: ensure workers joined k3s with --node-ip=<mesh_ip>")
		sendDone()
		return nil
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
			sendLine(scanner.Text())
		}
	}

	sendDone()
	return nil
}

// FetchRuntimeLogs fetches a snapshot of container logs as plain text (no streaming).
// Used by the download endpoint.
func (s *DeploymentService) FetchRuntimeLogs(ctx context.Context, serviceID uuid.UUID, opts LogOptions) (string, error) {
	if s.k8s == nil {
		return "Kubernetes is not configured on this instance.\n", nil
	}

	podName, namespace, err := s.findRuntimePod(ctx, serviceID)
	if err != nil {
		return "", err
	}

	opts.Follow = false
	req := s.k8s.CoreV1().Pods(namespace).GetLogs(podName, buildPodLogOptions(opts, 0))
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("cannot fetch container logs: %w", err)
	}
	defer stream.Close()

	var buf strings.Builder
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		buf.WriteString(scanner.Text())
		buf.WriteByte('\n')
	}
	return buf.String(), nil
}

// waitForBuildPod polls until the pod for a K8s Job appears, up to 60 s.
func (s *DeploymentService) waitForBuildPod(ctx context.Context, namespace, jobName string) (string, error) {
	deadline := time.Now().Add(60 * time.Second)
	selector := "batch.kubernetes.io/job-name=" + jobName
	for {
		pods, err := s.k8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err == nil && len(pods.Items) > 0 {
			return pods.Items[0].Name, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for build pod (job: %s)", jobName)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// runtimeEnvVars parses a raw .env block into K8s EnvVar slice and ensures
// PORT is set to the container port. The user-supplied value wins if present.
func runtimeEnvVars(envBlock string, port int32) []corev1.EnvVar {
	portStr := fmt.Sprintf("%d", port)
	hasPort := false
	var envs []corev1.EnvVar
	for _, line := range strings.Split(envBlock, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes.
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		if key == "PORT" {
			hasPort = true
			portStr = val
		}
		envs = append(envs, corev1.EnvVar{Name: key, Value: val})
	}
	if !hasPort {
		envs = append(envs, corev1.EnvVar{Name: "PORT", Value: portStr})
	}
	return envs
}

// mergeSecretEnvs appends secret key-value pairs to the existing env slice,
// skipping any key that the explicit env block already defines (explicit wins).
func mergeSecretEnvs(envs []corev1.EnvVar, secrets map[string]string) []corev1.EnvVar {
	if len(secrets) == 0 {
		return envs
	}
	existing := make(map[string]struct{}, len(envs))
	for _, e := range envs {
		existing[e.Name] = struct{}{}
	}
	for k, v := range secrets {
		if _, ok := existing[k]; !ok {
			envs = append(envs, corev1.EnvVar{Name: k, Value: v})
		}
	}
	return envs
}

// slugify converts a name to a K8s-safe lowercase slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

// ─── Database provisioning ────────────────────────────────────────────────────

// dbDataPath returns the container data directory for each engine.
func dbDataPath(engine db.DatabaseEngine) string {
	switch engine {
	case db.DatabaseMySQL:
		return "/var/lib/mysql"
	case db.DatabaseRedis, db.DatabaseDragonfly:
		return "/data"
	case db.DatabaseMongoDB:
		return "/data/db"
	case db.DatabaseClickHouse:
		return "/var/lib/clickhouse"
	default: // postgres
		return "/var/lib/postgresql/data"
	}
}

// dbEnvVars returns the env vars required to initialise the database container.
func dbEnvVars(dc db.DatabaseConfig) []corev1.EnvVar {
	pass := string(dc.DBPassword)
	switch dc.Engine {
	case db.DatabasePostgres:
		return []corev1.EnvVar{
			{Name: "POSTGRES_DB", Value: dc.DBName},
			{Name: "POSTGRES_USER", Value: dc.DBUser},
			{Name: "POSTGRES_PASSWORD", Value: pass},
			{Name: "PGDATA", Value: "/var/lib/postgresql/data/pgdata"},
		}
	case db.DatabaseMySQL:
		return []corev1.EnvVar{
			{Name: "MYSQL_DATABASE", Value: dc.DBName},
			{Name: "MYSQL_USER", Value: dc.DBUser},
			{Name: "MYSQL_PASSWORD", Value: pass},
			{Name: "MYSQL_ROOT_PASSWORD", Value: pass},
		}
	case db.DatabaseRedis, db.DatabaseDragonfly:
		if pass != "" {
			return []corev1.EnvVar{{Name: "REDIS_PASSWORD", Value: pass}}
		}
		return nil
	case db.DatabaseClickHouse:
		return []corev1.EnvVar{
			{Name: "CLICKHOUSE_DB", Value: dc.DBName},
			{Name: "CLICKHOUSE_USER", Value: dc.DBUser},
			{Name: "CLICKHOUSE_PASSWORD", Value: pass},
		}
	case db.DatabaseMongoDB:
		return []corev1.EnvVar{
			{Name: "MONGO_INITDB_ROOT_USERNAME", Value: dc.DBUser},
			{Name: "MONGO_INITDB_ROOT_PASSWORD", Value: pass},
			{Name: "MONGO_INITDB_DATABASE", Value: dc.DBName},
		}
	default:
		return nil
	}
}

func (s *DeploymentService) provisionDatabase(ctx context.Context, svc *db.Service) (*db.Deployment, error) {
	var dc db.DatabaseConfig
	if err := s.db.WithContext(ctx).Where("service_id = ?", svc.ID).First(&dc).Error; err != nil {
		return nil, fmt.Errorf("database config not found")
	}

	deploymentID := uuid.New()
	deployment := db.Deployment{
		Base:      db.Base{ID: deploymentID},
		ServiceID: svc.ID,
		Status:    db.DeploymentDeploying,
		Image:     svc.Image,
		Log:       "Provisioning database…\n",
	}
	if err := s.db.WithContext(ctx).Create(&deployment).Error; err != nil {
		return nil, fmt.Errorf("create deployment record: %w", err)
	}

	go func() {
		bgCtx := context.Background()

		namespace := ""
		var project db.Project
		if err := s.db.Where("id = ?", svc.ProjectID).First(&project).Error; err == nil {
			namespace = project.Slug
		}

		appendLog := func(msg string) {
			s.db.Model(&db.Deployment{}).Where("id = ?", deploymentID).
				Update("log", gorm.Expr("log || ?", msg+"\n"))
		}

		appendLog("Ensuring namespace " + namespace + "…")
		if err := appk8s.EnsureNamespace(bgCtx, s.k8s, namespace); err != nil {
			s.failDeployment(deploymentID, "failed to ensure namespace: "+err.Error())
			return
		}

		nodeName := ""
		if svc.NodeID != nil {
			var node db.Node
			if err := s.db.Where("id = ?", svc.NodeID).First(&node).Error; err == nil {
				nodeName = node.Name
			}
		}

		slug := dc.Slug
		if slug == "" {
			slug = slugify(svc.Name)
		}

		// Load the service's single "db" port.
		var dbPorts []db.ServicePort
		s.db.Where("service_id = ?", svc.ID).Find(&dbPorts)
		dbPort := primaryPort(dbPorts)

		appendLog("Applying database workload…")
		dbProbe := buildProbeFromService(svc)
		wp := appk8s.DatabaseWorkloadParams{
			Name:           slug,
			Namespace:      namespace,
			Image:          svc.Image,
			Port:           dbPort,
			Env:            dbEnvVars(dc),
			StorageGB:      dc.StorageGB,
			DataPath:       dbDataPath(dc.Engine),
			NodeName:       nodeName,
			LivenessProbe:  dbProbe,
			ReadinessProbe: dbProbe,
		}
		if err := appk8s.ApplyDatabaseDeployment(bgCtx, s.k8s, wp); err != nil {
			s.failDeployment(deploymentID, "failed to apply K8s workload: "+err.Error())
			return
		}
		// ClusterIP service for intra-cluster access (database ports are never public-routed).
		dbPortSpecs := toPortSpecs(dbPorts)
		if err := appk8s.ApplyService(bgCtx, s.k8s, slug, namespace, dbPortSpecs); err != nil {
			s.failDeployment(deploymentID, "failed to apply K8s service: "+err.Error())
			return
		}

		now := time.Now()
		s.db.Model(&db.Deployment{}).Where("id = ?", deploymentID).Updates(map[string]any{
			"status":      db.DeploymentSuccess,
			"log":         gorm.Expr("log || ?", "Database provisioned successfully.\n"),
			"deployed_at": &now,
		})
		s.db.Model(&db.Service{}).Where("id = ?", svc.ID).Updates(map[string]any{
			"status": db.ServiceRunning,
		})
	}()

	return &deployment, nil
}

// ResetDatabase deletes the data PVC and re-provisions the database workload.
// All stored data is permanently lost.
func (s *DeploymentService) ResetDatabase(ctx context.Context, serviceID uuid.UUID) (*db.Deployment, error) {
	if s.k8s == nil {
		return nil, fmt.Errorf("kubernetes is not configured on this instance")
	}
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", serviceID).Error; err != nil {
		return nil, fmt.Errorf("service not found")
	}
	if svc.Type != db.ServiceTypeDatabase {
		return nil, fmt.Errorf("reset is only supported for database services")
	}

	namespace := svc.Project.Slug
	var dc db.DatabaseConfig
	_ = s.db.WithContext(ctx).Where("service_id = ?", serviceID).First(&dc).Error
	slug := dc.Slug
	if slug == "" {
		slug = slugify(svc.Name)
	}

	// Scale to 0 first so the PVC is not in use.
	if err := appk8s.ScaleDeployment(ctx, s.k8s, slug, namespace, 0); err != nil {
		return nil, fmt.Errorf("scale down before reset: %w", err)
	}
	if err := appk8s.DeleteDatabasePVC(ctx, s.k8s, slug, namespace); err != nil {
		return nil, fmt.Errorf("delete PVC: %w", err)
	}

	// Mark service stopped, then re-provision.
	s.db.Model(&db.Service{}).Where("id = ?", serviceID).Update("status", db.ServiceStopped)
	return s.provisionDatabase(ctx, &svc)
}
