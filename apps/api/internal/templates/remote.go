package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// RemoteCatalog serves the one-click catalog from a public GitHub repo laid out
// as templates/<id>/{meta.yaml,docker-compose.yml,logo.svg}. It discovers ids
// from the git tree API, fetches each template's meta + compose over raw.
// githubusercontent, and holds the parsed result in memory. A background
// goroutine refreshes on an interval; on a fetch failure the last good snapshot
// keeps being served (serve-stale). It satisfies Catalog, so the deploy engine
// is identical whether templates come from disk or GitHub.
type RemoteCatalog struct {
	repo    string // "owner/repo"
	ref     string // branch/tag
	rawBase string // https://raw.githubusercontent.com/<repo>/<ref>/
	treeURL string // git tree API (recursive)
	refresh time.Duration
	client  *http.Client

	fetchMu sync.Mutex // serializes refreshes so we never fan out duplicate fetches

	mu          sync.RWMutex
	manifests   []*Manifest
	byID        map[string]*Template
	fetchedAt   time.Time // last SUCCESSFUL fetch; zero = never loaded
	lastAttempt time.Time // last attempt (success or failure) — throttles lazy retries
	lastErr     error
}

// NewRemoteCatalog builds a catalog over repo (e.g. "meshploy/meshploy-templates")
// at ref, refreshing every refresh interval.
func NewRemoteCatalog(repo, ref string, refresh time.Duration) *RemoteCatalog {
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	if ref == "" {
		ref = "main"
	}
	if refresh <= 0 {
		refresh = time.Hour
	}
	return &RemoteCatalog{
		repo:    repo,
		ref:     ref,
		rawBase: fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/", repo, ref),
		treeURL: fmt.Sprintf("https://api.github.com/repos/%s/git/trees/%s?recursive=1", repo, ref),
		refresh: refresh,
		client:  &http.Client{Timeout: 20 * time.Second},
	}
}

// List returns the cached manifests, sorted by id. It never returns nil and
// never errors: a failed fetch yields the last good snapshot, or an empty list
// if nothing has loaded yet (the UI shows "no templates" rather than a 500).
func (c *RemoteCatalog) List() ([]*Manifest, error) {
	c.ensureLoaded()
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*Manifest, len(c.manifests))
	copy(out, c.manifests)
	return out, nil
}

// Get returns a single cached template by id. When the catalog was loaded from
// index.json (manifests only), the compose is fetched lazily on first Get and
// cached, so the gallery stays a single fetch while deploy still gets the spec.
func (c *RemoteCatalog) Get(id string) (*Template, error) {
	c.ensureLoaded()
	c.mu.RLock()
	t, ok := c.byID[id]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("template %q not found in catalog", id)
	}
	if t.Compose != "" {
		return t, nil
	}
	// Lazily fill the compose for an index-sourced template.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	composeB, err := c.fetchRaw(ctx, "templates/"+id+"/docker-compose.yml")
	if err != nil {
		return nil, fmt.Errorf("fetch compose for %q: %w", id, err)
	}
	c.mu.Lock()
	t.Compose = string(composeB)
	c.mu.Unlock()
	return t, nil
}

// ensureLoaded does a one-time synchronous fetch on the first request so the UI
// gets data immediately, without waiting for the background ticker. On failure
// it throttles retries to avoid blocking every request against an unreachable
// repo.
func (c *RemoteCatalog) ensureLoaded() {
	c.mu.RLock()
	loaded := !c.fetchedAt.IsZero()
	recentlyTried := !c.lastAttempt.IsZero() && time.Since(c.lastAttempt) < 30*time.Second
	c.mu.RUnlock()
	if loaded || recentlyTried {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	_ = c.Refresh(ctx)
}

// StartRefresh fetches once, then refreshes on the configured interval until ctx
// is cancelled. Run in a goroutine at startup.
func (c *RemoteCatalog) StartRefresh(ctx context.Context) {
	if err := c.Refresh(ctx); err != nil {
		log.Printf("templates: initial catalog fetch from %s@%s failed (will retry): %v", c.repo, c.ref, err)
	}
	t := time.NewTicker(c.refresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_ = c.Refresh(rctx)
			cancel()
		}
	}
}

