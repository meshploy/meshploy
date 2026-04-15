package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// BuilderImage is the container image that performs git clone + build + push.
	// Contains: git, nixpacks, railpack, buildah.
	BuilderImage = "ghcr.io/meshploy/builder:latest"

	// BuilderNodeLabel is the node label used to schedule build jobs.
	BuilderNodeLabel = "meshploy.com/role"
	BuilderNodeValue = "builder"
)

// BuildJobParams holds everything the build job needs.
type BuildJobParams struct {
	// Unique name for the K8s Job resource.
	JobName string
	// K8s namespace to run in (= project slug).
	Namespace string
	// Git source
	GitRepo   string // "owner/repo"
	GitBranch string
	GitToken  string // short-lived GitHub installation token
	// Build method: "nixpacks" | "railpack" | "dockerfile"
	Builder string
	// Full destination image ref, e.g. "registry.example.com/myapp:abc1234"
	ImageDest    string
	RegistryHost string
	RegistryUser string
	RegistryPass string
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
					DNSConfig: &corev1.PodDNSConfig{
						Options: []corev1.PodDNSConfigOption{
							{Name: "ndots", Value: func() *string { s := "1"; return &s }()},
						},
					},
					// Run builds on nodes labelled meshploy.com/role=builder.
					// Falls back to any node if none are labelled.
					NodeSelector: map[string]string{BuilderNodeLabel: BuilderNodeValue},
					Tolerations: []corev1.Toleration{
						{
							Key:      BuilderNodeLabel,
							Operator: corev1.TolerationOpEqual,
							Value:    BuilderNodeValue,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "builder",
							Image:           BuilderImage,
							ImagePullPolicy: corev1.PullAlways,
							Command:         []string{"/usr/local/bin/meshploy-build"},
							Env: []corev1.EnvVar{
								{Name: "GIT_REPO", Value: p.GitRepo},
								{Name: "GIT_BRANCH", Value: p.GitBranch},
								{Name: "GIT_TOKEN", Value: p.GitToken},
								{Name: "BUILDER", Value: p.Builder},
								{Name: "IMAGE_DEST", Value: p.ImageDest},
								{Name: "REGISTRY_HOST", Value: p.RegistryHost},
								{Name: "REGISTRY_USER", Value: p.RegistryUser},
								{Name: "REGISTRY_PASS", Value: p.RegistryPass},
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

	_, err := client.BatchV1().Jobs(p.Namespace).Create(ctx, job, metav1.CreateOptions{})
	return err
}

// JobResult is the outcome of a completed build job.
type JobResult struct {
	Success bool
	Log     string
}

// WaitForJob polls the job until it succeeds, fails, or the context is cancelled.
// Returns the job log regardless of outcome.
func WaitForJob(ctx context.Context, client kubernetes.Interface, namespace, jobName string, timeout time.Duration) JobResult {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return JobResult{Success: false, Log: "build timed out after " + timeout.String()}
		}
		select {
		case <-ctx.Done():
			return JobResult{Success: false, Log: "context cancelled"}
		default:
		}

		job, err := client.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				time.Sleep(3 * time.Second)
				continue
			}
			return JobResult{Success: false, Log: "failed to get job: " + err.Error()}
		}

		log := fetchJobLog(ctx, client, namespace, jobName)

		if job.Status.Succeeded > 0 {
			return JobResult{Success: true, Log: log}
		}
		if job.Status.Failed > 0 {
			return JobResult{Success: false, Log: log}
		}
		time.Sleep(5 * time.Second)
	}
}

// fetchJobLog returns stdout+stderr from the first pod of the job.
func fetchJobLog(ctx context.Context, client kubernetes.Interface, namespace, jobName string) string {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}
	pod := pods.Items[0]
	req := client.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: "builder"})
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
