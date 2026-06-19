package client

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Service struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Image     string `json:"image"`
	CreatedAt string `json:"created_at"`
}

type Deployment struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	Image      string  `json:"image"`
	DeployedAt *string `json:"deployed_at"`
	CreatedAt  string  `json:"created_at"`
}

func (c *Client) ListServices(orgID, projectID string) ([]Service, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Service](resp)
}

func (c *Client) Deploy(orgID, projectID, serviceID string) (*Deployment, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/deployments", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[Deployment](resp)
}

func (c *Client) StartService(orgID, projectID, serviceID string) error {
	return c.doNoContent("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/start")
}

func (c *Client) StopService(orgID, projectID, serviceID string) error {
	return c.doNoContent("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/stop")
}

func (c *Client) DeleteService(orgID, projectID, serviceID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID)
}

// StreamLogs streams live container logs via SSE, writing each line to w.
// tail=0 uses the server default (200). since="" means no time filter.
// follow=false fetches a snapshot then exits.
func (c *Client) StreamLogs(orgID, projectID, serviceID string, tail int, since string, follow bool, w io.Writer) error {
	params := url.Values{}
	if tail > 0 {
		params.Set("tail", strconv.Itoa(tail))
	}
	if since != "" {
		params.Set("since", since)
	}
	if !follow {
		params.Set("follow", "false")
	}
	endpoint := c.baseURL + "/api/v1/orgs/" + orgID + "/projects/" + projectID + "/services/" + serviceID + "/logs/stream"
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			fmt.Fprintln(w, data)
		}
	}
	return scanner.Err()
}

func (c *Client) ListDeployments(orgID, projectID, serviceID string) ([]Deployment, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/deployments", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Deployment](resp)
}

func (c *Client) CancelDeployment(orgID, projectID, serviceID, deploymentID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/deployments/"+deploymentID)
}

func (c *Client) RollbackDeployment(orgID, projectID, serviceID, deploymentID string) (*Deployment, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/deployments/"+deploymentID+"/rollback", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[Deployment](resp)
}

func (c *Client) RetryDeployment(orgID, projectID, serviceID, deploymentID string) (*Deployment, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/deployments/"+deploymentID+"/retry", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[Deployment](resp)
}

type BuildConfig struct {
	ID                    string  `json:"id"`
	ServiceID             string  `json:"service_id"`
	Builder               string  `json:"builder"`
	GitRepo               string  `json:"git_repo"`
	Branch                string  `json:"branch"`
	DockerfilePath        string  `json:"dockerfile_path"`
	AutoDeploy            bool    `json:"auto_deploy"`
	GitIntegrationID      *string `json:"git_integration_id"`
	RegistryIntegrationID *string `json:"registry_integration_id"`
	LastBuiltImage        string  `json:"last_built_image"`
	LastBuiltAt           *string `json:"last_built_at"`
}

type UpdateBuildConfigBody struct {
	GitRepo               *string `json:"git_repo,omitempty"`
	Branch                *string `json:"branch,omitempty"`
	Builder               *string `json:"builder,omitempty"`
	DockerfilePath        *string `json:"dockerfile_path,omitempty"`
	AutoDeploy            *bool   `json:"auto_deploy,omitempty"`
	GitIntegrationID      *string `json:"git_integration_id,omitempty"`
	RegistryIntegrationID *string `json:"registry_integration_id,omitempty"`
}

func (c *Client) GetEnvVars(orgID, projectID, serviceID string) (string, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/env-vars", nil)
	if err != nil {
		return "", err
	}
	type body struct {
		EnvVars string `json:"env_vars"`
	}
	b, err := decode[body](resp)
	return b.EnvVars, err
}

func (c *Client) SetEnvVars(orgID, projectID, serviceID, envVars string) error {
	type body struct {
		EnvVars *string `json:"env_vars,omitempty"`
	}
	resp, err := c.do("PATCH", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID, body{EnvVars: &envVars})
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) GetBuildConfig(orgID, projectID, serviceID string) (*BuildConfig, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/build-config", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[BuildConfig](resp)
}

func (c *Client) UpdateBuildConfig(orgID, projectID, serviceID string, body UpdateBuildConfigBody) (*BuildConfig, error) {
	resp, err := c.do("PATCH", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/build-config", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[BuildConfig](resp)
}

func (c *Client) GetBuildEnvVars(orgID, projectID, serviceID string) (string, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/build-config/env-vars", nil)
	if err != nil {
		return "", err
	}
	type body struct {
		BuildEnvVars string `json:"build_env_vars"`
	}
	b, err := decode[body](resp)
	return b.BuildEnvVars, err
}

func (c *Client) SetBuildEnvVars(orgID, projectID, serviceID, envVars string) error {
	type body struct {
		BuildEnvVars string `json:"build_env_vars"`
	}
	resp, err := c.do("PUT", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/build-config/env-vars", body{BuildEnvVars: envVars})
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// GetServiceByName resolves a service by ID or name within a project.
func (c *Client) GetServiceByName(orgID, projectID, ref string) (*Service, error) {
	services, err := c.ListServices(orgID, projectID)
	if err != nil {
		return nil, err
	}
	for i, s := range services {
		if s.ID == ref || s.Name == ref {
			return &services[i], nil
		}
	}
	return nil, ErrNotFound("service", ref)
}
