package client

type Job struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	IsCron            bool    `json:"is_cron"`
	Image             string  `json:"image"`
	Command           string  `json:"command"`
	Schedule          string  `json:"schedule"`
	ConcurrencyPolicy string  `json:"concurrency_policy"`
	HistoryLimit      int     `json:"history_limit"`
	Status            string  `json:"status"`
	LastRunAt         *string `json:"last_run_at"`
}

type JobRun struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	Log        string  `json:"log"`
	StartedAt  *string `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
}

type CreateJobBody struct {
	Name              string  `json:"name"`
	IsCron            bool    `json:"is_cron"`
	Image             string  `json:"image"`
	Command           string  `json:"command,omitempty"`
	Schedule          string  `json:"schedule,omitempty"`
	ConcurrencyPolicy string  `json:"concurrency_policy,omitempty"`
	HistoryLimit      int     `json:"history_limit,omitempty"`
	CPURequest        string  `json:"cpu_request,omitempty"`
	CPULimit          string  `json:"cpu_limit,omitempty"`
	MemoryRequest     string  `json:"memory_request,omitempty"`
	MemoryLimit       string  `json:"memory_limit,omitempty"`
	EnvVars           string  `json:"env_vars,omitempty"`
	NodeID            *string `json:"node_id,omitempty"`
}

type UpdateJobBody struct {
	Image             *string `json:"image,omitempty"`
	Command           *string `json:"command,omitempty"`
	Schedule          *string `json:"schedule,omitempty"`
	ConcurrencyPolicy *string `json:"concurrency_policy,omitempty"`
	HistoryLimit      *int    `json:"history_limit,omitempty"`
}

func (c *Client) ListJobs(orgID, projectID string) ([]Job, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/jobs", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Job](resp)
}

func (c *Client) CreateJob(orgID, projectID string, body CreateJobBody) (*Job, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/jobs", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[Job](resp)
}

func (c *Client) GetJob(orgID, projectID, jobID string) (*Job, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/jobs/"+jobID, nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[Job](resp)
}

func (c *Client) UpdateJob(orgID, projectID, jobID string, body UpdateJobBody) (*Job, error) {
	resp, err := c.do("PATCH", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/jobs/"+jobID, body)
	if err != nil {
		return nil, err
	}
	return decodePtr[Job](resp)
}

func (c *Client) TriggerJob(orgID, projectID, jobID string) (*JobRun, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/jobs/"+jobID+"/trigger", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[JobRun](resp)
}

func (c *Client) DeleteJob(orgID, projectID, jobID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/jobs/"+jobID)
}

func (c *Client) ListJobRuns(orgID, projectID, jobID string) ([]JobRun, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/jobs/"+jobID+"/runs", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]JobRun](resp)
}

func (c *Client) DeleteJobRun(orgID, projectID, jobID, runID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/jobs/"+jobID+"/runs/"+runID)
}

func (c *Client) GetJobByName(orgID, projectID, ref string) (*Job, error) {
	jobs, err := c.ListJobs(orgID, projectID)
	if err != nil {
		return nil, err
	}
	for i, j := range jobs {
		if j.ID == ref || j.Name == ref {
			return &jobs[i], nil
		}
	}
	return nil, ErrNotFound("job", ref)
}
