package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
)

// The API's paths for an edge, and how an edge names itself to them.
const (
	SnapshotPath  = "/api/v1/edge/snapshot"
	TCPStatusPath = "/api/v1/edge/tcp-status"
	NodeHeader    = "X-Meshploy-Node"
)

// MaxWait is the longest the API holds a request for a snapshot that has not
// changed.
const MaxWait = 25 * time.Second

// Client reaches the API as the edge: the node the edge runs on, and the
// secret that node registered with.
type Client struct {
	// Base is the API's address, e.g. http://100.64.0.1:4000.
	Base   string
	NodeID uuid.UUID
	Secret string
	HTTP   *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: MaxWait + 15*time.Second}
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	req.Header.Set(NodeHeader, c.NodeID.String())
	return req, nil
}

// Fetch asks for the snapshot. With have, the API answers once its versions
// differ from have, or after wait with nothing (nil, nil); without, at once.
func (c *Client) Fetch(ctx context.Context, have *Versions, wait time.Duration) (*Snapshot, error) {
	q := url.Values{}
	if have != nil {
		q.Set("routes", strconv.FormatInt(have.Routes, 10))
		q.Set("mesh", strconv.FormatInt(have.Mesh, 10))
		q.Set("wait", strconv.Itoa(int(wait/time.Second)))
	}
	path := SnapshotPath
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	req, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusOK:
		var s Snapshot
		if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
			return nil, fmt.Errorf("reading the snapshot: %w", err)
		}
		return &s, nil
	default:
		return nil, statusError(resp)
	}
}

// TCPStatus is what the edge managed to do with one TCP route.
type TCPStatus struct {
	ID        uuid.UUID         `json:"id"`
	Status    db.TCPRouteStatus `json:"status"`
	LastError string            `json:"last_error"`
}

// ReportTCP tells the API what became of TCP routes on this edge.
func (c *Client) ReportTCP(ctx context.Context, statuses []TCPStatus) error {
	body, err := json.Marshal(map[string]any{"routes": statuses})
	if err != nil {
		return err
	}
	req, err := c.request(ctx, http.MethodPost, TCPStatusPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return statusError(resp)
	}
	return nil
}

// ErrRefused is the API refusing the edge's credentials.
var ErrRefused = errors.New("the API refused this edge's credentials")

func statusError(resp *http.Response) error {
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w (%d): %s", ErrRefused, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return fmt.Errorf("the API answered %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
}
