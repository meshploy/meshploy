package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
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

// ClearBuildCache deletes the buildah layer-cache PVC for the given project
// namespace. It is recreated automatically on the next build.
func (s *DeploymentService) ClearBuildCache(ctx context.Context, namespace string) error {
	if s.k8s == nil {
		return fmt.Errorf("kubernetes is not configured on this instance")
	}
	return appk8s.DeleteBuildCachePVC(ctx, s.k8s, namespace)
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
	zero := int64(0)
	var stream io.ReadCloser
	streamDeadline := time.Now().Add(3 * time.Minute)
	attempt := 0
	for {
		req := s.k8s.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
			Follow:    true,
			TailLines: &zero,
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

// slugify converts a name to a K8s-safe lowercase slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}
