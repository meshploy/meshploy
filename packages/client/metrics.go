package client

import "net/url"

// MetricPoint is one stored metrics sample: a node's fields or a service's,
// keyed as the API names them (cpu_pct, mem_used, cpu_millis, ...), with at.
type MetricPoint map[string]any

// MetricHistory is a node's or service's stored samples over a range.
type MetricHistory struct {
	Range         string        `json:"range"`
	Resolution    string        `json:"resolution"`
	RetentionDays int           `json:"retention_days"`
	Points        []MetricPoint `json:"points"`
}

// NodeMetricsHistory reads a node's stored metrics over rng (1h, 6h, 24h, 7d, 30d).
func (c *Client) NodeMetricsHistory(orgID, nodeID, rng string) (*MetricHistory, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/nodes/"+nodeID+"/metrics/history?range="+url.QueryEscape(rng), nil)
	if err != nil {
		return nil, err
	}
	return decode[*MetricHistory](resp)
}

// ServiceMetricsHistory reads a service's stored metrics over rng.
func (c *Client) ServiceMetricsHistory(orgID, projectID, serviceID, rng string) (*MetricHistory, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/metrics/history?range="+url.QueryEscape(rng), nil)
	if err != nil {
		return nil, err
	}
	return decode[*MetricHistory](resp)
}
