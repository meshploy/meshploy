package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/rest"
)

// PodMetrics holds current CPU and memory usage for a single pod.
type PodMetrics struct {
	PodName   string `json:"pod_name"`
	CPUMillis int64  `json:"cpu_millis"`  // milli-cores (e.g. 150 = 150m = 0.15 cores)
	MemoryMiB int64  `json:"memory_mib"`  // mebibytes
}

// minimal structs for parsing metrics.k8s.io/v1beta1 PodMetricsList
type podMetricsList struct {
	Items []podMetricsItem `json:"items"`
}

type podMetricsItem struct {
	Metadata   struct{ Name string `json:"name"` }   `json:"metadata"`
	Containers []struct {
		Usage struct {
			CPU    string `json:"cpu"`
			Memory string `json:"memory"`
		} `json:"usage"`
	} `json:"containers"`
}

// GetPodMetrics queries the Kubernetes Metrics API for all pods matching
// labelSelector in the given namespace. Returns an empty slice (not an error)
// if metrics-server is unavailable or no pods are found.
func GetPodMetrics(ctx context.Context, restCfg *rest.Config, namespace, labelSelector string) ([]PodMetrics, error) {
	httpClient, err := rest.HTTPClientFor(restCfg)
	if err != nil {
		return nil, fmt.Errorf("build http client: %w", err)
	}

	apiURL := fmt.Sprintf(
		"%s/apis/metrics.k8s.io/v1beta1/namespaces/%s/pods?labelSelector=%s",
		restCfg.Host,
		url.PathEscape(namespace),
		url.QueryEscape(labelSelector),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metrics api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusServiceUnavailable {
		// metrics-server not installed — return empty, not an error
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("metrics api %d: %s", resp.StatusCode, body)
	}

	var list podMetricsList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode metrics: %w", err)
	}

	out := make([]PodMetrics, 0, len(list.Items))
	for _, item := range list.Items {
		var cpuMillis, memMiB int64
		for _, c := range item.Containers {
			if q, err := resource.ParseQuantity(c.Usage.CPU); err == nil {
				cpuMillis += q.MilliValue()
			}
			if q, err := resource.ParseQuantity(c.Usage.Memory); err == nil {
				memMiB += q.Value() / (1024 * 1024)
			}
		}
		out = append(out, PodMetrics{
			PodName:   item.Metadata.Name,
			CPUMillis: cpuMillis,
			MemoryMiB: memMiB,
		})
	}
	return out, nil
}
