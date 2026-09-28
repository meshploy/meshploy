package service

import (
	"context"
	"time"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"

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

// RecordStackForTest keeps what a build log says about the app on its
// deployment, as the end of a build does.
func RecordStackForTest(s *Services, deploymentID uuid.UUID, log string) {
	s.Deployments.recordStack(context.Background(), deploymentID, log)
}

// ResolveRegistryForTest is the registry a build config's builds push to.
func ResolveRegistryForTest(s *Services, ctx context.Context, bc *meshdb.BuildConfig) (string, error) {
	host, _, _, err := s.Deployments.resolveRegistry(ctx, bc)
	return host, err
}

// FailDeploymentForTest and SucceedDeploymentForTest end a deployment the way
// a build or rollout does.
func FailDeploymentForTest(s *Services, id uuid.UUID, reason string) {
	s.Deployments.failDeployment(id, reason)
}

func SucceedDeploymentForTest(s *Services, ctx context.Context, id, serviceID uuid.UUID, image string) {
	s.Deployments.succeedDeployment(ctx, id, serviceID, "", image)
}

// PruneImagesForTest keeps a service's images to its limit, as after a build,
// and waits for it.
func PruneImagesForTest(s *Services, ctx context.Context, serviceID uuid.UUID) {
	var bc meshdb.BuildConfig
	if s.Deployments.db.Where("service_id = ?", serviceID).First(&bc).Error == nil {
		s.Deployments.pruneOldImages(ctx, serviceID, bc)
	}
}

// RequestRegistryGCForTest asks for the registry's garbage collection if it
// is due at now, on a gateway whose host agent reports into hostDir.
func RequestRegistryGCForTest(s *Services, ctx context.Context, hostDir string, now time.Time) error {
	s.Deployments.cfg = &config.Config{HostDir: hostDir, BuiltinRegistryEndpoint: "100.64.0.1:5000"}
	defer func() { s.Deployments.cfg = nil }()
	return s.Deployments.requestRegistryGC(ctx, now)
}
