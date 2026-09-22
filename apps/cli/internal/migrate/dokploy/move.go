package dokploy

import (
	"fmt"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// Stage 2: move one group.
//
// A group is what has to move together so data never lives in two places. The
// operator triggers it, the steps inside run without asking, and the group's
// downtime is the time from stopping Dokploy's copy to its domains answering
// from Meshploy.
//
// The rule that shapes everything here: **a half-moved group never serves.** If
// any step fails, the group undoes itself - Meshploy's copies stopped, the
// domains given back, Dokploy's copies started again - and is marked failed
// with the reason. Better down and explained than serving from two places.

// MoveAPI is what moving a group needs from Meshploy, beyond what stage 1 used.
type MoveAPI interface {
	// StartService starts a workload stage 1 created stopped.
	StartService(projectID, serviceID string) error
	// StopService stops it again, for a move that undoes itself.
	StopService(projectID, serviceID string) error
	// ServiceStatus is the workload's current state, polled while waiting.
	ServiceStatus(projectID, serviceID string) (string, error)
	// PublishRoute takes a route out of paused, so it serves and gets a
	// certificate.
	PublishRoute(projectID, routeID string) error
	// PauseRoute puts it back.
	PauseRoute(projectID, routeID string) error
	// PublishTCPRoute opens a published database's port on the gateway, and
	// PauseTCPRoute closes it again.
	PublishTCPRoute(projectID, routeID string) error
	PauseTCPRoute(projectID, routeID string) error
	// ApplyStack starts a compose app: a stack is not started but applied, and
	// what it creates are services like any other.
	ApplyStack(projectID, stackID string) error
	// StackServices are the services a stack created, which is what a rollback
	// stops and what health is waited on.
	StackServices(projectID, stackID string) ([]string, error)
}

// Prober checks that a hostname answers. Separated so a move can be tested
// without a network, and so the real one can be given a timeout.
type Prober interface {
	Probe(hostname string) error
}

// MoveDeps is what moving one group needs.
type MoveDeps struct {
	Plan    Plan
	Group   Group
	API     MoveAPI
	Edge    EdgeSwitcher
	Control Control
	Probe   Prober
	// Data copies what the group carries. Nil refuses a group with data rather
	// than moving an application away from it.
	Data    *DataMover
	Journal *journal.Journal
	// HealthTimeout bounds the wait for Meshploy's copies to come up. A group
	// that will not start must fail while the operator is watching, not hang.
	HealthTimeout time.Duration
	// Now and Sleep are seams for tests.
	Now   func() time.Time
	Sleep func(time.Duration)
}

// MoveResult is what happened.
type MoveResult struct {
	Group string `json:"group"`
	// Moved is true when the group's domains now answer from Meshploy.
	Moved bool `json:"moved"`
	// UndoneAfterFailure is true when a step failed and the group was put back.
	UndoneAfterFailure bool   `json:"undone_after_failure,omitempty"`
	Error              string `json:"error,omitempty"`
	// Downtime is how long the group was unavailable.
	Downtime string `json:"downtime,omitempty"`
}

// Move runs stage 2 for one group.
//
// A group carrying data moves the same way, with the copy in the middle: its
// applications stop, its databases are read, both sides change hands, and
// nothing serves until what arrived has been checked against what was read.
func Move(d MoveDeps) (MoveResult, error) {
	out := MoveResult{Group: d.Group.ID}
	if d.API == nil || d.Journal == nil {
		return out, fmt.Errorf("moving a group needs an API and a journal")
	}
	if !d.Group.CanMove {
		return out, fmt.Errorf("%s cannot move yet: %s", d.Group.Name, strings.Join(d.Group.Blockers, "; "))
	}
	if len(d.Group.Data) > 0 && d.Data == nil {
		return out, fmt.Errorf("%s carries data, and this move was given no way to copy it", d.Group.Name)
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}

	started := now()
	if err := d.run(); err != nil {
		out.Error = err.Error()
		// The group puts itself back. Whatever that cannot undo is added to the
		// error, because an operator told "rolled back" has to be able to
		// believe it.
		if undoErr := d.undo(); undoErr != nil {
			out.Error += "; and the undo did not finish: " + undoErr.Error()
		} else {
			out.UndoneAfterFailure = true
		}
		return out, fmt.Errorf("%s", out.Error)
	}
	out.Moved = true
	out.Downtime = now().Sub(started).Round(time.Second).String()
	return out, nil
}

func (d MoveDeps) run() error {
	// 1. Stop writes, applications first. A database is stopped after the
	// things that write to it, and only once nothing needs to read it either -
	// which for a group carrying a dump is after the dump.
	for _, m := range d.applications() {
		w, ok := d.workloadFor(m)
		if !ok {
			continue
		}
		if err := d.Control.Stop(d.step(m.ID, "stop"), d.Group.ID, w); err != nil {
			return err
		}
	}

	// 2. Read the data out of Dokploy's databases, which are still running
	// with nothing writing to them.
	if d.Data != nil {
		if err := d.Data.Dump(d.Group, d.meshployIDs); err != nil {
			return err
		}
	}

	// 3. Now the databases stop too.
	for _, m := range d.databases() {
		w, ok := d.workloadFor(m)
		if !ok {
			continue
		}
		if err := d.Control.Stop(d.step(m.ID, "stop"), d.Group.ID, w); err != nil {
			return err
		}
	}

	// 4. Files are copied with both sides down: Dokploy's so nothing is
	// writing what is read, Meshploy's because it has not started yet and its
	// claims are free for the copy to fill.
	if d.Data != nil {
		if err := d.Data.CopyFiles(d.Group, d.meshployIDs); err != nil {
			return err
		}
	}

	// 5. Start Meshploy's databases and wait: a restore needs somewhere to go,
	// and an application must not come up before what it reads.
	for _, m := range d.databases() {
		if err := d.start(m); err != nil {
			return err
		}
	}

	// 6. Load the dumps, and check what arrived is what was read.
	if d.Data != nil {
		if err := d.Data.Restore(d.Group, d.meshployIDs); err != nil {
			return err
		}
	}

	// 7. Then the applications.
	for _, m := range d.applications() {
		if err := d.start(m); err != nil {
			return err
		}
	}

	// 8. Open the ports the group's databases were published on. Dokploy's
	// copies are stopped by now, so the host port it held is free for the
	// gateway to take.
	for _, m := range d.databases() {
		if err := d.publishTCPRoute(m); err != nil {
			return err
		}
	}

	// 9. Switch the group's domains through the edge Dokploy still runs, and
	// publish their routes. Until this point nothing has changed for a visitor.
	for _, dom := range d.domains() {
		if err := d.switchDomain(dom); err != nil {
			return err
		}
	}

	// 10. Check each domain answers. A group that moved but does not serve is a
	// failure, not a success with a warning.
	if d.Probe != nil {
		for _, dom := range d.domains() {
			if err := d.Probe.Probe(dom.host); err != nil {
				return fmt.Errorf("%s did not answer after the move: %w", dom.host, err)
			}
		}
	}
	return nil
}

// publishTCPRoute opens the gateway port a database was published on.
func (d MoveDeps) publishTCPRoute(m GroupMember) error {
	routeID := d.Journal.CreatedBy("prepare/tcp/" + m.ID)
	step := d.step(m.ID, "tcp")
	if routeID == "" || d.Journal.Done(step) {
		return nil
	}
	_, projectID := d.meshployIDs(m)
	if err := d.API.PublishTCPRoute(projectID, routeID); err != nil {
		return d.record(step, "publish-tcp-route", m.Name, err, nil)
	}
	return d.Journal.Append(journal.Entry{Step: step, Group: d.Group.ID, Action: "publish-tcp-route",
		Target: m.Name, Result: journal.OK, Undo: &journal.Undo{
			Kind: journal.UndoPauseTCPRoute,
			Args: map[string]string{"project_id": projectID, "route_id": routeID},
		}})
}

// start brings up one of Meshploy's copies and waits for it.
func (d MoveDeps) start(m GroupMember) error {
	if m.Kind == "compose" {
		return d.startStack(m)
	}
	serviceID, projectID := d.meshployIDs(m)
	if serviceID == "" {
		return fmt.Errorf("%s has no Meshploy copy: run prepare first", m.Name)
	}
	step := d.step(m.ID, "start")
	if !d.Journal.Done(step) {
		if err := d.API.StartService(projectID, serviceID); err != nil {
			return d.record(step, "start-service", m.Name, err, nil)
		}
		_ = d.Journal.Append(journal.Entry{Step: step, Group: d.Group.ID, Action: "start-service",
			Target: m.Name, Result: journal.OK, Created: serviceID, Undo: &journal.Undo{
				Kind: journal.UndoStopService,
				Args: map[string]string{"project_id": projectID, "service_id": serviceID},
			}})
	}
	return d.waitHealthy(projectID, serviceID, m.Name)
}

// startStack applies a compose app, starts what it creates, and waits for it.
//
// A stack is not started, it is applied: what comes out are services, and they
// are what a rollback stops - the stack itself stays, so a second attempt
// reuses it rather than creating another.
//
// Each of those services is then started explicitly, and that is not
// belt-and-braces. Applying a stack reconciles its shape, not its power state,
// so a service prepare created stopped stays stopped; and after a rollback has
// stopped them, re-applying does not bring them back either. Relying on the
// apply to start them left a stack's deployment at zero replicas while the move
// waited three minutes for a workload nothing had asked to run - found on a
// real server, on a group that had been moved and rolled back once already.
//
// The apply is guarded by the journal because it only needs doing once. The
// starts are not: they are idempotent, and they are the part a rollback undid.
func (d MoveDeps) startStack(m GroupMember) error {
	stackID, projectID := d.meshployIDs(m)
	if stackID == "" {
		return fmt.Errorf("%s has no Meshploy copy: run prepare first", m.Name)
	}
	step := d.step(m.ID, "start")
	if !d.Journal.Done(step) {
		if err := d.API.ApplyStack(projectID, stackID); err != nil {
			return d.record(step, "apply-stack", m.Name, err, nil)
		}
		_ = d.Journal.Append(journal.Entry{Step: step, Group: d.Group.ID, Action: "apply-stack",
			Target: m.Name, Result: journal.OK, Created: stackID})
	}

	services, err := d.API.StackServices(projectID, stackID)
	if err != nil {
		return err
	}
	for _, id := range services {
		// One entry per service, each with its own undo, so putting the group
		// back stops every one of them.
		svcStep := step + "/" + id
		if !d.Journal.Done(svcStep) {
			if err := d.API.StartService(projectID, id); err != nil {
				return d.record(svcStep, "start-service", m.Name, err, nil)
			}
			_ = d.Journal.Append(journal.Entry{Step: svcStep, Group: d.Group.ID, Action: "start-service",
				Target: m.Name, Result: journal.OK, Created: id, Undo: &journal.Undo{
					Kind: journal.UndoStopService,
					Args: map[string]string{"project_id": projectID, "service_id": id},
				}})
		}
	}
	for _, id := range services {
		if err := d.waitHealthy(projectID, id, m.Name); err != nil {
			return err
		}
	}
	return nil
}

// databases and applications split the group by what it stops and starts
// first. The order is the whole reason the split exists: data is read before
// its database stops, and an application starts after what it reads.
func (d MoveDeps) databases() []GroupMember {
	var out []GroupMember
	for _, m := range d.Group.Members {
		if m.Kind == "database" {
			out = append(out, m)
		}
	}
	return out
}

func (d MoveDeps) applications() []GroupMember {
	var out []GroupMember
	for _, m := range d.Group.Members {
		if m.Kind != "database" {
			out = append(out, m)
		}
	}
	return out
}

// domain is one hostname moving with the group.
type domain struct {
	itemID    string
	host      string
	routeID   string
	projectID string
	// appName is what Dokploy calls the workload, which names its dynamic file.
	appName string
	// labelled is true for a compose app, whose router comes from labels and
	// has to be taken over by a new file rather than rewritten.
	labelled bool
}

func (d MoveDeps) domains() []domain {
	members := map[string]bool{}
	for _, m := range d.Group.Members {
		members[m.ID] = true
	}
	var out []domain
	for _, it := range d.Plan.Items {
		if it.Kind != "domain" || it.Verdict != Moves {
			continue
		}
		owner := it.Details["application_id"]
		labelled := false
		if owner == "" {
			owner, labelled = it.Details["compose_id"], true
		}
		if !members[owner] {
			continue
		}
		out = append(out, domain{
			itemID:    it.ID,
			host:      splitHostPath(it.Name),
			routeID:   d.Journal.CreatedBy("prepare/route/" + it.ID),
			projectID: d.projectOf(owner),
			appName:   d.appName(owner),
			labelled:  labelled,
		})
	}
	return out
}

func (d MoveDeps) switchDomain(dom domain) error {
	step := d.step(dom.itemID, "domain")
	if dom.labelled {
		if err := d.Edge.TakeOverHosts(step, d.Group.ID, dom.appName, []string{dom.host}); err != nil {
			return err
		}
	} else if err := d.Edge.SwitchApp(step, d.Group.ID, dom.appName); err != nil {
		return err
	}

	publish := d.step(dom.itemID, "publish")
	if dom.routeID == "" || d.Journal.Done(publish) {
		return nil
	}
	if err := d.API.PublishRoute(dom.projectID, dom.routeID); err != nil {
		return d.record(publish, "publish-route", dom.host, err, nil)
	}
	return d.Journal.Append(journal.Entry{Step: publish, Group: d.Group.ID, Action: "publish-route",
		Target: dom.host, Result: journal.OK, Undo: &journal.Undo{
			Kind: journal.UndoPauseRoute,
			Args: map[string]string{"project_id": dom.projectID, "route_id": dom.routeID},
		}})
}

// waitHealthy polls until the workload is running, or the timeout says it will
// not be.
func (d MoveDeps) waitHealthy(projectID, serviceID, name string) error {
	timeout := d.HealthTimeout
	if timeout == 0 {
		timeout = 3 * time.Minute
	}
	now, sleep := d.Now, d.Sleep
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = time.Sleep
	}

	deadline := now().Add(timeout)
	var last string
	for {
		status, err := d.API.ServiceStatus(projectID, serviceID)
		if err == nil {
			last = status
			if status == "running" {
				return nil
			}
			if status == "failed" {
				return fmt.Errorf("%s failed to start in Meshploy", name)
			}
		}
		if !now().Before(deadline) {
			return fmt.Errorf("%s did not become healthy within %s (last status: %s)", name, timeout, orUnknown(last))
		}
		sleep(2 * time.Second)
	}
}

