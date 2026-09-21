package service

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// What has happened in this workspace lately.
//
// Two tables answer that question, because two things in Meshploy run and then
// either worked or did not: a deployment and a job run. They are different
// records with different pages behind them, so this does not try to make them
// one row shape in the database - it reads both and interleaves them by time.
//
// Backups are deliberately absent. There is no backup-runs table: a backup
// config keeps only its last status and time, so including them would put one
// row per config into a list that everything else has a history for, and a
// reader could not tell the difference.
//
// Nodes joining, routes being published, members being added: none of those are
// recorded anywhere as events, and inventing an entry for them here would be
// inventing the timestamp too. They belong to an audit log, which is its own
// piece of work.

// ActivityService reads the workspace's feed.
type ActivityService struct{ db *gorm.DB }

// ActivityKind is which table an entry came from, and so which page it links
// to.
type ActivityKind string

const (
	ActivityDeployment ActivityKind = "deployment"
	ActivityJobRun     ActivityKind = "job_run"
)

// ActivityEntry is one thing that ran.
type ActivityEntry struct {
	Kind ActivityKind `json:"kind"`
	ID   uuid.UUID    `json:"id"`
	// Status is the record's own: a deployment's or a job run's. They overlap
	// on success and failed and differ elsewhere, and the console reads each by
	// its kind rather than pretending there is one vocabulary.
	Status string `json:"status"`
	// Detail is the one thing worth showing beside the name: the image a
	// deployment used, or a job's schedule.
	Detail     string     `json:"detail,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`

	// What it happened to, and where that lives.
	ResourceID   uuid.UUID `json:"resource_id"`
	ResourceName string    `json:"resource_name"`
	// ResourceType is application, database or job.
	ResourceType string    `json:"resource_type"`
	ProjectID    uuid.UUID `json:"project_id"`
	ProjectName  string    `json:"project_name"`
}

// ListRecent is the newest entries across a set of projects.
//
// The caller decides which projects those are: a member who can see three of a
// dozen must not learn the other nine exist by reading what ran there. No
// projects means nothing, which is why the guard is on the slice being empty
// rather than on it being nil.
//
// Each source is asked for limit rows and the merge keeps the newest limit of
// them, which is correct: the newest N overall are always within the newest N
// of each source.
func (s *ActivityService) ListRecent(ctx context.Context, projectIDs []uuid.UUID, limit int) ([]ActivityEntry, error) {
	out := make([]ActivityEntry, 0)
	if len(projectIDs) == 0 {
		return out, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	deployments, err := s.recentDeployments(ctx, projectIDs, limit)
	if err != nil {
		return out, err
	}
	runs, err := s.recentJobRuns(ctx, projectIDs, limit)
	if err != nil {
		return out, err
	}

	out = append(append(out, deployments...), runs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *ActivityService) recentDeployments(ctx context.Context, projectIDs []uuid.UUID, limit int) ([]ActivityEntry, error) {
	rows := make([]ActivityEntry, 0)
	err := s.db.WithContext(ctx).
		Table("deployments d").
		Select(`'deployment' AS kind, d.id, d.status, d.image AS detail,
			d.created_at, d.deployed_at AS finished_at,
			s.id AS resource_id, s.name AS resource_name, s.type AS resource_type,
			p.id AS project_id, p.name AS project_name`).
		Joins("JOIN services s ON s.id = d.service_id").
		Joins("JOIN projects p ON p.id = s.project_id").
		Where("p.id IN ?", projectIDs).
		Order("d.created_at DESC").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}

func (s *ActivityService) recentJobRuns(ctx context.Context, projectIDs []uuid.UUID, limit int) ([]ActivityEntry, error) {
	rows := make([]ActivityEntry, 0)
	err := s.db.WithContext(ctx).
		Table("job_runs r").
		Select(`'job_run' AS kind, r.id, r.status, j.schedule AS detail,
			r.created_at, r.finished_at,
			j.id AS resource_id, j.name AS resource_name, 'job' AS resource_type,
			p.id AS project_id, p.name AS project_name`).
		Joins("JOIN jobs j ON j.id = r.job_id").
		Joins("JOIN projects p ON p.id = j.project_id").
		Where("p.id IN ?", projectIDs).
		Order("r.created_at DESC").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}
