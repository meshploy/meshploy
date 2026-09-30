package dokploy

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"

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

// StackImageAPI is what starting a compose app on the images it ran needs,
// beyond MoveAPI: the API applied without rolling anything out, the stack's
// services by name, and a way to set what one runs.
type StackImageAPI interface {
	ApplyStackRecords(projectID, stackID string) error
	StackServiceRefs(projectID, stackID string) ([]StackServiceRef, error)
	SetServiceImage(projectID, serviceID, image string) error
}

// RepoReader reads a file, or every file under a directory, from disk: keyed
// by the path below root, "" for root itself when it is a file.
type RepoReader interface {
	Tree(root string) (map[string]string, error)
}

// DiskRepo reads from this machine's disk.
type DiskRepo struct{}

func (DiskRepo) Tree(root string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			rel = ""
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	return out, err
}

// StackServiceRef is one service a stack created.
type StackServiceRef struct {
	ID, Name, Image string
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
	Data *DataMover
	// Images carries the images a compose app's services run into the
	// cluster, so a moved stack starts on them rather than on a build. Nil,
	// or an API without StackImageAPI, applies the stack as it is.
	Images  *ImageMover
	Journal *journal.Journal
	// HealthTimeout bounds the wait for Meshploy's copies to come up. A group
	// that will not start must fail while the operator is watching, not hang.
	HealthTimeout time.Duration
	// Repo reads what a compose app mounts from its repository out of
	// Dokploy's checkout. Nil sends nothing.
	Repo RepoReader

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

	// Downtime is counted from the first stop: readying the stack happens
	// while the old copy still serves.
	started := now()
	if err := d.run(func() { started = now() }); err != nil {
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

func (d MoveDeps) run(stopping func()) error {
	// A compose app that never ran on this server has no images to move on,
	// and a plan read before it did still offers it. Refused here, before
	// anything is touched, rather than half-way.
	if d.Images != nil {
		for _, m := range d.applications() {
			if m.Kind != "compose" {
				continue
			}
			out, err := d.Images.Runner.Output("docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+d.appName(m.ID))
			if err == nil && strings.TrimSpace(out) == "" {
				return fmt.Errorf("%s has nothing running on Dokploy to move: deploy it there first, then move it", m.Name)
			}
		}
	}

	// 0. Ready each compose app's stack while Dokploy's copy still serves:
	// nothing of it runs yet, and carrying its images is the slow part.
	for _, m := range d.applications() {
		if m.Kind != "compose" {
			continue
		}
		if err := d.readyStack(m); err != nil {
			return err
		}
	}

	stopping()

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
	// And the ports a compose app published on every address, which anyone
	// could reach on this server before and still can, at the same number.
	for _, m := range d.applications() {
		if err := d.stackPorts(m, true); err != nil {
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

// stackPorts opens on the gateway each port a compose app published on
// every address, at the same number. The route is made once, recorded like
// prepare's, and opened by a step of the move's own, so putting the group back
// closes it and moving again opens the same one.
//
// Made while the stack is readied, before anything starts (open false): a port
// the stack keeps in the cluster - one published on the host's loopback -
// becomes routable when a route names it, and only the deploy that follows
// gives it the address the route forwards to. Opened once the app is up.
func (d MoveDeps) stackPorts(m GroupMember, open bool) error {
	if m.Kind != "compose" {
		return nil
	}
	// "service:host:container", and the zone it listens in.
	type hostPort struct{ spec, zone string }
	var ports []hostPort
	for _, it := range d.Plan.Items {
		if it.Kind != "compose" || it.ID != m.ID {
			continue
		}
		for _, z := range [][2]string{{"public_ports", "public"}, {"local_ports", "local"}} {
			if it.Details[z[0]] == "" {
				continue
			}
			for _, p := range strings.Split(it.Details[z[0]], ",") {
				ports = append(ports, hostPort{p, z[1]})
			}
		}
	}
	if len(ports) == 0 {
		return nil
	}
	api, ok := d.API.(interface {
		StackServiceRefs(projectID, stackID string) ([]StackServiceRef, error)
		CreateServiceTCPRoute(projectID string, spec TCPRouteSpec) (string, error)
	})
	if !ok {
		return fmt.Errorf("%s publishes ports on every address, and this API cannot open them on the gateway", m.Name)
	}
	stackID, projectID := d.meshployIDs(m)
	refs, err := api.StackServiceRefs(projectID, stackID)
	if err != nil {
		return err
	}
	for _, p := range ports {
		parts := strings.Split(p.spec, ":")
		if len(parts) != 3 {
			continue
		}
		name, host, container := parts[0], atoiOr(parts[1], 0), atoiOr(parts[2], 0)
		serviceID := ""
		for _, r := range refs {
			if r.Name == name {
				serviceID = r.ID
			}
		}
		create := fmt.Sprintf("prepare/tcp/%s/%d", m.ID, host)
		if serviceID == "" {
			return d.record(create, "create-tcp-route", m.Name, fmt.Errorf("its stack has no service %q for port %d", name, host), nil)
		}
		routeID := d.Journal.CreatedBy(create)
		if routeID == "" {
			routeID, err = api.CreateServiceTCPRoute(projectID, TCPRouteSpec{GatewayPort: host, ServiceID: serviceID,
				ServicePort: container, Zone: p.zone})
			if err != nil {
				return d.record(create, "create-tcp-route", m.Name, err, nil)
			}
			if err := d.Journal.Append(journal.Entry{Step: create, Group: d.Group.ID, Action: "create-tcp-route",
				Target: fmt.Sprintf("%s:%d", name, host), Result: journal.OK, Created: routeID}); err != nil {
				return err
			}
		}
		step := d.step(m.ID, fmt.Sprintf("tcp:%d", host))
		if !open || d.Journal.Done(step) {
			continue
		}
		if err := d.API.PublishTCPRoute(projectID, routeID); err != nil {
			return d.record(step, "publish-tcp-route", fmt.Sprintf("%s:%d", name, host), err, nil)
		}
		if err := d.Journal.Append(journal.Entry{Step: step, Group: d.Group.ID, Action: "publish-tcp-route",
			Target: fmt.Sprintf("%s:%d", name, host), Result: journal.OK, Undo: &journal.Undo{
				Kind: journal.UndoPauseTCPRoute,
				Args: map[string]string{"project_id": projectID, "route_id": routeID},
			}}); err != nil {
			return err
		}
	}
	return nil
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

// startStack applies a compose app's stack, which the move made ready before
// anything stopped (readyStack), starts what it creates, and waits for it.
//
// Each service is started explicitly: applying a stack reconciles its shape,
// not its power state, so a service created stopped, or stopped by a rollback,
// stays stopped. The apply is guarded by the journal because it only needs
// doing once; the starts are not, being idempotent and the part a rollback
// undoes.
func (d MoveDeps) startStack(m GroupMember) error {
	stackID, projectID := d.meshployIDs(m)
	step := d.step(m.ID, "start")
	services, err := d.API.StackServices(projectID, stackID)
	if err != nil {
		return err
	}
	names := map[string]string{}
	if refs, ok := d.API.(interface {
		StackServiceRefs(projectID, stackID string) ([]StackServiceRef, error)
	}); ok {
		if list, err := refs.StackServiceRefs(projectID, stackID); err == nil {
			for _, r := range list {
				names[r.ID] = r.Name
			}
		}
	}
	// In the order compose started them, a layer at a time: a migration step
	// waited on must have run before what reads its tables starts, or that
	// crash-loops into a back-off longer than the move waits.
	for _, layer := range d.startLayers(m, services, names) {
		for _, id := range layer {
			// One entry per service, each with its own undo, so putting the
			// group back stops every one of them.
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
		for _, id := range layer {
			name := m.Name
			if n := names[id]; n != "" {
				name += "/" + n // so a stack that does not come up says which part
			}
			if err := d.waitHealthy(projectID, id, name); err != nil {
				return err
			}
		}
	}
	// The stack counts as started once every service is up: what says the
	// group has moved, so it is undone with the rest when the group goes back.
	if !d.Journal.Done(step) {
		return d.Journal.Append(journal.Entry{Step: step, Group: d.Group.ID, Action: "stack-started",
			Target: m.Name, Result: journal.OK, Undo: &journal.Undo{Kind: journal.UndoForget}})
	}
	return nil
}

// startLayers groups a stack's services in the plan's start order; anything
// the order does not name comes last. Without an order, one layer.
func (d MoveDeps) startLayers(m GroupMember, services []string, names map[string]string) [][]string {
	var order string
	for _, it := range d.Plan.Items {
		if it.Kind == "compose" && it.ID == m.ID {
			order = it.Details["start_order"]
		}
	}
	if order == "" {
		return [][]string{services}
	}
	byName := map[string]string{}
	for _, id := range services {
		byName[names[id]] = id
	}
	placed := map[string]bool{}
	var layers [][]string
	for _, l := range strings.Split(order, "|") {
		var layer []string
		for _, n := range strings.Split(l, ",") {
			if id, ok := byName[n]; ok && !placed[id] {
				layer = append(layer, id)
				placed[id] = true
			}
		}
		if len(layer) > 0 {
			layers = append(layers, layer)
		}
	}
	var rest []string
	for _, id := range services {
		if !placed[id] {
			rest = append(rest, id)
		}
	}
	if len(rest) > 0 {
		layers = append(layers, rest)
	}
	return layers
}

// readyStack makes a compose app's stack ready to start: its records, the
// images its containers run carried to the registry, its routes. All of it is
// inert, so the move does it before Dokploy's copy stops - pushing a large
// app's images took minutes, which were minutes of the app being down.
func (d MoveDeps) readyStack(m GroupMember) error {
	stackID, projectID := d.meshployIDs(m)
	if stackID == "" {
		return fmt.Errorf("%s has no Meshploy copy: run prepare first", m.Name)
	}
	// Not "start": that step says the stack is up, and applying records is
	// not that. Recorded as start, a move that failed later still read as
	// moved, since an apply has nothing to undo.
	step := d.step(m.ID, "apply")
	images, carry := d.API.(StackImageAPI)
	carry = carry && d.Images != nil
	switch {
	case carry:
		// Records only: rolling out would build every service with a build:
		// section from source, and the move must not hang on a build
		// succeeding. What each runs is set below. Applied on every attempt,
		// which changes nothing twice, so a retried move sends the files its
		// services mount as they are now.
		files, err := d.repoFiles(m)
		if err != nil {
			return d.record(step, "apply-stack", m.Name, err, nil)
		}
		apply := images.ApplyStackRecords
		if withFiles, ok := d.API.(interface {
			ApplyStackRecordsWithFiles(projectID, stackID string, files map[string]string) error
		}); ok && len(files) > 0 {
			apply = func(projectID, stackID string) error {
				return withFiles.ApplyStackRecordsWithFiles(projectID, stackID, files)
			}
		}
		if err := apply(projectID, stackID); err != nil {
			return d.record(step, "apply-stack", m.Name, err, nil)
		}
		if !d.Journal.Done(step) {
			_ = d.Journal.Append(journal.Entry{Step: step, Group: d.Group.ID, Action: "apply-stack",
				Target: m.Name, Result: journal.OK, Created: stackID})
		}
		if err := d.carryStackImages(images, m, projectID, stackID, step); err != nil {
			return err
		}
		if err := d.carryLimits(m, projectID, stackID); err != nil {
			return err
		}
		if err := d.stackPorts(m, false); err != nil {
			return err
		}
	case !d.Journal.Done(step):
		if err := d.API.ApplyStack(projectID, stackID); err != nil {
			return d.record(step, "apply-stack", m.Name, err, nil)
		}
		_ = d.Journal.Append(journal.Entry{Step: step, Group: d.Group.ID, Action: "apply-stack",
			Target: m.Name, Result: journal.OK, Created: stackID})
	}
	return d.routeStack(m, projectID, stackID)
}

// repoFiles reads the paths a compose app bind-mounts from beside its compose
// file out of Dokploy's checkout, keyed as the file names them, for a stack
// whose repository cannot be read yet: a provider connected through a
// callback on this server is reconnected after the move, not before.
func (d MoveDeps) repoFiles(m GroupMember) (map[string]string, error) {
	if d.Repo == nil {
		return nil, nil
	}
	for _, it := range d.Plan.Items {
		if it.Kind != "compose" || it.ID != m.ID || it.Details["repo_files"] == "" {
			continue
		}
		dir := path.Join("/etc/dokploy/compose", it.Details["app_name"], "code",
			path.Dir(path.Clean("/"+it.Details["compose_path"])))
		out := map[string]string{}
		total := 0
		for _, src := range strings.Split(it.Details["repo_files"], ",") {
			key := path.Clean(src)
			tree, err := d.Repo.Tree(path.Join(dir, key))
			if err != nil {
				return nil, fmt.Errorf("read %s from %s's checkout: %w", src, m.Name, err)
			}
			for rel, content := range tree {
				if len(content) > maxRepoFile {
					return nil, fmt.Errorf("%s/%s is larger than 1 MiB, which a stack cannot mount", key, rel)
				}
				total += len(content)
				if rel == "" {
					out[key] = content
				} else {
					out[key+"/"+rel] = content
				}
			}
		}
		if total > maxRepoFiles {
			return nil, fmt.Errorf("%s mounts %d MiB of files from its repository, more than a move can send", m.Name, total>>20)
		}
		return out, nil
	}
	return nil, nil
}

const (
	maxRepoFile  = 1 << 20  // a stack's file is a Kubernetes Secret, 1 MiB at most
	maxRepoFiles = 12 << 20 // what one apply carries
)

// carryLimits gives each of a compose app's services the memory and CPU its
// container had. Docker set none unless asked, so most had the whole machine,
// and so they get it here too: a Redpanda that takes 1 GiB could not start
// inside Meshploy's default 1 GiB limit. What was carried shows on each
// service, to be tightened on purpose rather than by accident.
func (d MoveDeps) carryLimits(m GroupMember, projectID, stackID string) error {
	api, ok := d.API.(interface {
		StackServiceRefs(projectID, stackID string) ([]StackServiceRef, error)
		SetServiceLimits(projectID, serviceID, cpu, memory string) error
	})
	if !ok || d.Images == nil {
		return nil
	}
	refs, err := api.StackServiceRefs(projectID, stackID)
	if err != nil {
		return err
	}
	machineCPU, machineMem := hostCapacity(d.Images.Runner)
	for _, svc := range refs {
		container, err := d.Images.ComposeContainer(d.appName(m.ID), svc.Name)
		if err != nil || container == "" {
			continue
		}
		out, err := d.Images.Runner.Output("docker", "inspect", container, "--format", "{{.HostConfig.NanoCpus}} {{.HostConfig.Memory}}")
		if err != nil {
			continue
		}
		var nano, mem int64
		fmt.Sscan(strings.TrimSpace(out), &nano, &mem)
		cpu, memory := machineCPU, machineMem
		if nano > 0 {
			cpu = fmt.Sprintf("%dm", (nano+999999)/1000000)
		}
		if mem > 0 {
			memory = fmt.Sprint(mem)
		}
		if cpu == "" || memory == "" {
			continue
		}
		if err := api.SetServiceLimits(projectID, svc.ID, cpu, memory); err != nil {
			return d.record(d.step(m.ID, "limits")+"/"+svc.ID, "set-limits", svc.Name, err, nil)
		}
	}
	return nil
}

// hostCapacity is this machine's CPU and memory, as limits: what a container
// Docker set no limit on could use.
func hostCapacity(r migrate.Runner) (cpu, memory string) {
	if out, err := r.Output("nproc"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(out)); err == nil && n > 0 {
			cpu = fmt.Sprintf("%dm", n*1000)
		}
	}
	if out, err := r.Output("cat", "/proc/meminfo"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if f := strings.Fields(line); len(f) >= 2 && f[0] == "MemTotal:" {
				if kb, err := strconv.ParseInt(f[1], 10, 64); err == nil {
					memory = fmt.Sprintf("%dMi", kb/1024)
				}
			}
		}
	}
	return cpu, memory
}

// routeStack makes the routes for a compose app's domains, now that its stack
// has created the services they point at: paused, like every route prepare
// makes, and published when the group's domains switch over below.
func (d MoveDeps) routeStack(m GroupMember, projectID, stackID string) error {
	api, ok := d.API.(interface {
		StackServiceRefs(projectID, stackID string) ([]StackServiceRef, error)
		CreateRoute(projectID string, spec RouteSpec) (string, error)
	})
	if !ok {
		return nil
	}
	var refs []StackServiceRef
	for _, it := range d.Plan.Items {
		if it.Kind != "domain" || it.Verdict != Moves || it.Details["compose_id"] != m.ID {
			continue
		}
		step := "prepare/route/" + it.ID
		if d.Journal.Done(step) {
			continue
		}
		if refs == nil {
			var err error
			if refs, err = api.StackServiceRefs(projectID, stackID); err != nil {
				return err
			}
		}
		serviceID := ""
		for _, r := range refs {
			if r.Name == it.Details["service_name"] {
				serviceID = r.ID
			}
		}
		if serviceID == "" {
			return d.record(step, "create-route", it.Name, fmt.Errorf("its stack has no service %q to route to", it.Details["service_name"]), nil)
		}
		spec := RouteSpec{Hostname: splitHostPath(it.Name), ServiceID: serviceID, Port: atoiOr(it.Details["port"], 0),
			Path: it.Details["path"], StripPath: it.Details["strip_path"] == "true"}
		id, err := createOrExtendRoute(api, d.Journal, d.Plan, it.ID, projectID, spec)
		if err != nil {
			return d.record(step, "create-route", it.Name, err, nil)
		}
		if err := d.Journal.Append(journal.Entry{Step: step, Group: d.Group.ID, Action: "create-route",
			Target: spec.Hostname, Result: journal.OK, Created: id}); err != nil {
			return err
		}
	}
	return nil
}

// carryStackImages sets each of a compose app's services to run the image its
// Dokploy container runs, pinned: a service built from source then starts
// without being built, and one from a registry on the exact image it ran. A
// service with no container in Dokploy keeps what the stack gave it.
func (d MoveDeps) carryStackImages(api StackImageAPI, m GroupMember, projectID, stackID, step string) error {
	refs, err := api.StackServiceRefs(projectID, stackID)
	if err != nil {
		return err
	}
	project := d.appName(m.ID)
	for _, svc := range refs {
		container, err := d.Images.ComposeContainer(project, svc.Name)
		if err != nil {
			return fmt.Errorf("find %s's container: %w", svc.Name, err)
		}
		if container == "" {
			continue
		}
		ref, err := d.Images.Carry(step+"/image/"+svc.ID, d.Group.ID, container)
		if err != nil {
			return d.record(step+"/image/"+svc.ID, "carry-image", svc.Name, err, nil)
		}
		if ref == svc.Image {
			continue
		}
		if err := api.SetServiceImage(projectID, svc.ID, ref); err != nil {
			return d.record(step+"/set-image/"+svc.ID, "set-image", svc.Name, err, nil)
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
	seen := map[string]bool{}
	for _, it := range d.Plan.Items {
		if it.Kind != "domain" || it.Verdict != Moves {
			continue
		}
		owner := it.Details["application_id"]
		labelled := false
		if owner == "" {
			owner, labelled = it.Details["compose_id"], true
		}
		// Dokploy's rows for other paths on a hostname share its one route:
		// the hostname switches once.
		if !members[owner] || seen[splitHostPath(it.Name)] {
			continue
		}
		seen[splitHostPath(it.Name)] = true
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
	// After the edge was taken first, Meshploy's edge already holds the
	// domain and sends it to the old one only while no route of its own is
	// published: publishing the route below is the whole switch.
	if d.Journal.Done("cutover/fallback") {
		return d.publishDomainRoute(dom)
	}
	if dom.labelled {
		if err := d.Edge.TakeOverHosts(step, d.Group.ID, dom.appName, []string{dom.host}); err != nil {
			return err
		}
	} else if err := d.Edge.SwitchApp(step, d.Group.ID, dom.appName); err != nil {
		return err
	}

	return d.publishDomainRoute(dom)
}

// publishDomainRoute takes the domain's route out of paused.
func (d MoveDeps) publishDomainRoute(dom domain) error {
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
			// A run-once service - a migration step the others wait on - is
			// done when it has completed.
			if status == "running" || status == "completed" {
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
