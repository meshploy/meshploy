package db

import (
	"testing"
	"time"
)

// Opening lazily does not reach the database: with nothing listening it
// still returns at once, and the first query is what fails.
func TestFromEnvLazilyNeedsNoDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=2")
	start := time.Now()
	conn, err := FromEnvLazily()
	if err != nil {
		t.Fatalf("opening needed the database: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("opening waited %s", time.Since(start))
	}
	if _, err := EdgeVersionOf(conn, EdgeRoutes); err == nil {
		t.Error("a query against no database succeeded")
	}
}
