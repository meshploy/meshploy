package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/meshploy/packages/client"
)

// metricPointsShown is the most points get_metrics returns: enough to see a
// shape, few enough to read. The summary covers every point.
const metricPointsShown = 60

func (s *srv) registerMetricsTools(ms *server.MCPServer) {
	ms.AddTool(
		mcp.NewTool("get_metrics",
			mcp.WithDescription("Stored metrics history for a node (node_id) or a service (project_id and service_id): CPU, memory, disk and network for a node; CPU, memory, pods and restarts for a service. Answers questions like what memory was doing last night. Returns min, average and max of each metric over the range, and up to 60 points across it."),
			mcp.WithString("node_id", mcp.Description("Node ID or name, for a node's history")),
			mcp.WithString("project_id", mcp.Description("Project ID, with service_id")),
			mcp.WithString("service_id", mcp.Description("Service ID or name, for a service's history")),
			mcp.WithString("range", mcp.Description("How far back: 1h, 6h, 24h (default), 7d or 30d, within what the organization keeps")),
		),
		s.handleGetMetrics,
	)
}

// MCPMetricSummary is one metric over the range.
type MCPMetricSummary struct {
	Min float64 `json:"min"`
	Avg float64 `json:"avg"`
	Max float64 `json:"max"`
}

// MCPMetrics is what get_metrics returns.
type MCPMetrics struct {
	Range         string                      `json:"range"`
	Resolution    string                      `json:"resolution"`
	RetentionDays int                         `json:"retention_days"`
	Samples       int                         `json:"samples"`
	Summary       map[string]MCPMetricSummary `json:"summary"`
	Points        []client.MetricPoint        `json:"points"`
}

func (s *srv) handleGetMetrics(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rng := mcp.ParseString(req, "range", "24h")
	nodeRef := mcp.ParseString(req, "node_id", "")
	projectID := mcp.ParseString(req, "project_id", "")
	serviceRef := mcp.ParseString(req, "service_id", "")

	var hist *client.MetricHistory
	switch {
	case nodeRef != "":
		nodes, err := s.c.ListNodes(s.orgID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		id := ""
		for _, n := range nodes {
			if n.ID == nodeRef || n.Name == nodeRef {
				id = n.ID
			}
		}
		if id == "" {
			return mcp.NewToolResultError("node " + nodeRef + " not found"), nil
		}
		if hist, err = s.c.NodeMetricsHistory(s.orgID, id, rng); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	case projectID != "" && serviceRef != "":
		svc, err := s.c.GetServiceByName(s.orgID, projectID, serviceRef)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if hist, err = s.c.ServiceMetricsHistory(s.orgID, projectID, svc.ID, rng); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	default:
		return mcp.NewToolResultError("name a node (node_id) or a service (project_id and service_id)"), nil
	}
	return jsonResult(summarizeMetrics(hist))
}

// summarizeMetrics reduces a history to each metric's min, average and max,
// and at most metricPointsShown points spread evenly across it.
func summarizeMetrics(h *client.MetricHistory) MCPMetrics {
	out := MCPMetrics{Range: h.Range, Resolution: h.Resolution, RetentionDays: h.RetentionDays,
		Samples: len(h.Points), Summary: map[string]MCPMetricSummary{}, Points: []client.MetricPoint{}}
	counts := map[string]int{}
	for _, p := range h.Points {
		for k, v := range p {
			f, ok := v.(float64)
			if !ok {
				continue
			}
			m, seen := out.Summary[k]
			if !seen {
				m = MCPMetricSummary{Min: f, Max: f}
			}
			m.Min, m.Max = min(m.Min, f), max(m.Max, f)
			m.Avg += f
			out.Summary[k] = m
			counts[k]++
		}
	}
	for k, m := range out.Summary {
		m.Avg /= float64(counts[k])
		out.Summary[k] = m
	}
	step := max(1, (len(h.Points)+metricPointsShown-1)/metricPointsShown)
	for i := 0; i < len(h.Points); i += step {
		out.Points = append(out.Points, h.Points[i])
	}
	return out
}
