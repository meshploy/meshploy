package handler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	appk8s "github.com/meshploy/apps/api/internal/k8s"
	"github.com/meshploy/apps/api/internal/service"
	"github.com/meshploy/packages/db"
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
		MeshRole string `json:"mesh_role,omitempty" enum:"workload_builder,workload,builder"`
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
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteNode)

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

	// K3s cluster join token — authenticated, gateway-only value from config
	huma.Register(api, huma.Operation{
		OperationID: "get-cluster-join-token",
		Method:      "GET",
		Path:        "/api/v1/cluster/join-token",
		Summary:     "Get the k3s node token for joining the cluster",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetClusterJoinToken)

	// Headscale preauth key — get the most recent active key, or generate a new one
	huma.Register(api, huma.Operation{
		OperationID: "get-headscale-preauth-key",
		Method:      "GET",
		Path:        "/api/v1/cluster/headscale-preauth-key",
		Summary:     "Get the most recent active Headscale preauth key",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetHeadscalePreAuthKey)

	huma.Register(api, huma.Operation{
		OperationID: "create-headscale-preauth-key",
		Method:      "POST",
		Path:        "/api/v1/cluster/headscale-preauth-key",
		Summary:     "Generate a new Headscale preauth key for joining the WireGuard mesh",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.CreateHeadscalePreAuthKey)

	huma.Register(api, huma.Operation{
		OperationID: "get-node-metrics",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/nodes/{nodeId}/metrics",
		Summary:     "Get live resource metrics for a node (requires node_exporter)",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetNodeMetrics)
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

func (h *Handler) DeleteNode(ctx context.Context, input *NodePathInput) (*struct{}, error) {
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
	// Remove the WireGuard peer from Headscale before deleting from the DB.
	// Non-fatal: if Headscale is unavailable the DB record is still cleaned up.
	if h.svc.Headscale != nil && node.HeadscaleID != "" {
		if err := h.svc.Headscale.DeleteNode(ctx, node.HeadscaleID); err != nil {
			log.Printf("warning: delete headscale peer %s for node %s: %v", node.HeadscaleID, node.Name, err)
		}
	}
	// Remove the node object from the k3s cluster so it doesn't linger as NotReady.
	// The k3s-agent process on the worker keeps running until manually uninstalled.
	if h.svc.K8s != nil && node.Name != "" {
		if err := appk8s.DeleteNode(ctx, h.svc.K8s, node.Name); err != nil {
			log.Printf("warning: delete k8s node %s: %v", node.Name, err)
		}
	}
	return nil, h.svc.Nodes.Delete(ctx, nodeID)
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
		Token      string `json:"token"`       // empty if k3s not installed
		ServerURL  string `json:"server_url"`  // e.g. https://100.64.0.1:6443
	}
}

const k3sTokenPath = "/var/lib/rancher/k3s/server/node-token"

func (h *Handler) GetClusterJoinToken(ctx context.Context, _ *struct{}) (*ClusterJoinTokenOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	out := &ClusterJoinTokenOutput{}
	// Read from filesystem at request time so rotated tokens are always current.
	if raw, err := os.ReadFile(k3sTokenPath); err == nil {
		out.Body.Token = strings.TrimSpace(string(raw))
	} else if h.cfg != nil && h.cfg.K3sToken != "" {
		// Fallback to env var (dev / non-gateway environments).
		out.Body.Token = h.cfg.K3sToken
	}
	out.Body.ServerURL = "https://100.64.0.1:6443"
	return out, nil
}

// SelfRegisterNode is unauthenticated — called by the worker install script
// over the WireGuard mesh after joining Headscale.
// Accepts both mprov- (provisioning token, one-time) and mreg- (legacy org token).
type SelfRegisterNodeInput struct {
	Body struct {
		Token       string      `json:"token"        minLength:"1"`
		Name        string      `json:"name"         minLength:"1" maxLength:"100"`
		TailscaleIP string      `json:"tailscale_ip" minLength:"1"`
		MeshRole    db.MeshRole `json:"mesh_role,omitempty" enum:"workload_builder,workload,builder"`
	}
}

// SelfRegisterNodeOutput extends the node response with a one-time node secret.
// node_secret is non-empty only when a provisioning token (mprov-) was used.
// The caller must persist this secret; it is never retrievable again.
type SelfRegisterNodeOutput struct {
	Body struct {
		NodeResponse
		NodeSecret string `json:"node_secret,omitempty"`
	}
}

func (h *Handler) SelfRegisterNode(ctx context.Context, input *SelfRegisterNodeInput) (*SelfRegisterNodeOutput, error) {
	var node *db.Node
	var nodeSecret string
	var err error

	if strings.HasPrefix(input.Body.Token, "mprov-") {
		node, nodeSecret, err = h.svc.Nodes.RegisterWithProvisioningToken(ctx, input.Body.Token, input.Body.Name, input.Body.TailscaleIP, input.Body.MeshRole)
	} else {
		// Legacy org-wide mreg- token — no node secret issued
		node, err = h.svc.Nodes.RegisterWithToken(ctx, input.Body.Token, input.Body.Name, input.Body.TailscaleIP, input.Body.MeshRole)
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
	plaintext, row, err := h.svc.Nodes.CreateProvisioningToken(ctx, orgID, input.Body.Label, input.Body.ExpiresAt)
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
func (h *Handler) GetHeadscalePreAuthKey(ctx context.Context, _ *struct{}) (*HeadscalePreAuthKeyStatusOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	out := &HeadscalePreAuthKeyStatusOutput{}
	if h.cfg != nil {
		if h.cfg.Domain != "" {
			out.Body.HeadscaleURL = "https://headscale." + h.cfg.Domain
		} else {
			out.Body.HeadscaleURL = h.cfg.HeadscaleURL
		}
	}

	orgs, err := h.svc.Orgs.ListForUser(ctx, userID)
	if err != nil || len(orgs) == 0 {
		return out, nil
	}
	org := orgs[0]

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
func (h *Handler) CreateHeadscalePreAuthKey(ctx context.Context, _ *struct{}) (*HeadscalePreAuthKeyOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	if h.svc.Headscale == nil {
		return nil, huma.NewError(503, "Headscale is not configured on this gateway")
	}

	// Resolve the caller's org and enforce admin role before generating a mesh join token.
	orgs, err := h.svc.Orgs.ListForUser(ctx, userID)
	if err != nil || len(orgs) == 0 {
		return nil, huma.Error403Forbidden("no organization found")
	}
	if err := h.enforceAdminRole(ctx, orgs[0].ID, userID); err != nil {
		return nil, err
	}

	hsUser := "meshploy"
	if h.cfg != nil && h.cfg.HeadscaleUser != "" {
		hsUser = h.cfg.HeadscaleUser
	}
	key, err := h.svc.Headscale.CreatePreAuthKey(ctx, hsUser)
	if err != nil {
		return nil, huma.NewError(502, "failed to generate Headscale preauth key: "+err.Error())
	}

	if storeErr := h.svc.Orgs.StoreHeadscalePreAuthKey(ctx, orgs[0].ID, key.Key, key.Expiration); storeErr != nil {
		log.Printf("warning: persist headscale preauth key for org %s: %v", orgs[0].ID, storeErr)
	}

	out := &HeadscalePreAuthKeyOutput{}
	out.Body.Key = key.Key
	out.Body.Reusable = key.Reusable
	out.Body.Expiration = key.Expiration
	if h.cfg != nil {
		if h.cfg.Domain != "" {
			out.Body.HeadscaleURL = "https://headscale." + h.cfg.Domain
		} else {
			out.Body.HeadscaleURL = h.cfg.HeadscaleURL
		}
	}
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
	if err != nil {
		if notFoundErr := notFound(err); notFoundErr != err {
			return nil, notFoundErr
		}
		return nil, huma.Error422UnprocessableEntity("node metrics unavailable — install node_exporter: sudo meshploy install node-exporter")
	}
	return &NodeMetricsOutput{Body: m}, nil
}

// ServeInstallScript serves deploy/install.sh (mounted at /opt/meshploy/install.sh) to authenticated users.
func (h *Handler) ServeInstallScript(w http.ResponseWriter, r *http.Request) {
	if _, err := requireUser(r.Context()); err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript")
	http.ServeFile(w, r, "/opt/meshploy/install.sh")
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
