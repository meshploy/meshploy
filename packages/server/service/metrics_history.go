package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Metrics history: every minute each online node's node_exporter and every
// service's pods are read and stored, one row per node and per service. Raw
// rows are kept for a day; after that each five minutes of them become one
// row, and rows older than the organization's retention are deleted.

const (
	metricSampleEvery = time.Minute
	metricRawFor      = 24 * time.Hour
	metricRollupEvery = time.Hour
	metricBucket      = 5 * time.Minute
)

// ErrMetricRange is a history range that is not offered.
var ErrMetricRange = errors.New("unknown metrics range")

// MetricRanges are the ranges a history can be read over.
var MetricRanges = map[string]time.Duration{
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// MetricsHistoryService samples, rolls up and reads metrics history.
type MetricsHistoryService struct {
	db *gorm.DB
	// scrape reads a node's node_exporter.
	scrape func(ctx context.Context, nodeID uuid.UUID) (*NodeMetrics, error)
	// cluster is how the cluster is reached; nil without one.
	cluster appk8s.Driver
	now     func() time.Time

	mu   sync.Mutex
	prev map[uuid.UUID]nodeReading
}

type nodeReading struct {
	m  *NodeMetrics
	at time.Time
}

func newMetricsHistory(gdb *gorm.DB, scrape func(context.Context, uuid.UUID) (*NodeMetrics, error), cluster appk8s.Driver) *MetricsHistoryService {
	return &MetricsHistoryService{db: gdb, scrape: scrape, cluster: cluster, now: time.Now, prev: map[uuid.UUID]nodeReading{}}
}

// Start samples once a minute and rolls up once an hour until ctx ends.
func (s *MetricsHistoryService) Start(ctx context.Context) {
	sample := time.NewTicker(metricSampleEvery)
	rollup := time.NewTicker(metricRollupEvery)
	defer sample.Stop()
	defer rollup.Stop()
	s.Rollup(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-sample.C:
			s.Sample(ctx)
		case <-rollup.C:
			s.Rollup(ctx)
		}
	}
}

// Sample takes one reading of every online node and every service.
func (s *MetricsHistoryService) Sample(ctx context.Context) {
	at := s.now().UTC().Truncate(time.Second)
	if err := s.sampleNodes(ctx, at); err != nil {
		log.Printf("warning: metrics history, nodes: %v", err)
	}
	if err := s.sampleServices(ctx, at); err != nil {
		log.Printf("warning: metrics history, services: %v", err)
	}
}

func (s *MetricsHistoryService) sampleNodes(ctx context.Context, at time.Time) error {
	var nodes []db.Node
	if err := s.db.WithContext(ctx).Select("id").
		Where("status = ? AND tailscale_ip <> '' AND os <> ?", db.NodeOnline, db.NodeOSWindows).
		Find(&nodes).Error; err != nil {
		return err
	}
	type read struct {
		id uuid.UUID
		m  *NodeMetrics
	}
	results := make(chan read, len(nodes))
	var wg sync.WaitGroup
	for _, n := range nodes {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if m, err := s.scrape(rctx, id); err == nil && m != nil {
				results <- read{id, m}
			}
		}(n.ID)
	}
	wg.Wait()
	close(results)

	var rows []db.NodeMetricSample
	s.mu.Lock()
	for r := range results {
		prev, ok := s.prev[r.id]
		s.prev[r.id] = nodeReading{r.m, at}
		if !ok {
			continue // rates need a reading before
		}
		if row, ok := nodeSample(r.id, prev.m, r.m, at.Sub(prev.at), at); ok {
			rows = append(rows, row)
		}
	}
	s.mu.Unlock()
	if len(rows) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Create(&rows).Error
}

