package k8s

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

const depUID = types.UID("dep-1")

func deployment(revision string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api", Namespace: "proj", UID: depUID,
			Annotations: map[string]string{"deployment.kubernetes.io/revision": revision},
		},
	}
}

func replicaSet(name, revision, hash string, owner types.UID) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "proj",
			Labels: map[string]string{
				"app": "api", "managed-by": "meshploy", "pod-template-hash": hash,
			},
			Annotations:     map[string]string{"deployment.kubernetes.io/revision": revision},
			OwnerReferences: []metav1.OwnerReference{{UID: owner}},
		},
	}
}

// The bug this exists for: a previous revision left in CrashLoopBackOff still
// matches the deployment's labels, so its BackOff events and its dead container
// were reported as the new rollout's - and every later deploy failed until the
// service was stopped by hand.
func TestTheWatcherOnlyLooksAtTheRevisionBeingRolledOut(t *testing.T) {
	client := fake.NewSimpleClientset(
		deployment("2"),
		replicaSet("api-old", "1", "aaa111", depUID),
		replicaSet("api-new", "2", "bbb222", depUID),
	)

	got, exact := rolloutPodSelector(context.Background(), client, "api", "proj")
	if !exact {
		t.Fatal("the new revision is known, so the selector is exact")
	}
	if !strings.Contains(got, "pod-template-hash=bbb222") {
		t.Fatalf("selector = %q, want the new revision's hash", got)
	}
	if strings.Contains(got, "aaa111") {
		t.Errorf("selector = %q, want the old revision left out", got)
	}
	// Still scoped to this service, not the whole namespace.
	if !strings.Contains(got, "app=api,managed-by=meshploy") {
		t.Errorf("selector = %q", got)
	}
}

// A ReplicaSet of the same revision that belongs to a different deployment is
// somebody else's: two services in a project can both be on revision 2.
func TestAReplicaSetOfAnotherDeploymentIsIgnored(t *testing.T) {
	client := fake.NewSimpleClientset(
		deployment("2"),
		replicaSet("other-app", "2", "ccc333", types.UID("dep-2")),
	)
	got, _ := rolloutPodSelector(context.Background(), client, "api", "proj")
	if strings.Contains(got, "ccc333") {
		t.Errorf("selector = %q, want another deployment's ReplicaSet left out", got)
	}
}

// Reporting a superset of the pods is worse than reporting none, so where the
// revision cannot be identified the selector says it is not exact, and the
// watcher judges no pod until it is.
func TestTheSelectorFallsBackWhenTheRevisionIsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name   string
		client *fake.Clientset
	}{
		{"no deployment", fake.NewSimpleClientset()},
		{"no revision annotation", fake.NewSimpleClientset(&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "proj", UID: depUID},
		})},
		{"no matching replicaset", fake.NewSimpleClientset(deployment("2"))},
		// Just applied: the controller has not seen the new spec, and the
		// annotation still names the revision being replaced.
		{"new spec not yet observed", fake.NewSimpleClientset(func() *appsv1.Deployment {
			d := deployment("1")
			d.Generation, d.Status.ObservedGeneration = 2, 1
			return d
		}(), replicaSet("api-old", "1", "aaa111", depUID))},
	} {
		client := tc.client
		got, exact := rolloutPodSelector(context.Background(), client, "api", "proj")
		if got != "app=api,managed-by=meshploy" || exact {
			t.Errorf("%s: selector = %q, exact = %v", tc.name, got, exact)
		}
	}
}

