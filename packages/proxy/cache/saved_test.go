package cache

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/meshploy/packages/proxy/saved"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A proxy that starts while its database is down serves the routes it saved
// last, the old edge's hostnames included, and does not ask the database
// about a hostname it has no route for.
func TestRoutesServeWithoutTheDatabase(t *testing.T) {
	down, err := gorm.Open(postgres.Open("postgres://nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=5"),
		&gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := saved.Open(filepath.Join(t.TempDir(), "edge"))
	if err != nil {
		t.Fatal(err)
	}

	c := New(down, time.Hour)
	c.Keep(dir)
	c.Start()
	if _, ok := c.Get("app.example.com", "/"); ok {
		t.Fatal("nothing saved, yet a route answered")
	}

	if err := dir.Save(savedName, State{
		Routes: map[string][]TargetEntry{
			"app.example.com":    {{Path: "/", TargetIP: "100.64.0.2", TargetPort: 30080}, {Path: "/api", TargetIP: "100.64.0.3", TargetPort: 30081}},
			"*.apps.example.com": {{Path: "/", TargetIP: "100.64.0.4", TargetPort: 30082}},
		},
		Fallback: &Fallback{Upstream: "127.0.0.1:18080", Hostnames: map[string]bool{"old.example.com": true}},
	}); err != nil {
		t.Fatal(err)
	}
	c = New(down, time.Hour)
	c.Keep(dir)
	c.Start()
	if e, ok := c.Get("app.example.com", "/api/users"); !ok || e.TargetPort != 30081 {
		t.Errorf("saved route: %+v %v", e, ok)
	}
	if e, ok := c.Get("shop.apps.example.com", "/"); !ok || e.TargetPort != 30082 {
		t.Errorf("saved wildcard: %+v %v", e, ok)
	}
	if up, ok := c.FallbackFor("old.example.com"); !ok || up != "127.0.0.1:18080" {
		t.Errorf("saved fallback: %q %v", up, ok)
	}
	start := time.Now()
	if _, ok := c.Get("unknown.example.com", "/"); ok {
		t.Error("an unknown hostname answered")
	}
	if waited := time.Since(start); waited > 100*time.Millisecond {
		t.Errorf("an unknown hostname waited %s on the database", waited)
	}
}
