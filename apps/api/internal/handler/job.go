package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/service"
	db "github.com/meshploy/packages/db"
)

// ─── DTOs ─────────────────────────────────────────────────────────────────────

type JobDTO struct {
	ID                uuid.UUID            `json:"id"`
	ProjectID         uuid.UUID            `json:"project_id"`
	NodeID            *uuid.UUID           `json:"node_id"`
	Name              string               `json:"name"`
	IsCron            bool                 `json:"is_cron"`
	Image             string               `json:"image"`
	Command           string               `json:"command"`
	Schedule          string               `json:"schedule"`
	ConcurrencyPolicy db.ConcurrencyPolicy `json:"concurrency_policy"`
	HistoryLimit      int                  `json:"history_limit"`
	CPURequest        string               `json:"cpu_request"`
	CPULimit          string               `json:"cpu_limit"`
	MemoryRequest     string               `json:"memory_request"`
	MemoryLimit       string               `json:"memory_limit"`
	EnvVars           string               `json:"env_vars"`
	Status            db.JobStatus         `json:"status"`
	LastRunAt         *string              `json:"last_run_at"`
	K8sName           string               `json:"k8s_name"`
	CreatedAt         string               `json:"created_at"`
	UpdatedAt         string               `json:"updated_at"`
}

type JobRunDTO struct {
	ID         uuid.UUID    `json:"id"`
	JobID      uuid.UUID    `json:"job_id"`
	Status     db.JobStatus `json:"status"`
	StartedAt  *string      `json:"started_at"`
	FinishedAt *string      `json:"finished_at"`
	Log        string       `json:"log"`
	K8sJobName string       `json:"k8s_job_name"`
	CreatedAt  string       `json:"created_at"`
}

func toJobDTO(j db.Job) JobDTO {
	d := JobDTO{
		ID:                j.ID,
		ProjectID:         j.ProjectID,
		NodeID:            j.NodeID,
		Name:              j.Name,
		IsCron:            j.IsCron,
		Image:             j.Image,
		Command:           j.Command,
		Schedule:          j.Schedule,
		ConcurrencyPolicy: j.ConcurrencyPolicy,
		HistoryLimit:      j.HistoryLimit,
		CPURequest:        j.CPURequest,
		CPULimit:          j.CPULimit,
		MemoryRequest:     j.MemoryRequest,
		MemoryLimit:       j.MemoryLimit,
		EnvVars:           string(j.EnvVars),
		Status:            j.Status,
		K8sName:           j.K8sName,
		CreatedAt:         j.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:         j.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if j.LastRunAt != nil {
		s := j.LastRunAt.Format("2006-01-02T15:04:05Z")
		d.LastRunAt = &s
	}
	return d
}

func toJobRunDTO(r db.JobRun) JobRunDTO {
	d := JobRunDTO{
		ID:         r.ID,
		JobID:      r.JobID,
		Status:     r.Status,
		Log:        r.Log,
		K8sJobName: r.K8sJobName,
		CreatedAt:  r.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if r.StartedAt != nil {
		s := r.StartedAt.Format("2006-01-02T15:04:05Z")
		d.StartedAt = &s
	}
	if r.FinishedAt != nil {
		s := r.FinishedAt.Format("2006-01-02T15:04:05Z")
		d.FinishedAt = &s
	}
	return d
}

// ─── Input types ─────────────────────────────────────────────────────────────

type JobProjectInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
}

type JobPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	JobID     string `path:"jobId"`
}

type ListJobsOutput struct {
	Body []JobDTO
}

type GetJobOutput struct {
	Body JobDTO
}

type CreateJobInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		Name              string               `json:"name"               minLength:"1" maxLength:"100"`
		IsCron            bool                 `json:"is_cron"`
		Image             string               `json:"image"              minLength:"1"`
		Command           string               `json:"command,omitempty"`
		Schedule          string               `json:"schedule,omitempty"`
		ConcurrencyPolicy db.ConcurrencyPolicy `json:"concurrency_policy,omitempty"`
		HistoryLimit      int                  `json:"history_limit,omitempty"`
		CPURequest        string               `json:"cpu_request,omitempty"`
		CPULimit          string               `json:"cpu_limit,omitempty"`
		MemoryRequest     string               `json:"memory_request,omitempty"`
		MemoryLimit       string               `json:"memory_limit,omitempty"`
		EnvVars           string               `json:"env_vars,omitempty"`
		NodeID            *string              `json:"node_id,omitempty"`
	}
}

type CreateJobOutput struct {
	Body JobDTO
}

type UpdateJobInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	JobID     string `path:"jobId"`
	Body      struct {
		IsCron            *bool                 `json:"is_cron,omitempty"`
		Name              *string               `json:"name,omitempty"`
		Image             *string               `json:"image,omitempty"`
		Command           *string               `json:"command,omitempty"`
		Schedule          *string               `json:"schedule,omitempty"`
		ConcurrencyPolicy *db.ConcurrencyPolicy `json:"concurrency_policy,omitempty"`
		HistoryLimit      *int                  `json:"history_limit,omitempty"`
		CPURequest        *string               `json:"cpu_request,omitempty"`
		CPULimit          *string               `json:"cpu_limit,omitempty"`
		MemoryRequest     *string               `json:"memory_request,omitempty"`
		MemoryLimit       *string               `json:"memory_limit,omitempty"`
		EnvVars           *string               `json:"env_vars,omitempty"`
		NodeID            *string               `json:"node_id,omitempty"`
	}
}

type UpdateJobOutput struct {
	Body JobDTO
}

type ListJobRunsOutput struct {
	Body []JobRunDTO
}

type TriggerJobOutput struct {
	Body JobRunDTO
}

// ─── Registration ─────────────────────────────────────────────────────────────

func (h *Handler) registerJobRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-jobs",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/jobs",
		Summary:     "List jobs in a project",
		Tags:        []string{"Jobs"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListJobs)

	huma.Register(api, huma.Operation{
		OperationID:   "create-job",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/jobs",
		Summary:       "Create a job or cron job",
		Tags:          []string{"Jobs"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 201,
	}, h.CreateJob)

	huma.Register(api, huma.Operation{
		OperationID: "get-job",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/jobs/{jobId}",
		Summary:     "Get a job",
		Tags:        []string{"Jobs"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetJob)

	huma.Register(api, huma.Operation{
		OperationID: "update-job",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/jobs/{jobId}",
		Summary:     "Update a job",
		Tags:        []string{"Jobs"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateJob)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-job",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/jobs/{jobId}",
		Summary:       "Delete a job",
		Tags:          []string{"Jobs"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.DeleteJob)

	huma.Register(api, huma.Operation{
		OperationID: "list-job-runs",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/jobs/{jobId}/runs",
		Summary:     "List run history for a job",
		Tags:        []string{"Jobs"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListJobRuns)

	huma.Register(api, huma.Operation{
		OperationID:   "trigger-job",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/jobs/{jobId}/trigger",
		Summary:       "Manually trigger a job run",
		Tags:          []string{"Jobs"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 201,
	}, h.TriggerJob)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-job-run",
		Method:        "DELETE",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/jobs/{jobId}/runs/{runId}",
		Summary:       "Delete a job run record",
		Tags:          []string{"Jobs"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 204,
	}, h.DeleteJobRun)
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

func (h *Handler) ListJobs(ctx context.Context, input *JobProjectInput) (*ListJobsOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Permissions.CheckAccess(ctx, orgID, userID, projectID, db.ResourceProject, db.ActionView, nil); err != nil {
		return nil, huma.Error403Forbidden(err.Error())
	}
	rows, err := h.svc.Jobs.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	dtos := make([]JobDTO, len(rows))
	for i, r := range rows {
		dtos[i] = toJobDTO(r)
	}
	return &ListJobsOutput{Body: dtos}, nil
}

func (h *Handler) GetJob(ctx context.Context, input *JobPathInput) (*GetJobOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	jobID, err := parseUUID(input.JobID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Permissions.CheckAccess(ctx, orgID, userID, jobID, db.ResourceJob, db.ActionView, &projectID); err != nil {
		return nil, huma.Error403Forbidden(err.Error())
	}
	row, err := h.svc.Jobs.Get(ctx, jobID)
	if err != nil {
		return nil, notFound(err)
	}
	return &GetJobOutput{Body: toJobDTO(*row)}, nil
}

func (h *Handler) CreateJob(ctx context.Context, input *CreateJobInput) (*CreateJobOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Permissions.CheckAccess(ctx, orgID, userID, projectID, db.ResourceProject, db.ActionCreate, nil); err != nil {
		return nil, huma.Error403Forbidden(err.Error())
	}
	var nodeID *uuid.UUID
	if input.Body.NodeID != nil && *input.Body.NodeID != "" {
		id, err := parseUUID(*input.Body.NodeID)
		if err != nil {
			return nil, err
		}
		nodeID = &id
	}
	row, err := h.svc.Jobs.Create(ctx, service.CreateJobInput{
		ProjectID:         projectID,
		Name:              input.Body.Name,
		IsCron:            input.Body.IsCron,
		Image:             input.Body.Image,
		Command:           input.Body.Command,
		Schedule:          input.Body.Schedule,
		ConcurrencyPolicy: input.Body.ConcurrencyPolicy,
		HistoryLimit:      input.Body.HistoryLimit,
		CPURequest:        input.Body.CPURequest,
		CPULimit:          input.Body.CPULimit,
		MemoryRequest:     input.Body.MemoryRequest,
		MemoryLimit:       input.Body.MemoryLimit,
		EnvVars:           input.Body.EnvVars,
		NodeID:            nodeID,
	})
	if err != nil {
		return nil, huma.Error409Conflict("a job with that name already exists in this project")
	}
	return &CreateJobOutput{Body: toJobDTO(*row)}, nil
}

func (h *Handler) UpdateJob(ctx context.Context, input *UpdateJobInput) (*UpdateJobOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	jobID, err := parseUUID(input.JobID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Permissions.CheckAccess(ctx, orgID, userID, jobID, db.ResourceJob, db.ActionUpdate, &projectID); err != nil {
		return nil, huma.Error403Forbidden(err.Error())
	}
	in := service.UpdateJobInput{
		IsCron:            input.Body.IsCron,
		Name:              input.Body.Name,
		Image:             input.Body.Image,
		Command:           input.Body.Command,
		Schedule:          input.Body.Schedule,
		ConcurrencyPolicy: input.Body.ConcurrencyPolicy,
		HistoryLimit:      input.Body.HistoryLimit,
		CPURequest:        input.Body.CPURequest,
		CPULimit:          input.Body.CPULimit,
		MemoryRequest:     input.Body.MemoryRequest,
		MemoryLimit:       input.Body.MemoryLimit,
		EnvVars:           input.Body.EnvVars,
	}
	if input.Body.NodeID != nil && *input.Body.NodeID != "" {
		id, err := parseUUID(*input.Body.NodeID)
		if err != nil {
			return nil, err
		}
		in.NodeID = &id
	}
	row, err := h.svc.Jobs.Update(ctx, jobID, in)
	if err != nil {
		return nil, notFound(err)
	}
	return &UpdateJobOutput{Body: toJobDTO(*row)}, nil
}

func (h *Handler) DeleteJob(ctx context.Context, input *JobPathInput) (*struct{}, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	jobID, err := parseUUID(input.JobID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Permissions.CheckAccess(ctx, orgID, userID, jobID, db.ResourceJob, db.ActionDelete, &projectID); err != nil {
		return nil, huma.Error403Forbidden(err.Error())
	}
	return nil, h.svc.Jobs.Delete(ctx, jobID)
}

func (h *Handler) ListJobRuns(ctx context.Context, input *JobPathInput) (*ListJobRunsOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	jobID, err := parseUUID(input.JobID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Permissions.CheckAccess(ctx, orgID, userID, jobID, db.ResourceJob, db.ActionView, &projectID); err != nil {
		return nil, huma.Error403Forbidden(err.Error())
	}
	rows, err := h.svc.Jobs.ListRuns(ctx, jobID)
	if err != nil {
		return nil, err
	}
	dtos := make([]JobRunDTO, len(rows))
	for i, r := range rows {
		dtos[i] = toJobRunDTO(r)
	}
	return &ListJobRunsOutput{Body: dtos}, nil
}

func (h *Handler) DeleteJobRun(ctx context.Context, input *struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	JobID     string `path:"jobId"`
	RunID     string `path:"runId"`
}) (*struct{}, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	jobID, err := parseUUID(input.JobID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Permissions.CheckAccess(ctx, orgID, userID, jobID, db.ResourceJob, db.ActionDelete, &projectID); err != nil {
		return nil, huma.Error403Forbidden(err.Error())
	}
	runID, err := parseUUID(input.RunID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.Jobs.DeleteRun(ctx, runID)
}

func (h *Handler) TriggerJob(ctx context.Context, input *JobPathInput) (*TriggerJobOutput, error) {
	userID, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseUUID(input.OrgID)
	if err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	jobID, err := parseUUID(input.JobID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Permissions.CheckAccess(ctx, orgID, userID, jobID, db.ResourceJob, db.ActionDeploy, &projectID); err != nil {
		return nil, huma.Error403Forbidden(err.Error())
	}
	run, err := h.svc.Jobs.Trigger(ctx, jobID)
	if err != nil {
		return nil, notFound(err)
	}
	return &TriggerJobOutput{Body: toJobRunDTO(*run)}, nil
}
