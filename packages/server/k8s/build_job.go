package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/meshploy/packages/server/version"
)

const (
	// builderImageRepo is the image that performs git clone + build + push.
	// Contains: git, nixpacks, railpack, buildah.
	builderImageRepo = "ghcr.io/meshploy/builder"

	// BuilderNodeLabel is the node label used to schedule build jobs.
	BuilderNodeLabel = "meshploy.com/role"
	BuilderNodeValue = "builder"

	// buildCachePVC is the shared PVC for all build caches in a namespace.
	// Subpaths partition it: "buildah" for buildah/nixpacks layers,
	// "buildkit" for railpack's BuildKit layer cache.
	buildCachePVC        = "build-cache"
	buildCacheSize       = "20Gi"
	buildahCacheMountAt  = "/var/lib/containers/storage" // root buildah storage
	buildkitCacheMountAt = "/tmp/buildkit-root"          // buildkitd --root
)

// DefaultBuilderImage is the builder matching this API's release channel. An
// edge API runs the builder built from main, so a change to the build script
// reaches edge servers with the rest of an edge build instead of waiting for a
// release.
func DefaultBuilderImage() string {
	if version.Channel == "edge" {
		return builderImageRepo + ":main"
	}
	return builderImageRepo + ":latest"
}

// BuildJobParams holds everything the build job needs.
type BuildJobParams struct {
	// Unique name for the K8s Job resource.
	JobName string
	// K8s namespace to run in (= project slug).
	Namespace string
	// Builder image to run. Empty = DefaultBuilderImage().
	Image string
	// Git source
	GitRepo   string // as stored: owner/repo, a GitLab group path, or a full URL; for log lines
	GitURL    string // https clone URL, without credentials
	GitUser   string // presented with GitToken: x-access-token (GitHub), oauth2 (GitLab, Gitea)
	GitBranch string
	GitToken  string // empty for a public repository
	// Build method: "nixpacks" | "railpack" | "dockerfile"
	Builder string
	// Full destination image ref, e.g. "registry.example.com/myapp:abc1234"
	ImageDest    string
	RegistryHost string
	RegistryUser string
	RegistryPass string
	// Subdirectory within the cloned repo to build from. Empty = repo root.
	RootDir string
	// Build-time env vars — KEY=VALUE, one per line.
	// Forwarded to nixpacks (--env), railpack (export), dockerfile (--build-arg).
	BuildEnvVars string
	// BuilderNode pins the job to a specific K8s node name (k8s_node_name).
	// Empty = use NodeSelector meshploy.com/role=builder (auto-schedule).
	BuilderNode string
	// Resource requests for the build pod. Empty = defaults (1000m / 1Gi).
	CPURequest    string
	MemoryRequest string
}

// EnsureBuildCachePVC creates the buildah layer-cache PVC in the namespace if
// it does not already exist. The PVC is ReadWriteOnce / local-path so layers
// survive across build jobs on the same node.
func EnsureBuildCachePVC(ctx context.Context, client kubernetes.Interface, namespace string) error {
	_, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, buildCachePVC, metav1.GetOptions{})
	if err == nil {
		return nil // already exists
	}
	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("check build-cache PVC: %w", err)
	}

	storageClass := "local-path"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildCachePVC,
			Namespace: namespace,
			Labels:    map[string]string{"managed-by": "meshploy", "purpose": "build-cache"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: &storageClass,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(buildCacheSize),
				},
			},
		},
	}
	_, err = client.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	return err
}

// DeleteBuildCachePVC removes the buildah layer-cache PVC for a namespace.
// The PVC is recreated automatically on the next build trigger.
// Returns nil if the PVC does not exist.
func DeleteBuildCachePVC(ctx context.Context, client kubernetes.Interface, namespace string) error {
	err := client.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, buildCachePVC, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete build-cache PVC: %w", err)
	}
	return nil
}

