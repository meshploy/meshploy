package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	db "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
)

// Where everything runs, and what taking a node out would do. For the maps,
// the CLI and agents alike, so each gives the same answer. Org admins only,
// as the cluster page is: it spans every project.

type PlacementOutput struct {
	Body *service.Placement
}

type NodeWhatIfInput struct {
	OrgID string `path:"orgId"`
	Node  string `path:"node" doc:"The cluster node's name (k8s_node_name)"`
}

type NodeWhatIfOutput struct {
	Body *service.NodeDownForecast
}

func (h *Handler) registerPlacement(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-placement",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/placement",
		Summary:     "Where every service runs: nodes with their room, services with their pods, requests, pin and where their data is",
		Tags:        []string{"Cluster"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetPlacement)

	huma.Register(api, huma.Operation{
		OperationID: "node-what-if",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/placement/nodes/{node}/what-if",
		Summary:     "What would happen if a node went down: per service, keeps running, moves, or stays down and why. Nothing is stopped",
		Tags:        []string{"Cluster"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.NodeWhatIf)

	huma.Register(api, huma.Operation{
		OperationID: "get-project-map",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/map",
		Summary:     "Everything a level's map is drawn from, in one read: services, stacks, routes, volumes, why any service is not staying up, build advice, and who reads whose published variables",
		Tags:        []string{"Projects"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetProjectMap)
}

type ProjectMapOutput struct {
	Body *service.ProjectMap
}

func (h *Handler) GetProjectMap(ctx context.Context, input *ProjectPathInput) (*ProjectMapOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionView, "")
	if err != nil {
		return nil, err
	}
	m, err := h.svc.ProjectMaps.Get(ctx, projectID)
	if err != nil {
		return nil, huma.Error500InternalServerError("read the project's map", err)
	}
	return &ProjectMapOutput{Body: m}, nil
}

func (h *Handler) GetPlacement(ctx context.Context, input *ClusterPathInput) (*PlacementOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	p, err := h.svc.Placement.Get(ctx, orgID)
	if err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	return &PlacementOutput{Body: p}, nil
}

func (h *Handler) NodeWhatIf(ctx context.Context, input *NodeWhatIfInput) (*NodeWhatIfOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	p, err := h.svc.Placement.Get(ctx, orgID)
	if err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	f, err := service.ForecastNodeDown(p, input.Node)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}
	return &NodeWhatIfOutput{Body: f}, nil
}
