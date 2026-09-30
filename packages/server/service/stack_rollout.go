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
	// NeverDeployed: it has no deployment at all, so it was never started,
	// rather than stopped by someone. Unchanged is set when that is the only
	// reason it is here.
	NeverDeployed bool
	Unchanged     bool
}

// rolloutPlan decides which of the services an apply changed it rolls out, and
// what to say about the ones it leaves alone.
//
// A running or failed service takes the change: rolling it out is what applying
// a changed file means, and the failed case is how a bad image gets replaced. A
// stopped service is never started, since an apply must not resurrect what an
// operator deliberately stopped, and one already deploying is left to the
// rollout in flight.
//
// A service that was never deployed is not stopped, only not started yet, so
// it goes, changed or not: after a failed first rollout that is how the rest of
// a stack starts. A managed database is provisioned rather than deployed, so a
// changed engine or size needs its own path and is only reported here.
func rolloutPlan(changed []changedService) (deploy []changedService, warnings []string) {
	for _, c := range changed {
		switch {
		case c.NeverDeployed && c.Status != meshdb.ServiceDeploying:
			deploy = append(deploy, c)
		case c.Unchanged:
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
	// Commit is what a build of it takes: the commit the stack's last sync
	// read, for a service built from the stack's own repository and branch.
	Commit string
	// Builds is whether rolling it out builds an image, which a cap on
	// parallel builds counts; one that only runs an image never waits.
	Builds bool
}

// buildsFromSource says a service's deploy builds: it has a repository or an
// uploaded folder to build from.
func (s *StackService) buildsFromSource(ctx context.Context, serviceID uuid.UUID) bool {
	var bc meshdb.BuildConfig
	if s.db.WithContext(ctx).Where("service_id = ?", serviceID).First(&bc).Error != nil {
		return false
	}
	return bc.GitRepo != "" || bc.BuildsFromUpload()
}

// capBuilds splits a layer into what starts now - every service that builds
// nothing, and the first max that do - and the builds that wait for a place.
// max 0 is no cap.
func capBuilds(layer []rollItem, max int) (now, later []rollItem) {
	if max <= 0 {
		return layer, nil
	}
	builds := 0
	for _, it := range layer {
		if it.Builds {
			if builds >= max {
				later = append(later, it)
				continue
			}
			builds++
		}
		now = append(now, it)
	}
	return now, later
}

// stackCommit is the commit a stack's rollout builds a service at: the one
// its last sync read, when the service is built from the stack's own
// repository and branch - so an apply builds what that sync saw, and only a
// sync brings in anything newer. Empty builds the branch as it is now: a
// pasted stack, one synced in compose-file-only mode, which records no
// commit, or a service built from another repository.
func (s *StackService) stackCommit(ctx context.Context, stack *meshdb.Stack, serviceID uuid.UUID) string {
	if stack.GitLastSyncSHA == "" || !fromGit(*stack) {
		return ""
	}
	var bc meshdb.BuildConfig
	if s.db.WithContext(ctx).Where("service_id = ?", serviceID).First(&bc).Error != nil {
		return ""
	}
	return pinnedCommit(*stack, bc)
}

// buildBehind says whether a service built from source needs building again:
// when the commit the stack's last sync read is known, whether its last
// successful build was of another; when it is not - a compose-file-only sync,
// or a service built from another repository - a sync rebuilds it, since a
// sync means taking the newest code, and an apply again does not.
func buildBehind(pinned, lastBuilt string, sync bool) bool {
	if pinned == "" {
		return sync
	}
	return lastBuilt == "" || !strings.HasPrefix(pinned, lastBuilt)
}

// lastBuiltCommit is the commit a service's last successful build recorded.
func (s *StackService) lastBuiltCommit(ctx context.Context, serviceID uuid.UUID) string {
	var d meshdb.Deployment
	err := s.db.WithContext(ctx).
		Where("service_id = ? AND status = ? AND source_commit <> ''", serviceID, meshdb.DeploymentSuccess).
		Order("created_at DESC").First(&d).Error
	if err != nil {
		return ""
	}
	return d.SourceCommit
}

// pinnedCommit is stackCommit's rule, given the service's build config.
func pinnedCommit(stack meshdb.Stack, bc meshdb.BuildConfig) string {
	if stack.GitLastSyncSHA == "" || !fromGit(stack) || bc.GitRepo == "" {
		return ""
	}
	if normalizeRepoPath(bc.GitRepo) != normalizeRepoPath(stack.GitRepo) || bc.Branch != stack.GitBranch {
		return ""
	}
	return stack.GitLastSyncSHA
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
		d, err := s.deployment.Trigger(ctx, TriggerInput{ServiceID: it.ID, TriggeredBy: triggerBy, Commit: it.Commit})
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
// up - then starts the next. A failure holds back only what depends on it,
// directly or through a service it held back. dependsOn is each service's
// depends_on, by name.
func (s *StackService) rolloutInOrder(ctx context.Context, stack string, run *runTracker, previous []uuid.UUID, queued []rollItem, layers [][]rollItem, triggerBy uuid.UUID, dependsOn map[string][]string, maxBuilds int) {
	blocked := map[string]string{} // a service held back -> the failure it waits on
	for i := 0; ; i++ {
		// Builds the cap held back start as the ones before them finish
		// building - not deploying, which costs little - so the layer is not
		// slower than it must be.
		previous = s.startQueuedBuilds(ctx, previous, queued, maxBuilds, triggerBy, run)
		outcome, err := s.waitDeployments(ctx, previous, stackRolloutWait)
		for id, status := range outcome {
			run.stepByDeployment(id, status)
		}
		if err != nil {
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
		for name := range run.failedNames() {
			blocked[name] = name
		}
		toStart, later := capBuilds(holdBack(layers[i], dependsOn, blocked), maxBuilds)
		queued = later
		for _, it := range layers[i] {
			if cause, held := blocked[it.Name]; held {
				run.step(it.ID, func(st *RolloutStep) {
					st.Status, st.Error = RolloutNotStarted, fmt.Sprintf("not started: it depends on %s, which failed", cause)
				})
			}
		}
		previous = s.triggerLayer(ctx, toStart, triggerBy, nil, run)
	}
}

// holdBack is what of a layer may start: everything but what depends on a
// failed service, or on one already held back. What it holds back it adds to
// blocked, with the failure behind it, so a later layer holds back its own.
func holdBack(layer []rollItem, dependsOn map[string][]string, blocked map[string]string) []rollItem {
	var start []rollItem
	for _, it := range layer {
		cause := ""
		for _, dep := range dependsOn[it.Name] {
			if c, ok := blocked[dep]; ok {
				cause = c
				break
			}
		}
		if cause == "" {
			start = append(start, it)
			continue
		}
		blocked[it.Name] = cause
	}
	return start
}

// startQueuedBuilds starts the queued builds as places under the cap free up,
// and returns every deployment of the layer, started now or before.
func (s *StackService) startQueuedBuilds(ctx context.Context, started []uuid.UUID, queued []rollItem, max int, triggerBy uuid.UUID, run *runTracker) []uuid.UUID {
	deadline := time.Now().Add(stackRolloutWait)
	for len(queued) > 0 {
		var building int64
		s.db.WithContext(ctx).Model(&meshdb.Deployment{}).
			Where("id IN ? AND status IN ?", started, []meshdb.DeploymentStatus{meshdb.DeploymentPending, meshdb.DeploymentBuilding}).
			Count(&building)
		if free := max - int(building); free > 0 {
			n := min(free, len(queued))
			started = append(started, s.triggerLayer(ctx, queued[:n], triggerBy, nil, run)...)
			queued = queued[n:]
			continue
		}
		if time.Now().After(deadline) {
			log.Printf("stack rollout: %d builds still queued after %s", len(queued), stackRolloutWait)
			return started
		}
		select {
		case <-ctx.Done():
			return started
		case <-time.After(buildQueuePoll):
		}
	}
	return started
}

// buildQueuePoll is how often queued builds look for a free place.
var buildQueuePoll = 5 * time.Second

// dependsOnOf is each compose service's depends_on, by name.
func dependsOnOf(services composetypes.Services) map[string][]string {
	out := map[string][]string{}
	for name, svc := range services {
		for dep := range svc.DependsOn {
			out[name] = append(out[name], dep)
		}
	}
	return out
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