// CreateBuildJob submits a K8s Job that clones, builds, and pushes the image.
// Returns the job name.
func CreateBuildJob(ctx context.Context, client kubernetes.Interface, p BuildJobParams) error {
	ttl := int32(3600) // clean up finished jobs after 1h
	backoff := int32(0)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.JobName,
			Namespace: p.Namespace,
			Labels: map[string]string{
				"managed-by": "meshploy",
				"job-type":   "build",
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoff,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"job-name": p.JobName},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					// Use ndots:1 so external hostnames like github.com are
					// queried directly without appending cluster search domains.
					// Alpine's musl libc resolver sends all search domain variants
					// in parallel and picks the first response — without this,
					// github.com.mesh.<domain> matches the wildcard zone and
					// returns the gateway IP instead of the real GitHub IP.
					// dnsPolicy None + explicit nameservers: bypass CoreDNS entirely.
					// Build pods only need external DNS (github.com for clone) and
					// the registry is accessed by IP (100.64.x.x), not hostname.
					DNSPolicy: corev1.DNSNone,
					DNSConfig: &corev1.PodDNSConfig{
						Nameservers: []string{"1.1.1.1", "8.8.8.8"},
						Options: []corev1.PodDNSConfigOption{
							{Name: "ndots", Value: func() *string { s := "1"; return &s }()},
						},
					},
					// If a specific node is requested, pin via NodeName (bypasses
					// scheduler). Otherwise fall back to the role label selector.
					NodeName: p.BuilderNode,
					NodeSelector: func() map[string]string {
						if p.BuilderNode != "" {
							return nil
						}
						return map[string]string{BuilderNodeLabel: BuilderNodeValue}
					}(),
					Tolerations: []corev1.Toleration{
						{
							Key:      BuilderNodeLabel,
							Operator: corev1.TolerationOpEqual,
							Value:    BuilderNodeValue,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					Volumes: []corev1.Volume{
						{
							// Single PVC shared by both buildah and buildkitd via subpaths.
							Name: "build-cache",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: buildCachePVC,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "builder",
							Image:           builderImageOr(p.Image),
							ImagePullPolicy: corev1.PullAlways,
							Command:         []string{"/usr/local/bin/meshploy-build"},
							// Resource requests give the scheduler enough signal to prefer
							// less-loaded builder nodes (LeastAllocated scoring).
							// No limits so builds can burst freely on spare capacity.
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse(builderCPU(p.CPURequest)),
									corev1.ResourceMemory: resource.MustParse(builderMemory(p.MemoryRequest)),
								},
							},
							Env: []corev1.EnvVar{
								{Name: "GIT_REPO", Value: p.GitRepo},
								{Name: "GIT_URL", Value: p.GitURL},
								{Name: "GIT_USER", Value: p.GitUser},
								{Name: "GIT_BRANCH", Value: p.GitBranch},
								{Name: "GIT_TOKEN", Value: p.GitToken},
								{Name: "ROOT_DIR", Value: p.RootDir},
								{Name: "BUILDER", Value: p.Builder},
								{Name: "IMAGE_DEST", Value: p.ImageDest},
								{Name: "REGISTRY_HOST", Value: p.RegistryHost},
								{Name: "REGISTRY_USER", Value: p.RegistryUser},
								{Name: "REGISTRY_PASS", Value: p.RegistryPass},
								{Name: "BUILD_ENV_VARS", Value: p.BuildEnvVars},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									// buildah (nixpacks/dockerfile) layer cache
									Name:      "build-cache",
									MountPath: buildahCacheMountAt,
									SubPath:   "buildah",
								},
								{
									// buildkitd (railpack) layer cache
									Name:      "build-cache",
									MountPath: buildkitCacheMountAt,
									SubPath:   "buildkit",
								},
							},
							// Buildah needs elevated privileges to create overlay mounts.
							SecurityContext: &corev1.SecurityContext{
								Privileged: boolPtr(true),
							},
						},
					},
				},
			},
		},
	}

	// Extension mutators run last, on the final object. A mutator that errors
	// aborts submission — build isolation must fail closed rather than let an
	// unisolated build run. No-op in CE builds.
	if err := applyJobMutators(ctx, JobBuild, job); err != nil {
		return err
	}

	_, err := client.BatchV1().Jobs(p.Namespace).Create(ctx, job, metav1.CreateOptions{})
	return err
}

// JobResult is the outcome of a completed build job.
type JobResult struct {
	Success bool
	Log     string
	// Unschedulable is set when the wait gave up because no node could take
	// the pod. Log then holds the scheduler's reason.
	Unschedulable bool
}

