package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"github.com/meshploy/packages/server/service"
)

// NodeResponse extends db.Node with live data from Headscale and the K8s cluster.
// If Headscale or K8s is unavailable the extra fields are zeroed — the DB data
// is always returned.
type NodeResponse struct {
	db.Node

	// Headscale peer info
	HeadscaleID       string     `json:"headscale_id,omitempty"`
	HeadscaleOnline   bool       `json:"headscale_online"`
	HeadscaleLastSeen *time.Time `json:"headscale_last_seen,omitempty"`
	HeadscaleExpiry   *time.Time `json:"headscale_expiry,omitempty"`
	HeadscaleTags     []string   `json:"headscale_tags"`
	HeadscaleUser     string     `json:"headscale_user,omitempty"`
	// MagicDNS FQDN: {givenName}.mesh.{domain} — reachable from any node on the mesh
	HeadscaleFQDN string `json:"headscale_fqdn,omitempty"`

	// K8s cluster membership
	K8sMember   bool   `json:"k8s_member"`
	K8sReady    bool   `json:"k8s_ready"`
	K8sNodeName string `json:"k8s_node_name,omitempty"`

	// Namespaces (project slugs) with running pods on this node
	ActiveProjects []string `json:"active_projects"`
}

// enrichTimeout is the max time we wait for external enrichment sources.
// DB results are always returned; slow external calls are abandoned.
const enrichTimeout = 4 * time.Second

// enrichNodes fetches headscale nodes and k8s cluster nodes concurrently, then
// annotates each DB node. Errors from external sources are logged but never
// propagated — callers always get at minimum the DB data.
func (h *Handler) enrichNodes(ctx context.Context, nodes []db.Node) []NodeResponse {
	tctx, cancel := context.WithTimeout(ctx, enrichTimeout)
	defer cancel()

	type hsIndex struct{ node service.HeadscaleNode }
	type k8sIndex struct {
		name       string
		ready      bool
		Labels     map[string]string
		cpuCores   float32
		memoryGB   float32
		diskGB     float32
		k3sVersion string
	}

	hsByIP := make(map[string]hsIndex)
	k8sByIP := make(map[string]k8sIndex)
	k8sByName := make(map[string]k8sIndex)

	var wg sync.WaitGroup

	if h.svc.Headscale != nil {
		wg.Go(func() {
			// Single-node shortcut: when the node has a stored headscale_id use a
			// direct GET /api/v1/node/{id} (O(1)) instead of listing all peers.
			if len(nodes) == 1 && nodes[0].HeadscaleID != "" {
				hn, err := h.svc.Headscale.GetNode(tctx, nodes[0].HeadscaleID)
				if err != nil {
					log.Printf("warning: headscale GetNode(%s): %v", nodes[0].HeadscaleID, err)
					return
				}
				// Key by stored TailscaleIP so the match below always works,
				// even if Headscale has assigned a new IP after re-registration.
				hsByIP[nodes[0].TailscaleIP] = hsIndex{node: *hn}
				return
			}
			// Multiple nodes (or no stored ID): full list scan.
			hsNodes, err := h.svc.Headscale.ListNodes(tctx)
			if err != nil {
				log.Printf("warning: headscale ListNodes: %v", err)
				return
			}
			for _, hn := range hsNodes {
				if len(hn.IPAddresses) > 0 {
					hsByIP[hn.IPAddresses[0]] = hsIndex{node: hn}
				}
			}
		})
	}

	if h.svc.K8s != nil {
		wg.Go(func() {
			clusterNodes, err := appk8s.GetClusterNodes(tctx, h.svc.K8s)
			if err != nil {
				log.Printf("warning: k8s GetClusterNodes: %v", err)
				return
			}
			for _, cn := range clusterNodes {
				idx := k8sIndex{
					name:       cn.Name,
					ready:      cn.Ready,
					Labels:     cn.Labels,
					cpuCores:   cn.CPUCores,
					memoryGB:   cn.MemoryGB,
					diskGB:     cn.DiskGB,
					k3sVersion: cn.K3sVersion,
				}
				k8sByName[cn.Name] = idx
				for _, ip := range cn.InternalIPs {
					k8sByIP[ip] = idx
				}
			}
		})
	}

	wg.Wait()

	out := make([]NodeResponse, 0, len(nodes))
	for _, n := range nodes {
		r := NodeResponse{
			Node:           n,
			HeadscaleTags:  []string{},
			ActiveProjects: []string{},
		}

		if hs, ok := hsByIP[n.TailscaleIP]; ok {
			r.HeadscaleID = hs.node.ID
			r.HeadscaleOnline = hs.node.Online
			r.HeadscaleLastSeen = hs.node.LastSeen
			r.HeadscaleExpiry = hs.node.Expiry
			r.HeadscaleTags = hs.node.Tags()
			r.HeadscaleUser = hs.node.User.Name
			// MagicDNS FQDN matches headscale config: base_domain = mesh.{DOMAIN}
			if hs.node.GivenName != "" && h.cfg != nil && h.cfg.Domain != "" {
				r.HeadscaleFQDN = fmt.Sprintf("%s.mesh.%s", hs.node.GivenName, h.cfg.Domain)
			}
			// Lazy backfill: store the headscale_id so future requests can use the
			// direct GET /api/v1/node/{id} shortcut instead of a full list scan.
			if n.HeadscaleID == "" && h.svc != nil {
				go func(nID uuid.UUID, hsID string) {
					if err := h.svc.Nodes.SetHeadscaleID(context.Background(), nID, hsID); err != nil {
						log.Printf("warning: backfill headscale_id for node %s: %v", nID, err)
					}
				}(n.ID, hs.node.ID)
			}
		}

		kn, ok := k8sByIP[n.TailscaleIP]
		if !ok {
			// k3s may report the host's real NIC IP rather than the WireGuard IP.
			// Fall back to matching by node name.
			kn, ok = k8sByName[n.Name]
		}
		if ok {
			r.K8sMember = true
			r.K8sReady = kn.ready
			r.K8sNodeName = kn.name
			// Override hardware fields with live k8s capacity data.
			r.CPUCores = kn.cpuCores
			r.MemoryGB = kn.memoryGB
			r.DiskGB = kn.diskGB
			r.K3sVersion = kn.k3sVersion
			// Merge live K8s labels so callers can filter by meshploy.com/role etc.
			if len(kn.Labels) > 0 {
				labels := make(db.JSONObject, len(kn.Labels))
				for k, v := range kn.Labels {
					labels[k] = v
				}
				r.K3sLabels = labels
			}
			// Reflect live cluster readiness in the status field.
			if kn.ready {
				r.Status = db.NodeOnline
			} else {
				r.Status = db.NodeOffline
			}
			// Reconcile mesh_role labels: if the DB has a role set but the k8s node
			// doesn't have the expected label yet (e.g. node just joined k3s after
			// self-register), apply them async so the next request sees them.
			if n.MeshRole != "" && h.svc.K8s != nil && !meshRoleLabelsMatch(kn.Labels, n.MeshRole) {
				go func(name string, role db.MeshRole) {
					if err := appk8s.SetNodeMeshRole(context.Background(), h.svc.K8s, name, role); err != nil {
						log.Printf("warning: reconcile mesh role labels for %s: %v", name, err)
					}
				}(kn.name, n.MeshRole)
			}
		}

		out = append(out, r)
	}
	return out
}

