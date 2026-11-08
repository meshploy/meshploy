package cache

import (
	"context"
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/proxy/saved"
	"gorm.io/gorm"
)

// TargetEntry is one path rule for a hostname.
type TargetEntry struct {
	Path       string
	StripPath  bool
	TargetIP   string
	TargetPort int
	// TargetTLS: the target speaks HTTPS, so the hop to it is too.
	TargetTLS        bool
	RedirectHostname string // non-empty when this target is a redirect
	RedirectCode     int    // 301 or 302
	// Internal: the route is reached over the mesh, so who may open it is
	// decided per caller.
	Internal bool

	// Whose request this is, so a gate needs no lookup of its own. ServiceID
	// is zero for a node or address target.
	RouteID   uuid.UUID
	ServiceID uuid.UUID
	ProjectID uuid.UUID
	OrgID     uuid.UUID
}

// entryFor is one target of a route as the proxy serves it.
func entryFor(t db.RouteTarget, route *db.Route) TargetEntry {
	entry := TargetEntry{
		Path:       t.Path,
		StripPath:  t.StripPath,
		TargetIP:   t.TargetIP,
		TargetPort: t.TargetPort,
		TargetTLS:  t.TargetTLS,
		Internal:   route.Zone == db.RouteZoneInternal,
		RouteID:    route.ID,
		ProjectID:  route.ProjectID,
		OrgID:      route.OrganizationID,
	}
	if t.ServiceID != nil {
		entry.ServiceID = *t.ServiceID
	}
	if t.RedirectRouteID != nil && t.RedirectRoute != nil {
		entry.RedirectHostname = t.RedirectRoute.Hostname
		entry.RedirectCode = t.RedirectCode
	}
	return entry
}

// Cache holds an in-memory snapshot of routes + targets, refreshed in the background.
// Map key is hostname; slice is sorted longest-path-first for prefix matching.
type Cache struct {
	db      *gorm.DB
	mu      sync.RWMutex
	routes  map[string][]TargetEntry
	refresh time.Duration
	// fallback is where a hostname with no published route goes while a
	// migration serves it from the old platform's edge; see db.EdgeFallback.
	fallback *Fallback
	// saved keeps the last load, which a proxy serves when it starts without
	// the database.
	saved *saved.Dir
	// reached is whether the last load from the database succeeded. While it
	// did not, a hostname missing from the table is not looked up there:
	// each lookup would wait on a database that is not answering.
	reached atomic.Bool
}

// savedName is the routes' file in the saved directory.
const savedName = "routes"

// State is the routes as the proxy serves them: what a load from the
// database makes, what is saved, and what the API hands a proxy that has no
// database.
type State struct {
	Routes   map[string][]TargetEntry `json:"routes"`
	Fallback *Fallback                `json:"fallback,omitempty"`
}

// missLookupTimeout bounds asking the database about a hostname missing from
// the table, since a request waits on the answer.
const missLookupTimeout = 2 * time.Second

// Fallback is the old edge, and the hostnames it still serves.
type Fallback struct {
	Upstream  string
	Hostnames map[string]bool
}

// Keep has every load saved in d, and the saved copy served when the first
// load fails. Call it before Start.
func (c *Cache) Keep(d *saved.Dir) { c.saved = d }

func New(database *gorm.DB, refreshInterval time.Duration) *Cache {
	return &Cache{
		db:      database,
		routes:  make(map[string][]TargetEntry),
		refresh: refreshInterval,
	}
}

// Start loads the routes, then reloads them within a second of any change
// (db.EdgeRoutes) and every refresh interval regardless. When the first load
// fails the saved copy is served until one succeeds; a later failure keeps
// what is already served.
func (c *Cache) Start() {
	if err := c.load(); err != nil {
		log.Printf("cache: initial load failed: %v", err)
		c.Restore()
	}
	db.FollowEdgeVersion(c.db, db.EdgeRoutes, c.refresh, func() {
		if err := c.load(); err != nil {
			log.Printf("cache: refresh failed, keeping the last routes: %v", err)
		}
	})
}

// Restore serves the saved copy of the routes, until a load succeeds.
func (c *Cache) Restore() {
	var s State
	at, err := c.saved.Load(savedName, &s)
	switch {
	case errors.Is(err, saved.ErrNone):
		log.Printf("cache: no saved routes either; serving none until the source answers")
		return
	case err != nil:
		log.Printf("cache: the saved routes cannot be read: %v", err)
		return
	}
	if s.Routes == nil {
		s.Routes = map[string][]TargetEntry{}
	}
	for _, entries := range s.Routes {
		sortEntries(entries)
	}
	c.mu.Lock()
	c.routes = s.Routes
	c.fallback = s.Fallback
	c.mu.Unlock()
	log.Printf("cache: serving %d hostnames saved at %s until the source answers", len(s.Routes), at.Format(time.RFC3339))
}

// Get returns the best-matching TargetEntry for the given hostname and request path.
// Lookup order: exact match → wildcard match (*.parent) → DB fallback.
// On a wildcard hit the full hostname is also stored so subsequent requests are exact O(1).
func (c *Cache) Get(hostname, path string) (TargetEntry, bool) {
	c.mu.RLock()
	entries, ok := c.routes[hostname]
	wildcardHit := false
	if !ok {
		if wc := wildcardKey(hostname); wc != "" {
			entries, ok = c.routes[wc]
			wildcardHit = ok
		}
	}
	c.mu.RUnlock()

	if wildcardHit {
		// Warm the exact hostname so subsequent requests skip the wildcard scan.
		c.mu.Lock()
		c.routes[hostname] = entries
		c.mu.Unlock()
	} else if !ok {
		// Full cache miss - query DB (also tries wildcard pattern in DB).
		entries = c.loadHostname(hostname)
		if len(entries) == 0 {
			return TargetEntry{}, false
		}
		c.mu.Lock()
		c.routes[hostname] = entries
		c.mu.Unlock()
	}

	return longestPrefixMatch(entries, path)
}

