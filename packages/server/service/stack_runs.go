package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// A stack run is one Sync or Apply and its rollout, kept on record: the
// request answers with the first depends_on layer, and the rest roll out after
// it, so the record is where the whole of it can be followed and looked back on.

// RolloutStep is one service of a run's rollout.
type RolloutStep struct {
	Name         string     `json:"name"`
	ServiceID    uuid.UUID  `json:"service_id"`
	Layer        int        `json:"layer"`
	DeploymentID *uuid.UUID `json:"deployment_id,omitempty"`
	Status       string     `json:"status"`
	Error        string     `json:"error,omitempty"`
}

// Rollout step states.
const (
	RolloutWaiting    = "waiting"     // its layer has not started
	RolloutStarted    = "started"     // building or deploying
	RolloutSucceeded  = "succeeded"   // up, or run once and completed
	RolloutFailed     = "failed"      // its deployment failed, or could not start
	RolloutNotStarted = "not_started" // something it depends on failed
)

// RunResult is what the apply said, kept with the run.
type RunResult struct {
	Created  []string `json:"created"`
	Updated  []string `json:"updated"`
	Deleted  []string `json:"deleted"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// StackRunView is a run as the console shows it.
type StackRunView struct {
	meshdb.StackRun
	Result  RunResult     `json:"result"`
	Rollout []RolloutStep `json:"rollout"`
}

// runTracker is a run being followed: its steps in memory, saved as they
// change. Nil is a run not kept, and every method is a no-op on it.
type runTracker struct {
	id    uuid.UUID
	mu    sync.Mutex
	steps []RolloutStep
	// errors is whether the apply itself reported any, which fail the run
	// even when every service it rolled out came up.
	errors bool
}

func (r *runTracker) step(serviceID uuid.UUID, f func(*RolloutStep)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.steps {
		if r.steps[i].ServiceID == serviceID {
			f(&r.steps[i])
		}
	}
}

func (r *runTracker) stepByDeployment(deploymentID uuid.UUID, status string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.steps {
		if d := r.steps[i].DeploymentID; d != nil && *d == deploymentID {
			r.steps[i].Status = status
		}
	}
}

func (r *runTracker) anyFailed() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, st := range r.steps {
		if st.Status == RolloutFailed {
			return true
		}
	}
	return false
}

// failedNames are the services whose deployment failed.
func (r *runTracker) failedNames() map[string]bool {
	out := map[string]bool{}
	if r == nil {
		return out
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, st := range r.steps {
		if st.Status == RolloutFailed {
			out[st.Name] = true
		}
	}
	return out
}

// stopRest marks what never started as not started, because of a layer
// before it.
func (r *runTracker) stopRest() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.steps {
		if r.steps[i].Status == RolloutWaiting {
			r.steps[i].Status = RolloutNotStarted
		}
	}
}

func (r *runTracker) save(ctx context.Context, db *gorm.DB) {
	if r == nil {
		return
	}
	r.mu.Lock()
	body, _ := json.Marshal(r.steps)
	r.mu.Unlock()
	db.WithContext(ctx).Model(&meshdb.StackRun{}).Where("id = ?", r.id).Update("rollout", string(body))
}

// startRun keeps a record of an apply and the rollout it starts.
func (s *StackService) startRun(ctx context.Context, stackID, triggerBy uuid.UUID, opts ApplyOptions, layers [][]rollItem) *runTracker {
	kind := opts.Kind
	if kind == "" {
		kind = "apply"
	}
	r := &runTracker{id: uuid.New()}
	for n, l := range layers {
		for _, it := range l {
			r.steps = append(r.steps, RolloutStep{Name: it.Name, ServiceID: it.ID, Layer: n, Status: RolloutWaiting})
		}
	}
	body, _ := json.Marshal(r.steps)
	row := meshdb.StackRun{StackID: stackID, Kind: kind, Commit: opts.Commit, Status: meshdb.StackRunRunning, Rollout: string(body)}
	row.ID = r.id
	if triggerBy != uuid.Nil {
		by := triggerBy
		row.TriggeredBy = &by
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil
	}
	return r
}

// recordResult keeps what the apply said with its run.
func (s *StackService) recordResult(ctx context.Context, r *runTracker, result *ApplyResult) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.errors = len(result.Errors) > 0
	r.mu.Unlock()
	body, _ := json.Marshal(RunResult{Created: result.Created, Updated: result.Updated, Deleted: result.Deleted,
		Errors: result.Errors, Warnings: result.Warnings})
	s.db.WithContext(ctx).Model(&meshdb.StackRun{}).Where("id = ?", r.id).Update("result", string(body))
}

// finishRun closes a run: stopped when a layer failed before others could
// start, failed when a service failed, succeeded otherwise.
func (s *StackService) finishRun(ctx context.Context, r *runTracker) {
	if r == nil {
		return
	}
	status := meshdb.StackRunSucceeded
	r.mu.Lock()
	for _, st := range r.steps {
		switch st.Status {
		case RolloutNotStarted:
			status = meshdb.StackRunStopped
		case RolloutFailed:
			if status != meshdb.StackRunStopped {
				status = meshdb.StackRunFailed
			}
		}
	}
	if status == meshdb.StackRunSucceeded && r.errors {
		status = meshdb.StackRunFailed
	}
	r.mu.Unlock()
	r.save(ctx, s.db)
	now := time.Now()
	s.db.WithContext(ctx).Model(&meshdb.StackRun{}).Where("id = ?", r.id).
		Updates(map[string]any{"status": status, "finished_at": &now})
}

// stackRunStale is how long a run may say it is running without a change
// before it is shown as interrupted: an API restart ends the rollout that
// was following it.
const stackRunStale = time.Hour

func runView(row meshdb.StackRun) StackRunView {
	v := StackRunView{StackRun: row, Rollout: []RolloutStep{}}
	_ = json.Unmarshal([]byte(row.Result), &v.Result)
	_ = json.Unmarshal([]byte(row.Rollout), &v.Rollout)
	if v.Rollout == nil {
		v.Rollout = []RolloutStep{}
	}
	if row.Status == meshdb.StackRunRunning && time.Since(row.UpdatedAt) > stackRunStale {
		v.Status = "interrupted"
	}
	return v
}

// ListRuns is a stack's runs, newest first.
func (s *StackService) ListRuns(ctx context.Context, stackID uuid.UUID, limit int) ([]StackRunView, error) {
	var rows []meshdb.StackRun
	if err := s.db.WithContext(ctx).Where("stack_id = ?", stackID).Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]StackRunView, 0, len(rows))
	for _, r := range rows {
		out = append(out, runView(r))
	}
	return out, nil
}

// GetRun is one of a stack's runs.
func (s *StackService) GetRun(ctx context.Context, stackID, runID uuid.UUID) (*StackRunView, error) {
	var row meshdb.StackRun
	if err := s.db.WithContext(ctx).First(&row, "id = ? AND stack_id = ?", runID, stackID).Error; err != nil {
		return nil, err
	}
	v := runView(row)
	return &v, nil
}
