package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
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

	// Health of the last observed call. A dead Headscale credential degrades
	// the mesh silently -- node liveness freezes at its last known value while
	// the UI keeps presenting it as current -- so the outcome is recorded and
	// surfaced rather than only logged.
	mu           sync.RWMutex
	lastErr      string
	lastErrAt    time.Time
	lastOKAt     time.Time
	unauthorized bool
}

// HeadscaleHealth is a point-in-time view of the API's ability to talk to
// Headscale. Unauthorized is tracked separately from a generic failure because
// an expired or wrong API key never recovers on its own -- it needs an operator
// to mint a new one -- whereas a timeout usually does.
type HeadscaleHealth struct {
	Checked       bool       `json:"checked"`
	Healthy       bool       `json:"healthy"`
	Unauthorized  bool       `json:"unauthorized"`
	LastError     string     `json:"last_error,omitempty"`
	LastErrorAt   *time.Time `json:"last_error_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
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

// observe records the outcome of a Headscale call. status is the HTTP status
// when there was a response, or 0 when the request never completed.
func (h *HeadscaleService) observe(err error, status int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err == nil {
		h.lastOKAt = time.Now()
		h.lastErr = ""
		h.unauthorized = false
		return
	}
	h.lastErr = err.Error()
	h.lastErrAt = time.Now()
	h.unauthorized = status == http.StatusUnauthorized || status == http.StatusForbidden
}

// Health reports the last observed state of the Headscale connection. A service
// that has not been called yet reports Checked false, so callers can distinguish
// "no evidence" from "known good".
func (h *HeadscaleService) Health() HeadscaleHealth {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := HeadscaleHealth{
		Checked:      !h.lastOKAt.IsZero() || h.lastErr != "",
		Healthy:      h.lastErr == "",
		Unauthorized: h.unauthorized,
		LastError:    h.lastErr,
	}
	if !h.lastErrAt.IsZero() {
		t := h.lastErrAt
		out.LastErrorAt = &t
	}
	if !h.lastOKAt.IsZero() {
		t := h.lastOKAt
		out.LastSuccessAt = &t
	}
	return out
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

// GetNode calls GET {url}/api/v1/node/{id} and returns a single node by its
// Headscale numeric ID. Prefer this over ListNodes when the ID is known.
func (h *HeadscaleService) GetNode(ctx context.Context, id string) (*HeadscaleNode, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url+"/api/v1/node/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("headscale get node: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.key)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("headscale get node: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("headscale get node: unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Node HeadscaleNode `json:"node"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("headscale get node: decode: %w", err)
	}
	return &body.Node, nil
}

// DeleteNode calls DELETE {url}/api/v1/node/{id} to remove a peer from Headscale.
// Called when a Meshploy node is deleted so the WireGuard peer is also cleaned up.
func (h *HeadscaleService) DeleteNode(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, h.url+"/api/v1/node/"+id, nil)
	if err != nil {
		return fmt.Errorf("headscale delete node: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.key)

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("headscale delete node: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("headscale delete node: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// RenameNode calls POST {url}/api/v1/node/{id}/rename/{name} to update the
// Headscale peer's given name, keeping MagicDNS in sync with Meshploy.
func (h *HeadscaleService) RenameNode(ctx context.Context, id, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url+"/api/v1/node/"+id+"/rename/"+name, nil)
	if err != nil {
		return fmt.Errorf("headscale rename node: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.key)

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("headscale rename node: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("headscale rename node: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// ListNodes calls GET {url}/api/v1/node and returns all nodes.
// ListNodes doubles as the health probe for the Headscale connection: it runs
// on every node-list request and on each tick of the node monitor, so its
// outcome is recorded for Health().
func (h *HeadscaleService) ListNodes(ctx context.Context) ([]HeadscaleNode, error) {
	nodes, status, err := h.listNodes(ctx)
	h.observe(err, status)
	return nodes, err
}

func (h *HeadscaleService) listNodes(ctx context.Context) ([]HeadscaleNode, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url+"/api/v1/node", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("headscale list nodes: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.key)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("headscale list nodes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("headscale list nodes: unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Nodes []HeadscaleNode `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("headscale list nodes: decode: %w", err)
	}
	return body.Nodes, resp.StatusCode, nil
}