func (c *Cache) load() error {
	st, err := LoadState(c.db)
	if err != nil {
		c.reached.Store(false)
		return err
	}
	c.Serve(st)
	c.reached.Store(true)
	return nil
}

// LoadState reads the published routes and the migration fallback from the
// database, as the proxy serves them.
func LoadState(database *gorm.DB) (State, error) {
	var targets []db.RouteTarget
	if err := database.Preload("Route").Preload("RedirectRoute").Find(&targets).Error; err != nil {
		return State{}, err
	}
	m := make(map[string][]TargetEntry, len(targets))
	for _, t := range targets {
		if t.Route == nil || t.Route.Hostname == "" || !t.Route.Published {
			continue // a paused route answers as if it did not exist
		}
		m[t.Route.Hostname] = append(m[t.Route.Hostname], entryFor(t, t.Route))
	}
	var fb *Fallback
	var row db.EdgeFallback
	if err := database.Order("created_at").Limit(1).Find(&row).Error; err != nil {
		// Half a load is not served: without the fallback, a migration's
		// hostnames would stop reaching the old edge.
		return State{}, err
	} else if row.Upstream != "" {
		fb = &Fallback{Upstream: row.Upstream, Hostnames: map[string]bool{}}
		for _, h := range row.Hostnames {
			fb.Hostnames[strings.ToLower(h)] = true
		}
	}
	return State{Routes: m, Fallback: fb}, nil
}

// Serve replaces every route with s, and saves it.
func (c *Cache) Serve(s State) {
	m := make(map[string][]TargetEntry, len(s.Routes))
	targets := 0
	for host, entries := range s.Routes {
		entries = append([]TargetEntry(nil), entries...)
		sortEntries(entries)
		m[host] = entries
		targets += len(entries)
	}
	s.Routes = m
	// Saved before it is served: once served, a wildcard hit adds to the map.
	if err := c.saved.Save(savedName, s); err != nil {
		log.Printf("cache: saving the routes: %v", err)
	}
	c.mu.Lock()
	c.routes = m
	c.fallback = s.Fallback
	c.mu.Unlock()
	log.Printf("cache: loaded %d targets across %d hostnames", targets, len(m))
}

// Set replaces a hostname's entries until the next refresh; the refresh
// itself replaces them all from the database.
func (c *Cache) Set(hostname string, entries []TargetEntry) {
	entries = append([]TargetEntry(nil), entries...)
	sortEntries(entries)
	c.mu.Lock()
	c.routes[hostname] = entries
	c.mu.Unlock()
}

// SetFallback replaces the fallback; the refresh does this from the database.
func (c *Cache) SetFallback(f *Fallback) {
	c.mu.Lock()
	c.fallback = f
	c.mu.Unlock()
}

// FallbackFor is the old edge's address for a hostname it still serves, when
// a migration has one: asked only after a hostname found no published route,
// which always wins.
func (c *Cache) FallbackFor(hostname string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.fallback == nil || !c.fallback.Hostnames[strings.ToLower(hostname)] {
		return "", false
	}
	return c.fallback.Upstream, true
}

func (c *Cache) loadHostname(hostname string) []TargetEntry {
	if c.db == nil || !c.reached.Load() {
		return nil // a cache filled by Set alone, as in tests, or a database not answering
	}
	ctx, cancel := context.WithTimeout(context.Background(), missLookupTimeout)
	defer cancel()
	conn := c.db.WithContext(ctx)
	var route db.Route
	err := conn.Where("hostname = ? AND published = ?", hostname, true).First(&route).Error
	if err != nil {
		// Try wildcard pattern (*.parent.domain).
		wc := wildcardKey(hostname)
		if wc == "" {
			return nil
		}
		if err2 := conn.Where("hostname = ? AND published = ?", wc, true).First(&route).Error; err2 != nil {
			return nil
		}
	}
	var targets []db.RouteTarget
	if err := conn.Preload("RedirectRoute").Where("route_id = ?", route.ID).Find(&targets).Error; err != nil {
		return nil
	}
	entries := make([]TargetEntry, 0, len(targets))
	for _, t := range targets {
		entries = append(entries, entryFor(t, &route))
	}
	sortEntries(entries)
	return entries
}

// longestPrefixMatch returns the first entry whose Path is a prefix of reqPath.
// Entries are pre-sorted longest-first so the most specific match wins.
func longestPrefixMatch(entries []TargetEntry, reqPath string) (TargetEntry, bool) {
	for _, e := range entries {
		if e.Path == "/" || len(reqPath) >= len(e.Path) && reqPath[:len(e.Path)] == e.Path {
			return e, true
		}
	}
	return TargetEntry{}, false
}

func sortEntries(entries []TargetEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return len(entries[i].Path) > len(entries[j].Path)
	})
}

// wildcardKey returns the wildcard hostname for a given hostname by replacing
// the leftmost label with "*". Returns "" when there is no parent domain
// (i.e. hostname has no dot, so a wildcard makes no sense).
// Example: "tenant1.some-app.example.com" → "*.some-app.example.com"
func wildcardKey(hostname string) string {
	if i := strings.IndexByte(hostname, '.'); i != -1 {
		return "*." + hostname[i+1:]
	}
	return ""
}