// Refresh fetches the full catalog and atomically replaces the cache on success.
// It prefers a prebuilt index.json (one request, no API rate limit); if that is
// absent it falls back to discovering templates via the git tree API. On failure
// the previous snapshot is left intact (serve-stale).
func (c *RemoteCatalog) Refresh(ctx context.Context) error {
	c.fetchMu.Lock()
	defer c.fetchMu.Unlock()

	c.mu.Lock()
	c.lastAttempt = time.Now()
	c.mu.Unlock()

	manifests, byID, err := c.fetchViaIndex(ctx)
	if err != nil {
		// No index.json (or unreadable) — fall back to per-template discovery.
		manifests, byID, err = c.fetchViaTree(ctx)
		if err != nil {
			c.setErr(err)
			return err
		}
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].ID < manifests[j].ID })

	c.mu.Lock()
	c.manifests = manifests
	c.byID = byID
	c.fetchedAt = time.Now()
	c.lastErr = nil
	c.mu.Unlock()
	return nil
}

// fetchViaIndex loads the catalog from a prebuilt index.json. Templates carry
// only their manifest here — the compose is fetched lazily on Get.
func (c *RemoteCatalog) fetchViaIndex(ctx context.Context) ([]*Manifest, map[string]*Template, error) {
	b, err := c.fetchRaw(ctx, "index.json")
	if err != nil {
		return nil, nil, err
	}
	var idx struct {
		Templates []*Manifest `json:"templates"`
	}
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, nil, fmt.Errorf("parse index.json: %w", err)
	}
	manifests := make([]*Manifest, 0, len(idx.Templates))
	byID := make(map[string]*Template, len(idx.Templates))
	for _, m := range idx.Templates {
		if m == nil || m.ID == "" {
			continue
		}
		manifests = append(manifests, m)
		byID[m.ID] = &Template{Manifest: m} // Compose filled lazily by Get
	}
	return manifests, byID, nil
}

// fetchViaTree discovers templates from the git tree and fetches each one's
// meta + compose. Used when no index.json is published.
func (c *RemoteCatalog) fetchViaTree(ctx context.Context) ([]*Manifest, map[string]*Template, error) {
	ids, err := c.listIDs(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list template ids: %w", err)
	}
	manifests := make([]*Manifest, 0, len(ids))
	byID := make(map[string]*Template, len(ids))
	for _, id := range ids {
		tpl, err := c.fetchTemplate(ctx, id)
		if err != nil {
			log.Printf("templates: skipping %q: %v", id, err) // one bad template must not break the catalog
			continue
		}
		manifests = append(manifests, tpl.Manifest)
		byID[id] = tpl
	}
	return manifests, byID, nil
}

func (c *RemoteCatalog) setErr(err error) {
	c.mu.Lock()
	c.lastErr = err
	c.mu.Unlock()
	log.Printf("templates: catalog refresh failed: %v", err)
}

// listIDs discovers template ids from the git tree — every templates/<id>/meta.yaml.
func (c *RemoteCatalog) listIDs(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.treeURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok) // optional — raises the API rate limit
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("git tree status %d", resp.StatusCode)
	}
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&tree); err != nil {
		return nil, err
	}
	if tree.Truncated {
		log.Printf("templates: git tree for %s@%s is truncated; some templates may be missing", c.repo, c.ref)
	}
	var ids []string
	for _, e := range tree.Tree {
		// Match exactly templates/<id>/meta.yaml (one dir deep).
		parts := strings.Split(e.Path, "/")
		if len(parts) == 3 && parts[0] == "templates" && parts[2] == "meta.yaml" {
			ids = append(ids, parts[1])
		}
	}
	return ids, nil
}

func (c *RemoteCatalog) fetchTemplate(ctx context.Context, id string) (*Template, error) {
	metaB, err := c.fetchRaw(ctx, "templates/"+id+"/meta.yaml")
	if err != nil {
		return nil, fmt.Errorf("meta.yaml: %w", err)
	}
	m, err := ParseManifest(metaB)
	if err != nil {
		return nil, err
	}
	composeB, err := c.fetchRaw(ctx, "templates/"+id+"/docker-compose.yml")
	if err != nil {
		return nil, fmt.Errorf("docker-compose.yml: %w", err)
	}
	return &Template{Manifest: m, Compose: string(composeB)}, nil
}

func (c *RemoteCatalog) fetchRaw(ctx context.Context, p string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.rawBase+p, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap per file
}
