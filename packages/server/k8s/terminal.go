package k8s

import (
	"context"
	"fmt"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ResolveK8sNodeName finds the K8s node name for a Meshploy node by matching
// its TailscaleIP against cluster node internal IPs, falling back to name match.
func ResolveK8sNodeName(ctx context.Context, client kubernetes.Interface, tailscaleIP, nodeName string) (string, error) {
	nodes, err := GetClusterNodes(ctx, client)
	if err != nil {
		return "", fmt.Errorf("list cluster nodes: %w", err)
	}
	for _, n := range nodes {
		for _, ip := range n.InternalIPs {
			if ip == tailscaleIP {
				return n.Name, nil
			}
		}
	}
	for _, n := range nodes {
		if n.Name == nodeName {
			return n.Name, nil
		}
	}
	return "", fmt.Errorf("node %q (IP %s) not found in k8s cluster", nodeName, tailscaleIP)
}

func int64Ptr(i int64) *int64 { return &i }

// CreateShellPod launches an ephemeral privileged pod on the given k8s node.
// The pod mounts the host root filesystem at /host; exec sessions use
// `chroot /host` to get a real root shell on the node without requiring SSH.
func CreateShellPod(ctx context.Context, client kubernetes.Interface, k8sNodeName, podName string) (*corev1.Pod, error) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: "kube-system",
			Labels: map[string]string{
				"app":        "meshploy-shell",
				"managed-by": "meshploy",
			},
		},
		Spec: corev1.PodSpec{
			NodeName:                      k8sNodeName,
			HostPID:                       true,
			HostNetwork:                   true,
			RestartPolicy:                 corev1.RestartPolicyNever,
			TerminationGracePeriodSeconds: int64Ptr(0),
			Tolerations:                   []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
			Containers: []corev1.Container{{
				Name:  "shell",
				Image: "busybox:stable",
				// Sleep loop keeps the container alive; individual sessions use exec.
				Command: []string{"sh", "-c", "while true; do sleep 86400; done"},
				SecurityContext: &corev1.SecurityContext{
					Privileged: boolPtr(true),
				},
				// Minimal requests satisfy LimitRange policies and give the scheduler
				// a non-zero cost; no limits so admin sessions aren't OOMKilled mid-task.
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10m"),
						corev1.ResourceMemory: resource.MustParse("32Mi"),
					},
				},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "host-root",
					MountPath: "/host",
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "host-root",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{Path: "/"},
				},
			}},
		},
	}
	return client.CoreV1().Pods("kube-system").Create(ctx, pod, metav1.CreateOptions{})
}

// WaitForPodRunning polls until the pod reaches Running phase or ctx expires.
func WaitForPodRunning(ctx context.Context, client kubernetes.Interface, podName string) error {
	for {
		pod, err := client.CoreV1().Pods("kube-system").Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get pod: %w", err)
		}
		switch pod.Status.Phase {
		case corev1.PodRunning:
			return nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return fmt.Errorf("pod ended unexpectedly with phase %s", pod.Status.Phase)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// DeleteShellPod removes the shell pod immediately. Safe to call after disconnect.
func DeleteShellPod(ctx context.Context, client kubernetes.Interface, podName string) {
	grace := int64(0)
	_ = client.CoreV1().Pods("kube-system").Delete(ctx, podName, metav1.DeleteOptions{
		GracePeriodSeconds: &grace,
	})
}

// CleanupOrphanedShellPods deletes any meshploy-shell-* pods left over from a
// previous API instance that exited without cleaning up (e.g. crash, SIGKILL).
func CleanupOrphanedShellPods(ctx context.Context, client kubernetes.Interface) {
	pods, err := client.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{
		LabelSelector: "managed-by=meshploy,app=meshploy-shell",
	})
	if err != nil || len(pods.Items) == 0 {
		return
	}
	for _, pod := range pods.Items {
		DeleteShellPod(ctx, client, pod.Name)
		log.Printf("terminal: cleaned up orphaned shell pod %s", pod.Name)
	}
}
