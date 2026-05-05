package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
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
	db      *gorm.DB
	cfg     *config.Config
	k8s     kubernetes.Interface // nil when K8s is not configured
	git     *GitIntegrationService
	secrets *SecretService
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

	if svc.Type == db.ServiceTypeDatabase {
		return s.provisionDatabase(ctx, &svc)
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

	// Load the git integration stored on the build config.
	if bc.GitIntegrationID == nil {
		return nil, fmt.Errorf("build config has no git integration set — edit the service build source to select one")
	}
	var gitIntegration db.GitIntegration
	if err := s.db.WithContext(ctx).First(&gitIntegration, "id = ?", bc.GitIntegrationID).Error; err != nil {
		return nil, fmt.Errorf("git integration not found — it may have been deleted")
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

	// Get a short-lived GitHub installation token using per-integration credentials.
	if gitIntegration.GHAppID == "" || string(gitIntegration.InstallationID) == "" {
		s.failDeployment(deployment.ID, "GitHub App not fully configured — complete setup and installation in Integrations")
		return &deployment, nil
	}
	gitToken, err := getInstallationToken(gitIntegration.GHAppID, string(gitIntegration.GHPrivateKey), string(gitIntegration.InstallationID))
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

	// Use the service's configured port, fall back to 3000.
	port := int32(a.svc.Port)
	if port == 0 {
		port = 3000
	}

	// Resolve node name if a specific node is pinned.
	nodeName := ""
	if a.svc.NodeID != nil {
		var node db.Node
		if err := s.db.WithContext(ctx).First(&node, a.svc.NodeID).Error; err == nil {
			nodeName = node.Name
		}
	}

	// Merge secrets into env vars (service-level env vars win over secrets on key collision).
	secretEnvs, _ := s.secrets.ResolveForService(ctx, a.svc.ID)
	envVars := mergeSecretEnvs(runtimeEnvVars(string(a.svc.EnvVars), port), secretEnvs)

	// Apply K8s Deployment + Service.
	wp := appk8s.WorkloadParams{
		Name:          slugify(a.svc.Name),
		Namespace:     a.namespace,
		Image:         a.imageName,
		Port:          port,
		Replicas:      int32(a.svc.Replicas),
		Env:           envVars,
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
		"status":      db.DeploymentSuccess,
		"log":         result.Log + "\nDeployment applied successfully.",
		"deployed_at": &now,
	})
	serviceUpdates := map[string]any{
		"image":  a.imageName,
		"status": db.ServiceRunning,
	}
	// For auto-scheduled services, resolve which node K8s actually placed the pod on
	// and persist it so routes resolve to the correct mesh IP.
	if a.svc.NodeID == nil && s.k8s != nil {
		if nodeID := s.resolveScheduledNode(ctx, slugify(a.svc.Name), a.namespace); nodeID != nil {
			serviceUpdates["node_id"] = nodeID
		}
	}
	s.db.Model(&db.Service{}).Where("id = ?", a.svc.ID).Updates(serviceUpdates)

	// Prune old images from the registry (best-effort, non-blocking).
	go s.pruneOldImages(context.Background(), a.svc.ID, a.bc)
}

