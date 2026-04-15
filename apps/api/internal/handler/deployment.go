package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/meshploy/apps/api/internal/middleware"
	"github.com/meshploy/apps/api/internal/service"
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
		OperationID:   "trigger-deployment",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments",
		Summary:       "Trigger a new deployment",
		Tags:          []string{"Deployments"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 202,
	}, h.TriggerDeployment)

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

	huma.Register(api, huma.Operation{
		OperationID:   "cancel-deployment",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}",
		Summary:       "Cancel an active deployment",
		Tags:          []string{"Deployments"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.CancelDeployment)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-deployment-record",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}/record",
		Summary:       "Delete a deployment record",
		Tags:          []string{"Deployments"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.DeleteDeploymentRecord)
}

func (h *Handler) TriggerDeployment(ctx context.Context, input *ListDeploymentsInput) (*GetDeploymentOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	deployment, err := h.svc.Deployments.Trigger(ctx, service.TriggerInput{
		ServiceID:   serviceID,
		TriggeredBy: userID,
	})
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &GetDeploymentOutput{Body: deployment}, nil
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

func (h *Handler) CancelDeployment(ctx context.Context, input *DeploymentPathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	deploymentID, err := parseUUID(input.DeploymentID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Deployments.Cancel(ctx, deploymentID); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return nil, nil
}

func (h *Handler) DeleteDeploymentRecord(ctx context.Context, input *DeploymentPathInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	deploymentID, err := parseUUID(input.DeploymentID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Deployments.DeleteRecord(ctx, deploymentID); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return nil, nil
}

// StreamDeploymentLogs is a raw SSE handler — registered via RegisterRaw, not Huma.
// GET /api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}/logs/stream
func (h *Handler) StreamDeploymentLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserFromContext(r.Context()); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	deploymentID, err := parseUUID(chi.URLParam(r, "deploymentId"))
	if err != nil {
		http.Error(w, "invalid deployment id", http.StatusBadRequest)
		return
	}

	sseHeaders(w)

	flusher, canFlush := w.(http.Flusher)
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}
	flush()

	_ = h.svc.Deployments.StreamBuildLogs(r.Context(), deploymentID, w, flush)
}

// StreamServiceLogs is a raw SSE handler for live runtime container logs.
// GET /api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/logs/stream
func (h *Handler) StreamServiceLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserFromContext(r.Context()); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	serviceID, err := parseUUID(chi.URLParam(r, "serviceId"))
	if err != nil {
		http.Error(w, "invalid service id", http.StatusBadRequest)
		return
	}

	sseHeaders(w)

	flusher, canFlush := w.(http.Flusher)
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}
	flush()

	_ = h.svc.Deployments.StreamRuntimeLogs(r.Context(), serviceID, w, flush)
}

func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}
