package service

import (
	"testing"

	"github.com/meshploy/packages/db"
)

// A stack's database is made at the version its compose image names, unless
// its block names one; another image says nothing about the version.
func TestAStackDatabaseTakesTheVersionItsImageNames(t *testing.T) {
	for name, tc := range map[string]struct {
		engine          db.DatabaseEngine
		declared, image string
		want            string
	}{
		"image tag":     {db.DatabaseRedis, "", "redis:7", "7"},
		"declared wins": {db.DatabaseRedis, "8", "redis:7", "8"},
		"hub prefix":    {db.DatabasePostgres, "", "docker.io/library/postgres:16", "16"},
		"no image":      {db.DatabaseRedis, "", "", ""},
		"no tag":        {db.DatabaseRedis, "", "redis", ""},
		"another image": {db.DatabaseRedis, "", "bitnami/redis:7", ""},
		"digest":        {db.DatabaseRedis, "", "redis@sha256:abc", ""},
		"clickhouse":    {db.DatabaseClickHouse, "", "clickhouse/clickhouse-server:25.8", "25.8"},
		"dragonfly":     {db.DatabaseDragonfly, "", "docker.dragonflydb.io/dragonflydb/dragonfly:v1.39.0", "v1.39.0"},
	} {
		if got := stackDBVersion(tc.engine, tc.declared, tc.image); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}
