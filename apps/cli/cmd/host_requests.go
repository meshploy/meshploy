package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/dokploy"
	"github.com/meshploy/packages/hostagent"
)

// hostRunRequest does one request's work and returns the result to write:
// the file name under state/migrate/, its contents, and its permissions.
// A seam for tests.
var hostRunRequest = func(req hostagent.Request) (file string, body []byte, perm os.FileMode, err error) {
	now := time.Now()
	switch req.Type {
	case hostagent.RequestMigrateDetect:
		src, err := dokploy.CollectDetect(migrate.ExecRunner{})
		if err != nil && !src.Detection.Dokploy {
			return "", nil, 0, err
		}
		plan := dokploy.BuildPlan(dokploy.Source{Detection: src.Detection, Docker: src.Docker, Listeners: src.Listeners,
			Resources: src.Resources, DynamicFiles: src.DynamicFiles, AcmeBytes: src.AcmeBytes}, now)
		body, err := json.Marshal(map[string]any{"generated_at": plan.GeneratedAt, "detection": plan.Detection,
			"edge": plan.Edge, "resources": plan.Resources, "mode": plan.Mode})
		return hostagent.DetectFile, body, 0o644, err
	case hostagent.RequestMigratePlan:
		src, err := dokploy.Collect(migrate.ExecRunner{})
		if err != nil {
			return "", nil, 0, err
		}
		if !src.Detection.Dokploy {
			return "", nil, 0, fmt.Errorf("Dokploy was not found on this server")
		}
		if !src.Detection.Supported {
			return "", nil, 0, fmt.Errorf("cannot plan: %s", src.Detection.SupportNote)
		}
		body, err := json.Marshal(dokploy.BuildPlan(src, now))
		// Names of apps, domains and paths: readable by root only.
		return hostagent.PlanFile, body, 0o600, err
	}
	return "", nil, 0, fmt.Errorf("unknown request type %q", req.Type)
}

// requestRunner takes requests from the inbox, one of each type at a time.
type requestRunner struct {
	mu      sync.Mutex
	running map[string]bool
	wg      sync.WaitGroup
}

// check starts every waiting request whose type is not already running.
// report is called with the result of each request as it finishes.
func (rr *requestRunner) check(report func(hostagent.Task)) {
	inbox := hostagent.InboxDir(hostDir)
	entries, err := os.ReadDir(inbox)
	if err != nil {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		path := filepath.Join(inbox, e.Name())
		if e.Name()[0] == '.' {
			continue // a request still being written, under a temporary name
		}
		req, err := hostagent.ReadRequestFile(path)
		if err != nil {
			// Not a request the agent will ever run: take it out of the way.
			_ = os.Remove(path)
			report(hostagent.Task{Error: "discarded " + e.Name() + ": " + err.Error(), At: time.Now().UTC()})
			continue
		}
		rr.mu.Lock()
		busy := rr.running[req.Type]
		if !busy {
			rr.running[req.Type] = true
		}
		rr.mu.Unlock()
		if busy {
			continue // left in the inbox until the one running finishes
		}
		if err := os.Remove(path); err != nil {
			rr.done(req.Type)
			continue
		}
		rr.wg.Add(1)
		go func() {
			defer rr.wg.Done()
			defer rr.done(req.Type)
			report(runHostRequest(req))
		}()
	}
}

func (rr *requestRunner) done(kind string) {
	rr.mu.Lock()
	delete(rr.running, kind)
	rr.mu.Unlock()
}

// runHostRequest runs one request and records its status and result.
func runHostRequest(req hostagent.Request) hostagent.Task {
	status := hostagent.RequestStatus{ID: req.ID, Type: req.Type, State: hostagent.RequestRunning,
		RequestedBy: req.RequestedBy, StartedAt: time.Now().UTC()}
	statusPath := filepath.Join(hostagent.RequestsDir(hostDir), req.ID+".json")
	_ = os.MkdirAll(hostagent.RequestsDir(hostDir), 0o755)
	_ = writeHostJSON(statusPath, status)

	file, body, perm, err := hostRunRequest(req)
	if err == nil {
		if mkErr := os.MkdirAll(hostagent.MigrateDir(hostDir), 0o755); mkErr != nil {
			err = mkErr
		} else {
			err = writeFileAtomic(filepath.Join(hostagent.MigrateDir(hostDir), file), body, perm)
		}
	}

	finished := time.Now().UTC()
	status.FinishedAt = &finished
	status.State = hostagent.RequestSucceeded
	if err != nil {
		status.State, status.Error = hostagent.RequestFailed, clipLine(err.Error())
	}
	_ = writeHostJSON(statusPath, status)
	return hostagent.Task{OK: err == nil, Error: status.Error, At: finished}
}
