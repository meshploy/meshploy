package cache

import (
	"log"
	"sort"
	"sync"
	"time"

	db "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// TargetEntry is one path rule for a hostname.
type TargetEntry struct {
	Path             string
	StripPath        bool
	TargetIP         string
	TargetPort       int
	RedirectHostname string // non-empty when this target is a redirect
	RedirectCode     int    // 301 or 302
}

// Cache holds an in-memory snapshot of routes + targets, refreshed in the background.
// Map key is hostname; slice is sorted longest-path-first for prefix matching.
type Cache struct {
	db      *gorm.DB
	mu      sync.RWMutex
	routes  map[string][]TargetEntry
	refresh time.Duration
}

func New(database *gorm.DB, refreshInterval time.Duration) *Cache {
	return &Cache{
		db:      database,
		routes:  make(map[string][]TargetEntry),
		refresh: refreshInterval,
	}
}

func (c *Cache) Start() {
	if err := c.load(); err != nil {
		log.Printf("cache: initial load failed: %v", err)
	}
	go func() {
		t := time.NewTicker(c.refresh)
		defer t.Stop()
		for range t.C {
			if err := c.load(); err != nil {
				log.Printf("cache: refresh failed: %v", err)
			}
		}
	}()
}

// Get returns the best-matching TargetEntry for the given hostname and request path.
// It tries the in-memory cache first, then falls back to a live DB query on miss.
func (c *Cache) Get(hostname, path string) (TargetEntry, bool) {
	c.mu.RLock()
	entries, ok := c.routes[hostname]
	c.mu.RUnlock()

	if !ok {
		// Cache miss — query DB and warm the entry.
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
	var targets []db.RouteTarget
	if err := c.db.Preload("Route").Preload("RedirectRoute").Find(&targets).Error; err != nil {
		return err
	}
	m := make(map[string][]TargetEntry, len(targets))
	for _, t := range targets {
		if t.Route == nil || t.Route.Hostname == "" {
			continue
		}
		entry := TargetEntry{
			Path:       t.Path,
			StripPath:  t.StripPath,
			TargetIP:   t.TargetIP,
			TargetPort: t.TargetPort,
		}
		if t.RedirectRouteID != nil && t.RedirectRoute != nil {
			entry.RedirectHostname = t.RedirectRoute.Hostname
			entry.RedirectCode = t.RedirectCode
		}
		m[t.Route.Hostname] = append(m[t.Route.Hostname], entry)
	}
	for k := range m {
		sortEntries(m[k])
	}
	c.mu.Lock()
	c.routes = m
	c.mu.Unlock()
	log.Printf("cache: loaded %d targets across %d hostnames", len(targets), len(m))
	return nil
}

func (c *Cache) loadHostname(hostname string) []TargetEntry {
	var route db.Route
	if err := c.db.Where("hostname = ?", hostname).First(&route).Error; err != nil {
		return nil
	}
	var targets []db.RouteTarget
	if err := c.db.Preload("RedirectRoute").Where("route_id = ?", route.ID).Find(&targets).Error; err != nil {
		return nil
	}
	entries := make([]TargetEntry, 0, len(targets))
	for _, t := range targets {
		entry := TargetEntry{
			Path:       t.Path,
			StripPath:  t.StripPath,
			TargetIP:   t.TargetIP,
			TargetPort: t.TargetPort,
		}
		if t.RedirectRouteID != nil && t.RedirectRoute != nil {
			entry.RedirectHostname = t.RedirectRoute.Hostname
			entry.RedirectCode = t.RedirectCode
		}
		entries = append(entries, entry)
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
