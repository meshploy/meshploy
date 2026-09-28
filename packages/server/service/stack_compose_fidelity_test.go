package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A monorepo compose file builds each service from the Dockerfile and build
// args it names, not a guess at the repository's root, and a service answers
// to its container_name and network aliases as well as its name. Changing the
// file changes both on the next apply.
func TestStackKeepsComposeBuildsAndNames(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "mono", Email: "mono@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, db.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "trips", "trips")
	require.NoError(t, err)

	spec := func(path string) string {
		return `
services:
  auth-service:
    build:
      context: .
      dockerfile: Dockerfile.service
      args:
        SERVICE_PATH: ` + path + `
  ui:
    build: ./ui
  migrator:
    image: postgres:16-alpine
    restart: "no"
  topic-init:
    image: redpandadata/redpanda:v24.2.7
    restart: on-failure:2
  s1:
    image: alpine
    depends_on:
      migrator: {condition: service_completed_successfully}
      topic-init: {condition: service_completed_successfully}
      redpanda: {condition: service_started}
  redpanda:
    image: redpandadata/redpanda:v24.2.7
    container_name: pi-redpanda
    networks:
      backend:
        aliases: [broker, pi-redpanda]
networks:
  backend: {}
`
	}
	r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "trips", Spec: spec("services/auth-service")}, user.ID)
	require.NoError(t, err)
	require.Empty(t, r.Errors)
	for _, w := range r.Warnings {
		assert.NotContains(t, w, "starts alongside", "a rollout follows depends_on, so nothing starts alongside what it waits for")
	}

	once := func(name string) (bool, int32) {
		t.Helper()
		var svc meshdb.Service
		require.NoError(t, db.Where("project_id = ? AND name = ?", proj.ID, name).First(&svc).Error)
		return svc.RunOnce, svc.RunRetries
	}
	ok, retries := once("migrator")
	assert.True(t, ok, "waited on to complete: runs once")
	assert.Equal(t, int32(0), retries)
	ok, retries = once("topic-init")
	assert.True(t, ok)
	assert.Equal(t, int32(2), retries, "on-failure:2")
	ok, _ = once("redpanda")
	assert.False(t, ok, "waited on only to start: kept up")

	build := func(name string) meshdb.BuildConfig {
		t.Helper()
		var bc meshdb.BuildConfig
		require.NoError(t, db.Joins("JOIN services ON services.id = build_configs.service_id").
			Where("services.project_id = ? AND services.name = ?", proj.ID, name).First(&bc).Error)
		return bc
	}
	auth := build("auth-service")
	assert.Equal(t, meshdb.BuilderDockerfile, auth.Builder)
	assert.Equal(t, "Dockerfile.service", auth.DockerfilePath)
	assert.Contains(t, []string{"", "."}, auth.RootDir, "the repository root")
	assert.Equal(t, "services/auth-service", auth.BuildArgs["SERVICE_PATH"])
	ui := build("ui")
	assert.Equal(t, meshdb.BuilderDockerfile, ui.Builder)
	assert.Equal(t, "Dockerfile", ui.DockerfilePath)
	assert.Equal(t, "ui", ui.RootDir)

	var rp meshdb.Service
	require.NoError(t, db.Where("project_id = ? AND name = ?", proj.ID, "redpanda").First(&rp).Error)
	assert.Equal(t, []string{"pi-redpanda", "broker"}, []string(rp.DNSAliases))

	_, err = svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "trips", Spec: spec("services/auth-v2")}, user.ID)
	require.NoError(t, err)
	assert.Equal(t, "services/auth-v2", build("auth-service").BuildArgs["SERVICE_PATH"])
}

