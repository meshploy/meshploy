package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const cloudflareAPI = "https://api.cloudflare.com/client/v4"

// cloudflare talks to Cloudflare's REST API with an API token. Records are
// always written DNS only: Cloudflare's proxy would end TLS before the gateway,
// and its own certificates cover one level of subdomain only.
type cloudflare struct {
	token string
	base  string
	http  *http.Client
}

// NewCloudflare returns a Cloudflare client. An empty base is Cloudflare's own
// API; tests pass a server of their own.
func NewCloudflare(token, base string) Provider {
	if base == "" {
		base = cloudflareAPI
	}
	return &cloudflare{token: token, base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 20 * time.Second}}
}

type cfEnvelope struct {
	Success bool            `json:"success"`
	Errors  []cfError       `json:"errors"`
	Result  json.RawMessage `json:"result"`
	Info    *struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *cloudflare) call(ctx context.Context, method, path string, body any, out any) (*cfEnvelope, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: %w", err)
	}
	defer resp.Body.Close()
	var env cfEnvelope
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&env); err != nil {
		return nil, fmt.Errorf("cloudflare: %s: unreadable answer (%s)", path, resp.Status)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: %s", ErrRejected, cfMessage(env.Errors, resp.Status))
	}
	if !env.Success || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cloudflare: %s", cfMessage(env.Errors, resp.Status))
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return nil, fmt.Errorf("cloudflare: %s: %w", path, err)
		}
	}
	return &env, nil
}

func cfMessage(errs []cfError, status string) string {
	if len(errs) == 0 {
		return status
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = fmt.Sprintf("%s (%d)", e.Message, e.Code)
	}
	return strings.Join(msgs, "; ")
}

func (c *cloudflare) Zones(ctx context.Context) ([]Zone, error) {
	zones := make([]Zone, 0)
	for page := 1; ; page++ {
		var got []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		env, err := c.call(ctx, http.MethodGet, fmt.Sprintf("/zones?per_page=50&page=%d", page), nil, &got)
		if err != nil {
			return nil, err
		}
		for _, z := range got {
			zones = append(zones, Zone{ID: z.ID, Name: z.Name})
		}
		if env.Info == nil || page >= env.Info.TotalPages {
			return zones, nil
		}
	}
}

func (c *cloudflare) Records(ctx context.Context, zone Zone, name string) ([]Record, error) {
	var got []Record
	q := url.Values{"name": {name}, "per_page": {"100"}}
	if _, err := c.call(ctx, http.MethodGet, "/zones/"+url.PathEscape(zone.ID)+"/dns_records?"+q.Encode(), nil, &got); err != nil {
		return nil, err
	}
	if got == nil {
		got = []Record{}
	}
	return got, nil
}

func (c *cloudflare) CreateRecord(ctx context.Context, zone Zone, r Record) (Record, error) {
	ttl := r.TTL
	if ttl == 0 {
		ttl = 1 // Cloudflare's "automatic"
	}
	body := map[string]any{
		"type":    r.Type,
		"name":    r.Name,
		"content": r.Content,
		"ttl":     ttl,
		"proxied": false,
		"comment": r.Comment,
	}
	var out Record
	_, err := c.call(ctx, http.MethodPost, "/zones/"+url.PathEscape(zone.ID)+"/dns_records", body, &out)
	return out, err
}

func (c *cloudflare) DeleteRecord(ctx context.Context, zone Zone, id string) error {
	_, err := c.call(ctx, http.MethodDelete, "/zones/"+url.PathEscape(zone.ID)+"/dns_records/"+url.PathEscape(id), nil, nil)
	return err
}
