package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appk8s "github.com/meshploy/apps/api/internal/k8s"
	"github.com/meshploy/packages/db"
	batchv1 "k8s.io/api/batch/v1"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

type JobService struct {
	db  *gorm.DB
	k8s kubernetes.Interface
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

	if row.IsCron && s.k8s != nil && row.Schedule != "" {
		s.applyCronJob(ctx, &row)
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

	// Re-apply K8s CronJob if any cron-relevant field changed.
	if row.IsCron && s.k8s != nil {
		s.db.WithContext(ctx).First(&row, "id = ?", jobID)
		if row.Schedule != "" {
			s.applyCronJob(ctx, &row)
		}
	}

	return &row, nil
}

func (s *JobService) Delete(ctx context.Context, jobID uuid.UUID) error {
	var row db.Job
	if err := s.db.WithContext(ctx).First(&row, "id = ?", jobID).Error; err != nil {
		return fmt.Errorf("job not found")
	}

	if row.IsCron && s.k8s != nil {
		var project db.Project
		if s.db.WithContext(ctx).First(&project, "id = ?", row.ProjectID).Error == nil {
			_ = appk8s.DeleteCronJob(ctx, s.k8s, project.Slug, row.K8sName)
		}
	}

	res := s.db.WithContext(ctx).Delete(&db.Job{}, "id = ?", jobID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("job not found")
	}
	return nil
}

// applyCronJob ensures the K8s CronJob matches the current job config.
func (s *JobService) applyCronJob(ctx context.Context, job *db.Job) {
	var project db.Project
	if err := s.db.WithContext(ctx).First(&project, "id = ?", job.ProjectID).Error; err != nil {
		return
	}
	nodeName := ""
	if job.NodeID != nil {
		var node db.Node
		if s.db.WithContext(ctx).First(&node, "id = ?", job.NodeID).Error == nil {
			nodeName = node.Name
		}
	}
	_ = appk8s.EnsureNamespace(ctx, s.k8s, project.Slug)
	_ = appk8s.ApplyCronJob(ctx, s.k8s, appk8s.CronJobParams{
		Name:              job.K8sName,
		Namespace:         project.Slug,
		Schedule:          job.Schedule,
		ConcurrencyPolicy: dbConcurrencyToK8s(job.ConcurrencyPolicy),
		HistoryLimit:      int32(job.HistoryLimit),
		Image:             job.Image,
		Command:           job.Command,
		EnvVars:           appk8s.ParseEnvBlock(string(job.EnvVars)),
		CPURequest:        job.CPURequest,
		CPULimit:          job.CPULimit,
		MemRequest:        job.MemoryRequest,
		MemLimit:          job.MemoryLimit,
		NodeName:          nodeName,
		JobID:             job.ID.String(),
	})
}

func dbConcurrencyToK8s(p db.ConcurrencyPolicy) batchv1.ConcurrencyPolicy {
	switch p {
	case db.ConcurrencyForbid:
		return batchv1.ForbidConcurrent
	case db.ConcurrencyReplace:
		return batchv1.ReplaceConcurrent
	default:
		return batchv1.AllowConcurrent
	}
}

// StartReconciler runs a background loop that records JobRuns for cron-fired K8s Jobs.
func (s *JobService) StartReconciler(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileCronRuns(context.Background())
		}
	}
}

func (s *JobService) reconcileCronRuns(ctx context.Context) {
	k8sJobs, err := appk8s.ListCronRuns(ctx, s.k8s)
	if err != nil {
		return
	}

	for _, kj := range k8sJobs {
		isSucceeded := kj.Status.Succeeded > 0
		isFailed := kj.Status.Failed > 0

		if !isSucceeded && !isFailed {
			continue // still running
		}

		// Skip if we already have a record for this K8s job.
		var existing db.JobRun
		if s.db.WithContext(ctx).Where("k8s_job_name = ?", kj.Name).First(&existing).Error == nil {
			continue
		}

		jobIDStr := kj.Labels["meshploy-job-id"]
		if jobIDStr == "" {
			continue
		}
		jobID, err := uuid.Parse(jobIDStr)
		if err != nil {
			continue
		}
		var job db.Job
		if err := s.db.WithContext(ctx).First(&job, "id = ?", jobID).Error; err != nil {
			continue
		}

		status := db.JobStatusSuccess
		if isFailed {
			status = db.JobStatusFailed
		}

		log := appk8s.FetchContainerLog(ctx, s.k8s, kj.Namespace, kj.Name, "job")

		var startedAt, finishedAt *time.Time
		if kj.Status.StartTime != nil {
			t := kj.Status.StartTime.Time
			startedAt = &t
		}
		if kj.Status.CompletionTime != nil {
			t := kj.Status.CompletionTime.Time
			finishedAt = &t
		}

		run := db.JobRun{
			JobID:      job.ID,
			Status:     status,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			Log:        log,
			K8sJobName: kj.Name,
		}
		s.db.WithContext(ctx).Create(&run)

		updates := map[string]any{"status": status}
		if finishedAt != nil {
			updates["last_run_at"] = finishedAt.Format(time.RFC3339)
		}
		s.db.WithContext(ctx).Model(&job).Updates(updates)
	}
}

