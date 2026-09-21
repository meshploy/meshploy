package service

import (
	"testing"

	"github.com/meshploy/packages/db"
)

// The notice that backups are not running is sent once, not every night: a
// nightly reminder that nothing happened is how people learn to ignore the
// notifications that matter.
func TestTheSkipNoticeIsSentOncePerStoppedPeriod(t *testing.T) {
	status := func(s db.BackupStatus) *db.BackupStatus { return &s }

	for _, tc := range []struct {
		name     string
		previous *db.BackupStatus
		want     bool
	}{
		{"never run before", nil, true},
		{"after a backup that worked", status(db.BackupSuccess), true},
		{"after one that failed", status(db.BackupFailed), true},
		{"already skipping", status(db.BackupSkipped), false},
	} {
		if got := firstSkip(tc.previous); got != tc.want {
			t.Errorf("%s: firstSkip = %v, want %v", tc.name, got, tc.want)
		}
	}
}
