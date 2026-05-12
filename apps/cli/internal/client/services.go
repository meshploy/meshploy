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
	ID        string `json:"id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
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
		if strings.HasPrefix(line, "data: ") {
			fmt.Fprintln(w, strings.TrimPrefix(line, "data: "))
		}
	}
	return scanner.Err()
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
