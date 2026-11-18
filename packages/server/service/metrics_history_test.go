package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A node sampled each minute is stored from its second reading, rolled into
// five minutes after a day, read back over a range, and gone past the
// organization's retention.
func TestMetricsHistory(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	org := meshdb.Organization{Name: "metrics", Slug: "metrics"}
	require.NoError(t, gdb.Create(&org).Error)
	node := meshdb.Node{OrganizationID: org.ID, Name: "n1", TailscaleIP: "100.64.0.5", Status: meshdb.NodeOnline, K3sRole: meshdb.K3sRoleAgent}
	require.NoError(t, gdb.Create(&node).Error)

	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	reading := 0
	scrape := func(_ context.Context, id uuid.UUID) (*service.NodeMetrics, error) {
		reading++
		return &service.NodeMetrics{CPUTotalSeconds: float64(60 * reading), CPUIdleSeconds: float64(30 * reading),
			MemoryTotalBytes: 1 << 30, MemoryAvailableBytes: 1 << 29, DiskTotalBytes: 100, DiskAvailBytes: 50}, nil
	}
	h := service.NewMetricsHistoryForTest(gdb, scrape, &now)

	for i := 0; i < 11; i++ {
		h.Sample(ctx)
		now = now.Add(time.Minute)
	}
	hist, err := h.NodeHistory(ctx, org.ID, node.ID, "1h")
	require.NoError(t, err)
	require.Len(t, hist.Points, 10, "the first reading only starts the rates")
	assert.Equal(t, meshdb.MetricRaw, hist.Resolution)
	assert.InDelta(t, 50, hist.Points[0].CPUPct, 0.01)
	assert.Equal(t, 7, hist.RetentionDays)

	// A day on, the raw rows are five-minute rows.
	now = now.Add(25 * time.Hour)
	h.Rollup(ctx)
	var raw, rolled int64
	gdb.Model(&meshdb.NodeMetricSample{}).Where("resolution = ?", meshdb.MetricRaw).Count(&raw)
	gdb.Model(&meshdb.NodeMetricSample{}).Where("resolution = ?", meshdb.MetricRollup).Count(&rolled)
	assert.Zero(t, raw)
	assert.Equal(t, int64(3), rolled, "12:01 to 12:10 is three five-minute buckets")
	week, err := h.NodeHistory(ctx, org.ID, node.ID, "7d")
	require.NoError(t, err)
	assert.Equal(t, meshdb.MetricRollup, week.Resolution)
	assert.Len(t, week.Points, 3)

	_, err = h.SetRetention(ctx, org.ID, 2)
	assert.ErrorIs(t, err, service.ErrInvalidMetricRetention)
	set, err := h.SetRetention(ctx, org.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, set.RetentionDays)
	h.Rollup(ctx)
	gdb.Model(&meshdb.NodeMetricSample{}).Count(&rolled)
	assert.Zero(t, rolled, "past a one-day retention")

	_, err = h.NodeHistory(ctx, org.ID, node.ID, "2w")
	assert.ErrorIs(t, err, service.ErrMetricRange)
}
