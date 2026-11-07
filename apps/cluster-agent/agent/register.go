package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// Registration is what this node tells the API when it registers.
type Registration struct {
	Token  string // the single-use provisioning token minted for this agent
	Name   string
	MeshIP string
	Port   int
}

// Registered is the API's answer.
type Registered struct {
	NodeID     string `json:"id"`
	NodeSecret string `json:"node_secret"`
}

// NodeSecretPath is where the node secret the API hands back is kept.
func (id Identity) NodeSecretPath() string { return filepath.Join(id.Dir, "node-secret") }

// Register sends the agent's certificate and token to the API over the mesh,
// spending the provisioning token, and keeps the node secret it returns.
func Register(ctx context.Context, apiURL string, id Identity, r Registration) (*Registered, error) {
	cert, err := id.CertPEM()
	if err != nil {
		return nil, err
	}
	token, err := id.Token()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{
		"token":        r.Token,
		"name":         r.Name,
		"tailscale_ip": r.MeshIP,
		"cluster_agent": map[string]any{
			"certificate": string(cert),
			"token":       token,
			"port":        r.Port,
		},
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(apiURL, "/")+"/api/v1/nodes/self-register", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registration refused (%d): %s", resp.StatusCode, apiDetail(raw))
	}
	var out Registered
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("registration answer: %w", err)
	}
	if out.NodeSecret != "" {
		if err := writeFile(id.NodeSecretPath(), []byte(out.NodeSecret+"\n")); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

// apiDetail is the human part of an API error body.
func apiDetail(raw []byte) string {
	var e struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Detail != "" {
		return e.Detail
	}
	return strings.TrimSpace(string(raw))
}