// Bind mounts of the repository's own files arrive where compose put them: a
// directory as one config file per file, a single file as itself. A path on
// the machine that ran compose is left out, and the apply says why.
func TestStackCarriesRepositoryBindMounts(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "binds", Email: "binds@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, db.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "pi", "pi")
	require.NoError(t, err)

	r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "pi", Spec: `
services:
  migrator:
    image: postgres:16-alpine
    entrypoint: [sh, /scripts/migrate.sh]
    volumes:
      - ./migrations:/migrations:ro
      - ./scripts:/scripts:ro
      - /var/run/docker.sock:/var/run/docker.sock
`, Files: map[string]string{
		"migrations/001_init.sql": "create table a (id int);",
		"migrations/002_more.sql": "create table b (id int);",
		"scripts/migrate.sh":      "#!/bin/sh\nfor f in /migrations/*.sql; do psql -f $f; done\n",
		"scripts/seed/migrate.sh": "echo seed",
	}}, user.ID)
	require.NoError(t, err)
	require.Empty(t, r.Errors)

	var files []meshdb.ConfigFile
	require.NoError(t, db.Where("project_id = ?", proj.ID).Order("path").Find(&files).Error)
	var paths []string
	names := map[string]bool{}
	for _, f := range files {
		paths = append(paths, f.Path)
		names[f.Name] = true
	}
	assert.Equal(t, []string{"/migrations/001_init.sql", "/migrations/002_more.sql", "/scripts/migrate.sh", "/scripts/seed/migrate.sh"}, paths)
	assert.Len(t, names, 4, "two migrate.sh files, two names")
	assert.Contains(t, r.Warnings, "migrator: bind mount of /var/run/docker.sock at /var/run/docker.sock left out: it is a path on the machine that runs compose; use configs: for a file, or a named volume")
}

// A stack runs with the resources its compose file gives Docker, and keeps
// what an operator (or a migration) set where the file says nothing, rather
// than going back to Meshploy's defaults on the next apply.
func TestStackTakesComposeResourcesAndKeepsOthers(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "res", Email: "res@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, db.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "res", "res")
	require.NoError(t, err)

	spec := `
services:
  broker:
    image: redpandadata/redpanda
    mem_limit: 2g
    cpus: 1.5
  worker:
    image: alpine
    deploy:
      resources:
        limits: {memory: 256M}
        reservations: {cpus: "0.25", memory: 64M}
  web:
    image: nginx
`
	apply := func() {
		t.Helper()
		r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, service.ManifestInput{Name: "res", Spec: spec}, user.ID,
			service.ApplyOptions{NoDeploy: true})
		require.NoError(t, err)
		require.Empty(t, r.Errors)
	}
	get := func(name string) meshdb.Service {
		t.Helper()
		var svc meshdb.Service
		require.NoError(t, db.Where("project_id = ? AND name = ?", proj.ID, name).First(&svc).Error)
		return svc
	}
	apply()
	assert.Equal(t, "2147483648", get("broker").MemoryLimit)
	assert.Equal(t, "1500m", get("broker").CPULimit)
	w := get("worker")
	assert.Equal(t, "268435456", w.MemoryLimit)
	assert.Equal(t, "250m", w.CPURequest)
	assert.Equal(t, "67108864", w.MemoryRequest)
	assert.Equal(t, "1Gi", get("web").MemoryLimit, "nothing declared: Meshploy's default")

	// A git stack's file is applied as written, with nothing filled in: a
	// limit it does not declare - set in the console, or carried from the old
	// platform by a migration - survives the next sync.
	stack := meshdb.Stack{ProjectID: proj.ID, Name: "git", Spec: "services:\n  api:\n    image: nginx\n",
		GitMode: meshdb.StackGitModeRepo, GitRepo: "https://example.com/acme/api.git", GitBranch: "main"}
	require.NoError(t, db.Create(&stack).Error)
	_, err = svcs.Stacks.Apply(ctx, stack.ID, user.ID, nil, service.ApplyOptions{NoDeploy: true})
	require.NoError(t, err)
	assert.Equal(t, "1Gi", get("api").MemoryLimit)
	require.NoError(t, db.Model(&meshdb.Service{}).Where("project_id = ? AND name = ?", proj.ID, "api").
		Update("memory_limit", "3Gi").Error)
	_, err = svcs.Stacks.Apply(ctx, stack.ID, user.ID, nil, service.ApplyOptions{NoDeploy: true})
	require.NoError(t, err)
	assert.Equal(t, "3Gi", get("api").MemoryLimit, "a limit the file does not declare is kept")
}
