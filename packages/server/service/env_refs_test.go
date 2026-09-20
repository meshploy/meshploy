package service

import (
	"reflect"
	"strings"
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

// A build step often needs one value the service already has, and re-typing it
// is how the two drift apart. A reference in the build block may name a runtime
// variable - but only the build block's keys reach the builder.
func TestResolveBuildEnvFillsFromRuntimeWithoutCarryingIt(t *testing.T) {
	runtime := []corev1.EnvVar{
		{Name: "DATABASE_URL", Value: "postgres://app:secret@db:5432/app"},
		{Name: "SESSION_KEY", Value: "not for the build"},
	}
	got, unresolved := resolveBuildEnv("PRISMA_DB_URL=${DATABASE_URL}\nNODE_ENV=production\n", runtime)
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %v", unresolved)
	}
	if !strings.Contains(got, "PRISMA_DB_URL=postgres://app:secret@db:5432/app") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "NODE_ENV=production") {
		t.Errorf("got %q", got)
	}
	// Nothing else is carried into the build: a runtime secret has no business
	// in an image layer or a build log.
	if strings.Contains(got, "SESSION_KEY") || strings.Contains(got, "DATABASE_URL=") {
		t.Errorf("a runtime variable leaked into the build block: %q", got)
	}
}

// The build block is the more specific statement of what this build needs, so
// its own value wins over a runtime one of the same name.
func TestABuildVariableWinsOverTheRuntimeOne(t *testing.T) {
	runtime := []corev1.EnvVar{{Name: "NODE_ENV", Value: "production"}}
	got, _ := resolveBuildEnv("NODE_ENV=development\n", runtime)
	if strings.TrimSpace(got) != "NODE_ENV=development" {
		t.Errorf("got %q", got)
	}
}

// A name that resolves to nothing is left as written and reported, so a typo
// shows up in the build rather than becoming an empty value.
func TestAnUnknownBuildReferenceIsReported(t *testing.T) {
	got, unresolved := resolveBuildEnv("API_URL=${NOT_A_THING}\n", nil)
	if len(unresolved) != 1 || unresolved[0] != "NOT_A_THING" {
		t.Fatalf("unresolved = %v", unresolved)
	}
	if !strings.Contains(got, "${NOT_A_THING}") {
		t.Errorf("got %q, want the reference left as written", got)
	}
}

// Quotes, comments and blank lines are how people actually write these.
func TestParseEnvBlockReadsWhatPeopleWrite(t *testing.T) {
	got := parseEnvBlock("# a comment\n\nA=\"with spaces\"\nB='single'\nC=plain\nnot a pair\n=novalue\n")
	want := map[string]string{"A": "with spaces", "B": "single", "C": "plain"}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for _, e := range got {
		if want[e.Name] != e.Value {
			t.Errorf("%s = %q, want %q", e.Name, e.Value, want[e.Name])
		}
	}
}

// An empty build block stays empty rather than becoming a stray newline.
func TestAnEmptyBuildBlockIsUnchanged(t *testing.T) {
	if got, _ := resolveBuildEnv("", nil); got != "" {
		t.Errorf("got %q", got)
	}
}
