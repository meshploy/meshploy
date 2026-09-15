package service

import (
	"testing"

	db "github.com/meshploy/packages/db"
)

// A stack's status summarises its services. A failure anywhere wins, because a
// half-up stack is not working and "running" would hide the broken part.
func TestDeriveStackStatus(t *testing.T) {
	cases := []struct {
		name string
		in   []db.ServiceStatus
		want db.StackStatus
	}{
		{"all running", []db.ServiceStatus{db.ServiceRunning, db.ServiceRunning}, db.StackRunning},
		{"one failed wins", []db.ServiceStatus{db.ServiceRunning, db.ServiceFailed}, db.StackFailed},
		{"failed alone", []db.ServiceStatus{db.ServiceFailed}, db.StackFailed},
		{"partly up is still serving", []db.ServiceStatus{db.ServiceRunning, db.ServiceStopped}, db.StackRunning},
		{"still rolling out", []db.ServiceStatus{db.ServiceDeploying, db.ServiceStopped}, db.StackApplying},
		{"all stopped", []db.ServiceStatus{db.ServiceStopped, db.ServiceStopped}, db.StackIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svcs := make([]db.Service, len(tc.in))
			for i, st := range tc.in {
				svcs[i] = db.Service{Status: st}
			}
			if got := deriveStackStatus(svcs); got != tc.want {
				t.Errorf("deriveStackStatus(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A failed service is examined again, so one whose pod recovered stops reading
// as failed. A stopped one is not: that is a decision, not an observation.
func TestReconcilerLooksAtFailedButNotStoppedServices(t *testing.T) {
	has := func(want db.ServiceStatus) bool {
		for _, s := range reconciledStatuses {
			if s == want {
				return true
			}
		}
		return false
	}
	for _, s := range []db.ServiceStatus{db.ServiceRunning, db.ServiceDeploying, db.ServiceFailed} {
		if !has(s) {
			t.Errorf("%q is not reconciled against the cluster", s)
		}
	}
	if has(db.ServiceStopped) {
		t.Error("a stopped service must be left alone")
	}
}
