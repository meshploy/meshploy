package service

import (
	"reflect"
	"testing"

	"github.com/meshploy/packages/db"
	corev1 "k8s.io/api/core/v1"
)

func envMap(envs []corev1.EnvVar) map[string]string {
	m := map[string]string{}
	for _, e := range envs {
		m[e.Name] = e.Value
	}
	return m
}

func TestResolveEnvRefs(t *testing.T) {
	envs := []corev1.EnvVar{
		{Name: "PRIMARY_PG_DB_URL", Value: "postgresql://u:p@db:5432/app"},
		{Name: "DATABASE_URL", Value: "${PRIMARY_PG_DB_URL}"},
		{Name: "MIXED", Value: "host=${PRIMARY_PG_DB_URL};x"},
		{Name: "ESCAPED", Value: "$${PRIMARY_PG_DB_URL}"},
		{Name: "TYPO", Value: "${PRIMARY_PG_DB_ULR} and ${MISSING} and ${MISSING}"},
		{Name: "CHAIN", Value: "${DATABASE_URL}"},
		{Name: "LONGER_CHAIN", Value: "${CHAIN}?sslmode=disable"},
		{Name: "ESCAPED_VIA", Value: "${ESCAPED}"},
		{Name: "TYPO_VIA", Value: "${TYPO}"},
		{Name: "PLAIN", Value: "$HOME and $ and ${lower-case}"},
	}
	got, unknown, looped := resolveEnvRefs(envs)
	m := envMap(got)

	const url = "postgresql://u:p@db:5432/app"
	want := map[string]string{
		"DATABASE_URL": url,
		"MIXED":        "host=" + url + ";x",
		"ESCAPED":      "${PRIMARY_PG_DB_URL}",
		"TYPO":         "${PRIMARY_PG_DB_ULR} and ${MISSING} and ${MISSING}",
		// References are followed to a value.
		"CHAIN":        url,
		"LONGER_CHAIN": url + "?sslmode=disable",
		// An escaped ${ stays literal however it is reached.
		"ESCAPED_VIA": "${PRIMARY_PG_DB_URL}",
		"TYPO_VIA":    "${PRIMARY_PG_DB_ULR} and ${MISSING} and ${MISSING}",
		"PLAIN":       "$HOME and $ and ${lower-case}",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
	if !reflect.DeepEqual(unknown, []string{"MISSING", "PRIMARY_PG_DB_ULR"}) {
		t.Errorf("unknown = %v", unknown)
	}
	if len(looped) != 0 {
		t.Errorf("looped = %v", looped)
	}
}

// A loop of references is left as written, with whatever leads into it, and
// named; the rest of the env still resolves.
func TestResolveEnvRefsLoops(t *testing.T) {
	envs := []corev1.EnvVar{
		{Name: "A", Value: "${B}"},
		{Name: "B", Value: "${A}"},
		{Name: "SELF", Value: "x${SELF}"},
		{Name: "INTO", Value: "pre-${A}"},
		{Name: "OK", Value: "fine"},
		{Name: "FINE", Value: "${OK}"},
	}
	got, unknown, looped := resolveEnvRefs(envs)
	m := envMap(got)

	for k, v := range map[string]string{"A": "${B}", "B": "${A}", "SELF": "x${SELF}", "INTO": "pre-${A}", "FINE": "fine"} {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
	if !reflect.DeepEqual(looped, []string{"A", "B", "INTO", "SELF"}) {
		t.Errorf("looped = %v", looped)
	}
	if len(unknown) != 0 {
		t.Errorf("unknown = %v", unknown)
	}
}

func TestDatabaseURL(t *testing.T) {
	const host = "primary-db.vars.svc.cluster.local"
	cases := []struct {
		engine db.DatabaseEngine
		port   int32
		want   string
	}{
		{db.DatabasePostgres, 5432, "postgresql://app:p%40ss%3Aw%2Frd@" + host + ":5432/appdb"},
		{db.DatabaseMySQL, 3306, "mysql://app:p%40ss%3Aw%2Frd@" + host + ":3306/appdb"},
		{db.DatabaseMongoDB, 27017, "mongodb://app:p%40ss%3Aw%2Frd@" + host + ":27017/appdb?authSource=admin"},
		{db.DatabaseClickHouse, 9000, "clickhouse://app:p%40ss%3Aw%2Frd@" + host + ":9000/appdb"},
		{db.DatabaseRedis, 6379, "redis://:p%40ss%3Aw%2Frd@" + host + ":6379"},
		{db.DatabaseDragonfly, 6379, "redis://:p%40ss%3Aw%2Frd@" + host + ":6379"},
	}
	for _, c := range cases {
		if got := databaseURL(c.engine, "app", "p@ss:w/rd", host, c.port, "appdb"); got != c.want {
			t.Errorf("%s: %s, want %s", c.engine, got, c.want)
		}
	}
	if got := databaseURL(db.DatabaseRedis, "", "", host, 6379, ""); got != "redis://"+host+":6379" {
		t.Errorf("redis without a password: %s", got)
	}
}

// The password, and the URL carrying it, are secret; a database item replaces
// a port-derived one under the same key.
func TestBuildDatabaseItems(t *testing.T) {
	items := buildDatabaseItems("PG", "pg.ns.svc.cluster.local", 5432, db.DatabaseConfig{
		Engine: db.DatabasePostgres, DBName: "app", DBUser: "app", DBPassword: "secret",
	})
	byKey := map[string]db.VariableGroupItem{}
	for _, it := range items {
		byKey[it.Key] = it
	}
	for key, secret := range map[string]bool{"PG_USER": false, "PG_DB": false, "PG_PASSWORD": true, "PG_URL": true} {
		it, ok := byKey[key]
		if !ok || it.IsSecret != secret {
			t.Errorf("%s: present %v, secret %v; want secret %v", key, ok, it.IsSecret, secret)
		}
	}

	redis := buildDatabaseItems("CACHE", "cache", 6379, db.DatabaseConfig{Engine: db.DatabaseRedis, DBPassword: "x"})
	for _, it := range redis {
		if it.Key == "CACHE_USER" || it.Key == "CACHE_DB" {
			t.Errorf("redis has no %s", it.Key)
		}
	}

	merged := mergeItems(
		[]db.VariableGroupItem{{Key: "PG_HOST"}, {Key: "PG_URL", Value: "http://wrong"}},
		[]db.VariableGroupItem{{Key: "PG_URL", Value: "postgresql://right"}},
	)
	if len(merged) != 2 || merged[1].Value != "postgresql://right" {
		t.Errorf("merged = %+v", merged)
	}
}

// Redis is only given a password it enforces; Dragonfly takes it from its env.
func TestDatabasePasswordsAreEnforced(t *testing.T) {
	redis := db.DatabaseConfig{Engine: db.DatabaseRedis, DBPassword: "pw"}
	if got := dbArgs(redis); !reflect.DeepEqual(got, []string{"--requirepass", "$(REDIS_PASSWORD)"}) {
		t.Errorf("redis args = %v", got)
	}
	if env := envMap(dbEnvVars(redis)); env["REDIS_PASSWORD"] != "pw" {
		t.Errorf("redis env = %v", env)
	}
	dragonfly := db.DatabaseConfig{Engine: db.DatabaseDragonfly, DBPassword: "pw"}
	if env := envMap(dbEnvVars(dragonfly)); env["DFLY_requirepass"] != "pw" || dbArgs(dragonfly) != nil {
		t.Errorf("dragonfly env = %v, args = %v", env, dbArgs(dragonfly))
	}
	if dbArgs(db.DatabaseConfig{Engine: db.DatabasePostgres, DBPassword: "pw"}) != nil {
		t.Error("postgres keeps its image's arguments")
	}
}
