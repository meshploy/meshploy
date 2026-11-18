package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Hooks for metrics history tests in package service_test.

// NewMetricsHistoryForTest samples nodes through scrape, with no cluster, at
// the time *now holds.
func NewMetricsHistoryForTest(gdb *gorm.DB, scrape func(context.Context, uuid.UUID) (*NodeMetrics, error), now *time.Time) *MetricsHistoryService {
	s := newMetricsHistory(gdb, scrape, nil)
	s.now = func() time.Time { return *now }
	return s
}
