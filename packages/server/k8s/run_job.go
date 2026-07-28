package k8s

import (
	"bufio"
	"context"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RunJobParams holds configuration for a user-defined job run.
type RunJobParams struct {
	JobName    string // unique K8s Job name
	Namespace  string
	Image      string
	Command    string // executed via: sh -c <Command>
	EnvVars    []corev1.EnvVar
	CPURequest string
	CPULimit   string
	MemRequest string
	MemLimit   string
	NodeName   string // optional: pin to specific node
}

// CreateRunJob submits a K8s Job that runs the user's image and command.
func CreateRunJob(ctx context.Context, client kubernetes.Interface, p RunJobParams) error {
	ttl := int32(3600)
	backoff := int32(0) // no retries — let the user re-trigger

	container := corev1.Container{
		Name:  "job",
		Image: p.Image,
		Env:   p.EnvVars,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(p.CPURequest),
				corev1.ResourceMemory: resource.MustParse(p.MemRequest),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(p.CPULimit),
				corev1.ResourceMemory: resource.MustParse(p.MemLimit),
			},
		},
	}
	if p.Command != "" {
		container.Command = []string{"sh"}
		container.Args = []string{"-c", p.Command}
	}

	podSpec := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever,
		Containers:    []corev1.Container{container},
	}
	if p.NodeName != "" {
		podSpec.NodeName = p.NodeName
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.JobName,
			Namespace: p.Namespace,
			Labels: map[string]string{
				"managed-by": "meshploy",
				"job-type":   "run",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"job-name": p.JobName},
				},
				Spec: podSpec,
			},
		},
	}

	_, err := client.BatchV1().Jobs(p.Namespace).Create(ctx, job, metav1.CreateOptions{})
	return err
}

// WaitForRunJob polls a user-defined K8s Job until completion, using the "job" container for logs.
func WaitForRunJob(ctx context.Context, client kubernetes.Interface, namespace, jobName string, timeout time.Duration) JobResult {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return JobResult{Success: false, Log: "job timed out after " + timeout.String()}
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

		log := FetchContainerLog(ctx, client, namespace, jobName, "job")

		if job.Status.Succeeded > 0 {
			return JobResult{Success: true, Log: log}
		}
		if job.Status.Failed > 0 {
			return JobResult{Success: false, Log: log}
		}
		time.Sleep(5 * time.Second)
	}
}

// ListCronRuns lists all K8s Jobs created by meshploy-managed CronJobs across all namespaces.
func ListCronRuns(ctx context.Context, client kubernetes.Interface) ([]batchv1.Job, error) {
	list, err := client.BatchV1().Jobs("").List(ctx, metav1.ListOptions{
		LabelSelector: "managed-by=meshploy,job-type=cron-run",
	})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// ParseEnvBlock converts a raw .env block (KEY=VALUE lines) into a K8s env var list.
// Blank lines and # comments are ignored.
func ParseEnvBlock(raw string) []corev1.EnvVar {
	var envs []corev1.EnvVar
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		envs = append(envs, corev1.EnvVar{
			Name:  strings.TrimSpace(k),
			Value: strings.TrimSpace(v),
		})
	}
	return envs
}
