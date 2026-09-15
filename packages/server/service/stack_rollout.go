package service

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
)

// ApplyOptions carries what an apply does beyond reconciling records. The zero
// value rolls out the services the apply changed.
type ApplyOptions struct {
	// NoDeploy writes the records and starts no rollout, for staging several
	// edits or applying during maintenance.
	NoDeploy bool
}

func applyOptions(opts []ApplyOptions) ApplyOptions {
	if len(opts) > 0 {
		return opts[0]
	}
	return ApplyOptions{}
}

// serviceSpec is what a compose definition says a service should be, in the
// fields an apply writes. Two that compare equal need no rollout, which is what
// keeps re-applying an unchanged file from restarting a stack.
//
// Command and args are joined rather than kept as slices, so the whole struct
// compares with ==, and so an empty list and a missing one read alike.
type serviceSpec struct {
	Image         string
	EnvVars       string
	Replicas      int
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
	Healthcheck   string
	HCInterval    int32
	HCTimeout     int32
	HCRetries     int32
	HCStartPeriod int32
	Command       string
	Args          string
}

// storedServiceSpec reads the spec a service already has.
func storedServiceSpec(svc meshdb.Service) serviceSpec {
	return serviceSpec{
		Image:         svc.Image,
		EnvVars:       string(svc.EnvVars),
		Replicas:      svc.Replicas,
		CPURequest:    svc.CPURequest,
		CPULimit:      svc.CPULimit,
		MemoryRequest: svc.MemoryRequest,
		MemoryLimit:   svc.MemoryLimit,
		Healthcheck:   svc.HealthcheckCmd,
		HCInterval:    svc.HealthcheckIntervalSecs,
		HCTimeout:     svc.HealthcheckTimeoutSecs,
		HCRetries:     svc.HealthcheckRetries,
		HCStartPeriod: svc.HealthcheckStartPeriodSecs,
		Command:       joinArgs(svc.Command),
		Args:          joinArgs(svc.Args),
	}
}

// joinArgs makes a command list comparable; NUL cannot appear in an argument.
func joinArgs(v []string) string { return strings.Join(v, "\x00") }

// changedService is a service an apply updated, and what it would take to roll
// the change out.
type changedService struct {
	ID     uuid.UUID
	Name   string
	Type   meshdb.ServiceType
	Status meshdb.ServiceStatus
}

// rolloutPlan decides which of the services an apply changed it rolls out, and
// what to say about the ones it leaves alone.
//
// A running or failed service takes the change: rolling it out is what applying
// a changed file means, and the failed case is how a bad image gets replaced. A
// stopped service is never started, since an apply must not resurrect what an
// operator deliberately stopped, and one already deploying is left to the
// rollout in flight. A managed database is provisioned rather than deployed, so
// a changed engine or size needs its own path and is only reported here.
func rolloutPlan(changed []changedService) (deploy []changedService, warnings []string) {
	for _, c := range changed {
		switch {
		case c.Type == meshdb.ServiceTypeDatabase:
			warnings = append(warnings, fmt.Sprintf("%s changed: a managed database is not rolled out automatically", c.Name))
		case c.Status == meshdb.ServiceRunning || c.Status == meshdb.ServiceFailed:
			deploy = append(deploy, c)
		case c.Status == meshdb.ServiceDeploying:
			warnings = append(warnings, fmt.Sprintf("%s changed while it was deploying: deploy it again to pick the change up", c.Name))
		default:
			warnings = append(warnings, fmt.Sprintf("%s changed but is not running: deploy it to pick the change up", c.Name))
		}
	}
	return deploy, warnings
}