// enrichNode enriches a single node and additionally populates ActiveProjects.
func (h *Handler) enrichNode(ctx context.Context, n *db.Node) NodeResponse {
	enriched := h.enrichNodes(ctx, []db.Node{*n})
	r := enriched[0]

	if h.svc.K8s != nil && r.K8sMember && r.K8sNodeName != "" {
		namespaces, err := appk8s.GetNamespacesOnNode(ctx, h.svc.K8s, r.K8sNodeName)
		if err != nil {
			log.Printf("warning: k8s GetNamespacesOnNode(%s): %v", r.K8sNodeName, err)
		} else if namespaces != nil {
			r.ActiveProjects = namespaces
		}
	}
	return r
}

// ─── Input / Output types ────────────────────────────────────────────────────

type ListNodesInput struct {
	OrgID string `path:"orgId"`
}

type ListNodesOutput struct {
	Body []NodeResponse
}

type NodePathInput struct {
	OrgID  string `path:"orgId"`
	NodeID string `path:"nodeId"`
}

type GetNodeOutput struct {
	Body *NodeResponse
}

type RegisterNodeInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Name        string `json:"name" minLength:"1" maxLength:"100"`
		TailscaleIP string `json:"tailscale_ip"`
	}
}

type RegisterNodeOutput struct {
	Body *NodeResponse
}

type UpdateNodeInput struct {
	OrgID  string `path:"orgId"`
	NodeID string `path:"nodeId"`
	Body   struct {
		Name     string `json:"name,omitempty"      maxLength:"100"`
		K3sRole  string `json:"k3s_role,omitempty"  enum:"server,agent"`
		MeshRole string `json:"mesh_role,omitempty" enum:"workload_builder,workload,builder,mesh"`
	}
}

type UpdateNodeOutput struct {
	Body *NodeResponse
}

// ─── Route registration ──────────────────────────────────────────────────────