// JobWaitOptions tunes WaitForJobWith.
type JobWaitOptions struct {
	// Timeout bounds the whole wait.
	Timeout time.Duration
	// Unschedulable is how long the pod may go unplaced because no node
	// matches its selector or taints before the wait gives up, so a build with
	// nowhere to run fails in minutes rather than at Timeout. A pod only short
	// of CPU or memory is queued behind other builds and keeps waiting. Zero
	// never gives up early.
	Unschedulable time.Duration
	// OnStarted runs once, when the pod has been placed on a node.
	OnStarted func(node string)
	// Poll is the interval between checks. Zero means five seconds.
	Poll time.Duration
}

// WaitForJob polls the job until it succeeds, fails, or the context is cancelled.
// Returns the job log regardless of outcome.
func WaitForJob(ctx context.Context, client kubernetes.Interface, namespace, jobName string, timeout time.Duration) JobResult {
	return WaitForJobWith(ctx, client, namespace, jobName, JobWaitOptions{Timeout: timeout})
}

// WaitForJobWith is WaitForJob with the options above: it also reports when the
// pod starts, and gives up on a pod no node can take.
func WaitForJobWith(ctx context.Context, client kubernetes.Interface, namespace, jobName string, opts JobWaitOptions) JobResult {
	poll := opts.Poll
	if poll == 0 {
		poll = 5 * time.Second
	}
	deadline := time.Now().Add(opts.Timeout)
	started := false
	var unplacedSince time.Time
	for {
		if time.Now().After(deadline) {
			return JobResult{Success: false, Log: "build timed out after " + opts.Timeout.String()}
		}
		select {
		case <-ctx.Done():
			return JobResult{Success: false, Log: "context cancelled"}
		default:
		}

		job, err := client.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				time.Sleep(min(3*time.Second, poll))
				continue
			}
			return JobResult{Success: false, Log: "failed to get job: " + err.Error()}
		}

		if pod := jobPod(ctx, client, namespace, jobName); pod != nil {
			switch reason := noNodeMatches(pod); {
			case pod.Spec.NodeName != "":
				unplacedSince = time.Time{}
				if !started {
					started = true
					if opts.OnStarted != nil {
						opts.OnStarted(pod.Spec.NodeName)
					}
				}
			case reason != "" && opts.Unschedulable > 0:
				if unplacedSince.IsZero() {
					unplacedSince = time.Now()
				}
				if time.Since(unplacedSince) >= opts.Unschedulable {
					return JobResult{Success: false, Unschedulable: true, Log: reason}
				}
			default:
				unplacedSince = time.Time{}
			}
		}

		log := FetchContainerLog(ctx, client, namespace, jobName, "builder")

		if job.Status.Succeeded > 0 {
			return JobResult{Success: true, Log: log}
		}
		if job.Status.Failed > 0 {
			return JobResult{Success: false, Log: log}
		}
		time.Sleep(poll)
	}
}

// jobPod returns the job's pod, or nil before it exists.
func jobPod(ctx context.Context, client kubernetes.Interface, namespace, jobName string) *corev1.Pod {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil || len(pods.Items) == 0 {
		return nil
	}
	return &pods.Items[0]
}

// noNodeMatches returns the scheduler's reason when the pod cannot be placed
// because no node fits its selector or taints, and "" otherwise, including when
// it is only waiting for CPU or memory to free up.
func noNodeMatches(pod *corev1.Pod) string {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse && c.Reason == corev1.PodReasonUnschedulable {
			if strings.Contains(c.Message, "Insufficient") {
				return ""
			}
			return c.Message
		}
	}
	return ""
}

// FetchContainerLog returns stdout+stderr from the first pod of a K8s Job.
// containerName must match the container name used in the pod spec.
func FetchContainerLog(ctx context.Context, client kubernetes.Interface, namespace, jobName, containerName string) string {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}
	pod := pods.Items[0]
	req := client.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: containerName})
	rc, err := req.Stream(ctx)
	if err != nil {
		return ""
	}
	defer rc.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return sb.String()
}

func boolPtr(b bool) *bool { return &b }

func builderImageOr(image string) string {
	if image == "" {
		return DefaultBuilderImage()
	}
	return image
}

func builderCPU(v string) string {
	if v == "" {
		return "1000m"
	}
	return v
}

func builderMemory(v string) string {
	if v == "" {
		return "1Gi"
	}
	return v
}
