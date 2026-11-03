package k8s

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// pruneScript removes every image on the node that no container uses, through
// the node's own k3s. A running app's image stays; an earlier one comes back
// from the registry when a rollback asks for it. It counts images before and
// after, because crictl names only the tagged ones it removes.
const pruneScript = `PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin
export PATH
before=$(k3s crictl images -q | sort -u | wc -l)
k3s crictl rmi --prune >/dev/null || exit $?
after=$(k3s crictl images -q | sort -u | wc -l)
echo "removed $((before - after))"`

// PruneNodeImages removes the images no container uses from one cluster node
// and says how many went. It runs as a short privileged pod on that node, as
// the node terminal does, and waits up to timeout for it.
func PruneNodeImages(ctx context.Context, client kubernetes.Interface, k8sNodeName string, timeout time.Duration) (int, error) {
	pods := client.CoreV1().Pods("kube-system")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "meshploy-image-prune-",
			Labels:       map[string]string{"app": "meshploy-image-prune", "managed-by": "meshploy"},
		},
		Spec: corev1.PodSpec{
			EnableServiceLinks:            boolPtr(false),
			NodeName:                      k8sNodeName,
			HostPID:                       true,
			RestartPolicy:                 corev1.RestartPolicyNever,
			TerminationGracePeriodSeconds: int64Ptr(0),
			Tolerations:                   []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
			Containers: []corev1.Container{{
				Name:            "prune",
				Image:           "busybox:stable",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"chroot", "/host", "/bin/sh", "-c", pruneScript},
				SecurityContext: &corev1.SecurityContext{Privileged: boolPtr(true)},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10m"),
						corev1.ResourceMemory: resource.MustParse("32Mi"),
					},
				},
				VolumeMounts: []corev1.VolumeMount{{Name: "host-root", MountPath: "/host"}},
			}},
			Volumes: []corev1.Volume{{
				Name:         "host-root",
				VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}},
			}},
		},
	}
	created, err := pods.Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return 0, fmt.Errorf("start the cleanup on %s: %w", k8sNodeName, err)
	}
	defer func() {
		// Its own context: the caller's may be the one that ran out.
		dctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = pods.Delete(dctx, created.Name, metav1.DeleteOptions{})
	}()

	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var phase corev1.PodPhase
	for {
		p, err := pods.Get(wctx, created.Name, metav1.GetOptions{})
		if err != nil {
			return 0, fmt.Errorf("the cleanup on %s: %w", k8sNodeName, err)
		}
		phase = p.Status.Phase
		if phase == corev1.PodSucceeded || phase == corev1.PodFailed {
			break
		}
		select {
		case <-wctx.Done():
			return 0, fmt.Errorf("the cleanup on %s did not finish within %s", k8sNodeName, timeout)
		case <-time.After(time.Second):
		}
	}

	out := podOutput(ctx, client, created.Name)
	if phase == corev1.PodFailed {
		return 0, fmt.Errorf("the cleanup on %s failed: %s", k8sNodeName, lastLine(out))
	}
	return countDeleted(out), nil
}

// podOutput is what the prune pod printed, empty when it cannot be read.
func podOutput(ctx context.Context, client kubernetes.Interface, name string) string {
	rc, err := client.CoreV1().Pods("kube-system").GetLogs(name, &corev1.PodLogOptions{Container: "prune"}).Stream(ctx)
	if err != nil {
		return ""
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
	return string(b)
}

// countDeleted reads the script's "removed N" line.
func countDeleted(out string) int {
	for _, l := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "removed "); ok {
			if n, err := strconv.Atoi(rest); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return "no output"
}
