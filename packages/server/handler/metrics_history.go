package handler

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
)

func (h *Handler) registerMetricsHistoryRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-node-metrics-history",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/nodes/{nodeId}/metrics/history",
		Summary:     "A node's stored CPU, memory, disk and network over a range",
		Tags:        []string{"Nodes"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetNodeMetricsHistory)

	huma.Register(api, huma.Operation{
		OperationID: "get-service-metrics-history",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/metrics/history",
		Summary:     "A service's stored CPU, memory, pods and restarts over a range",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetServiceMetricsHistory)

	huma.Register(api, huma.Operation{
		OperationID: "get-metric-settings",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/metrics/settings",
		Summary:     "How long the organization keeps metrics",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetMetricSettings)

	huma.Register(api, huma.Operation{
		OperationID: "update-metric-settings",
		Method:      "PUT",
		Path:        "/api/v1/orgs/{orgId}/metrics/settings",
		Summary:     "Change how long the organization keeps metrics (admins)",
		Tags:        []string{"Organizations"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateMetricSettings)
}

type MetricRangeQuery struct {
	Range string `query:"range" enum:"1h,6h,24h,7d,30d" default:"24h" doc:"How far back"`
}

type NodeMetricsHistoryInput struct {
	NodePathInput
	MetricRangeQuery
}

type NodeMetricsHistoryOutput struct {
	Body *service.MetricHistory[db.NodeMetricSample]
}

func (h *Handler) GetNodeMetricsHistory(ctx context.Context, input *NodeMetricsHistoryInput) (*NodeMetricsHistoryOutput, error) {
	_, orgID, nodeID, err := h.checkOrgMemberAccess(ctx, input.OrgID, input.NodeID)
	if err != nil {
		return nil, err
	}
	if _, err := h.svc.Nodes.GetInOrg(ctx, orgID, nodeID); err != nil {
		return nil, notFound(err)
	}
	hist, err := h.svc.MetricsHistory.NodeHistory(ctx, orgID, nodeID, input.Range)
	if err != nil {
		return nil, metricsError(err)
	}
	return &NodeMetricsHistoryOutput{Body: hist}, nil
}

type ServiceMetricsHistoryInput struct {
	WorkloadPathInput
	MetricRangeQuery
}

type ServiceMetricsHistoryOutput struct {
	Body *service.MetricHistory[db.ServiceMetricSample]
}

func (h *Handler) GetServiceMetricsHistory(ctx context.Context, input *ServiceMetricsHistoryInput) (*ServiceMetricsHistoryOutput, error) {
	_, orgID, serviceID, _, err := h.checkAccess(ctx, input.OrgID, input.ServiceID, db.ResourceService, db.ActionView, input.ProjectID)
	if err != nil {
		return nil, err
	}
	hist, err := h.svc.MetricsHistory.ServiceHistory(ctx, orgID, serviceID, input.Range)
	if err != nil {
		return nil, metricsError(err)
	}
	return &ServiceMetricsHistoryOutput{Body: hist}, nil
}

type MetricSettingsOutput struct {
	Body *service.MetricSettings
}

func (h *Handler) GetMetricSettings(ctx context.Context, input *OrgPathInput) (*MetricSettingsOutput, error) {
	_, orgID, _, err := h.checkOrgMemberAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	set, err := h.svc.MetricsHistory.Settings(ctx, orgID)
	if err != nil {
		return nil, notFound(err)
	}
	return &MetricSettingsOutput{Body: set}, nil
}

type UpdateMetricSettingsInput struct {
	OrgPathInput
	Body struct {
		RetentionDays int `json:"retention_days" doc:"Days to keep metrics: 1, 3, 7, 14 or 30"`
	}
}

func (h *Handler) UpdateMetricSettings(ctx context.Context, input *UpdateMetricSettingsInput) (*MetricSettingsOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	set, err := h.svc.MetricsHistory.SetRetention(ctx, orgID, input.Body.RetentionDays)
	if err != nil {
		return nil, metricsError(err)
	}
	return &MetricSettingsOutput{Body: set}, nil
}

func metricsError(err error) error {
	if errors.Is(err, service.ErrMetricRange) || errors.Is(err, service.ErrInvalidMetricRetention) {
		return huma.Error422UnprocessableEntity(err.Error())
	}
	return huma.Error500InternalServerError("metrics history: " + err.Error())
}
