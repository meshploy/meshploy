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