// nodeSample is the row two successive readings make. A counter that went
// backwards (a reboot) makes none.
func nodeSample(id uuid.UUID, prev, cur *NodeMetrics, dt time.Duration, at time.Time) (db.NodeMetricSample, bool) {
	secs := dt.Seconds()
	dTotal := cur.CPUTotalSeconds - prev.CPUTotalSeconds
	dIdle := cur.CPUIdleSeconds - prev.CPUIdleSeconds
	dRx := cur.NetRxBytes - prev.NetRxBytes
	dTx := cur.NetTxBytes - prev.NetTxBytes
	if secs <= 0 || dTotal <= 0 || dIdle < 0 || dRx < 0 || dTx < 0 {
		return db.NodeMetricSample{}, false
	}
	cpu := min(max(100*(1-dIdle/dTotal), 0), 100)
	mem := max(cur.MemoryTotalBytes-cur.MemoryAvailableBytes, 0)
	return db.NodeMetricSample{
		NodeID: id, Resolution: db.MetricRaw, At: at,
		CPUPct: cpu, CPUPctMax: cpu,
		MemUsed: mem, MemUsedMax: mem, MemTotal: cur.MemoryTotalBytes,
		DiskUsed: max(cur.DiskTotalBytes-cur.DiskAvailBytes, 0), DiskTotal: cur.DiskTotalBytes,
		NetRxBps: float64(dRx) / secs, NetTxBps: float64(dTx) / secs,
	}, true
}

// workloadKey is where a service's pods are: its namespace and app label.
type workloadKey struct{ namespace, app string }

func (s *MetricsHistoryService) sampleServices(ctx context.Context, at time.Time) error {
	if s.cluster == nil {
		return nil
	}
	client, cfg := s.cluster.Client(), s.cluster.RestConfig()
	if client == nil || cfg == nil {
		return nil
	}
	owners, err := s.workloadOwners(ctx)
	if err != nil || len(owners) == 0 {
		return err
	}
	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rows, err := serviceSamples(rctx, client, cfg, owners, at)
	if err != nil || len(rows) == 0 {
		return err
	}
	return s.db.WithContext(ctx).Create(&rows).Error
}

// workloadOwners maps each service's pods to the service.
func (s *MetricsHistoryService) workloadOwners(ctx context.Context) (map[workloadKey]uuid.UUID, error) {
	var svcs []db.Service
	if err := s.db.WithContext(ctx).Preload("Project").
		Where("type IN ?", []db.ServiceType{db.ServiceTypeApplication, db.ServiceTypeDatabase}).
		Find(&svcs).Error; err != nil {
		return nil, err
	}
	var dcs []db.DatabaseConfig
	if err := s.db.WithContext(ctx).Select("service_id", "slug").Find(&dcs).Error; err != nil {
		return nil, err
	}
	dbSlug := make(map[uuid.UUID]string, len(dcs))
	for _, dc := range dcs {
		dbSlug[dc.ServiceID] = dc.Slug
	}
	owners := make(map[workloadKey]uuid.UUID, len(svcs))
	for i := range svcs {
		sv := &svcs[i]
		name := appK8sName(sv)
		if sv.Type == db.ServiceTypeDatabase {
			name = dbSlug[sv.ID]
		}
		if name != "" && sv.Project.Slug != "" {
			owners[workloadKey{sv.Project.Slug, name}] = sv.ID
		}
	}
	return owners, nil
}

// serviceSamples sums each service's running pods: their usage from the
// metrics API, their count and restarts from the pods themselves.
func serviceSamples(ctx context.Context, client kubernetes.Interface, cfg *rest.Config, owners map[workloadKey]uuid.UUID, at time.Time) ([]db.ServiceMetricSample, error) {
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{LabelSelector: "managed-by=meshploy"})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	usage, err := appk8s.ListPodUsage(ctx, cfg, "managed-by=meshploy")
	if err != nil {
		return nil, err
	}
	used := make(map[[2]string]appk8s.PodUsage, len(usage))
	for _, u := range usage {
		used[[2]string{u.Namespace, u.Pod}] = u
	}
	byService := map[uuid.UUID]*db.ServiceMetricSample{}
	for i := range pods.Items {
		p := &pods.Items[i]
		id, ok := owners[workloadKey{p.Namespace, p.Labels["app"]}]
		if !ok || p.Status.Phase != corev1.PodRunning {
			continue
		}
		row := byService[id]
		if row == nil {
			row = &db.ServiceMetricSample{ServiceID: id, Resolution: db.MetricRaw, At: at}
			byService[id] = row
		}
		row.Pods++
		for _, c := range p.Status.ContainerStatuses {
			row.Restarts += int(c.RestartCount)
		}
		u := used[[2]string{p.Namespace, p.Name}]
		row.CPUMillis += u.CPUMillis
		row.MemBytes += u.MemBytes
	}
	rows := make([]db.ServiceMetricSample, 0, len(byService))
	for _, r := range byService {
		r.CPUMillisMax, r.MemBytesMax = r.CPUMillis, r.MemBytes
		rows = append(rows, *r)
	}
	return rows, nil
}

