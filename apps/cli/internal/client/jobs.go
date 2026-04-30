package client

type Job struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	IsCron   bool   `json:"is_cron"`
	Image    string `json:"image"`
	Schedule string `json:"schedule"`
	Status   string `json:"status"`
}

type jobsListBody struct {
	Jobs []Job `json:"jobs"`
}

type JobRun struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	StartedAt  *string `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
}

func (c *Client) ListJobs(orgID, projectID string) ([]Job, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/jobs", nil)
	if err != nil {
		return nil, err
	}
	out, err := decode[jobsListBody](resp)
	return out.Jobs, err
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

// GetJobByName resolves a job by ID or name within a project.
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
