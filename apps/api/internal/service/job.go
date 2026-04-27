package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type JobService struct {
	db *gorm.DB
}

// ─── Input types ─────────────────────────────────────────────────────────────

type CreateJobInput struct {
	ProjectID uuid.UUID
	Name      string
	IsCron    bool
	Image     string
	Command   string
	// Cron-only
	Schedule          string
	ConcurrencyPolicy db.ConcurrencyPolicy
	HistoryLimit      int
	// Resources
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
	// Env
	EnvVars string // raw .env block
	// Placement
	NodeID *uuid.UUID
}

type UpdateJobInput struct {
	Name              *string
	Image             *string
	Command           *string
	Schedule          *string
	ConcurrencyPolicy *db.ConcurrencyPolicy
	HistoryLimit      *int
	CPURequest        *string
	CPULimit          *string
	MemoryRequest     *string
	MemoryLimit       *string
	EnvVars           *string
	NodeID            *uuid.UUID
}

// ─── Read ─────────────────────────────────────────────────────────────────────

func (s *JobService) List(ctx context.Context, projectID uuid.UUID) ([]db.Job, error) {
	var rows []db.Job
	err := s.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Find(&rows).Error
	return rows, err
}

func (s *JobService) Get(ctx context.Context, jobID uuid.UUID) (*db.Job, error) {
	var row db.Job
	err := s.db.WithContext(ctx).First(&row, "id = ?", jobID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("job not found")
	}
	return &row, err
}

func (s *JobService) ListRuns(ctx context.Context, jobID uuid.UUID) ([]db.JobRun, error) {
	var rows []db.JobRun
	err := s.db.WithContext(ctx).
		Where("job_id = ?", jobID).
		Order("created_at DESC").
		Limit(50).
		Find(&rows).Error
	return rows, err
}

// ─── Write ────────────────────────────────────────────────────────────────────

func (s *JobService) Create(ctx context.Context, in CreateJobInput) (*db.Job, error) {
	k8sName, err := jobK8sName(in.Name)
	if err != nil {
		return nil, err
	}

	policy := in.ConcurrencyPolicy
	if policy == "" {
		policy = db.ConcurrencyAllow
	}
	historyLimit := in.HistoryLimit
	if historyLimit == 0 {
		historyLimit = 5
	}

	row := db.Job{
		ProjectID:         in.ProjectID,
		NodeID:            in.NodeID,
		Name:              in.Name,
		IsCron:            in.IsCron,
		Image:             in.Image,
		Command:           in.Command,
		Schedule:          in.Schedule,
		ConcurrencyPolicy: policy,
		HistoryLimit:      historyLimit,
		CPURequest:        orDefault(in.CPURequest, "100m"),
		CPULimit:          orDefault(in.CPULimit, "500m"),
		MemoryRequest:     orDefault(in.MemoryRequest, "128Mi"),
		MemoryLimit:       orDefault(in.MemoryLimit, "512Mi"),
		EnvVars:           db.EncryptedString(in.EnvVars),
		Status:            db.JobStatusIdle,
		K8sName:           k8sName,
	}

	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *JobService) Update(ctx context.Context, jobID uuid.UUID, in UpdateJobInput) (*db.Job, error) {
	var row db.Job
	if err := s.db.WithContext(ctx).First(&row, "id = ?", jobID).Error; err != nil {
		return nil, fmt.Errorf("job not found")
	}

	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Image != nil {
		updates["image"] = *in.Image
	}
	if in.Command != nil {
		updates["command"] = *in.Command
	}
	if in.Schedule != nil {
		updates["schedule"] = *in.Schedule
	}
	if in.ConcurrencyPolicy != nil {
		updates["concurrency_policy"] = *in.ConcurrencyPolicy
	}
	if in.HistoryLimit != nil {
		updates["history_limit"] = *in.HistoryLimit
	}
	if in.CPURequest != nil {
		updates["cpu_request"] = *in.CPURequest
	}
	if in.CPULimit != nil {
		updates["cpu_limit"] = *in.CPULimit
	}
	if in.MemoryRequest != nil {
		updates["memory_request"] = *in.MemoryRequest
	}
	if in.MemoryLimit != nil {
		updates["memory_limit"] = *in.MemoryLimit
	}
	if in.EnvVars != nil {
		updates["env_vars"] = db.EncryptedString(*in.EnvVars)
	}
	if in.NodeID != nil {
		updates["node_id"] = in.NodeID
	}

	if err := s.db.WithContext(ctx).Model(&row).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *JobService) Delete(ctx context.Context, jobID uuid.UUID) error {
	res := s.db.WithContext(ctx).Delete(&db.Job{}, "id = ?", jobID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("job not found")
	}
	return nil
}

// Trigger creates a JobRun record for a manual trigger.
// Actual K8s Job creation will be wired in a future iteration alongside the
// CronJob reconciler. For now it records the run as pending.
func (s *JobService) Trigger(ctx context.Context, jobID uuid.UUID) (*db.JobRun, error) {
	job, err := s.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}

	run := db.JobRun{
		JobID:  job.ID,
		Status: db.JobStatusPending,
	}
	if err := s.db.WithContext(ctx).Create(&run).Error; err != nil {
		return nil, err
	}

	// Update parent job status.
	s.db.WithContext(ctx).Model(job).Update("status", db.JobStatusPending)

	return &run, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func jobK8sName(name string) (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	// K8s name limit: 63 chars; leave room for suffix.
	if len(slug) > 50 {
		slug = slug[:50]
	}
	return fmt.Sprintf("%s-%s", slug, hex.EncodeToString(b)), nil
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
