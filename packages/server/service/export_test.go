package service

import (
	"context"
	appk8s "github.com/meshploy/packages/server/k8s"
	"net"
	"time"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/dnsprovider"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

// UseHostFolderForTest stands f in for the cluster agent's host folder, as if
// the agent were registered and carried for the API.
func UseHostFolderForTest(s *Services, f hostagent.Folder) {
	s.System.hostFolder = f
	s.Deployments.host = f
	s.TCPRoutes.host = f
}

// UseK8sForTest gives the deployment service a cluster client, so tests in
// service_test can drive a deploy against a fake one. Compiled only into tests.
func UseK8sForTest(s *Services, client kubernetes.Interface) {
	s.Deployments.k8s = client
	s.Deployments.cluster = appk8s.NewLocalDriver(client, nil)
}

// UseJobsK8sForTest gives the job service a cluster client, so a test can see
// the schedules it writes there.
func UseJobsK8sForTest(s *Services, client kubernetes.Interface) {
	s.Jobs.k8s = client
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
	_, err := s.Deployments.requestRegistryGC(ctx, now, false)
	return err
}

// RequestRegistryGCNowForTest asks for it at once, as freeing space does.
func RequestRegistryGCNowForTest(s *Services, ctx context.Context, hostDir string) (bool, error) {
	s.Deployments.cfg = &config.Config{HostDir: hostDir, BuiltinRegistryEndpoint: "100.64.0.1:5000"}
	defer func() { s.Deployments.cfg = nil }()
	return s.Deployments.RequestRegistryGCNow(ctx)
}

// UseNodeK8sForTest gives the node service a cluster client.
func UseNodeK8sForTest(s *Services, client kubernetes.Interface) {
	s.Nodes.k8s = client
	s.Nodes.cluster = appk8s.NewLocalDriver(client, nil)
}

// UsePlacementK8sForTest gives the placement service a cluster client.
func UsePlacementK8sForTest(s *Services, client kubernetes.Interface) {
	s.Placement.k8s = client
}

// PruneAbandonedAppsForTest runs one pass of the abandoned GitHub App reaper
// against cutoff, without waiting for its hour.
func PruneAbandonedAppsForTest(s *Services, cutoff time.Time) {
	s.GitIntegrations.pruneAbandonedApps(context.Background(), cutoff)
}

// AppName exposes the GitHub App name builder to tests.
var AppName = appName

// UseProjectConsolePrefixForTest registers a project console prefix for one
// test, and takes it away again when the test ends.
func UseProjectConsolePrefixForTest(t interface{ Cleanup(func()) }, prefix string) {
	before := projectConsolePrefixes
	RegisterProjectConsolePrefix(prefix)
	t.Cleanup(func() { projectConsolePrefixes = before })
}

// NewIsolationForTest is the isolation service over a given database and
// cluster.
func NewIsolationForTest(gdb *gorm.DB, client kubernetes.Interface) *IsolationService {
	return &IsolationService{db: gdb, k8s: client}
}

// NewDNSProvidersForTest is the DNS provider service with its clients built by
// open, so a test can stand in for the provider.
func NewDNSProvidersForTest(gdb *gorm.DB, open func(kind dnsprovider.Kind, token string) (dnsprovider.Provider, error)) *DNSProviderService {
	return &DNSProviderService{db: gdb, open: open}
}

// NewSandboxForTest is the sandbox service over a given database and cluster.
func NewSandboxForTest(gdb *gorm.DB, client kubernetes.Interface) *SandboxService {
	return &SandboxService{db: gdb, k8s: client}
}

// UseSandboxRuleForTest registers a sandbox rule for one test.
func UseSandboxRuleForTest(t interface{ Cleanup(func()) }, rule SandboxRule) {
	before := sandboxRules
	RegisterSandboxRule(rule)
	t.Cleanup(func() { sandboxRules = before })
}

// UseDNSProvidersForTest makes the services' DNS provider clients come from
// open, and the gateway's public address publicIP.
func UseDNSProvidersForTest(svc *Services, open func(kind dnsprovider.Kind, token string) (dnsprovider.Provider, error), publicIP string) {
	svc.DNSProviders.open = open
	svc.Domains.publicIP = publicIP
}

// UseDNSForTest answers custom hostname verification's lookups from maps, so
// a test needs no records in real DNS. Returns a func that puts DNS back.
func UseDNSForTest(cnames map[string]string, txts map[string][]string) func() {
	oldCNAME, oldTXT := lookupCNAME, lookupTXT
	lookupCNAME = func(host string) (string, error) {
		if c, ok := cnames[host]; ok {
			return c, nil
		}
		return host + ".", nil
	}
	lookupTXT = func(name string) ([]string, error) { return txts[name], nil }
	return func() { lookupCNAME, lookupTXT = oldCNAME, oldTXT }
}

// ServiceEnvForTest exposes what a service's containers are given.
func ServiceEnvForTest(ctx context.Context, s *Services, svc *meshdb.Service) map[string]string {
	out := map[string]string{}
	for _, e := range s.Deployments.serviceEnv(ctx, svc, 3000, uuid.Nil) {
		out[e.Name] = e.Value
	}
	return out
}

// UseEdgeForTest gives the gateway the public address publicIP and has the
// edge name resolve to addrs, forgetting any earlier lookup; the returned
// function puts the resolver back.
func UseEdgeForTest(svc *Services, publicIP string, addrs map[string][]string) func() {
	svc.Domains.publicIP = publicIP
	old := lookupEdge
	lookupEdge = func(_ context.Context, name string) ([]string, error) {
		if a, ok := addrs[name]; ok {
			return a, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
	edgeChecked.Lock()
	edgeChecked.name = ""
	edgeChecked.Unlock()
	return func() { lookupEdge = old }
}

// UseClusterAgentForTest puts the services in the mode where the cluster is
// reached only through a cluster agent, as config ClusterAgent does.
func UseClusterAgentForTest(s *Services) *appk8s.AgentCluster {
	a, err := appk8s.NewAgentCluster()
	if err != nil {
		panic(err)
	}
	s.Agent, s.Cluster = a, a
	s.Nodes.agent = a
	return a
}
