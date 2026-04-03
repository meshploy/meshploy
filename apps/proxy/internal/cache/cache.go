package cache

import (
	"log"
	"sync"
	"time"

	db "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// Entry is the hot-path lookup result: where to forward the request.
type Entry struct {
	TargetIP   string
	TargetPort int
}

// Cache holds an in-memory snapshot of the routes table, refreshed in the background.
type Cache struct {
	db      *gorm.DB
	mu      sync.RWMutex
	routes  map[string]Entry // hostname → Entry
	refresh time.Duration
}

func New(database *gorm.DB, refreshInterval time.Duration) *Cache {
	return &Cache{
		db:      database,
		routes:  make(map[string]Entry),
		refresh: refreshInterval,
	}
}

// Start loads routes immediately then refreshes on the given interval.
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

// Get looks up a hostname. Returns the entry and whether it was found.
func (c *Cache) Get(hostname string) (Entry, bool) {
	c.mu.RLock()
	e, ok := c.routes[hostname]
	c.mu.RUnlock()
	if ok {
		return e, true
	}
	// Cache miss — query DB directly and warm the entry.
	var route db.Route
	if err := c.db.Where("hostname = ?", hostname).First(&route).Error; err != nil {
		return Entry{}, false
	}
	entry := Entry{TargetIP: route.TargetIP, TargetPort: route.TargetPort}
	c.mu.Lock()
	c.routes[hostname] = entry
	c.mu.Unlock()
	return entry, true
}

func (c *Cache) load() error {
	var routes []db.Route
	if err := c.db.Find(&routes).Error; err != nil {
		return err
	}
	m := make(map[string]Entry, len(routes))
	for _, r := range routes {
		m[r.Hostname] = Entry{TargetIP: r.TargetIP, TargetPort: r.TargetPort}
	}
	c.mu.Lock()
	c.routes = m
	c.mu.Unlock()
	log.Printf("cache: loaded %d routes", len(routes))
	return nil
}