// Rollup turns raw rows older than a day into five-minute rows, and deletes
// what is past each organization's retention and what belongs to a node or
// service that is gone.
func (s *MetricsHistoryService) Rollup(ctx context.Context) {
	// Whole buckets only, so no five minutes are split across two runs.
	cutoff := s.now().UTC().Add(-metricRawFor).Truncate(metricBucket)
	if err := s.rollupNodes(ctx, cutoff); err != nil {
		log.Printf("warning: metrics rollup, nodes: %v", err)
	}
	if err := s.rollupServices(ctx, cutoff); err != nil {
		log.Printf("warning: metrics rollup, services: %v", err)
	}
	if err := s.expire(ctx); err != nil {
		log.Printf("warning: metrics retention: %v", err)
	}
}

func (s *MetricsHistoryService) rollupNodes(ctx context.Context, cutoff time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var raw []db.NodeMetricSample
		if err := tx.Where("resolution = ? AND at < ?", db.MetricRaw, cutoff).Order("at").Find(&raw).Error; err != nil {
			return err
		}
		if len(raw) == 0 {
			return nil
		}
		rolled := rollupNodeSamples(raw)
		if err := tx.Create(&rolled).Error; err != nil {
			return err
		}
		return tx.Where("resolution = ? AND at < ?", db.MetricRaw, cutoff).Delete(&db.NodeMetricSample{}).Error
	})
}

func (s *MetricsHistoryService) rollupServices(ctx context.Context, cutoff time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var raw []db.ServiceMetricSample
		if err := tx.Where("resolution = ? AND at < ?", db.MetricRaw, cutoff).Order("at").Find(&raw).Error; err != nil {
			return err
		}
		if len(raw) == 0 {
			return nil
		}
		rolled := rollupServiceSamples(raw)
		if err := tx.Create(&rolled).Error; err != nil {
			return err
		}
		return tx.Where("resolution = ? AND at < ?", db.MetricRaw, cutoff).Delete(&db.ServiceMetricSample{}).Error
	})
}

// rollupNodeSamples averages each node's rows over five-minute buckets,
// keeping the highest CPU and memory beside the averages. Totals are the
// bucket's last.
func rollupNodeSamples(rows []db.NodeMetricSample) []db.NodeMetricSample {
	type key struct {
		id uuid.UUID
		at time.Time
	}
	sums := map[key]*db.NodeMetricSample{}
	counts := map[key]int{}
	var order []key
	for _, r := range rows {
		k := key{r.NodeID, r.At.UTC().Truncate(metricBucket)}
		b := sums[k]
		if b == nil {
			b = &db.NodeMetricSample{NodeID: r.NodeID, Resolution: db.MetricRollup, At: k.at}
			sums[k] = b
			order = append(order, k)
		}
		counts[k]++
		b.CPUPct += r.CPUPct
		b.CPUPctMax = max(b.CPUPctMax, r.CPUPctMax)
		b.MemUsed += r.MemUsed
		b.MemUsedMax = max(b.MemUsedMax, r.MemUsedMax)
		b.DiskUsed += r.DiskUsed
		b.NetRxBps += r.NetRxBps
		b.NetTxBps += r.NetTxBps
		b.MemTotal, b.DiskTotal = r.MemTotal, r.DiskTotal
	}
	out := make([]db.NodeMetricSample, 0, len(order))
	for _, k := range order {
		b, n := sums[k], counts[k]
		b.CPUPct /= float64(n)
		b.MemUsed /= int64(n)
		b.DiskUsed /= int64(n)
		b.NetRxBps /= float64(n)
		b.NetTxBps /= float64(n)
		out = append(out, *b)
	}
	return out
}

