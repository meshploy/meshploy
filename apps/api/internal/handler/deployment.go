package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
)

type DeploymentPathInput struct {
	OrgID        string `path:"orgId"`
	ProjectID    string `path:"projectId"`
	ServiceID    string `path:"serviceId"`
	DeploymentID string `path:"deploymentId"`
}

type ListDeploymentsInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
}

type ListDeploymentsOutput struct {
	Body []db.Deployment
}

type GetDeploymentOutput struct {
	Body *db.Deployment
}

func (h *Handler) registerDeploymentRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-deployments",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments",
		Summary:     "List deployments for a service",
		Tags:        []string{"Deployments"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListDeployments)

	huma.Register(api, huma.Operation{
		OperationID: "get-deployment",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}",
		Summary:     "Get a deployment",
		Tags:        []string{"Deployments"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetDeployment)
}

func (h *Handler) ListDeployments(ctx context.Context, input *ListDeploymentsInput) (*ListDeploymentsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	deployments, err := h.svc.Deployments.List(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	return &ListDeploymentsOutput{Body: deployments}, nil
}

func (h *Handler) GetDeployment(ctx context.Context, input *DeploymentPathInput) (*GetDeploymentOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	deploymentID, err := parseUUID(input.DeploymentID)
	if err != nil {
		return nil, err
	}
	deployment, err := h.svc.Deployments.Get(ctx, deploymentID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetDeploymentOutput{Body: deployment}, nil
}
