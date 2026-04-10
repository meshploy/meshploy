package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HeadscaleNode mirrors the fields returned by GET /api/v1/node.
type HeadscaleNode struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	GivenName   string     `json:"givenName"`
	IPAddresses []string   `json:"ipAddresses"`
	Online      bool       `json:"online"`
	LastSeen    *time.Time `json:"lastSeen"`
	Expiry      *time.Time `json:"expiry"`
	ForcedTags  []string   `json:"forcedTags"`
	ValidTags   []string   `json:"validTags"`
	InvalidTags []string   `json:"invalidTags"`
	User        struct {
		Name string `json:"name"`
	} `json:"user"`
	RegisterMethod string    `json:"registerMethod"`
	CreatedAt      time.Time `json:"createdAt"`
}

// Tags returns the combined set of tags on this node (forced + valid).
func (n HeadscaleNode) Tags() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, t := range n.ForcedTags {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	for _, t := range n.ValidTags {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}

// HeadscaleService wraps the Headscale REST API.
type HeadscaleService struct {
	url    string
	key    string
	client *http.Client
}

func NewHeadscaleService(url, key string) *HeadscaleService {
	return &HeadscaleService{
		url: url,
		key: key,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// PreAuthKey mirrors the relevant fields from the Headscale preauth key response.
type PreAuthKey struct {
	Key        string    `json:"key"`
	Reusable   bool      `json:"reusable"`
	Expiration time.Time `json:"expiration"`
	Used       bool      `json:"used"`
}

// resolveUserID returns the numeric string ID for the given Headscale username.
// Headscale v0.28+ requires the numeric user ID (not the name) in API requests.
func (h *HeadscaleService) resolveUserID(ctx context.Context, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url+"/api/v1/user", nil)
	if err != nil {
		return "", fmt.Errorf("headscale list users: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.key)

	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("headscale list users: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("headscale list users: unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Users []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"users"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("headscale list users: decode: %w", err)
	}

	for _, u := range body.Users {
		if u.Name == name {
			return u.ID, nil
		}
	}
	return "", fmt.Errorf("headscale user %q not found", name)
}

// CreatePreAuthKey calls POST {url}/api/v1/preauthkey and returns a new reusable preauth key
// scoped to the given Headscale user. The key expires in 1 year.
// Headscale v0.28+ requires the numeric user ID in the request, so we resolve it first.
func (h *HeadscaleService) CreatePreAuthKey(ctx context.Context, user string) (*PreAuthKey, error) {
	userID, err := h.resolveUserID(ctx, user)
	if err != nil {
		return nil, err
	}

	expiry := time.Now().Add(365 * 24 * time.Hour)
	payload, _ := json.Marshal(map[string]any{
		"user":       userID,
		"reusable":   true,
		"ephemeral":  false,
		"expiration": expiry.Format(time.RFC3339),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url+"/api/v1/preauthkey", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("headscale create preauth key: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("headscale create preauth key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("headscale create preauth key: unexpected status %d", resp.StatusCode)
	}

	var out struct {
		PreAuthKey PreAuthKey `json:"preAuthKey"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("headscale create preauth key: decode: %w", err)
	}
	return &out.PreAuthKey, nil
}

// ListPreAuthKeys returns all preauth keys for the given Headscale user.
// Headscale v0.28+ requires the numeric user ID as the `user` query param.
func (h *HeadscaleService) ListPreAuthKeys(ctx context.Context, user string) ([]PreAuthKey, error) {
	userID, err := h.resolveUserID(ctx, user)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url+"/api/v1/preauthkey?user="+userID, nil)
	if err != nil {
		return nil, fmt.Errorf("headscale list preauth keys: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.key)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("headscale list preauth keys: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("headscale list preauth keys: unexpected status %d", resp.StatusCode)
	}

	var body struct {
		PreAuthKeys []PreAuthKey `json:"preAuthKeys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("headscale list preauth keys: decode: %w", err)
	}
	return body.PreAuthKeys, nil
}

// ListNodes calls GET {url}/api/v1/node and returns all nodes.
func (h *HeadscaleService) ListNodes(ctx context.Context) ([]HeadscaleNode, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url+"/api/v1/node", nil)
	if err != nil {
		return nil, fmt.Errorf("headscale list nodes: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.key)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("headscale list nodes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("headscale list nodes: unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Nodes []HeadscaleNode `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("headscale list nodes: decode: %w", err)
	}
	return body.Nodes, nil
}