func (h *Handler) registerNodeRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-nodes",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/nodes",
		Summary:     "List nodes in an organization",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListNodes)

	huma.Register(api, huma.Operation{
		OperationID: "register-node",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/nodes",
		Summary:     "Register a new node",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.RegisterNode)

	huma.Register(api, huma.Operation{
		OperationID: "get-node",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/nodes/{nodeId}",
		Summary:     "Get a node",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetNode)

	huma.Register(api, huma.Operation{
		OperationID: "update-node",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/nodes/{nodeId}",
		Summary:     "Update a node",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateNode)

	huma.Register(api, huma.Operation{
		OperationID: "delete-node",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/nodes/{nodeId}",
		Summary:     "Remove a node",
		Description: "Removes the node from Headscale, the cluster and Meshploy, in that order. 200 when it is gone; 202 while Headscale has not confirmed the peer is removed, which Meshploy retries every minute.",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteNode)

	huma.Register(api, huma.Operation{
		OperationID:   "cancel-node-removal",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/nodes/{nodeId}/cancel-removal",
		Summary:       "Stop a node removal that is waiting for Headscale",
		Tags:          []string{"Nodes"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.CancelNodeRemoval)

	// Provisioning tokens — per-node single-use tokens (authenticated management)
	huma.Register(api, huma.Operation{
		OperationID: "create-provisioning-token",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/node-provisioning-tokens",
		Summary:     "Create a single-use node provisioning token",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.CreateProvisioningToken)

	// Node registration token — authenticated management endpoints
	huma.Register(api, huma.Operation{
		OperationID: "get-node-registration-token",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/node-registration-token",
		Summary:     "Get the node registration token",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetNodeRegistrationToken)

	huma.Register(api, huma.Operation{
		OperationID: "generate-node-registration-token",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/node-registration-token",
		Summary:     "Generate (or rotate) the node registration token",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GenerateNodeRegistrationToken)

	// Unauthenticated — called by the worker install script over the mesh
	huma.Register(api, huma.Operation{
		OperationID: "self-register-node",
		Method:      "POST",
		Path:        "/api/v1/nodes/self-register",
		Summary:     "Self-register a node using a registration token",
		Tags:        []string{"Nodes"},
	}, h.SelfRegisterNode)

	// Unauthenticated — called by the worker uninstall script over the mesh
	huma.Register(api, huma.Operation{
		OperationID: "self-deregister-node",
		Method:      "DELETE",
		Path:        "/api/v1/nodes/self-deregister",
		Summary:     "Self-deregister a node using its registration token and node ID",
		Tags:        []string{"Nodes"},
	}, h.SelfDeregisterNode)

	// K3s cluster join token — org-scoped, admin-only. Hands out a credential that
	// lets a machine join the k3s cluster, so it is gated like its siblings
	// GetNodeRegistrationToken and CreateProvisioningToken.
	huma.Register(api, huma.Operation{
		OperationID: "get-cluster-join-token",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/cluster/join-token",
		Summary:     "Get the k3s node token for joining the cluster",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetClusterJoinToken)

	huma.Register(api, huma.Operation{
		OperationID: "provision-node",
		Method:      "POST",
		Path:        "/api/v1/nodes/provision",
		Summary:     "Exchange a provisioning token for what a machine needs to join the mesh",
		Description: "Public by necessity: the machine has no session and no mesh address yet. " +
			"Returns only the mesh credentials; everything else is asked for from inside the mesh.",
		Tags:     []string{"Nodes"},
		Security: []map[string][]string{},
	}, h.ProvisionNode)

	// Headscale preauth key — org-scoped, admin-only. Get the most recent active
	// key, or generate a new one.
	huma.Register(api, huma.Operation{
		OperationID: "get-headscale-preauth-key",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/cluster/headscale-preauth-key",
		Summary:     "Get the most recent active Headscale preauth key",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetHeadscalePreAuthKey)

	huma.Register(api, huma.Operation{
		OperationID: "create-headscale-preauth-key",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/cluster/headscale-preauth-key",
		Summary:     "Generate a new Headscale preauth key for joining the WireGuard mesh",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.CreateHeadscalePreAuthKey)

	// Orphaned workloads — org-scoped, admin-only. Read-only by design: it
	// reports drift between the database and the cluster, it does not act on it.
	huma.Register(api, huma.Operation{
		OperationID: "list-orphan-workloads",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/cluster/orphans",
		Summary:     "List cluster workloads that no service owns",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListOrphanWorkloads)

	// Removing one orphan — org-scoped, admin-only. A per-item action a human
	// takes, not a sweep: the operator has seen what it is and chosen it.
	huma.Register(api, huma.Operation{
		OperationID: "delete-orphan-workload",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/cluster/orphans/{namespace}/{name}",
		Summary:     "Remove a cluster workload that no service owns",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteOrphanWorkload)

	// Mesh health — org-scoped, admin-only. Reports whether the API can still
	// talk to Headscale, so a dead credential shows up in the UI instead of
	// silently freezing node liveness.
	huma.Register(api, huma.Operation{
		OperationID: "get-mesh-health",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/cluster/mesh-health",
		Summary:     "Report whether the control plane can reach Headscale",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetMeshHealth)

	huma.Register(api, huma.Operation{
		OperationID: "get-node-metrics",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/nodes/{nodeId}/metrics",
		Summary:     "Get live resource metrics for a node (requires node_exporter)",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetNodeMetrics)

	huma.Register(api, huma.Operation{
		OperationID: "list-node-containers",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/nodes/{nodeId}/containers",
		Summary:     "List containers running on a node that Meshploy does not manage",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListNodeContainers)
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func (h *Handler) ListNodes(ctx context.Context, input *ListNodesInput) (*ListNodesOutput, error) {
	_, orgID, _, err := h.checkOrgMemberAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	nodes, err := h.svc.Nodes.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return &ListNodesOutput{Body: h.enrichNodes(ctx, nodes)}, nil
}

func (h *Handler) RegisterNode(ctx context.Context, input *RegisterNodeInput) (*RegisterNodeOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	node, err := h.svc.Nodes.Register(ctx, orgID, input.Body.Name, input.Body.TailscaleIP)
	if err != nil {
		return nil, err
	}
	r := h.enrichNode(ctx, node)
	return &RegisterNodeOutput{Body: &r}, nil
}

func (h *Handler) GetNode(ctx context.Context, input *NodePathInput) (*GetNodeOutput, error) {
	_, _, nodeID, err := h.checkOrgMemberAccess(ctx, input.OrgID, input.NodeID)
	if err != nil {
		return nil, err
	}
	node, err := h.svc.Nodes.Get(ctx, nodeID)
	if err != nil {
		return nil, notFound(err)
	}
	r := h.enrichNode(ctx, node)
	return &GetNodeOutput{Body: &r}, nil
}

func (h *Handler) UpdateNode(ctx context.Context, input *UpdateNodeInput) (*UpdateNodeOutput, error) {
	_, _, nodeID, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.NodeID)
	if err != nil {
		return nil, err
	}
	// Fetch the current node so we know the stored headscale_id and old name.
	current, err := h.svc.Nodes.Get(ctx, nodeID)
	if err != nil {
		return nil, notFound(err)
	}
	node, err := h.svc.Nodes.Update(ctx, nodeID, service.UpdateNodeInput{
		Name:     input.Body.Name,
		K3sRole:  db.K3sRole(input.Body.K3sRole),
		MeshRole: db.MeshRole(input.Body.MeshRole),
	})
	if err != nil {
		if errors.Is(err, service.ErrMeshRoleSwitch) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, notFound(err)
	}
	// Keep Headscale MagicDNS in sync when the node is renamed.
	if input.Body.Name != "" && input.Body.Name != current.Name && h.svc.Headscale != nil && current.HeadscaleID != "" {
		if err := h.svc.Headscale.RenameNode(ctx, current.HeadscaleID, input.Body.Name); err != nil {
			log.Printf("warning: rename headscale peer %s → %s: %v", current.Name, input.Body.Name, err)
		}
	}
	// Apply k8s labels/taints if mesh_role changed and k8s client is available.
	if input.Body.MeshRole != "" && h.svc.K8s != nil {
		if err := appk8s.SetNodeMeshRole(ctx, h.svc.K8s, node.Name, node.MeshRole); err != nil {
			log.Printf("warning: apply mesh role to k8s node %s: %v", node.Name, err)
		}
	}
	r := h.enrichNode(ctx, node)
	return &UpdateNodeOutput{Body: &r}, nil
}

// DeleteNodeOutput reports whether the node is gone: 200 when it is, 202 while
// Headscale has not confirmed the peer is removed.
type DeleteNodeOutput struct {
	Status int
	Body   NodeRemovalBody
}

type NodeRemovalBody struct {
	Removed bool   `json:"removed"`
	Error   string `json:"error,omitempty" doc:"Why the removal is waiting; Meshploy retries every minute"`
}

func (h *Handler) DeleteNode(ctx context.Context, input *NodePathInput) (*DeleteNodeOutput, error) {
	_, _, nodeID, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.NodeID)
	if err != nil {
		return nil, err
	}
	node, err := h.svc.Nodes.Get(ctx, nodeID)
	if err != nil {
		return nil, notFound(err)
	}
	// The gateway/master node runs the control plane — deleting it would break
	// everything. Block it at the API level regardless of UI state.
	if node.K3sRole == db.K3sRoleServer {
		return nil, huma.Error400BadRequest("the gateway node cannot be deleted")
	}
	res, err := h.svc.Nodes.Remove(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	out := &DeleteNodeOutput{Status: http.StatusOK, Body: NodeRemovalBody{Removed: res.Removed, Error: res.Error}}
	if !res.Removed {
		out.Status = http.StatusAccepted
	}
	return out, nil
}

// CancelNodeRemoval stops a removal that is waiting for Headscale.
func (h *Handler) CancelNodeRemoval(ctx context.Context, input *NodePathInput) (*struct{}, error) {
	_, _, nodeID, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.NodeID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Nodes.CancelRemoval(ctx, nodeID); err != nil {
		if errors.Is(err, service.ErrNoPendingRemoval) {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, err
	}
	return nil, nil
}

// ─── Node registration token ─────────────────────────────────────────────────

type RegistrationTokenOutput struct {
	Body struct {
		Token string `json:"token"` // empty string if not yet generated
	}
}

func (h *Handler) GetNodeRegistrationToken(ctx context.Context, input *ListNodesInput) (*RegistrationTokenOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	token, err := h.svc.Nodes.GetRegistrationToken(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := &RegistrationTokenOutput{}
	out.Body.Token = token
	return out, nil
}

func (h *Handler) GenerateNodeRegistrationToken(ctx context.Context, input *ListNodesInput) (*RegistrationTokenOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	token, err := h.svc.Nodes.GenerateRegistrationToken(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := &RegistrationTokenOutput{}
	out.Body.Token = token
	return out, nil
}

// GetClusterJoinToken returns the k3s node token stored in server config.
// Empty string if K3S_TOKEN is not set (k3s not installed on master yet).
type ClusterJoinTokenOutput struct {
	Body struct {
		Token     string `json:"token"`      // empty if k3s not installed
		ServerURL string `json:"server_url"` // e.g. https://100.64.0.1:6443
	}
}

const k3sTokenPath = "/var/lib/rancher/k3s/server/node-token"

// k3sServerURL is the cluster's API, as a node on the mesh reaches it.
const k3sServerURL = "https://100.64.0.1:6443"

// MeshHealthOutput reports the control plane's ability to reach Headscale.
type MeshHealthOutput struct {
	Body struct {
		Configured    bool       `json:"configured"`   // Headscale is wired up at all
		Checked       bool       `json:"checked"`      // a call has been attempted
		Healthy       bool       `json:"healthy"`      // last call succeeded
		Unauthorized  bool       `json:"unauthorized"` // credential rejected -- needs a new API key
		LastError     string     `json:"last_error,omitempty"`
		LastErrorAt   *time.Time `json:"last_error_at,omitempty"`
		LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	}
}

// OrphanWorkloadsOutput lists workloads running with no service behind them.
type OrphanWorkloadsOutput struct {
	Body struct {
		Orphans []service.OrphanWorkload `json:"orphans"`
	}
}

// ListOrphanWorkloads reports what is running in the cluster that meshploy has
// no record of — a delete that failed, a rename from before renames moved the
// workload, a restore, or a manual kubectl change. Surfacing it is the point:
// these are invisible otherwise, and an unowned Deployment holds real memory on
// a node for as long as nobody looks.
func (h *Handler) ListOrphanWorkloads(ctx context.Context, input *ClusterPathInput) (*OrphanWorkloadsOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	orphans, err := h.svc.Orphans.List(ctx, orgID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list orphan workloads: " + err.Error())
	}
	out := &OrphanWorkloadsOutput{}
	out.Body.Orphans = orphans
	return out, nil
}

// DeleteOrphanInput identifies one orphan, and whether its data goes with it.
type DeleteOrphanInput struct {
	OrgID     string `path:"orgId"`
	Namespace string `path:"namespace"`
	Name      string `path:"name"`
	// DeleteData is off unless asked for: removing compute is recoverable by a
	// redeploy, removing a claim is not recoverable at all.
	DeleteData bool `query:"delete_data"`
}

type DeleteOrphanOutput struct {
	Body struct {
		Removed string `json:"removed"`
	}
}

// DeleteOrphanWorkload removes a single unowned workload. The service layer
// re-checks that it is still unowned before touching the cluster.
func (h *Handler) DeleteOrphanWorkload(ctx context.Context, input *DeleteOrphanInput) (*DeleteOrphanOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	if err := h.svc.Orphans.Delete(ctx, orgID, input.Namespace, input.Name, input.DeleteData); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	out := &DeleteOrphanOutput{}
	out.Body.Removed = input.Namespace + "/" + input.Name
	return out, nil
}

// GetMeshHealth surfaces the last observed Headscale result. An expired API key
// degrades the mesh invisibly: node liveness freezes at its last known value
// while the nodes list keeps rendering it as current. Reporting it lets the UI
// mark that state as stale rather than presenting it as fact.
func (h *Handler) GetMeshHealth(ctx context.Context, input *ClusterPathInput) (*MeshHealthOutput, error) {
	if _, _, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, ""); err != nil {
		return nil, err
	}
	out := &MeshHealthOutput{}
	if h.svc == nil || h.svc.Headscale == nil {
		return out, nil
	}
	hh := h.svc.Headscale.Health()
	out.Body.Configured = true
	out.Body.Checked = hh.Checked
	out.Body.Healthy = hh.Healthy
	out.Body.Unauthorized = hh.Unauthorized
	out.Body.LastError = hh.LastError
	out.Body.LastErrorAt = hh.LastErrorAt
	out.Body.LastSuccessAt = hh.LastSuccessAt
	return out, nil
}

// ClusterPathInput scopes the cluster-credential endpoints to an organization.
// These endpoints hand out credentials that let a machine join the mesh and the
// k3s cluster, so they are org-scoped and admin-only — matching their siblings
// GetNodeRegistrationToken and CreateProvisioningToken.
type ClusterPathInput struct {
	OrgID string `path:"orgId"`
}

func (h *Handler) GetClusterJoinToken(ctx context.Context, input *ClusterPathInput) (*ClusterJoinTokenOutput, error) {
	if _, _, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, ""); err != nil {
		return nil, err
	}
	out := &ClusterJoinTokenOutput{}
	out.Body.Token = h.k3sJoinToken()
	out.Body.ServerURL = k3sServerURL
	return out, nil
}

type ProvisionNodeInput struct {
	Body struct {
		Token string `json:"token" minLength:"1" doc:"A provisioning token (mprov-…)"`
	}
}

type ProvisionNodeOutput struct {
	Body *service.Provisioning
}

// ProvisionNode is the first half of joining, and the only half that happens
// before the mesh exists for this machine.
//
// Unauthenticated, because the caller is a blank server with a token and
// nothing else. The token is the credential, and every failure answers the same
// 401 so a caller cannot learn from the difference between "no such token",
// "already used" and "expired".
func (h *Handler) ProvisionNode(ctx context.Context, input *ProvisionNodeInput) (*ProvisionNodeOutput, error) {
	out, err := h.svc.Nodes.Provision(ctx, input.Body.Token)
	if err != nil {
		return nil, huma.Error401Unauthorized("this token cannot provision a machine")
	}
	return &ProvisionNodeOutput{Body: out}, nil
}

// SelfRegisterNode is unauthenticated — called by the worker install script
// over the WireGuard mesh after joining Headscale.
// Accepts both mprov- (provisioning token, one-time) and mreg- (legacy org token).
type SelfRegisterNodeInput struct {
	Body struct {
		Token       string      `json:"token"        minLength:"1"`
		Name        string      `json:"name"         minLength:"1" maxLength:"100"`
		TailscaleIP string      `json:"tailscale_ip" minLength:"1"`
		MeshRole    db.MeshRole `json:"mesh_role,omitempty" enum:"workload_builder,workload,builder,mesh"`
		// OS is what the join script runs on. Absent means Linux: install.sh
		// sent none before it was recorded.
		OS db.NodeOS `json:"os,omitempty" enum:"linux,darwin,windows"`
	}
}

// SelfRegisterNodeOutput extends the node response with a one-time node secret.
// node_secret is non-empty only when a provisioning token (mprov-) was used.
// The caller must persist this secret; it is never retrievable again.
//
// It also carries the k3s join token, and this is the right place for it: the
// call comes over the mesh, from a machine that has had to join it to get here,
// with a token that is spent by this very request. The public provisioning call
// before it deliberately hands out no such thing.
//
// Empty for a mesh-only node, which is not in the cluster and has nothing to
// join.
type SelfRegisterNodeOutput struct {
	Body struct {
		NodeResponse
		NodeSecret   string `json:"node_secret,omitempty"`
		K3sToken     string `json:"k3s_token,omitempty"`
		K3sServerURL string `json:"k3s_server_url,omitempty"`
	}
}

// k3sJoinToken is the cluster's own token, read at request time so a rotated
// one is always current.
func (h *Handler) k3sJoinToken() string {
	if raw, err := os.ReadFile(k3sTokenPath); err == nil {
		return strings.TrimSpace(string(raw))
	}
	if h.cfg != nil {
		return h.cfg.K3sToken
	}
	return ""
}

func (h *Handler) SelfRegisterNode(ctx context.Context, input *SelfRegisterNodeInput) (*SelfRegisterNodeOutput, error) {
	var node *db.Node
	var nodeSecret string
	var err error

	if strings.HasPrefix(input.Body.Token, "mprov-") {
		node, nodeSecret, err = h.svc.Nodes.RegisterWithProvisioningToken(ctx, input.Body.Token, input.Body.Name, input.Body.TailscaleIP, input.Body.MeshRole, input.Body.OS)
	} else {
		// Legacy org-wide mreg- token — no node secret issued
		node, err = h.svc.Nodes.RegisterWithToken(ctx, input.Body.Token, input.Body.Name, input.Body.TailscaleIP, input.Body.MeshRole, input.Body.OS)
	}
	if errors.Is(err, service.ErrNonLinuxClusterRole) {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	if err != nil {
		return nil, huma.Error401Unauthorized("invalid or unknown registration token")
	}

	// Eagerly store the Headscale peer ID at registration time. The worker just
	// called tailscale up so the peer already exists in Headscale.
	if h.svc.Headscale != nil {
		go func(nID uuid.UUID, ip string) {
			hsNodes, err := h.svc.Headscale.ListNodes(context.Background())
			if err != nil {
				log.Printf("warning: SelfRegisterNode headscale lookup for %s: %v", ip, err)
				return
			}
			for _, hn := range hsNodes {
				if len(hn.IPAddresses) > 0 && hn.IPAddresses[0] == ip {
					if err := h.svc.Nodes.SetHeadscaleID(context.Background(), nID, hn.ID); err != nil {
						log.Printf("warning: SelfRegisterNode set headscale_id for %s: %v", nID, err)
					}
					return
				}
			}
		}(node.ID, node.TailscaleIP)
	}

	r := h.enrichNode(ctx, node)
	out := &SelfRegisterNodeOutput{}
	out.Body.NodeResponse = r
	out.Body.NodeSecret = nodeSecret
	// A cluster node needs one more thing, and this is where it may have it:
	// the caller is on the mesh and has just spent a single-use token to get
	// here. A mesh-only node is not in the cluster and has nothing to join.
	if node.MeshRole != db.MeshRoleMesh {
		out.Body.K3sToken = h.k3sJoinToken()
		out.Body.K3sServerURL = k3sServerURL
	}
	return out, nil
}

// SelfDeregisterNode is unauthenticated — called by the worker uninstall script.
// Accepts either a per-node secret (node_secret, mprov flow) or the legacy
// org registration token (token, mreg flow). At least one must be provided.
type SelfDeregisterNodeInput struct {
	Body struct {
		NodeSecret string `json:"node_secret,omitempty"` // preferred: mnode-... per-node secret
		Token      string `json:"token,omitempty"`       // legacy: mreg-... org token
		NodeID     string `json:"node_id" minLength:"1"`
	}
}

func (h *Handler) SelfDeregisterNode(ctx context.Context, input *SelfDeregisterNodeInput) (*struct{}, error) {
	nodeID, err := parseUUID(input.Body.NodeID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid node_id")
	}

	node, err := h.svc.Nodes.Get(ctx, nodeID)
	if err != nil {
		return nil, huma.Error401Unauthorized("node not found")
	}

	switch {
	case input.Body.NodeSecret != "":
		// Per-node secret flow (mprov-registered nodes)
		if err := h.svc.Nodes.ValidateNodeSecret(ctx, nodeID, input.Body.NodeSecret); err != nil {
			return nil, huma.Error401Unauthorized("invalid node secret")
		}
	case input.Body.Token != "":
		// Legacy mreg token flow — verify token belongs to the same org
		orgID, err := h.svc.Nodes.OrgIDFromToken(ctx, input.Body.Token)
		if err != nil {
			return nil, huma.Error401Unauthorized("invalid or unknown registration token")
		}
		if node.OrganizationID != orgID {
			return nil, huma.Error401Unauthorized("node does not belong to this token's organisation")
		}
	default:
		return nil, huma.Error400BadRequest("node_secret or token is required")
	}

	if node.K3sRole == db.K3sRoleServer {
		return nil, huma.Error400BadRequest("the gateway node cannot be deregistered")
	}
	// Cascade: Headscale peer → k3s node object → DB record
	if h.svc.Headscale != nil && node.HeadscaleID != "" {
		if err := h.svc.Headscale.DeleteNode(ctx, node.HeadscaleID); err != nil {
			log.Printf("warning: self-deregister headscale peer %s: %v", node.HeadscaleID, err)
		}
	}
	if h.svc.K8s != nil && node.Name != "" {
		if err := appk8s.DeleteNode(ctx, h.svc.K8s, node.Name); err != nil {
			log.Printf("warning: self-deregister k8s node %s: %v", node.Name, err)
		}
	}
	return nil, h.svc.Nodes.Delete(ctx, nodeID)
}

// ─── Provisioning token CRUD ──────────────────────────────────────────────────

type CreateProvisioningTokenInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Label     string     `json:"label"      minLength:"1" maxLength:"100"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
		// MeshRole is what the machine becomes, decided here rather than asked
		// of it. Empty means the default, workload_builder.
		MeshRole db.MeshRole `json:"mesh_role,omitempty" enum:"workload_builder,workload,builder,mesh"`
	}
}

type CreateProvisioningTokenOutput struct {
	Body struct {
		db.NodeProvisioningToken
		Token string `json:"token"` // plaintext — shown once
	}
}

func (h *Handler) CreateProvisioningToken(ctx context.Context, input *CreateProvisioningTokenInput) (*CreateProvisioningTokenOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	plaintext, row, err := h.svc.Nodes.CreateProvisioningToken(ctx, orgID, input.Body.Label, input.Body.ExpiresAt, input.Body.MeshRole)
	if err != nil {
		return nil, err
	}
	out := &CreateProvisioningTokenOutput{}
	out.Body.NodeProvisioningToken = *row
	out.Body.Token = plaintext
	return out, nil
}

// ─── Headscale preauth key ───────────────────────────────────────────────────

// HeadscalePreAuthKeyStatusOutput is returned by GET.
// When a valid stored key exists, Key is populated so the UI can display it without
// requiring a fresh POST. Key is omitted when there is no stored key or it is expired.
type HeadscalePreAuthKeyStatusOutput struct {
	Body struct {
		HasActiveKey bool   `json:"has_active_key"`
		Key          string `json:"key,omitempty"` // full key value when a valid stored key exists
		HeadscaleURL string `json:"headscale_url"`
	}
}

// HeadscalePreAuthKeyOutput is returned by POST — contains the full key from the CREATE response.
type HeadscalePreAuthKeyOutput struct {
	Body struct {
		Key          string    `json:"key"`
		Reusable     bool      `json:"reusable"`
		Expiration   time.Time `json:"expiration"`
		HeadscaleURL string    `json:"headscale_url"`
	}
}

// GetHeadscalePreAuthKey returns the stored Headscale preauth key for this org (if any).
// The key is persisted encrypted in the DB by CreateHeadscalePreAuthKey and auto-cleared
// here when it has passed its expiry. This lets the UI display the key across page
// navigations without requiring the user to generate a new one every session.
func (h *Handler) GetHeadscalePreAuthKey(ctx context.Context, input *ClusterPathInput) (*HeadscalePreAuthKeyStatusOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	out := &HeadscalePreAuthKeyStatusOutput{}
	// The public name, from the primary. Never HEADSCALE_URL, which is the
	// API's own in-network address for Headscale.
	out.Body.HeadscaleURL = h.svc.Domains.PlatformURL(ctx, orgID, "headscale")

	org, err := h.svc.Orgs.Get(ctx, orgID)
	if err != nil {
		return out, nil
	}

	if org.HeadscalePreAuthKey == "" || org.HeadscalePreAuthKeyExpiry == nil {
		return out, nil
	}
	if time.Now().After(*org.HeadscalePreAuthKeyExpiry) {
		// Key has expired — clear it so the UI prompts for a new one.
		if err := h.svc.Orgs.ClearHeadscalePreAuthKey(ctx, org.ID); err != nil {
			log.Printf("warning: clear expired headscale preauth key for org %s: %v", org.ID, err)
		}
		return out, nil
	}

	out.Body.HasActiveKey = true
	out.Body.Key = string(org.HeadscalePreAuthKey)
	return out, nil
}

// CreateHeadscalePreAuthKey generates a fresh reusable Headscale preauth key and
// persists it encrypted on the org record so GetHeadscalePreAuthKey can return it
// on subsequent page loads without requiring another POST.
func (h *Handler) CreateHeadscalePreAuthKey(ctx context.Context, input *ClusterPathInput) (*HeadscalePreAuthKeyOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	if h.svc.Headscale == nil {
		return nil, huma.NewError(503, "Headscale is not configured on this gateway")
	}

	hsUser := "meshploy"
	if h.cfg != nil && h.cfg.HeadscaleUser != "" {
		hsUser = h.cfg.HeadscaleUser
	}
	key, err := h.svc.Headscale.CreatePreAuthKey(ctx, hsUser)
	if err != nil {
		return nil, huma.NewError(502, "failed to generate Headscale preauth key: "+err.Error())
	}

	if storeErr := h.svc.Orgs.StoreHeadscalePreAuthKey(ctx, orgID, key.Key, key.Expiration); storeErr != nil {
		log.Printf("warning: persist headscale preauth key for org %s: %v", orgID, storeErr)
	}

	out := &HeadscalePreAuthKeyOutput{}
	out.Body.Key = key.Key
	out.Body.Reusable = key.Reusable
	out.Body.Expiration = key.Expiration
	out.Body.HeadscaleURL = h.svc.Domains.PlatformURL(ctx, orgID, "headscale")
	return out, nil
}

// meshRoleLabelsMatch returns true if the k8s node's labels already reflect the
// desired MeshRole, meaning no reconciliation is needed.
func meshRoleLabelsMatch(labels map[string]string, role db.MeshRole) bool {
	const key = "meshploy.com/role"
	val, hasLabel := labels[key]
	switch role {
	case db.MeshRoleWorkloadBuilder, db.MeshRoleBuilder:
		return hasLabel && val == "builder"
	default: // workload
		return !hasLabel
	}
}

// ─── Node metrics ────────────────────────────────────────────────────────────

type NodeMetricsOutput struct {
	Body *service.NodeMetrics
}

func (h *Handler) GetNodeMetrics(ctx context.Context, input *NodePathInput) (*NodeMetricsOutput, error) {
	_, _, nodeID, err := h.checkOrgMemberAccess(ctx, input.OrgID, input.NodeID)
	if err != nil {
		return nil, err
	}
	m, err := h.svc.Nodes.GetNodeMetrics(ctx, nodeID)
	if errors.Is(err, service.ErrNodeMetricsUnsupported) {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	if err != nil {
		if notFoundErr := notFound(err); notFoundErr != err {
			return nil, notFoundErr
		}
		return nil, huma.Error422UnprocessableEntity("node metrics unavailable — install node_exporter: sudo meshploy install node-exporter")
	}
	return &NodeMetricsOutput{Body: m}, nil
}

type NodeContainersOutput struct {
	Body service.HostContainers
}

// ListNodeContainers reports what else runs on a node.
//
// Read-only, and gateway-only: the host agent runs on the gateway, so any
// other node answers "not available here" rather than an empty list that would
// read as "nothing else is running".
func (h *Handler) ListNodeContainers(ctx context.Context, input *NodePathInput) (*NodeContainersOutput, error) {
	_, _, nodeID, err := h.checkOrgMemberAccess(ctx, input.OrgID, input.NodeID)
	if err != nil {
		return nil, err
	}
	node, err := h.svc.Nodes.Get(ctx, nodeID)
	if err != nil {
		return nil, notFound(err)
	}
	if node.K3sRole != db.K3sRoleServer {
		return &NodeContainersOutput{Body: service.HostContainers{Containers: []service.HostContainer{}, Groups: []service.HostContainerGroup{}}}, nil
	}
	return &NodeContainersOutput{Body: h.svc.System.HostContainers()}, nil
}

// ServeInstallScript serves deploy/install.sh, mounted at
// /opt/meshploy/install.sh.
//
// Anonymous, because it is not a secret and pretending otherwise only breaks
// the one case that matters: a blank machine, with a provisioning token and no
// session, fetching the script it is about to run. The Community edition's
// repository, its images and this file are public, and meshploy.com serves the
// same bytes to anyone who asks.
//
// It is the gateway's own copy that is worth fetching rather than the canonical
// one: it is the script that matches the version of the server being joined,
// and a worker installed by a newer script than its gateway is the kind of skew
// nobody can reproduce afterwards.
func (h *Handler) ServeInstallScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript")
	body, err := os.ReadFile(installScriptPath)
	if err != nil {
		http.Error(w, "install script unavailable", http.StatusNotFound)
		return
	}
	_, _ = w.Write(withAPIBase(body, h.apiBaseURL(r.Context())))
}

// installScriptPath is where docker-compose mounts deploy/install.sh. A var so
// a test can point it somewhere else.
var installScriptPath = "/opt/meshploy/install.sh"

// apiBaseLine is the line install.sh leaves for the gateway to fill in. Matched
// literally, so if the script changes it, this stops substituting rather than
// substituting something wrong.
const apiBaseLine = `MESHPLOY_API_BASE="${MESHPLOY_API_BASE:-}"`

// withAPIBase writes this gateway's own address into the script it is serving.
//
// This is what lets the install command carry nothing but a token: the machine
// running it has no other way to know which Meshploy it is joining, and the URL
// it fetched the script from is the answer. A gateway that does not know its
// own public address substitutes nothing, and the script then asks for --api.
func withAPIBase(body []byte, base string) []byte {
	if base == "" || !bytes.Contains(body, []byte(apiBaseLine)) {
		return body
	}
	filled := fmt.Sprintf(`MESHPLOY_API_BASE="${MESHPLOY_API_BASE:-%s}"`, base)
	return bytes.Replace(body, []byte(apiBaseLine), []byte(filled), 1)
}

// joinScriptDir is where docker-compose mounts deploy/join/: the scripts that
// join a machine install.sh cannot run on. A var so a test can point it
// somewhere else.
var joinScriptDir = "/opt/meshploy/join"

// joinScripts are the files served from joinScriptDir, and the line in each
// that the gateway fills in with its own address, as it does for install.sh.
// Anything not listed here is not served, whatever the directory holds.
var joinScripts = map[string]struct {
	contentType string
	placeholder string
	filled      string // a format with one %s, the API base
}{
	"macos.sh": {
		contentType: "text/x-shellscript",
		placeholder: apiBaseLine,
		filled:      `MESHPLOY_API_BASE="${MESHPLOY_API_BASE:-%s}"`,
	},
	"windows.ps1": {
		contentType: "text/plain; charset=utf-8",
		placeholder: `$DefaultApiBase = ""`,
		filled:      `$DefaultApiBase = "%s"`,
	},
}

// ServeJoinScript serves a mesh-only join script for a Windows or macOS
// machine, anonymous for the same reason install.sh is: the machine fetching it
// has a provisioning token and no session, and the script is public anyway.
func (h *Handler) ServeJoinScript(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "script")
	script, ok := joinScripts[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	body, err := os.ReadFile(filepath.Join(joinScriptDir, name))
	if err != nil {
		http.Error(w, "join script unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", script.contentType)
	_, _ = w.Write(withLine(body, script.placeholder, script.filled, h.apiBaseURL(r.Context())))
}

// withLine fills in the one line a served script leaves for the gateway's
// address. Matched literally, so a script that changes the line stops being
// filled in rather than being filled in wrong.
func withLine(body []byte, placeholder, format, base string) []byte {
	if base == "" || !bytes.Contains(body, []byte(placeholder)) {
		return body
	}
	return bytes.Replace(body, []byte(placeholder), []byte(fmt.Sprintf(format, base)), 1)
}

// apiBaseURL is where this gateway answers from outside the mesh.
//
// The primary domain first, because it can move and API_BASE_URL is written
// once at install: after a switch, a script still naming the old api. name
// would join every new machine through a domain that is on its way out.
func (h *Handler) apiBaseURL(ctx context.Context) string {
	if h.svc != nil && h.svc.Domains != nil {
		if u := h.svc.Domains.GatewayPlatformURL(ctx, "api"); u != "" {
			return u
		}
	}
	if h.cfg == nil {
		return ""
	}
	if h.cfg.APIBaseURL != "" && !strings.Contains(h.cfg.APIBaseURL, "localhost") {
		return strings.TrimRight(h.cfg.APIBaseURL, "/")
	}
	return ""
}

// ServeUninstallScript serves deploy/uninstall.sh (mounted at /opt/meshploy/uninstall.sh) to authenticated users.
func (h *Handler) ServeUninstallScript(w http.ResponseWriter, r *http.Request) {
	if _, err := requireUser(r.Context()); err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript")
	http.ServeFile(w, r, "/opt/meshploy/uninstall.sh")
}
