package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) do(method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.httpClient.Do(req)
}

func decode[T any](resp *http.Response) (T, error) {
	defer resp.Body.Close()
	var out T
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		var zero T
		return zero, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		var zero T
		return zero, fmt.Errorf("decode response: %w", err)
	}
	return out, nil
}

func decodePtr[T any](resp *http.Response) (*T, error) {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

func (c *Client) doNoContent(method, path string) error {
	resp, err := c.do(method, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func ErrNotFound(resource, ref string) error {
	return fmt.Errorf("%s %q not found", resource, ref)
}

// ── Auth ──────────────────────────────────────────────────────────────────────

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginOutput struct {
	Token string `json:"token"`
}

func (c *Client) Login(email, password string) (string, error) {
	resp, err := c.do("POST", "/api/v1/auth/login", LoginInput{Email: email, Password: password})
	if err != nil {
		return "", err
	}
	out, err := decode[LoginOutput](resp)
	return out.Token, err
}

// ── Nodes ─────────────────────────────────────────────────────────────────────

type Node struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TailscaleIP string `json:"tailscale_ip"`
	Status      string `json:"status"`
	K3sRole     string `json:"k3s_role"`
	MeshRole    string `json:"mesh_role"`
	HeadscaleID string `json:"headscale_id"`
	CreatedAt   string `json:"created_at"`
}

type nodesListBody struct {
	Nodes []Node `json:"nodes"`
}

func (c *Client) ListNodes(orgID string) ([]Node, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/nodes", nil)
	if err != nil {
		return nil, err
	}
	out, err := decode[nodesListBody](resp)
	return out.Nodes, err
}

func (c *Client) DeleteNode(orgID, nodeID string) error {
	resp, err := c.do("DELETE", "/api/v1/orgs/"+orgID+"/nodes/"+nodeID, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// ── Org ───────────────────────────────────────────────────────────────────────

type Org struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type orgsListBody struct {
	Orgs []Org `json:"organizations"`
}

func (c *Client) ListOrgs() ([]Org, error) {
	resp, err := c.do("GET", "/api/v1/orgs", nil)
	if err != nil {
		return nil, err
	}
	out, err := decode[orgsListBody](resp)
	return out.Orgs, err
}

// ── Registration token ────────────────────────────────────────────────────────

type tokenBody struct {
	Token string `json:"token"`
}

func (c *Client) GetRegistrationToken(orgID string) (string, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/nodes/registration-token", nil)
	if err != nil {
		return "", err
	}
	out, err := decode[tokenBody](resp)
	return out.Token, err
}

func (c *Client) RotateRegistrationToken(orgID string) (string, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/nodes/registration-token", nil)
	if err != nil {
		return "", err
	}
	out, err := decode[tokenBody](resp)
	return out.Token, err
}