// Events are reported for the pods the selector names, and only those.
func TestOnlyTheSelectedPodsEvents(t *testing.T) {
	newPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "api-new-1", Namespace: "proj", UID: "pod-new",
		Labels: map[string]string{"app": "api", "managed-by": "meshploy", "pod-template-hash": "bbb222"},
	}}
	oldPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "api-old-1", Namespace: "proj", UID: "pod-old",
		Labels: map[string]string{"app": "api", "managed-by": "meshploy", "pod-template-hash": "aaa111"},
	}}
	client := fake.NewSimpleClientset(
		newPod, oldPod,
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "e1", Namespace: "proj", UID: "ev-1"},
			InvolvedObject: corev1.ObjectReference{UID: "pod-old"},
			Reason:         "BackOff", Message: "Back-off restarting failed container",
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "e2", Namespace: "proj", UID: "ev-2"},
			InvolvedObject: corev1.ObjectReference{UID: "pod-new"},
			Reason:         "Pulling", Message: "Pulling image",
		},
	)

	var lines []string
	names := emitNewPodEvents(context.Background(), client,
		"app=api,managed-by=meshploy,pod-template-hash=bbb222", "proj",
		map[string]bool{}, func(s string) { lines = append(lines, s) })

	if len(names) != 1 || names[0] != "api-new-1" {
		t.Fatalf("pods = %v", names)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Pulling") {
		t.Errorf("lines = %v", lines)
	}
	if strings.Contains(joined, "Back-off") {
		t.Errorf("the old revision's failure was reported as this rollout's: %v", lines)
	}
}

// The docai-backend deploy of 2026-09-29: the old revision's pod crash-looped
// while the new one was still pulling its image, and the rollout was failed
// on the old pod. Only the pods the watcher selected are judged.
func TestAnOldRevisionsCrashLoopDoesNotFailTheRollout(t *testing.T) {
	crashing := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api-old-1", Namespace: "proj",
			Labels: map[string]string{"app": "api", "managed-by": "meshploy", "pod-template-hash": "aaa111"}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
		}}},
	}
	pulling := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api-new-1", Namespace: "proj",
			Labels: map[string]string{"app": "api", "managed-by": "meshploy", "pod-template-hash": "bbb222"}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}},
		}}},
	}
	client := fake.NewSimpleClientset(crashing, pulling)

	if reason, _, fatal := terminalPodFailure(context.Background(), client, "api", "proj", []string{"api-new-1"}); fatal {
		t.Fatalf("failed on the old revision's pod: %s", reason)
	}
	if _, _, fatal := terminalPodFailure(context.Background(), client, "api", "proj", []string{"api-old-1"}); !fatal {
		t.Error("a selected pod in CrashLoopBackOff is still a failure")
	}
}

// settledRollout is a deployment whose one replica the cluster reports as
// updated and available, with its ReplicaSet and pod.
func settledRollout(state corev1.ContainerState, restarts int32) *fake.Clientset {
	dep := deployment("1")
	one := int32(1)
	dep.Spec.Replicas = &one
	dep.Status.UpdatedReplicas, dep.Status.AvailableReplicas = 1, 1
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "proj",
			Labels: map[string]string{"app": "api", "managed-by": "meshploy", "pod-template-hash": "aaa111"}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{State: state, RestartCount: restarts}}},
	}
	return fake.NewSimpleClientset(dep, replicaSet("api-rs", "1", "aaa111", depUID), pod)
}

// Pods that stay up past the settle time make a successful rollout.
func TestARolloutSucceedsOnceItsPodsStayUp(t *testing.T) {
	defer func(d time.Duration) { RolloutSettle = d }(RolloutSettle)
	RolloutSettle = time.Second
	client := settledRollout(corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}, 2)

	var lines []string
	r := WatchRollout(context.Background(), client, "api", "proj", 30*time.Second, func(s string) { lines = append(lines, s) })
	if !r.Succeeded {
		t.Fatalf("rollout failed: %s", r.Reason)
	}
	// Restarts from before it came up do not count against it.
	if !strings.Contains(strings.Join(lines, "\n"), "checking they stay up") {
		t.Errorf("lines = %v", lines)
	}
}

// The procureflow API of 2026-09-29: with no readiness check, its container
// was available the moment it started and exited a second later on missing
// variables, and the deploy was recorded as a success. Available is not up.
func TestAnAvailablePodThatCrashesFailsTheRollout(t *testing.T) {
	defer func(d time.Duration) { RolloutSettle = d }(RolloutSettle)
	RolloutSettle = time.Second
	client := settledRollout(corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}, 1)

	r := WatchRollout(context.Background(), client, "api", "proj", 30*time.Second, func(string) {})
	if r.Succeeded {
		t.Fatal("a pod in CrashLoopBackOff made a successful rollout")
	}
	if r.Reason != "CrashLoopBackOff" {
		t.Errorf("reason = %q", r.Reason)
	}
}
