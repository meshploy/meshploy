package db

import (
	"time"

	"github.com/google/uuid"
)

// MetricResolution is how much time one metric sample covers.
type MetricResolution string

const (
	// MetricRaw is one reading, taken once a minute.
	MetricRaw MetricResolution = "1m"
	// MetricRollup is five minutes of raw readings, averaged, with the highest
	// CPU and memory kept beside the average so a spike is not smoothed away.
	MetricRollup MetricResolution = "5m"
)

// Metrics are kept for one of these many days, 7 unless an organization
// chooses another.
var MetricRetentionChoices = []int{1, 3, 7, 14, 30}

// DefaultMetricRetentionDays is how long metrics are kept when nobody chose.
const DefaultMetricRetentionDays = 7

// NodeMetricSample is what a node's node_exporter read at At, or the average
// of five minutes of it. Rates are worked out when the row is written, so a
// reader never deals with counters or their resets. No foreign key: samples
// of a removed node go with the next cleanup, not with the node's delete.
type NodeMetricSample struct {
	NodeID     uuid.UUID        `gorm:"type:uuid;primaryKey"                  json:"-"`
	Resolution MetricResolution `gorm:"type:varchar(4);primaryKey"            json:"-"`
	At         time.Time        `gorm:"primaryKey;index:idx_node_metric_at"   json:"at"`
	CPUPct     float64          `gorm:"not null"                              json:"cpu_pct"`
	CPUPctMax  float64          `gorm:"not null"                              json:"cpu_pct_max"`
	MemUsed    int64            `gorm:"not null"                              json:"mem_used"`
	MemUsedMax int64            `gorm:"not null"                              json:"mem_used_max"`
	MemTotal   int64            `gorm:"not null"                              json:"mem_total"`
	DiskUsed   int64            `gorm:"not null"                              json:"disk_used"`
	DiskTotal  int64            `gorm:"not null"                              json:"disk_total"`
	NetRxBps   float64          `gorm:"not null"                              json:"net_rx_bps"`
	NetTxBps   float64          `gorm:"not null"                              json:"net_tx_bps"`
}

// ServiceMetricSample is the sum over a service's pods at At, from the
// cluster's metrics API, or the average of five minutes of it. Restarts is
// the pods' restart count as Kubernetes reports it, cumulative.
type ServiceMetricSample struct {
	ServiceID    uuid.UUID        `gorm:"type:uuid;primaryKey"                     json:"-"`
	Resolution   MetricResolution `gorm:"type:varchar(4);primaryKey"               json:"-"`
	At           time.Time        `gorm:"primaryKey;index:idx_service_metric_at"   json:"at"`
	CPUMillis    int64            `gorm:"not null"                                 json:"cpu_millis"`
	CPUMillisMax int64            `gorm:"not null"                                 json:"cpu_millis_max"`
	MemBytes     int64            `gorm:"not null"                                 json:"mem_bytes"`
	MemBytesMax  int64            `gorm:"not null"                                 json:"mem_bytes_max"`
	Pods         int              `gorm:"not null"                                 json:"pods"`
	Restarts     int              `gorm:"not null"                                 json:"restarts"`
}
