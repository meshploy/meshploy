package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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

	huma.Register(api, huma.Operation{
		OperationID:   "rollback-deployment",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}/rollback",
		Summary:       "Roll back to a previous successful deployment",
		Tags:          []string{"Deployments"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 202,
	}, h.RollbackDeployment)

}

func (h *Handler) TriggerDeployment(ctx context.Context, input *ListDeploymentsInput) (*GetDeploymentOutput, error) {
	userID, _, serviceID, _, err := h.checkAccess(ctx, input.OrgID, input.ServiceID, db.ResourceService, db.ActionDeploy, input.ProjectID)
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
	_, _, serviceID, _, err := h.checkAccess(ctx, input.OrgID, input.ServiceID, db.ResourceService, db.ActionView, input.ProjectID)
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
	_, _, serviceID, _, err := h.checkAccess(ctx, input.OrgID, input.ServiceID, db.ResourceService, db.ActionView, input.ProjectID)
	if err != nil {
		return nil, err
	}
	deploymentID, err := parseUUID(input.DeploymentID)
	if err != nil {
		return nil, err
	}
	deployment, err := h.svc.Deployments.Get(ctx, deploymentID, serviceID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetDeploymentOutput{Body: deployment}, nil
}

func (h *Handler) CancelDeployment(ctx context.Context, input *DeploymentPathInput) (*struct{}, error) {
	_, _, _, _, err := h.checkAccess(ctx, input.OrgID, input.ServiceID, db.ResourceService, db.ActionDeploy, input.ProjectID)
	if err != nil {
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

func (h *Handler) RollbackDeployment(ctx context.Context, input *DeploymentPathInput) (*GetDeploymentOutput, error) {
	_, _, _, _, err := h.checkAccess(ctx, input.OrgID, input.ServiceID, db.ResourceService, db.ActionDeploy, input.ProjectID)
	if err != nil {
		return nil, err
	}
	deploymentID, err := parseUUID(input.DeploymentID)
	if err != nil {
		return nil, err
	}
	dep, err := h.svc.Deployments.Rollback(ctx, deploymentID)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &GetDeploymentOutput{Body: dep}, nil
}

func (h *Handler) DeleteDeploymentRecord(ctx context.Context, input *DeploymentPathInput) (*struct{}, error) {
	_, _, _, _, err := h.checkAccess(ctx, input.OrgID, input.ServiceID, db.ResourceService, db.ActionDelete, input.ProjectID)
	if err != nil {
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
	userID, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "orgId"))
	if err != nil {
		http.Error(w, "invalid org id", http.StatusBadRequest)
		return
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		http.Error(w, "invalid project id", http.StatusBadRequest)
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "serviceId"))
	if err != nil {
		http.Error(w, "invalid service id", http.StatusBadRequest)
		return
	}
	if err := h.svc.Permissions.CheckAccess(r.Context(), orgID, userID, serviceID, db.ResourceService, db.ActionView, &projectID); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
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
// Query params: tail=<n>  since=<duration>  follow=<true|false>
func (h *Handler) StreamServiceLogs(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "orgId"))
	if err != nil {
		http.Error(w, "invalid org id", http.StatusBadRequest)
		return
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		http.Error(w, "invalid project id", http.StatusBadRequest)
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "serviceId"))
	if err != nil {
		http.Error(w, "invalid service id", http.StatusBadRequest)
		return
	}
	if err := h.svc.Permissions.CheckAccess(r.Context(), orgID, userID, serviceID, db.ResourceService, db.ActionView, &projectID); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	q := r.URL.Query()
	tail, _ := strconv.ParseInt(q.Get("tail"), 10, 64)
	follow := q.Get("follow") != "false" // default true
	opts := service.LogOptions{TailLines: tail, Follow: follow, Since: q.Get("since")}

	sseHeaders(w)

	flusher, canFlush := w.(http.Flusher)
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}
	flush()

	_ = h.svc.Deployments.StreamRuntimeLogs(r.Context(), serviceID, opts, w, flush)
}

// GetServiceLogs returns a plain-text snapshot of container logs (no streaming).
// GET /api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/logs
// Query params: tail=<n>  since=<duration>
func (h *Handler) GetServiceLogs(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "orgId"))
	if err != nil {
		http.Error(w, "invalid org id", http.StatusBadRequest)
		return
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		http.Error(w, "invalid project id", http.StatusBadRequest)
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "serviceId"))
	if err != nil {
		http.Error(w, "invalid service id", http.StatusBadRequest)
		return
	}
	if err := h.svc.Permissions.CheckAccess(r.Context(), orgID, userID, serviceID, db.ResourceService, db.ActionView, &projectID); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	q := r.URL.Query()
	tail, _ := strconv.ParseInt(q.Get("tail"), 10, 64)
	opts := service.LogOptions{TailLines: tail, Since: q.Get("since")}

	logs, err := h.svc.Deployments.FetchRuntimeLogs(r.Context(), serviceID, opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="service.log"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(logs))
}

func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}
