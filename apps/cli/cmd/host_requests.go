package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/dokploy"
	"github.com/meshploy/apps/cli/internal/migrate/journal"
	"github.com/meshploy/apps/cli/internal/setup"
	"github.com/meshploy/packages/client"
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
	case hostagent.RequestMigratePrepare:
		body, err := runMigratePrepare()
		return hostagent.PrepareFile, body, 0o600, err
	case hostagent.RequestMigrateMove:
		body, err := runMigrateMove(req.Args["group"])
		return hostagent.MoveFile, body, 0o600, err
	case hostagent.RequestMigrateCutover:
		body, err := runMigrateCutover()
		return hostagent.CutoverFile, body, 0o600, err
	case hostagent.RequestMigrateFinish:
		body, err := runMigrateFinish(req.Args["volumes"] == "true", req.Args["plan"] == "true")
		return hostagent.FinishFile, body, 0o600, err
	case hostagent.RequestMigrateRollback:
		body, err := runMigrateRollback(req.Args["group"])
		return hostagent.RollbackFile, body, 0o600, err
	case hostagent.RequestMigrateCredential:
		body, err := takeMigrationCredential()
		// Kept beside the plan, readable by root only: it is a credential that
		// can act on the whole organisation until finish revokes it.
		return migrateCredentialFile, body, 0o600, err
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
		if e.Name() == hostagent.CredentialFile {
			continue // the migration's token, taken by the request that needs it
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

// migrateCredentialFile is where the agent keeps the migration's token, under
// state/migrate/. The API mounts state/ read-only, so what the agent writes
// here it cannot read back.
const migrateCredentialFile = "dokploy-credential.json"

// takeMigrationCredential consumes the token the API left in the inbox and
// checks the API accepts it, so a credential that would fail later fails now,
// while somebody is watching.
func takeMigrationCredential() ([]byte, error) {
	cred, err := hostagent.TakeCredential(hostDir)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, fmt.Errorf("no credential was left for this request")
	}
	if _, err := client.New(cred.BaseURL, cred.Token).ListOrgs(); err != nil {
		return nil, fmt.Errorf("the API did not accept the migration credential: %w", err)
	}
	return json.Marshal(cred)
}

// runMigratePrepare builds the Meshploy side of a confirmed plan.
//
// Stage 1 touches nothing of Dokploy's: it creates projects, workloads and
// routes, all inert, so it can be run, interrupted and run again while Dokploy
// keeps serving.
func runMigratePrepare() ([]byte, error) {
	plan, choices, err := setup.ReadConfirmedPlan()
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, fmt.Errorf("no confirmed migration plan on this server: review and confirm one first")
	}
	// A migration that creates most of an application is worse than one that
	// will not start, so what this stage cannot carry yet stops it here.
	if missing := dokploy.Unsupported(*plan); len(missing) > 0 {
		return nil, fmt.Errorf("this plan needs what stage 1 does not carry yet:\n  %s", strings.Join(missing, "\n  "))
	}

	cred, err := readMigrationCredential()
	if err != nil {
		return nil, err
	}
	// Re-read Dokploy now rather than trusting the plan: it holds no secrets by
	// design, and the server may have changed since the plan was made.
	src, err := dokploy.Collect(migrate.ExecRunner{})
	if err != nil {
		return nil, err
	}

	j, err := journal.Open(setup.MigrationDir())
	if err != nil {
		return nil, err
	}
	defer j.Close()

	api := dokploy.ClientAPI{C: client.New(cred.BaseURL, cred.Token), OrgID: cred.OrgID}
	// Where locally built images are carried to. An install without a built-in
	// registry leaves images as they are, which is right for a server whose
	// images all came from registries and honest for one whose did not: the
	// failure is then a pull that cannot find the image, not a silent rewrite.
	registry, err := api.BuiltinRegistry()
	if err != nil {
		return nil, err
	}

	// The console follows the stages through this summary, so prepare publishes
	// one too - it is the stage that decides whether anything can move at all.
	defer func() {
		status := dokploy.Progress(*plan, j, time.Now())
		if body, mErr := json.Marshal(status); mErr == nil {
			_ = writeFileAtomic(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.StatusFile), body, 0o644)
		}
	}()

	answers := map[string]map[string]string{}
	if choices != nil {
		answers = choices.Decisions
	}
	result, err := dokploy.Prepare(dokploy.PrepareDeps{
		Plan:    *plan,
		Answers: answers,
		Source:  src,
		API:     api,
		Journal: j,
		Images:  &dokploy.ImageMover{Runner: migrate.ExecRunner{}, Registry: registry, Journal: j},
	})
	if err != nil {
		return nil, err
	}
	body, marshalErr := json.Marshal(result)
	if len(result.Failures) > 0 {
		// Reported as a failure so the console does not show a green stage that
		// created half a server, but the result is still written.
		_ = writeFileAtomic(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.PrepareFile), body, 0o600)
		return nil, fmt.Errorf("%d of %d steps failed; the first was: %s",
			len(result.Failures), result.Created+len(result.Failures), result.Failures[0])
	}
	return body, marshalErr
}

// readMigrationCredential reads the token the agent took from the inbox.
func readMigrationCredential() (*hostagent.Credential, error) {
	b, err := os.ReadFile(filepath.Join(hostagent.MigrateDir(hostDir), migrateCredentialFile))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("this server has no migration credential yet: start the migration from the console")
	}
	if err != nil {
		return nil, err
	}
	var cred hostagent.Credential
	if err := json.Unmarshal(b, &cred); err != nil {
		return nil, err
	}
	if cred.Token == "" || cred.OrgID == "" {
		return nil, fmt.Errorf("the stored migration credential is incomplete")
	}
	return &cred, nil
}
