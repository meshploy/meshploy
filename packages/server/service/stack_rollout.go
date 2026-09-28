package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
)

// ApplyOptions carries what an apply does beyond reconciling records. The zero
// value rolls out the services the apply changed.
type ApplyOptions struct {
	// NoDeploy writes the records and starts no rollout, for staging several
	// edits or applying during maintenance.
	NoDeploy bool
	// Kind and Commit are what the run's record says it was: "sync" with the
	// commit fetched, or an apply.
	Kind   string
	Commit string
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
		case c.Status == meshdb.ServiceCompleted:
			// A step that runs once - a migration - runs again with the
			// change, as it does on every deploy: its dependents wait for it.
			deploy = append(deploy, c)
		case c.Status == meshdb.ServiceDeploying:
			warnings = append(warnings, fmt.Sprintf("%s changed while it was deploying: deploy it again to pick the change up", c.Name))
		default:
			warnings = append(warnings, fmt.Sprintf("%s changed but is not running: deploy it to pick the change up", c.Name))
		}
	}
	return deploy, warnings
}

// portPrint is a port as it shapes the pod, without the NodePort a deploy
// assigns.
type portPrint struct {
	Name                  string
	Port                  int
	HTTP, Primary, Public bool
}

func portPrintsFromInputs(ports []PortInput) []portPrint {
	out := make([]portPrint, 0, len(ports))
	for _, p := range ports {
		out = append(out, portPrint{p.Name, p.Port, p.IsHTTP, p.IsPrimary, p.IsPublic})
	}
	return out
}

func portPrintsFromRows(ports []meshdb.ServicePort) []portPrint {
	out := make([]portPrint, 0, len(ports))
	for _, p := range ports {
		out = append(out, portPrint{p.Name, p.Port, p.IsHTTP, p.IsPrimary, p.IsPublic})
	}
	return out
}

// fingerprint identifies a service as the cluster would run it, so what was
// deployed can be compared with what is wanted. Config files and volumes are
// left out: they are compared directly during an apply, which sees both sides.
func fingerprint(spec serviceSpec, ports []portPrint) string {
	slices.SortFunc(ports, func(a, b portPrint) int {
		if a.Port != b.Port {
			return a.Port - b.Port
		}
		return strings.Compare(a.Name, b.Name)
	})
	h := sha256.New()
	fmt.Fprintf(h, "%#v", spec)
	for _, p := range ports {
		fmt.Fprintf(h, "|%s:%d:%t:%t:%t", p.Name, p.Port, p.HTTP, p.Primary, p.Public)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// rollItem is one service an apply rolls out.
type rollItem struct {
	ID      uuid.UUID
	Name    string
	Created bool
}

// serviceLayers places each compose service after everything it depends_on:
// 0 for one that depends on nothing. A cycle, which compose refuses, is broken
// where it is found.
func serviceLayers(services composetypes.Services) map[string]int {
	layer := map[string]int{}
	visiting := map[string]bool{}
	var place func(name string) int
	place = func(name string) int {
		if l, ok := layer[name]; ok {
			return l
		}
		if visiting[name] {
			return 0
		}
		visiting[name] = true
		l := 0
		for dep := range services[name].DependsOn {
			if _, ok := services[dep]; ok {
				if d := place(dep) + 1; d > l {
					l = d
				}
			}
		}
		visiting[name] = false
		layer[name] = l
		return l
	}
	for name := range services {
		place(name)
	}
	return layer
}

// rolloutLayers groups what an apply rolls out by where compose starts it.
// Only the layers that have something in them remain, in order.
func rolloutLayers(items []rollItem, layerOf map[string]int) [][]rollItem {
	byLayer := map[int][]rollItem{}
	var keys []int
	for _, it := range items {
		l := layerOf[it.Name]
		if _, ok := byLayer[l]; !ok {
			keys = append(keys, l)
		}
		byLayer[l] = append(byLayer[l], it)
	}
	slices.Sort(keys)
	out := make([][]rollItem, 0, len(keys))
	for _, k := range keys {
		out = append(out, byLayer[k])
	}
	return out
}

// triggerLayer starts one layer's deploys, recording in the result what
// started and what could not, and on the run each service's deployment, and
// returns the deployments it started.
func (s *StackService) triggerLayer(ctx context.Context, layer []rollItem, triggerBy uuid.UUID, result *ApplyResult, run *runTracker) []uuid.UUID {
	var started []uuid.UUID
	for _, it := range layer {
		d, err := s.deployment.Trigger(ctx, TriggerInput{ServiceID: it.ID, TriggeredBy: triggerBy})
		if err != nil {
			verb := "updated"
			if it.Created {
				verb = "created"
			}
			msg := fmt.Sprintf("%s but not deployed: %v", verb, err)
			if result != nil {
				result.Errors = append(result.Errors, it.Name+": "+msg)
			} else {
				log.Printf("stack rollout: %s: %s", it.Name, msg)
			}
			run.step(it.ID, func(st *RolloutStep) { st.Status, st.Error = RolloutFailed, msg })
			continue
		}
		if result != nil {
			result.Deployed = append(result.Deployed, it.Name)
		}
		if d != nil {
			started = append(started, d.ID)
			id := d.ID
			run.step(it.ID, func(st *RolloutStep) { st.Status, st.DeploymentID = RolloutStarted, &id })
		}
	}
	run.save(ctx, s.db)
	return started
}

// rolloutInOrder follows a run to its end: it waits for each layer's
// deployments to finish - a step that runs once has completed, the rest are
// up - starts the next, and stops at a layer that failed, since what depends
// on it would only fail after it.
func (s *StackService) rolloutInOrder(ctx context.Context, stack string, run *runTracker, previous []uuid.UUID, layers [][]rollItem, triggerBy uuid.UUID) {
	for i := 0; ; i++ {
		outcome, err := s.waitDeployments(ctx, previous, stackRolloutWait)
		for id, status := range outcome {
			run.stepByDeployment(id, status)
		}
		if err != nil || run.anyFailed() {
			if i < len(layers) {
				log.Printf("stack %s: rollout stopped before %s: %v", stack, layerNames(layers[i]), err)
			}
			run.stopRest()
			s.finishRun(ctx, run)
			return
		}
		if i == len(layers) {
			s.finishRun(ctx, run)
			return
		}
		previous = s.triggerLayer(ctx, layers[i], triggerBy, nil, run)
	}
}

// stackRolloutWait is how long one layer may take, builds included.
var stackRolloutWait = 45 * time.Minute

// waitDeployments waits until every deployment has finished, and says how
// each ended.
func (s *StackService) waitDeployments(ctx context.Context, ids []uuid.UUID, limit time.Duration) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	deadline := time.Now().Add(limit)
	for {
		var deps []meshdb.Deployment
		if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&deps).Error; err != nil {
			return out, err
		}
		done := 0
		for _, d := range deps {
			switch d.Status {
			case meshdb.DeploymentFailed:
				out[d.ID] = RolloutFailed
				done++
			case meshdb.DeploymentSuccess:
				out[d.ID] = RolloutSucceeded
				done++
			}
		}
		if done == len(deps) {
			return out, nil
		}
		if time.Now().After(deadline) {
			return out, fmt.Errorf("still rolling out after %s", limit)
		}
		time.Sleep(5 * time.Second)
	}
}

func layerNames(layer []rollItem) string {
	names := make([]string, 0, len(layer))
	for _, it := range layer {
		names = append(names, it.Name)
	}
	return strings.Join(names, ", ")
}