// rollupServiceSamples is rollupNodeSamples for services. Pods and restarts
// are the bucket's last, as restarts only ever count up.
func rollupServiceSamples(rows []db.ServiceMetricSample) []db.ServiceMetricSample {
	type key struct {
		id uuid.UUID
		at time.Time
	}
	sums := map[key]*db.ServiceMetricSample{}
	counts := map[key]int64{}
	var order []key
	for _, r := range rows {
		k := key{r.ServiceID, r.At.UTC().Truncate(metricBucket)}
		b := sums[k]
		if b == nil {
			b = &db.ServiceMetricSample{ServiceID: r.ServiceID, Resolution: db.MetricRollup, At: k.at}
			sums[k] = b
			order = append(order, k)
		}
		counts[k]++
		b.CPUMillis += r.CPUMillis
		b.CPUMillisMax = max(b.CPUMillisMax, r.CPUMillisMax)
		b.MemBytes += r.MemBytes
		b.MemBytesMax = max(b.MemBytesMax, r.MemBytesMax)
		b.Pods, b.Restarts = r.Pods, r.Restarts
	}
	out := make([]db.ServiceMetricSample, 0, len(order))
	for _, k := range order {
		b, n := sums[k], counts[k]
		b.CPUMillis /= n
		b.MemBytes /= n
		out = append(out, *b)
	}
	return out
}

