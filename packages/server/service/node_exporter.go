package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// NodeMetrics holds live resource usage scraped from a node_exporter instance.
// CPU and network fields are cumulative counters; the frontend derives rates
// from the delta between two successive readings.
type NodeMetrics struct {
	CPUTotalSeconds float64 `json:"cpu_total_seconds"`
	CPUIdleSeconds  float64 `json:"cpu_idle_seconds"`
	CPUCores        int     `json:"cpu_cores"`

	MemoryTotalBytes     int64 `json:"memory_total_bytes"`
	MemoryAvailableBytes int64 `json:"memory_available_bytes"`

	DiskTotalBytes int64 `json:"disk_total_bytes"`
	DiskAvailBytes int64 `json:"disk_avail_bytes"`

	NetRxBytes int64 `json:"net_rx_bytes"`
	NetTxBytes int64 `json:"net_tx_bytes"`
}

var neHTTPClient = &http.Client{Timeout: 5 * time.Second}

func scrapeNodeExporter(ctx context.Context, tailscaleIP string) (*NodeMetrics, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+tailscaleIP+":9100/metrics", nil)
	if err != nil {
		return nil, err
	}
	resp, err := neHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("node_exporter unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("node_exporter returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read metrics body: %w", err)
	}
	return parseNodeExporterBody(body)
}

func parseNodeExporterBody(body []byte) (*NodeMetrics, error) {
	var m NodeMetrics
	cpuSet := make(map[string]bool)
	var diskSizeFound, diskAvailFound bool

	for _, line := range strings.Split(string(body), "\n") {
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		name, labels, val, ok := promParseLine(line)
		if !ok {
			continue
		}
		switch name {
		case "node_cpu_seconds_total":
			if cpu := promLabel(labels, "cpu"); cpu != "" {
				cpuSet[cpu] = true
			}
			m.CPUTotalSeconds += val
			if promLabel(labels, "mode") == "idle" {
				m.CPUIdleSeconds += val
			}
		case "node_memory_MemTotal_bytes":
			m.MemoryTotalBytes = int64(val)
		case "node_memory_MemAvailable_bytes":
			m.MemoryAvailableBytes = int64(val)
		case "node_filesystem_size_bytes":
			if !diskSizeFound && promLabel(labels, "mountpoint") == "/" && !promVirtualFS(promLabel(labels, "fstype")) {
				m.DiskTotalBytes = int64(val)
				diskSizeFound = true
			}
		case "node_filesystem_avail_bytes":
			if !diskAvailFound && promLabel(labels, "mountpoint") == "/" && !promVirtualFS(promLabel(labels, "fstype")) {
				m.DiskAvailBytes = int64(val)
				diskAvailFound = true
			}
		case "node_network_receive_bytes_total":
			if promLabel(labels, "device") != "lo" {
				m.NetRxBytes += int64(val)
			}
		case "node_network_transmit_bytes_total":
			if promLabel(labels, "device") != "lo" {
				m.NetTxBytes += int64(val)
			}
		}
	}
	m.CPUCores = len(cpuSet)
	return &m, nil
}

// promParseLine parses one line of Prometheus text exposition format.
func promParseLine(line string) (name, labels string, val float64, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return
	}
	v, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return
	}
	nl := fields[0]
	if i := strings.IndexByte(nl, '{'); i >= 0 {
		return nl[:i], nl[i:], v, true
	}
	return nl, "", v, true
}

// promLabel extracts a label value from a Prometheus labels string like {key="value",…}.
func promLabel(labels, key string) string {
	search := key + `="`
	i := strings.Index(labels, search)
	if i < 0 {
		return ""
	}
	s := i + len(search)
	e := strings.IndexByte(labels[s:], '"')
	if e < 0 {
		return ""
	}
	return labels[s : s+e]
}

var promVirtualFSTypes = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "sysfs": true, "proc": true,
	"cgroup": true, "cgroup2": true, "devpts": true,
	"overlay": true, "overlayfs": true, "squashfs": true, "aufs": true,
}

func promVirtualFS(fstype string) bool { return promVirtualFSTypes[fstype] }
