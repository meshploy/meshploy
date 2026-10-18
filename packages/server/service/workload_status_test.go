package service

import (
	"testing"

	db "github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
)

// A service whose rollout finished and whose pod then started crashing is
// failed, not deploying: Kubernetes never marks a finished rollout as stalled,
// so the pods are what say it will not come up by itself.
func TestDeriveServiceStatus(t *testing.T) {
	waiting := appk8s.DeploymentState{Exists: true, Replicas: 1}
	cases := []struct {
		name  string
		state appk8s.DeploymentState
		stuck bool
		want  db.ServiceStatus
	}{
		{"no deployment", appk8s.DeploymentState{}, false, db.ServiceFailed},
		{"scaled down", appk8s.DeploymentState{Exists: true}, false, db.ServiceStopped},
		{"a pod ready", appk8s.DeploymentState{Exists: true, Replicas: 1, Available: 1}, false, db.ServiceRunning},
		{"a pod ready beside a crashing one", appk8s.DeploymentState{Exists: true, Replicas: 2, Available: 1}, true, db.ServiceRunning},
		{"rollout stalled", appk8s.DeploymentState{Exists: true, Replicas: 1, Stalled: true}, false, db.ServiceFailed},
		{"crashing after it rolled out", waiting, true, db.ServiceFailed},
		{"still starting", waiting, false, db.ServiceDeploying},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveServiceStatus(tc.state, tc.stuck); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Stuck is Kubernetes backing off. A pod pulling, starting, or whose first pull
// failed (a network blip gives that too) is not.
func TestPodsStuck(t *testing.T) {
	for reason, want := range map[string]bool{
		"CrashLoopBackOff":           true,
		"ImagePullBackOff":           true,
		"InvalidImageName":           true,
		"CreateContainerConfigError": true,
		"ContainerCreating":          false,
		"ErrImagePull":               false,
		"":                           false,
	} {
		pods := []appk8s.PodInfo{{App: "api"}, {App: "api", WaitingReason: reason}}
		if got := podsStuck(pods); got != want {
			t.Errorf("%q: got %v, want %v", reason, got, want)
		}
	}
}