// resolveScheduledNode queries K8s for the node the pod landed on and returns
// the corresponding db.Node UUID. Returns nil if the pod isn't found yet.
func (s *DeploymentService) resolveScheduledNode(ctx context.Context, podSlug, namespace string) *uuid.UUID {
	selector := fmt.Sprintf("app=%s,managed-by=meshploy", podSlug)
	pods, err := s.k8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil || len(pods.Items) == 0 {
		return nil
	}
	k8sNodeName := pods.Items[0].Spec.NodeName
	if k8sNodeName == "" {
		return nil
	}
	k8sNode, err := s.k8s.CoreV1().Nodes().Get(ctx, k8sNodeName, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	// Match by internal IP first, then fall back to node name.
	var node db.Node
	for _, addr := range k8sNode.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			if s.db.WithContext(ctx).Where("tailscale_ip = ?", addr.Address).First(&node).Error == nil {
				return &node.ID
			}
		}
	}
	if s.db.WithContext(ctx).Where("name = ?", k8sNodeName).First(&node).Error == nil {
		return &node.ID
	}
	return nil
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
	if err := s.db.WithContext(ctx).Preload("Service.Project").First(&target, "id = ?", deploymentID).Error; err != nil {
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
		port := int32(svc.Port)
		if port == 0 {
			port = 3000
		}
		nodeName := ""
		if svc.NodeID != nil {
			var node db.Node
			if err := s.db.First(&node, svc.NodeID).Error; err == nil {
				nodeName = node.Name
			}
		}
		secretEnvs, _ := s.secrets.ResolveForService(context.Background(), svc.ID)
		envVars := mergeSecretEnvs(runtimeEnvVars(string(svc.EnvVars), port), secretEnvs)
		wp := appk8s.WorkloadParams{
			Name:          slugify(svc.Name),
			Namespace:     namespace,
			Image:         target.Image,
			Port:          port,
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
		if err := appk8s.ApplyService(context.Background(), s.k8s, slugify(svc.Name), namespace, port); err != nil {
			s.failDeployment(dep.ID, "rollback failed to apply K8s service: "+err.Error())
			return
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

// StreamRuntimeLogs streams live stdout/stderr from the running pod for a
// service. Uses label selector app=<service-slug>,managed-by=meshploy.
// Each line is written as "data: <line>\n\n"; "event: done\ndata: \n\n" when
// the client disconnects or no running pod is found.
func (s *DeploymentService) StreamRuntimeLogs(ctx context.Context, serviceID uuid.UUID, w io.Writer, flush func()) error {
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

	// Load service + project for namespace and slug.
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", serviceID).Error; err != nil {
		sendLine("Error: service not found")
		sendDone()
		return nil
	}
	namespace := svc.Project.Slug
	podSlug := slugify(svc.Name)
	// Database pods are labeled with the stable slug stored in DatabaseConfig.
	if svc.Type == db.ServiceTypeDatabase {
		var dc db.DatabaseConfig
		if err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).First(&dc).Error; err == nil && dc.Slug != "" {
			podSlug = dc.Slug
		}
	}
	selector := fmt.Sprintf("app=%s,managed-by=meshploy", podSlug)

	// Find the running pod.
	pods, err := s.k8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil || len(pods.Items) == 0 {
		sendLine("No running pod found for this service.")
		sendDone()
		return nil
	}

	podName := pods.Items[0].Name
	sendLine(fmt.Sprintf("Streaming logs from pod %s", podName))
	flush()

	tail := int64(200)
	req := s.k8s.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Follow:    true,
		TailLines: &tail,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		// Kubelet may be unreachable due to node-ip mismatch.
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

		appendLog("Applying database workload…")
		wp := appk8s.DatabaseWorkloadParams{
			Name:      slug,
			Namespace: namespace,
			Image:     svc.Image,
			Port:      int32(svc.Port),
			Env:       dbEnvVars(dc),
			StorageGB: dc.StorageGB,
			DataPath:  dbDataPath(dc.Engine),
			NodeName:  nodeName,
		}
		if err := appk8s.ApplyDatabaseDeployment(bgCtx, s.k8s, wp); err != nil {
			s.failDeployment(deploymentID, "failed to apply K8s workload: "+err.Error())
			return
		}
		// ClusterIP service for intra-cluster access.
		if err := appk8s.ApplyService(bgCtx, s.k8s, slug, namespace, int32(svc.Port)); err != nil {
			s.failDeployment(deploymentID, "failed to apply K8s service: "+err.Error())
			return
		}
		// NodePort service for direct mesh access.
		appendLog("Creating NodePort service for mesh access…")
		nodePort, err := appk8s.ApplyNodePortService(bgCtx, s.k8s, slug, namespace, int32(svc.Port))
		if err != nil {
			appendLog("warning: NodePort service failed: " + err.Error())
		} else {
			s.db.Model(&db.DatabaseConfig{}).Where("service_id = ?", svc.ID).
				Update("node_port", nodePort)
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
