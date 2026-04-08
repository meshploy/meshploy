package handler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
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
		name  string
		ready bool
	}

	hsByIP := make(map[string]hsIndex)
	k8sByIP := make(map[string]k8sIndex)

	var wg sync.WaitGroup

	if h.svc.Headscale != nil {
		wg.Go(func() {
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
				for _, ip := range cn.InternalIPs {
					k8sByIP[ip] = k8sIndex{name: cn.Name, ready: cn.Ready}
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
		}

		if kn, ok := k8sByIP[n.TailscaleIP]; ok {
			r.K8sMember = true
			r.K8sReady = kn.ready
			r.K8sNodeName = kn.name
			// Reflect live cluster readiness in the status field.
			if kn.ready {
				r.Status = db.NodeOnline
			} else {
				r.Status = db.NodeOffline
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
		Name    string `json:"name,omitempty"     maxLength:"100"`
		K3sRole string `json:"k3s_role,omitempty" enum:"server,agent"`
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

	// K3s cluster join token — authenticated, gateway-only value from config
	huma.Register(api, huma.Operation{
		OperationID: "get-cluster-join-token",
		Method:      "GET",
		Path:        "/api/v1/cluster/join-token",
		Summary:     "Get the k3s node token for joining the cluster",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetClusterJoinToken)
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func (h *Handler) ListNodes(ctx context.Context, input *ListNodesInput) (*ListNodesOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
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
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
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
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	nodeID, err := parseUUID(input.NodeID)
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
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	nodeID, err := parseUUID(input.NodeID)
	if err != nil {
		return nil, err
	}
	node, err := h.svc.Nodes.Update(ctx, nodeID, service.UpdateNodeInput{
		Name:    input.Body.Name,
		K3sRole: db.K3sRole(input.Body.K3sRole),
	})
	if err != nil {
		return nil, notFound(err)
	}
	r := h.enrichNode(ctx, node)
	return &UpdateNodeOutput{Body: &r}, nil
}

func (h *Handler) DeleteNode(ctx context.Context, input *NodePathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	nodeID, err := parseUUID(input.NodeID)
	if err != nil {
		return nil, err
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
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
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
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
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

func (h *Handler) GetClusterJoinToken(ctx context.Context, _ *struct{}) (*ClusterJoinTokenOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	out := &ClusterJoinTokenOutput{}
	if h.cfg != nil {
		out.Body.Token = h.cfg.K3sToken
	}
	out.Body.ServerURL = "https://100.64.0.1:6443"
	return out, nil
}

// SelfRegisterNode is unauthenticated — called by the worker install script
// over the WireGuard mesh after joining Headscale.
type SelfRegisterNodeInput struct {
	Body struct {
		Token       string `json:"token"        minLength:"1"`
		Name        string `json:"name"         minLength:"1" maxLength:"100"`
		TailscaleIP string `json:"tailscale_ip" minLength:"1"`
	}
}

func (h *Handler) SelfRegisterNode(ctx context.Context, input *SelfRegisterNodeInput) (*RegisterNodeOutput, error) {
	node, err := h.svc.Nodes.RegisterWithToken(ctx, input.Body.Token, input.Body.Name, input.Body.TailscaleIP)
	if err != nil {
		return nil, huma.Error401Unauthorized("invalid or unknown registration token")
	}
	r := h.enrichNode(ctx, node)
	return &RegisterNodeOutput{Body: &r}, nil
}
