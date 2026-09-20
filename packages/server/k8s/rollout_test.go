package k8s

import (
	"context"
	"strings"
	"testing"

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

	got := rolloutPodSelector(context.Background(), client, "api", "proj")
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
	got := rolloutPodSelector(context.Background(), client, "api", "proj")
	if strings.Contains(got, "ccc333") {
		t.Errorf("selector = %q, want another deployment's ReplicaSet left out", got)
	}
}

// Reporting a superset of the pods is worse than reporting none, so where the
// revision cannot be identified the watcher falls back to the service's own
// pods rather than guessing.
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
	} {
		client := tc.client
		got := rolloutPodSelector(context.Background(), client, "api", "proj")
		if got != "app=api,managed-by=meshploy" {
			t.Errorf("%s: selector = %q", tc.name, got)
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
