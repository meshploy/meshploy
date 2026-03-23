package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
)

type ListNodesInput struct {
	OrgID string `path:"orgId"`
}

type ListNodesOutput struct {
	Body []db.Node
}

type NodePathInput struct {
	OrgID  string `path:"orgId"`
	NodeID string `path:"nodeId"`
}

type GetNodeOutput struct {
	Body *db.Node
}

type RegisterNodeInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		Name        string `json:"name" minLength:"1" maxLength:"100"`
		TailscaleIP string `json:"tailscale_ip"`
	}
}

type RegisterNodeOutput struct {
	Body *db.Node
}

type UpdateNodeInput struct {
	OrgID  string `path:"orgId"`
	NodeID string `path:"nodeId"`
	Body   struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
	}
}

type UpdateNodeOutput struct {
	Body *db.Node
}

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
}

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
	return &ListNodesOutput{Body: nodes}, nil
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
	return &RegisterNodeOutput{Body: node}, nil
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
	return &GetNodeOutput{Body: node}, nil
}

func (h *Handler) UpdateNode(ctx context.Context, input *UpdateNodeInput) (*UpdateNodeOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	nodeID, err := parseUUID(input.NodeID)
	if err != nil {
		return nil, err
	}
	node, err := h.svc.Nodes.Update(ctx, nodeID, input.Body.Name)
	if err != nil {
		return nil, notFound(err)
	}
	return &UpdateNodeOutput{Body: node}, nil
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