// expire deletes rows past each organization's retention, and rows of nodes
// and services that no longer exist.
func (s *MetricsHistoryService) expire(ctx context.Context) error {
	tx := s.db.WithContext(ctx)
	var orgs []db.Organization
	if err := tx.Select("id", "metric_retention_days").Find(&orgs).Error; err != nil {
		return err
	}
	now := s.now().UTC()
	for _, o := range orgs {
		before := now.Add(-time.Duration(MetricRetentionDays(o.MetricRetentionDays)) * 24 * time.Hour)
		nodes := tx.Model(&db.Node{}).Select("id").Where("organization_id = ?", o.ID)
		if err := tx.Where("node_id IN (?) AND at < ?", nodes, before).Delete(&db.NodeMetricSample{}).Error; err != nil {
			return err
		}
		services := tx.Model(&db.Service{}).Select("services.id").
			Joins("JOIN projects ON projects.id = services.project_id").Where("projects.organization_id = ?", o.ID)
		if err := tx.Where("service_id IN (?) AND at < ?", services, before).Delete(&db.ServiceMetricSample{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Where("node_id NOT IN (?)", tx.Model(&db.Node{}).Select("id")).Delete(&db.NodeMetricSample{}).Error; err != nil {
		return err
	}
	return tx.Where("service_id NOT IN (?)", tx.Model(&db.Service{}).Select("id")).Delete(&db.ServiceMetricSample{}).Error
}

// MetricRetentionDays is the retention an organization's stored choice means.
func MetricRetentionDays(stored int) int {
	if slices.Contains(db.MetricRetentionChoices, stored) {
		return stored
	}
	return db.DefaultMetricRetentionDays
}

// MetricSettings is how long an organization keeps metrics, and what it may
// choose from.
type MetricSettings struct {
	RetentionDays int   `json:"retention_days"`
	Choices       []int `json:"choices"`
}

// Settings returns an organization's metrics settings.
func (s *MetricsHistoryService) Settings(ctx context.Context, orgID uuid.UUID) (*MetricSettings, error) {
	var org db.Organization
	if err := s.db.WithContext(ctx).Select("id", "metric_retention_days").First(&org, "id = ?", orgID).Error; err != nil {
		return nil, err
	}
	return &MetricSettings{RetentionDays: MetricRetentionDays(org.MetricRetentionDays), Choices: db.MetricRetentionChoices}, nil
}

// SetRetention changes how long an organization keeps metrics. Lowering it
// frees the space at the next hourly cleanup.
func (s *MetricsHistoryService) SetRetention(ctx context.Context, orgID uuid.UUID, days int) (*MetricSettings, error) {
	if !slices.Contains(db.MetricRetentionChoices, days) {
		return nil, fmt.Errorf("%w: keep metrics for one of %v days", ErrInvalidMetricRetention, db.MetricRetentionChoices)
	}
	if err := s.db.WithContext(ctx).Model(&db.Organization{}).Where("id = ?", orgID).Update("metric_retention_days", days).Error; err != nil {
		return nil, err
	}
	return s.Settings(ctx, orgID)
}

// ErrInvalidMetricRetention is a retention that is not one of the choices.
var ErrInvalidMetricRetention = errors.New("invalid metrics retention")

// MetricHistory is a node's or service's samples over a range, oldest first.
// Resolution is 1m for a range of a day or less and 5m beyond, where the last
// day's raw rows are averaged into five minutes too, so the points are even.
type MetricHistory[T any] struct {
	Range         string              `json:"range"`
	Resolution    db.MetricResolution `json:"resolution"`
	RetentionDays int                 `json:"retention_days"`
	Points        []T                 `json:"points"`
}

func (s *MetricsHistoryService) window(ctx context.Context, orgID uuid.UUID, rng string) (time.Time, db.MetricResolution, int, error) {
	d, ok := MetricRanges[rng]
	if !ok {
		return time.Time{}, "", 0, fmt.Errorf("%w %q", ErrMetricRange, rng)
	}
	set, err := s.Settings(ctx, orgID)
	if err != nil {
		return time.Time{}, "", 0, err
	}
	res := db.MetricRaw
	if d > metricRawFor {
		res = db.MetricRollup
	}
	return s.now().UTC().Add(-d), res, set.RetentionDays, nil
}

// NodeHistory reads a node's samples over rng (a key of MetricRanges).
func (s *MetricsHistoryService) NodeHistory(ctx context.Context, orgID, nodeID uuid.UUID, rng string) (*MetricHistory[db.NodeMetricSample], error) {
	since, res, keep, err := s.window(ctx, orgID, rng)
	if err != nil {
		return nil, err
	}
	var rows []db.NodeMetricSample
	if err := s.db.WithContext(ctx).Where("node_id = ? AND at >= ?", nodeID, since).Order("at").Find(&rows).Error; err != nil {
		return nil, err
	}
	var raw, rolled []db.NodeMetricSample
	for _, r := range rows {
		if r.Resolution == db.MetricRaw {
			raw = append(raw, r)
		} else {
			rolled = append(rolled, r)
		}
	}
	points := raw
	if res == db.MetricRollup {
		points = append(rolled, rollupNodeSamples(raw)...)
	}
	if points == nil {
		points = []db.NodeMetricSample{}
	}
	return &MetricHistory[db.NodeMetricSample]{Range: rng, Resolution: res, RetentionDays: keep, Points: points}, nil
}

// ServiceHistory reads a service's samples over rng (a key of MetricRanges).
func (s *MetricsHistoryService) ServiceHistory(ctx context.Context, orgID, serviceID uuid.UUID, rng string) (*MetricHistory[db.ServiceMetricSample], error) {
	since, res, keep, err := s.window(ctx, orgID, rng)
	if err != nil {
		return nil, err
	}
	var rows []db.ServiceMetricSample
	if err := s.db.WithContext(ctx).Where("service_id = ? AND at >= ?", serviceID, since).Order("at").Find(&rows).Error; err != nil {
		return nil, err
	}
	var raw, rolled []db.ServiceMetricSample
	for _, r := range rows {
		if r.Resolution == db.MetricRaw {
			raw = append(raw, r)
		} else {
			rolled = append(rolled, r)
		}
	}
	points := raw
	if res == db.MetricRollup {
		points = append(rolled, rollupServiceSamples(raw)...)
	}
	if points == nil {
		points = []db.ServiceMetricSample{}
	}
	return &MetricHistory[db.ServiceMetricSample]{Range: rng, Resolution: res, RetentionDays: keep, Points: points}, nil
}