// Trigger creates a JobRun and launches the K8s Job in a background goroutine.
// If no k8s client is configured, the run stays pending (dev mode).
func (s *JobService) Trigger(ctx context.Context, jobID uuid.UUID) (*db.JobRun, error) {
	job, err := s.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}

	// Build a unique K8s job name from the run ID (filled in after Create).
	run := db.JobRun{
		JobID:  job.ID,
		Status: db.JobStatusPending,
	}
	if err := s.db.WithContext(ctx).Create(&run).Error; err != nil {
		return nil, err
	}

	// k8sJobName: "r-" + first 16 hex chars of the run UUID (no dashes, always unique)
	k8sJobName := "r-" + strings.ReplaceAll(run.ID.String(), "-", "")[:16]
	s.db.WithContext(ctx).Model(&run).Update("k8s_job_name", k8sJobName)
	run.K8sJobName = k8sJobName

	s.db.WithContext(ctx).Model(job).Update("status", db.JobStatusPending)

	if s.k8s == nil {
		// No k8s — leave pending (dev mode without a cluster).
		return &run, nil
	}

	// Load project namespace and optional node name before spawning goroutine.
	var project db.Project
	if err := s.db.WithContext(ctx).First(&project, "id = ?", job.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	namespace := project.Slug

	nodeName := ""
	if job.NodeID != nil {
		var node db.Node
		if s.db.WithContext(ctx).First(&node, "id = ?", job.NodeID).Error == nil {
			nodeName = node.Name
		}
	}

	envVars := appk8s.ParseEnvBlock(string(job.EnvVars))

	params := appk8s.RunJobParams{
		JobName:    k8sJobName,
		Namespace:  namespace,
		Image:      job.Image,
		Command:    job.Command,
		EnvVars:    envVars,
		CPURequest: job.CPURequest,
		CPULimit:   job.CPULimit,
		MemRequest: job.MemoryRequest,
		MemLimit:   job.MemoryLimit,
		NodeName:   nodeName,
	}

	go s.executeRun(job, &run, namespace, params)

	return &run, nil
}

func (s *JobService) executeRun(job *db.Job, run *db.JobRun, namespace string, params appk8s.RunJobParams) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	now := time.Now()
	s.db.Model(run).Updates(map[string]any{
		"status":     db.JobStatusRunning,
		"started_at": &now,
	})
	s.db.Model(job).Update("status", db.JobStatusRunning)

	if err := appk8s.EnsureNamespace(bgCtx, s.k8s, namespace); err != nil {
		s.failRun(job, run, "failed to ensure namespace: "+err.Error())
		return
	}

	if err := appk8s.CreateRunJob(bgCtx, s.k8s, params); err != nil {
		s.failRun(job, run, "failed to create k8s job: "+err.Error())
		return
	}

	result := appk8s.WaitForRunJob(bgCtx, s.k8s, namespace, params.JobName, 30*time.Minute)

	status := db.JobStatusSuccess
	if !result.Success {
		status = db.JobStatusFailed
	}
	finished := time.Now()
	s.db.Model(run).Updates(map[string]any{
		"status":      status,
		"log":         result.Log,
		"finished_at": &finished,
	})
	s.db.Model(job).Updates(map[string]any{
		"status":      status,
		"last_run_at": finished.Format(time.RFC3339),
	})
}

func (s *JobService) failRun(job *db.Job, run *db.JobRun, msg string) {
	finished := time.Now()
	s.db.Model(run).Updates(map[string]any{
		"status":      db.JobStatusFailed,
		"log":         msg,
		"finished_at": &finished,
	})
	s.db.Model(job).Update("status", db.JobStatusFailed)
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
