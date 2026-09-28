package service_test

import (
	"context"
	"testing"

	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
)

// A joining node installs the k3s release the server runs, never the day's
// stable, which may be newer than its control plane.
func TestAJoiningNodeIsGivenTheServersK3sRelease(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	assert.Empty(t, e.svcs.Nodes.ClusterVersion(ctx), "no cluster, nothing to match")

	client := fake.NewSimpleClientset()
	disc := client.Discovery().(*fakediscovery.FakeDiscovery)
	service.UseNodeK8sForTest(e.svcs, client)

	disc.FakedServerVersion = &version.Info{GitVersion: "v1.33.4+k3s1"}
	assert.Equal(t, "v1.33.4+k3s1", e.svcs.Nodes.ClusterVersion(ctx))

	// Not a k3s release: the installer is left to choose, as before.
	disc.FakedServerVersion = &version.Info{GitVersion: "v1.33.4; rm -rf /"}
	assert.Empty(t, e.svcs.Nodes.ClusterVersion(ctx))
}
