package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrSharingUnavailable is an API with no sharing route.
var ErrSharingUnavailable = errors.New("sharing an app with people needs Meshploy Enterprise or Meshploy Cloud")

// ShareBody is who an app is shared with, and as what.
type ShareBody struct {
	Emails []string `json:"emails"`
	// Role is viewer (default) or editor.
	Role string `json:"role,omitempty"`
}

// ShareService shares a service's app with people by email: each gets a
// link to sign in with. The answer is the edition's own, passed through.
func (c *Client) ShareService(orgID, projectID, serviceID string, body ShareBody) (map[string]any, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", c.baseURL+"/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/shares", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if routeMissing(resp) {
		return nil, ErrSharingUnavailable
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	out := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
	}
	return out, nil
}

// routeMissing is the router's answer for a path it has no route for, as
// opposed to a route answering that something is not found: the API's own
// errors are problem JSON, the router's are plain text.
func routeMissing(resp *http.Response) bool {
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		return false
	}
	return !strings.Contains(resp.Header.Get("Content-Type"), "json")
}
