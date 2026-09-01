package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// terminalWaitReasons are container waiting reasons that never resolve on their
// own. Waiting for the progress deadline on these only delays the report of a
// failure the cluster has already decided on.
var terminalWaitReasons = map[string]bool{
	"CrashLoopBackOff":           true,
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"InvalidImageName":           true,
	"CreateContainerConfigError": true,
	"CreateContainerError":       true,
}

// RolloutResult reports how a rollout ended.
type RolloutResult struct {
	Succeeded bool
	Reason    string // why it failed; empty on success
}

// WatchRollout follows a Deployment until its pods are actually serving, and
// reports what the cluster is doing while it happens.
//
// Applying a manifest only means the API server accepted it. Everything that
// decides whether the workload runs -- scheduling, pulling the image, the
// container starting and staying up -- happens afterwards and asynchronously.
// Treating the apply as success is what let a crash-looping service report
// "Live, all replicas healthy".
//
// Progress is emitted through emit as human-readable lines, drawn from the
// Kubernetes events on the deployment's own pods: Scheduled, Pulling, Pulled,
// Created, Started, BackOff. That is the cluster's equivalent of the pull and
// create output a container runtime prints directly.
func WatchRollout(
	ctx context.Context,
	client kubernetes.Interface,
	name, namespace string,
	timeout time.Duration,
	emit func(string),
) RolloutResult {
	if client == nil {
		return RolloutResult{Succeeded: false, Reason: "kubernetes is not configured"}
	}
	deadline := time.Now().Add(timeout)
	seen := make(map[string]bool) // event UID + count, so a repeating event is reported once per occurrence
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		podNames := emitNewPodEvents(ctx, client, name, namespace, seen, emit)

		dep, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			return RolloutResult{Reason: fmt.Sprintf("read deployment: %v", err)}
		}
		if err == nil {
			desired := int32(1)
			if dep.Spec.Replicas != nil {
				desired = *dep.Spec.Replicas
			}
			if dep.Status.UpdatedReplicas >= desired && dep.Status.AvailableReplicas >= desired {
				emit(fmt.Sprintf("Rollout complete — %d/%d replicas available.", dep.Status.AvailableReplicas, desired))
				return RolloutResult{Succeeded: true}
			}
			for _, c := range dep.Status.Conditions {
				if c.Type == appsv1.DeploymentProgressing &&
					c.Status == corev1.ConditionFalse &&
					c.Reason == "ProgressDeadlineExceeded" {
					emit("Rollout failed: the cluster gave up waiting for the new pods to become ready.")
					return RolloutResult{Reason: "progress deadline exceeded"}
				}
			}
		}

		// A container the cluster has already given up on. Reported immediately
		// rather than waiting out the timeout on a known-dead outcome.
		if reason, detail, fatal := terminalPodFailure(ctx, client, name, namespace, podNames); fatal {
			emit(fmt.Sprintf("Rollout failed: %s%s", reason, detail))
			return RolloutResult{Reason: reason}
		}

		if time.Now().After(deadline) {
			emit("Rollout did not become ready within the timeout — it may still settle; check the pods tab.")
			return RolloutResult{Reason: "timed out waiting for replicas to become available"}
		}
		select {
		case <-ctx.Done():
			return RolloutResult{Reason: "cancelled"}
		case <-ticker.C:
		}
	}
}

// emitNewPodEvents reports events on the deployment's pods that have not been
// reported yet, oldest first, and returns the pod names it saw.
func emitNewPodEvents(
	ctx context.Context,
	client kubernetes.Interface,
	name, namespace string,
	seen map[string]bool,
	emit func(string),
) []string {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s,managed-by=meshploy", name),
	})
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(pods.Items))
	uids := make(map[string]bool, len(pods.Items))
	for _, p := range pods.Items {
		names = append(names, p.Name)
		uids[string(p.UID)] = true
	}
	if len(names) == 0 {
		return nil
	}

	events, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return names
	}
	var mine []corev1.Event
	for _, e := range events.Items {
		if uids[string(e.InvolvedObject.UID)] {
			mine = append(mine, e)
		}
	}
	sort.Slice(mine, func(i, j int) bool {
		return eventTime(mine[i]).Before(eventTime(mine[j]))
	})
	for _, e := range mine {
		key := fmt.Sprintf("%s/%d", e.UID, e.Count)
		if seen[key] {
			continue
		}
		seen[key] = true
		emit(fmt.Sprintf("%s: %s", e.Reason, strings.TrimSpace(e.Message)))
	}
	return names
}

func eventTime(e corev1.Event) time.Time {
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}

// terminalPodFailure reports a container stuck in a state the cluster will not
// resolve by itself, with the container's own message when it has one.
func terminalPodFailure(
	ctx context.Context,
	client kubernetes.Interface,
	name, namespace string,
	podNames []string,
) (reason, detail string, fatal bool) {
	if len(podNames) == 0 {
		return "", "", false
	}
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s,managed-by=meshploy", name),
	})
	if err != nil {
		return "", "", false
	}
	for _, p := range pods.Items {
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Waiting == nil || !terminalWaitReasons[cs.State.Waiting.Reason] {
				continue
			}
			msg := strings.TrimSpace(cs.State.Waiting.Message)
			// A crash loop says nothing useful itself; the reason the container
			// exited is in the previous run's termination state.
			if t := cs.LastTerminationState.Terminated; t != nil {
				extra := strings.TrimSpace(t.Message)
				if extra == "" {
					extra = fmt.Sprintf("exit code %d", t.ExitCode)
				}
				msg = strings.TrimSpace(msg + " " + extra)
			}
			if msg != "" {
				return cs.State.Waiting.Reason, " — " + msg, true
			}
			return cs.State.Waiting.Reason, "", true
		}
	}
	return "", "", false
}
