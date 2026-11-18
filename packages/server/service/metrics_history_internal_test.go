package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
)

// Rates come from two readings; a counter that went backwards makes no row.
func TestNodeSampleRates(t *testing.T) {
	id := uuid.New()
	at := time.Unix(600, 0)
	prev := &NodeMetrics{CPUTotalSeconds: 100, CPUIdleSeconds: 80, NetRxBytes: 1000, NetTxBytes: 500,
		MemoryTotalBytes: 1000, MemoryAvailableBytes: 400, DiskTotalBytes: 100, DiskAvailBytes: 60}
	cur := &NodeMetrics{CPUTotalSeconds: 160, CPUIdleSeconds: 125, NetRxBytes: 7000, NetTxBytes: 1100,
		MemoryTotalBytes: 1000, MemoryAvailableBytes: 250, DiskTotalBytes: 100, DiskAvailBytes: 55}
	row, ok := nodeSample(id, prev, cur, time.Minute, at)
	if !ok {
		t.Fatal("no row from two good readings")
	}
	if row.CPUPct != 25 || row.MemUsed != 750 || row.DiskUsed != 45 || row.NetRxBps != 100 || row.NetTxBps != 10 {
		t.Errorf("row = %+v", row)
	}
	if _, ok := nodeSample(id, cur, prev, time.Minute, at); ok {
		t.Error("a reboot's readings made a row")
	}
}

// Five minutes of rows become one: averages, the highest beside them.
func TestRollupKeepsTheSpike(t *testing.T) {
	id := uuid.New()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	var rows []db.NodeMetricSample
	for i, cpu := range []float64{10, 10, 90, 10, 30, 50} { // the sixth is the next bucket
		rows = append(rows, db.NodeMetricSample{NodeID: id, Resolution: db.MetricRaw, At: base.Add(time.Duration(i) * time.Minute),
			CPUPct: cpu, CPUPctMax: cpu, MemUsed: int64(cpu), MemUsedMax: int64(cpu), MemTotal: 100})
	}
	out := rollupNodeSamples(rows)
	if len(out) != 2 {
		t.Fatalf("%d buckets, want 2", len(out))
	}
	if b := out[0]; !b.At.Equal(base) || b.Resolution != db.MetricRollup || b.CPUPct != 30 || b.CPUPctMax != 90 || b.MemUsedMax != 90 {
		t.Errorf("first bucket = %+v", b)
	}
	if out[1].CPUPct != 50 {
		t.Errorf("second bucket = %+v", out[1])
	}
}
