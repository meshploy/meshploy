package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// FindRunningPod returns the name of the first Running pod with label app=appLabel
// in the given namespace (Meshploy pods always carry managed-by=meshploy).
func FindRunningPod(ctx context.Context, client kubernetes.Interface, namespace, appLabel string) (string, error) {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s,managed-by=meshploy", appLabel),
	})
	if err != nil {
		return "", err
	}
	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodRunning {
			return p.Name, nil
		}
	}
	return "", fmt.Errorf("no running pod for app=%s in %s", appLabel, namespace)
}

// ExecDumpCommand runs cmd inside the specified pod container and returns an
// io.ReadCloser streaming stdout. A goroutine drives StreamWithContext and
// closes the pipe when the command exits or the context is cancelled.
func ExecDumpCommand(
	ctx context.Context,
	client kubernetes.Interface,
	restCfg *rest.Config,
	namespace, podName, containerName string,
	cmd []string,
) (io.ReadCloser, error) {
	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(restCfg, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("create executor: %w", err)
	}

	pr, pw := io.Pipe()
	var stderr bytes.Buffer

	go func() {
		streamErr := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdout: pw,
			Stderr: &stderr,
			Tty:    false,
		})
		if streamErr != nil {
			pw.CloseWithError(fmt.Errorf("%w; stderr=%s", streamErr, stderr.String()))
		} else {
			pw.Close()
		}
	}()

	return pr, nil
}

// CreateEphemeralPod starts a long-running sleep pod in kube-system, used for
// exec-based one-off operations (e.g. system backup via pg_dump).
func CreateEphemeralPod(ctx context.Context, client kubernetes.Interface, name, image string, env []corev1.EnvVar) error {
	grace := int64(0)
	_, err := client.CoreV1().Pods("kube-system").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "kube-system",
			Labels: map[string]string{
				"managed-by": "meshploy",
				"app":        "meshploy-backup",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			TerminationGracePeriodSeconds: &grace,
			Containers: []corev1.Container{{
				Name:    "backup",
				Image:   image,
				Command: []string{"sh", "-c", "while true; do sleep 86400; done"},
				Env:     env,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			}},
		},
	}, metav1.CreateOptions{})
	return err
}

// DeleteEphemeralPod forcefully removes the named pod from kube-system.
func DeleteEphemeralPod(ctx context.Context, client kubernetes.Interface, name string) {
	grace := int64(0)
	_ = client.CoreV1().Pods("kube-system").Delete(ctx, name, metav1.DeleteOptions{
		GracePeriodSeconds: &grace,
	})
}
