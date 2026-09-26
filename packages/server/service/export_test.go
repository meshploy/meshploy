package service

import (
	"context"

	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"
)

// UseK8sForTest gives the deployment service a cluster client, so tests in
// service_test can drive a deploy against a fake one. Compiled only into tests.
func UseK8sForTest(s *Services, client kubernetes.Interface) {
	s.Deployments.k8s = client
}

// CommitFromBuildLog exposes the build-log parser to tests.
var CommitFromBuildLog = commitFromBuildLog

// UseWorkloadK8sForTest gives the workload service a cluster client, so a test
// can see what deleting through it removes from the cluster.
func UseWorkloadK8sForTest(s *Services, client kubernetes.Interface) {
	s.Workloads.k8s = client
}

// UseJobK8sForTest gives the job service a cluster client, so a test can
// trigger a run.
func UseJobK8sForTest(s *Services, client kubernetes.Interface) {
	s.Jobs.k8s = client
}

// UseNodeMetricsForTest replaces how the overview reads a node's metrics.
func UseNodeMetricsForTest(s *Services, read func(ctx context.Context, nodeID uuid.UUID) (*NodeMetrics, error)) {
	s.Overview.metrics = read
}

// UseOrphansK8sForTest gives the orphan check a cluster client to compare with.
func UseOrphansK8sForTest(s *Services, client kubernetes.Interface) {
	s.Orphans.k8s = client
}

// UseWorkloadsK8sForTroubleTest is UseWorkloadK8sForTest under the name the
// health tests read best with.
var UseWorkloadsK8sForTroubleTest = UseWorkloadK8sForTest
