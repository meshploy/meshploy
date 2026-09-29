package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func clusterNode(name, ip, cpu, mem string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Addresses:   []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: ip}},
			Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(mem)},
			Conditions:  []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	}
}

func appPod(ns, app, name, node, cpu, mem string, claim string) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": app, "managed-by": "meshploy"}},
		Spec: corev1.PodSpec{NodeName: node, Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(mem)}}}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	if claim != "" {
		p.Spec.Volumes = []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim}}}}
	}
	return p
}

// Taking a node out says, service by service, what the scheduler would do:
// keep running where it has other replicas, move where there is room, or stay
// down because it is pinned, its data is on that node, or nothing has room.
// Taking the gateway out says nothing would be moved at all.
func TestANodeDownForecastSaysWhatMovesAndWhatStaysDown(t *testing.T) {
	ctx := context.Background()
	e := newTCPEnv(t)
	worker := meshdb.Node{OrganizationID: e.org.ID, Name: "worker-1", TailscaleIP: "100.64.0.2", Status: "online", MeshRole: meshdb.MeshRoleWorkload}
	require.NoError(t, e.gdb.Create(&worker).Error)

	mk := func(name string, pinned bool) *meshdb.Service {
		in := service.CreateWorkloadInput{Name: name, Type: meshdb.ServiceTypeApplication, Image: "app:1"}
		if pinned {
			in.NodeID = &worker.ID
		}
		svc, err := e.svcs.Workloads.Create(ctx, e.project.ID, in)
		require.NoError(t, err)
		return svc
	}
	pinned, free, stateful, spread, big := mk("pinned", true), mk("free", false), mk("stateful", false), mk("spread", false), mk("big", false)
	ns := e.project.Slug
	client := fake.NewSimpleClientset(
		clusterNode("gateway", "100.64.0.1", "4", "8Gi"),
		clusterNode("worker-1", "100.64.0.2", "16", "32Gi"),
		appPod(ns, pinned.Slug, "pinned-1", "worker-1", "100m", "128Mi", ""),
		appPod(ns, free.Slug, "free-1", "worker-1", "500m", "512Mi", ""),
		appPod(ns, stateful.Slug, "stateful-1", "worker-1", "250m", "256Mi", "stateful-data"),
		appPod(ns, spread.Slug, "spread-1", "worker-1", "100m", "128Mi", ""),
		appPod(ns, spread.Slug, "spread-2", "gateway", "100m", "128Mi", ""),
		appPod(ns, big.Slug, "big-1", "worker-1", "8", "4Gi", ""),
		&corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "pv-stateful"},
			Spec: corev1.PersistentVolumeSpec{
				ClaimRef: &corev1.ObjectReference{Namespace: ns, Name: "stateful-data"},
				NodeAffinity: &corev1.VolumeNodeAffinity{Required: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "kubernetes.io/hostname", Operator: corev1.NodeSelectorOpIn, Values: []string{"worker-1"}}},
				}}}},
			},
		},
	)
	service.UsePlacementK8sForTest(e.svcs, client)

	p, err := e.svcs.Placement.Get(ctx, e.org.ID)
	require.NoError(t, err)
	byName := map[string]service.PlacedService{}
	for _, s := range p.Services {
		byName[s.Name] = s
	}
	assert.Equal(t, "worker-1", byName["pinned"].PinnedNode)
	assert.Equal(t, []string{"worker-1"}, byName["stateful"].DataOn)
	assert.Len(t, byName["spread"].Pods, 2)
	assert.Equal(t, int64(500), byName["free"].CPURequest, "from the pod's own spec")

	f, err := service.ForecastNodeDown(p, "worker-1")
	require.NoError(t, err)
	outcome := map[string]service.ServiceForecast{}
	for _, s := range f.Services {
		outcome[s.Service.Name] = s
	}
	assert.Equal(t, service.OutcomeDown, outcome["pinned"].Outcome)
	assert.Contains(t, outcome["pinned"].Reason, "pinned")
	assert.Equal(t, service.OutcomeMoves, outcome["free"].Outcome)
	assert.Equal(t, "gateway", outcome["free"].To)
	assert.Equal(t, service.OutcomeDown, outcome["stateful"].Outcome)
	assert.Contains(t, outcome["stateful"].Reason, "data")
	assert.Equal(t, service.OutcomeKeeps, outcome["spread"].Outcome)
	assert.Equal(t, []string{"gateway"}, outcome["spread"].On)
	assert.Equal(t, service.OutcomeDown, outcome["big"].Outcome)
	assert.Contains(t, outcome["big"].Reason, "room")

	g, err := service.ForecastNodeDown(p, "gateway")
	require.NoError(t, err)
	assert.True(t, g.ControlPlane)
	require.Len(t, g.Services, 1)
	assert.Equal(t, service.OutcomeKeeps, g.Services[0].Outcome, "spread keeps running on the worker")

	_, err = service.ForecastNodeDown(p, "nowhere")
	assert.Error(t, err)
}
