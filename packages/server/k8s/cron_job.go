package k8s

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CronJobParams holds the configuration for a meshploy CronJob.
type CronJobParams struct {
	Name              string
	Namespace         string
	Schedule          string
	ConcurrencyPolicy batchv1.ConcurrencyPolicy
	HistoryLimit      int32
	Image             string
	Command           string
	EnvVars           []corev1.EnvVar
	CPURequest        string
	CPULimit          string
	MemRequest        string
	MemLimit          string
	NodeName          string
	JobID             string // meshploy Job UUID — stored as label on spawned K8s Jobs
}

// ApplyCronJob creates or updates a K8s CronJob for a meshploy cron job.
func ApplyCronJob(ctx context.Context, client kubernetes.Interface, p CronJobParams) error {
	spec := buildCronJobSpec(p)

	existing, err := client.BatchV1().CronJobs(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		cj := &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      p.Name,
				Namespace: p.Namespace,
				Labels: map[string]string{
					"managed-by":      "meshploy",
					"meshploy-job-id": p.JobID,
				},
			},
			Spec: spec,
		}
		_, err = client.BatchV1().CronJobs(p.Namespace).Create(ctx, cj, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}

	existing.Spec = spec
	_, err = client.BatchV1().CronJobs(p.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// DeleteCronJob removes the K8s CronJob. No-ops if it doesn't exist.
func DeleteCronJob(ctx context.Context, client kubernetes.Interface, namespace, name string) error {
	err := client.BatchV1().CronJobs(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

func buildCronJobSpec(p CronJobParams) batchv1.CronJobSpec {
	backoff := int32(0)
	histLimit := p.HistoryLimit
	if histLimit <= 0 {
		histLimit = 5
	}

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

	return batchv1.CronJobSpec{
		Schedule:                   p.Schedule,
		ConcurrencyPolicy:          p.ConcurrencyPolicy,
		SuccessfulJobsHistoryLimit: &histLimit,
		FailedJobsHistoryLimit:     &histLimit,
		JobTemplate: batchv1.JobTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"managed-by":      "meshploy",
					"job-type":        "cron-run",
					"meshploy-job-id": p.JobID,
				},
			},
			Spec: batchv1.JobSpec{
				BackoffLimit: &backoff,
				Template: corev1.PodTemplateSpec{
					Spec: podSpec,
				},
			},
		},
	}
}
