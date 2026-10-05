package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// UploadedSource is what the API stored of an uploaded folder.
type UploadedSource struct {
	Digest     string `json:"digest"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Files      int    `json:"files"`
	UploadedAt string `json:"uploaded_at"`
}

// UploadResult is the stored folder, and the deployment it started when one
// was asked for.
type UploadResult struct {
	Source     UploadedSource `json:"source"`
	Deployment *Deployment    `json:"deployment,omitempty"`
}

// UploadSource sends a packed folder as the service's source, and deploys it
// when deploy is set.
func (c *Client) UploadSource(orgID, projectID, serviceID string, folder *PackedFolder, deploy bool) (*UploadResult, error) {
	q := url.Values{"name": {folder.Name}}
	if deploy {
		q.Set("deploy", "true")
	}
	req, err := http.NewRequest("POST", c.baseURL+"/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/source?"+q.Encode(),
		bytes.NewReader(folder.Archive))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/gzip")
	c.authorize(req)
	// Up to 100 MB on a home connection: the usual 30 seconds is not enough.
	resp, err := (&http.Client{Timeout: 15 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		var problem struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(b, &problem) == nil && problem.Detail != "" {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, problem.Detail)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	var out UploadResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}
