package k8s

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A workload with a volume is replaced rather than rolled: a rolling update
// starts the new pod first, and with one replica Kubernetes keeps the old pod
// until the new one is ready, so anything holding a single-writer file on that
// volume deadlocks. A workload without one keeps the rolling update.
func TestDeploymentStrategyFollowsVolumes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params WorkloadParams
		want   appsv1.DeploymentStrategyType
	}{
		{
			name:   "with a volume",
			params: WorkloadParams{Name: "keycloak", Namespace: "proj", Image: "keycloak:26.0", VolumeMounts: []VolumeAttachment{{PVCName: "vol-1ef649", MountPath: "/opt/keycloak/data"}}},
			want:   appsv1.RecreateDeploymentStrategyType,
		},
		{
			name:   "without one",
			params: WorkloadParams{Name: "web", Namespace: "proj", Image: "nginx"},
			want:   appsv1.RollingUpdateDeploymentStrategyType,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			if err := ApplyDeployment(t.Context(), client, tc.params); err != nil {
				t.Fatalf("apply: %v", err)
			}
			got, err := client.AppsV1().Deployments(tc.params.Namespace).Get(t.Context(), tc.params.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Spec.Strategy.Type != tc.want {
				t.Errorf("strategy is %q, want %q", got.Spec.Strategy.Type, tc.want)
			}
		})
	}
}