// undo puts the group back: Meshploy's copies stopped, domains restored,
// Dokploy's copies started again - in the order the journal recorded them,
// backwards.
func (d MoveDeps) undo() error {
	entries, err := journal.Read(d.Journal.Dir())
	if err != nil {
		return err
	}
	res := Rollback{Runner: d.Control.Runner, Meshploy: moveRollback{d.API}, Journal: d.Journal}.
		Replay(journal.Undoable(entries, d.Group.ID))
	failures := res.Failures

	// Then stop every copy of this group outright, whether the journal knows it
	// started or not. A start that errored may still have started something,
	// and the promise being kept here is that a half-moved group never serves.
	// Stopping one that is already stopped costs nothing.
	for _, m := range d.Group.Members {
		id, projectID := d.meshployIDs(m)
		if id == "" {
			continue
		}
		if m.Kind == "compose" {
			// What a stack created is what runs, so that is what stops.
			services, err := d.API.StackServices(projectID, id)
			if err != nil {
				failures = append(failures, fmt.Sprintf("read what %s created: %v", m.Name, err))
				continue
			}
			for _, svc := range services {
				if err := d.API.StopService(projectID, svc); err != nil {
					failures = append(failures, fmt.Sprintf("stop %s: %v", m.Name, err))
				}
			}
			continue
		}
		if err := d.API.StopService(projectID, id); err != nil {
			failures = append(failures, fmt.Sprintf("stop %s: %v", m.Name, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

// moveRollback lets the rollback runner reach Meshploy.
type moveRollback struct{ api MoveAPI }

func (m moveRollback) StopService(projectID, serviceID string) error {
	return m.api.StopService(projectID, serviceID)
}

func (m moveRollback) PauseRoute(projectID, routeID string) error {
	return m.api.PauseRoute(projectID, routeID)
}

func (m moveRollback) PauseTCPRoute(projectID, routeID string) error {
	return m.api.PauseTCPRoute(projectID, routeID)
}

// workloadFor is the Dokploy side of a group member: the Swarm service or
// container to stop.
func (d MoveDeps) workloadFor(m GroupMember) (Workload, bool) {
	name := d.appName(m.ID)
	if name == "" {
		return Workload{}, false
	}
	// Dokploy runs applications and databases as Swarm services and compose
	// apps as containers, which is what the plan read from Docker.
	for _, s := range d.Plan.Items {
		if s.ID == m.ID && s.Kind == "compose" {
			return Workload{Name: name, Compose: true}, true
		}
	}
	return Workload{Name: name, Swarm: true}, true
}

func (d MoveDeps) meshployIDs(m GroupMember) (serviceID, projectID string) {
	id := d.Journal.CreatedBy("prepare/service/" + m.ID)
	if id == "" {
		// A compose app is a stack, created under its own step.
		id = d.Journal.CreatedBy("prepare/stack/" + m.ID)
	}
	return id, d.projectOf(m.ID)
}

// projectOf is the Meshploy project a Dokploy workload was created in.
func (d MoveDeps) projectOf(itemID string) string {
	var projectName string
	for _, it := range d.Plan.Items {
		if it.ID == itemID {
			projectName = it.Project
		}
	}
	for _, it := range d.Plan.Items {
		if it.Kind == "project" && it.Name == projectName {
			return d.Journal.CreatedBy("prepare/project/" + it.ID)
		}
	}
	return ""
}

func (d MoveDeps) appName(itemID string) string {
	for _, it := range d.Plan.Items {
		if it.ID == itemID {
			if n := it.Details["app_name"]; n != "" {
				return n
			}
			return it.Name
		}
	}
	return ""
}

func (d MoveDeps) step(id, what string) string {
	return fmt.Sprintf("move/%s/%s/%s", d.Group.ID, what, id)
}

// record writes a failure and returns it, so a caller can `return d.record(...)`.
func (d MoveDeps) record(step, action, target string, cause error, undo *journal.Undo) error {
	_ = d.Journal.Append(journal.Entry{Step: step, Group: d.Group.ID, Action: action,
		Target: target, Result: journal.Failed, Error: cause.Error(), Undo: undo})
	return fmt.Errorf("%s %s: %w", action, target, cause)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
