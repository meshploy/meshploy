package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Where everything runs: each node with the room Kubernetes leaves for
// workloads and what its pods ask for, and each service of the organization,
// at every level, with its pods, what one asks for, the node it is pinned to
// and the nodes holding its data. Read in one pass over the cluster, for the
// maps and for what-ifs, so no client asks pod by pod.

type PlacementNode struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	K8sName      string    `json:"k8s_node_name"`
	Online       bool      `json:"online"`
	ControlPlane bool      `json:"control_plane"`
	// TakesWorkloads is a node the scheduler may put a service on: in the
	// cluster, online, and not kept for builds or the mesh alone.
	TakesWorkloads bool `json:"takes_workloads"`
	// What Kubernetes leaves for pods, after its own reserve.
	AllocatableCPU    int64 `json:"allocatable_cpu_millis"`
	AllocatableMemory int64 `json:"allocatable_memory_bytes"`
	// What the pods on it ask for, every pod, the system's included.
	RequestedCPU    int64 `json:"requested_cpu_millis"`
	RequestedMemory int64 `json:"requested_memory_bytes"`
}

type PlacementPod struct {
	Name  string `json:"name"`
	Node  string `json:"node"`
	Phase string `json:"phase"`
}

type PlacedService struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	LevelID     uuid.UUID `json:"level_id"`
	LevelName   string    `json:"level_name"`
	ProjectID   uuid.UUID `json:"project_id"`
	ProjectName string    `json:"project_name"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	RunOnce     bool      `json:"run_once,omitempty"`
	// PinnedNode is the cluster node it is held to, when it is.
	PinnedNode string `json:"pinned_node,omitempty"`
	// What one pod asks for, from its spec as the cluster has it.
	CPURequest    int64          `json:"cpu_request_millis"`
	MemoryRequest int64          `json:"memory_request_bytes"`
	Pods          []PlacementPod `json:"pods"`
	// DataOn is the nodes its volumes' data is bound to: with local storage a
	// pod cannot leave them.
	DataOn []string `json:"data_on,omitempty"`
}

type Placement struct {
	Nodes    []PlacementNode `json:"nodes"`
	Services []PlacedService `json:"services"`
}

type PlacementService struct {
	db        *gorm.DB
	k8s       kubernetes.Interface
	workloads *WorkloadService
}

// hostnameKey is the label a local volume's node affinity names its node by.
const hostnameKey = "kubernetes.io/hostname"

func (s *PlacementService) Get(ctx context.Context, orgID uuid.UUID) (*Placement, error) {
	out := &Placement{Nodes: []PlacementNode{}, Services: []PlacedService{}}
	var nodes []db.Node
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&nodes).Error; err != nil {
		return nil, err
	}
	var projects []db.Project
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&projects).Error; err != nil {
		return nil, err
	}
	if s.k8s == nil {
		for _, n := range nodes {
			out.Nodes = append(out.Nodes, PlacementNode{ID: n.ID, Name: n.Name, Online: n.Status == db.NodeOnline, ControlPlane: n.K3sRole == db.K3sRoleServer})
		}
		return out, nil
	}

	kNodes, err := s.k8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list cluster nodes: %w", err)
	}
	pods, err := s.k8s.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	pvs, err := s.k8s.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}

	// A claim's data is on the node its volume is bound to.
	claimNode := map[string]string{} // namespace/claim -> node
	for _, pv := range pvs.Items {
		if pv.Spec.ClaimRef == nil || pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil {
			continue
		}
		for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
			for _, e := range term.MatchExpressions {
				if e.Key == hostnameKey && len(e.Values) == 1 {
					claimNode[pv.Spec.ClaimRef.Namespace+"/"+pv.Spec.ClaimRef.Name] = e.Values[0]
				}
			}
		}
	}

	// Every running pod counts against its node; Meshploy's are indexed by
	// namespace and workload.
	requested := map[string][2]int64{}
	byWorkload := map[string][]corev1.Pod{}
	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed || p.DeletionTimestamp != nil {
			continue
		}
		cpu, mem := podRequests(&p)
		r := requested[p.Spec.NodeName]
		requested[p.Spec.NodeName] = [2]int64{r[0] + cpu, r[1] + mem}
		if p.Labels["managed-by"] == "meshploy" && p.Labels["app"] != "" {
			key := p.Namespace + "/" + p.Labels["app"]
			byWorkload[key] = append(byWorkload[key], p)
		}
	}

	// A Meshploy node is the cluster node on its mesh address, or of its name.
	byIP := map[string]corev1.Node{}
	byName := map[string]corev1.Node{}
	for _, kn := range kNodes.Items {
		byName[kn.Name] = kn
		for _, a := range kn.Status.Addresses {
			if a.Type == corev1.NodeInternalIP {
				byIP[a.Address] = kn
			}
		}
	}
	k8sName := map[uuid.UUID]string{}
	for _, n := range nodes {
		kn, ok := byIP[n.TailscaleIP]
		if !ok {
			kn, ok = byName[n.Name]
		}
		pn := PlacementNode{ID: n.ID, Name: n.Name, Online: n.Status == db.NodeOnline, ControlPlane: n.K3sRole == db.K3sRoleServer}
		if ok {
			pn.K8sName = kn.Name
			k8sName[n.ID] = kn.Name
			pn.AllocatableCPU = kn.Status.Allocatable.Cpu().MilliValue()
			pn.AllocatableMemory = kn.Status.Allocatable.Memory().Value()
			r := requested[kn.Name]
			pn.RequestedCPU, pn.RequestedMemory = r[0], r[1]
			pn.TakesWorkloads = pn.Online && nodeReady(kn) && !kn.Spec.Unschedulable &&
				n.MeshRole != db.MeshRoleBuilder && n.MeshRole != db.MeshRoleMesh
		}
		out.Nodes = append(out.Nodes, pn)
	}

	// Each level's services, named as their production project and level.
	byID := map[uuid.UUID]db.Project{}
	for _, p := range projects {
		byID[p.ID] = p
	}
	for _, level := range projects {
		root := level
		if level.ParentProjectID != nil {
			if r, ok := byID[*level.ParentProjectID]; ok {
				root = r
			}
		}
		var services []db.Service
		if err := s.db.WithContext(ctx).Preload("DatabaseConfig").Where("project_id = ?", level.ID).Find(&services).Error; err != nil {
			return nil, err
		}
		for i := range services {
			svc := &services[i]
			ps := PlacedService{
				ID: svc.ID, Name: svc.Name, LevelID: level.ID, LevelName: level.EnvName, ProjectID: root.ID, ProjectName: root.Name,
				Type: string(svc.Type), Status: string(svc.Status), RunOnce: svc.RunOnce, Pods: []PlacementPod{},
			}
			if ps.LevelName == "" {
				ps.LevelName = "production"
			}
			if svc.NodeID != nil {
				ps.PinnedNode = k8sName[*svc.NodeID]
			}
			data := map[string]bool{}
			for _, p := range byWorkload[level.Slug+"/"+s.workloads.k8sName(ctx, svc)] {
				ps.Pods = append(ps.Pods, PlacementPod{Name: p.Name, Node: p.Spec.NodeName, Phase: string(p.Status.Phase)})
				if ps.CPURequest == 0 && ps.MemoryRequest == 0 {
					ps.CPURequest, ps.MemoryRequest = podRequests(&p)
				}
				for _, v := range p.Spec.Volumes {
					if v.PersistentVolumeClaim == nil {
						continue
					}
					if node := claimNode[p.Namespace+"/"+v.PersistentVolumeClaim.ClaimName]; node != "" {
						data[node] = true
					}
				}
			}
			for n := range data {
				ps.DataOn = append(ps.DataOn, n)
			}
			sort.Strings(ps.DataOn)
			out.Services = append(out.Services, ps)
		}
	}
	return out, nil
}

// podRequests is what a pod's containers ask for, in millicores and bytes.
func podRequests(p *corev1.Pod) (cpu, mem int64) {
	for _, c := range p.Spec.Containers {
		cpu += c.Resources.Requests.Cpu().MilliValue()
		mem += c.Resources.Requests.Memory().Value()
	}
	return cpu, mem
}

func nodeReady(n corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
