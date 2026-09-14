package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

// appDefaults is what the fill writes for an app service's deploy settings.
func appDefaults(indent, port string) string {
	lines := []string{"replicas: 1", "cpu_request: 100m", "cpu_limit: 1000m", "memory_request: 256Mi", "memory_limit: 1Gi"}
	if port != "" {
		lines = append([]string{"port: " + port}, lines...)
	}
	return indent + strings.Join(lines, "\n"+indent) + "\n"
}

// The ProcureFlow infra stack's shape: ports from expose: and ports:, a partial
// block, comments, blank lines and a config file all come through as written.
func TestFillMeshployDefaultsKeepsTheFileAsWritten(t *testing.T) {
	spec := `# Infra stack.

services:
  # Pools connections.
  pgbouncer:
    image: edoburu/pgbouncer:latest
    environment:
      DB_USER: ${POSTGRES_USER:?set it}
    expose:
      - '6432'

  # No password.
  redis:
    image: redis:7-alpine
    expose:
    - '6379'

  minio:
    image: minio/minio:latest
    expose:
      - '9000' # S3 API
      - '9001' # web console

  keycloak:
    image: quay.io/keycloak/keycloak:26.0
    ports:
      - '8080:8080'
    x-meshploy:
      deploy:
        port: 8080
        memory_limit: 2Gi

configs:
  healthcheck:
    content: |
      #!/bin/bash
      echo "$${HOME}"
`
	want := `# Infra stack.

services:
  # Pools connections.
  pgbouncer:
    image: edoburu/pgbouncer:latest
    environment:
      DB_USER: ${POSTGRES_USER:?set it}
    expose:
      - '6432'
    x-meshploy:
      deploy:
` + appDefaults("        ", "6432") + `
  # No password.
  redis:
    image: redis:7-alpine
    expose:
    - '6379'
    x-meshploy:
      deploy:
` + appDefaults("        ", "6379") + `
  minio:
    image: minio/minio:latest
    expose:
      - '9000' # S3 API
      - '9001' # web console
    x-meshploy:
      deploy:
` + appDefaults("        ", "9000") + `
  keycloak:
    image: quay.io/keycloak/keycloak:26.0
    ports:
      - '8080:8080'
    x-meshploy:
      deploy:
        port: 8080
        memory_limit: 2Gi
        replicas: 1
        cpu_request: 100m
        cpu_limit: 1000m
        memory_request: 256Mi

configs:
  healthcheck:
    content: |
      #!/bin/bash
      echo "$${HOME}"
`
	got, err := service.FillMeshployDefaults(spec)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	again, err := service.FillMeshployDefaults(got)
	require.NoError(t, err)
	assert.Equal(t, got, again, "a filled spec has nothing left to fill")
}

// A database gets the settings apply defaults a database to, and a git service
// its source and build settings, added to the sections already there.
func TestFillMeshployDefaultsForDatabasesAndGitServices(t *testing.T) {
	got, err := service.FillMeshployDefaults(`services:
  cache:
    image: redis:7
    x-meshploy:
      type: database
      database:
        engine: redis
  web:
    x-meshploy:
      source:
        git: https://github.com/acme/web
      deploy:
        port: 3000
`)
	require.NoError(t, err)
	assert.Equal(t, `services:
  cache:
    image: redis:7
    x-meshploy:
      type: database
      database:
        engine: redis
        version: "7"
        storage_gb: 10
      deploy:
        replicas: 1
  web:
    x-meshploy:
      source:
        git: https://github.com/acme/web
        branch: main
      deploy:
        port: 3000
        replicas: 1
        cpu_request: 100m
        cpu_limit: 1000m
        memory_request: 256Mi
        memory_limit: 1Gi
      build:
        builder: nixpacks
        builder_cpu_request: 1000m
        builder_memory_request: 1Gi
`, got, "a version is quoted: unquoted, 7 is a number and the block would not decode")
}

// The primary port is the one apply picks: the first public port, a loopback
// binding is internal, and long syntax counts. A port behind a variable, or no
// port at all, is left to apply.
func TestFillMeshployDefaultsPicksThePortApplyPicks(t *testing.T) {
	got, err := service.FillMeshployDefaults(`services:
  api:
    image: api
    ports: ["127.0.0.1:5432:5432", "8080:8080"]
  s3:
    image: minio
    ports:
      - target: 9000
        published: "9000"
  web:
    image: nginx
    ports:
      - "${WEB_PORT}:80"
  worker:
    image: alpine
`)
	require.NoError(t, err)
	var doc struct {
		Services map[string]struct {
			XMeshploy struct {
				Deploy map[string]any `yaml:"deploy"`
			} `yaml:"x-meshploy"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(got), &doc))
	assert.Equal(t, 8080, doc.Services["api"].XMeshploy.Deploy["port"])
	assert.Equal(t, 9000, doc.Services["s3"].XMeshploy.Deploy["port"])
	for _, name := range []string{"web", "worker"} {
		assert.NotContains(t, doc.Services[name].XMeshploy.Deploy, "port", name)
		assert.Equal(t, "1Gi", doc.Services[name].XMeshploy.Deploy["memory_limit"], name)
	}
}

// What cannot be read without resolving the file is left for apply to default.
func TestFillMeshployDefaultsLeavesWhatItCannotRead(t *testing.T) {
	for name, spec := range map[string]string{
		"flow-style block": "services:\n  web:\n    image: nginx\n    x-meshploy: {deploy: {port: 80}}\n",
		"merge key":        "x-base: &base\n  image: nginx\nservices:\n  web:\n    <<: *base\n",
		"extends":          "services:\n  web:\n    extends:\n      file: base.yml\n      service: web\n",
		"anchored service": "services:\n  web: &web\n    image: nginx\n  web2: *web\n",
		"no services":      "volumes:\n  data: {}\n",
		"empty":            "",
	} {
		got, err := service.FillMeshployDefaults(spec)
		require.NoError(t, err, name)
		assert.Equal(t, spec, got, name)
	}

	broken := "services: [\n"
	got, err := service.FillMeshployDefaults(broken)
	require.Error(t, err)
	assert.Equal(t, broken, got, "broken YAML comes back as it was")
}

// Indentation and line ends follow the file.
func TestFillMeshployDefaultsFollowsIndentationAndLineEnds(t *testing.T) {
	got, err := service.FillMeshployDefaults("services:\r\n    web:\r\n        image: nginx\r\n")
	require.NoError(t, err)
	want := "services:\r\n    web:\r\n        image: nginx\r\n        x-meshploy:\r\n            deploy:\r\n" +
		strings.ReplaceAll(appDefaults(strings.Repeat(" ", 16), ""), "\n", "\r\n")
	assert.Equal(t, want, got)
}

// Applying a file and applying it filled produce the same services: the fill
// only writes down what apply would do anyway.
func TestFilledSpecAppliesTheSame(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "filler", Email: "filler@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, gdb.Where("slug = ?", user.Username).First(&org).Error)

	raw := `
services:
  pgbouncer:
    image: edoburu/pgbouncer
    expose: ["6432"]
  web:
    image: nginx
    ports: ["127.0.0.1:9090:9090", "8080:80"]
  worker:
    image: alpine
  cache:
    image: redis:7
    x-meshploy:
      type: database
      database:
        engine: redis
`
	filled, err := service.FillMeshployDefaults(raw)
	require.NoError(t, err)
	require.NotEqual(t, raw, filled)

	// A git stack is applied as stored, so the raw file reaches apply unfilled.
	applyIn := func(project, spec string) uuid.UUID {
		proj, err := svcs.Projects.Create(ctx, org.ID, project, project)
		require.NoError(t, err)
		stack, err := svcs.Stacks.Create(ctx, proj.ID, service.CreateStackInput{
			Name: "infra", Spec: spec, GitMode: meshdb.StackGitModeFile, GitRepo: "https://example.com/infra.git",
		})
		require.NoError(t, err)
		require.Equal(t, spec, stack.Spec, "a git stack is stored as the repo has it")
		r, err := svcs.Stacks.Apply(ctx, stack.ID, user.ID, nil)
		require.NoError(t, err)
		require.Empty(t, r.Errors)
		return proj.ID
	}
	rawProject, filledProject := applyIn("raw", raw), applyIn("filled", filled)

	type port struct {
		Port                  int
		Public, HTTP, Primary bool
	}
	type view struct {
		Type                                             meshdb.ServiceType
		Replicas                                         int
		CPURequest, CPULimit, MemoryRequest, MemoryLimit string
		Ports                                            []port
		Engine                                           meshdb.DatabaseEngine
		Version                                          string
		StorageGB                                        int
	}
	load := func(projectID uuid.UUID, name string) view {
		var svc meshdb.Service
		require.NoError(t, gdb.Preload("Ports", func(tx *gorm.DB) *gorm.DB { return tx.Order("port") }).
			Where("project_id = ? AND name = ?", projectID, name).First(&svc).Error)
		v := view{
			Type: svc.Type, Replicas: svc.Replicas,
			CPURequest: svc.CPURequest, CPULimit: svc.CPULimit, MemoryRequest: svc.MemoryRequest, MemoryLimit: svc.MemoryLimit,
		}
		for _, p := range svc.Ports {
			v.Ports = append(v.Ports, port{p.Port, p.IsPublic, p.IsHTTP, p.IsPrimary})
		}
		var cfg meshdb.DatabaseConfig
		if gdb.Where("service_id = ?", svc.ID).First(&cfg).Error == nil {
			v.Engine, v.Version, v.StorageGB = cfg.Engine, cfg.Version, cfg.StorageGB
		}
		return v
	}
	for _, name := range []string{"pgbouncer", "web", "worker", "cache"} {
		assert.Equal(t, load(rawProject, name), load(filledProject, name), name)
	}
	assert.Equal(t, meshdb.ServiceTypeDatabase, load(filledProject, "cache").Type, "the filled block still decodes")
}

// A pasted stack is stored with its defaults written out, on create, on update,
// and on the first apply of one saved before this; a git stack is not touched.
func TestPastedStacksAreStoredFilled(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "paster", Email: "paster@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, gdb.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "pasted", "pasted")
	require.NoError(t, err)

	raw := "services:\n  web:\n    image: nginx\n    expose: [\"80\"]\n"
	filled, err := service.FillMeshployDefaults(raw)
	require.NoError(t, err)
	require.Contains(t, filled, "x-meshploy:")

	stack, err := svcs.Stacks.Create(ctx, proj.ID, service.CreateStackInput{Name: "pasted", Spec: raw})
	require.NoError(t, err)
	assert.Equal(t, filled, stack.Spec)

	raw2 := "services:\n  api:\n    image: alpine\n"
	filled2, err := service.FillMeshployDefaults(raw2)
	require.NoError(t, err)
	updated, err := svcs.Stacks.Update(ctx, stack.ID, service.UpdateStackInput{Spec: &raw2})
	require.NoError(t, err)
	assert.Equal(t, filled2, updated.Spec)

	old := meshdb.Stack{ProjectID: proj.ID, Name: "old", Spec: raw}
	require.NoError(t, gdb.Create(&old).Error)
	_, err = svcs.Stacks.Apply(ctx, old.ID, user.ID, nil)
	require.NoError(t, err)
	require.NoError(t, gdb.First(&old, "id = ?", old.ID).Error)
	assert.Equal(t, filled, old.Spec, "applying a stack saved before fills it")

	git, err := svcs.Stacks.Create(ctx, proj.ID, service.CreateStackInput{
		Name: "from-git", Spec: raw, GitMode: meshdb.StackGitModeFile, GitRepo: "https://example.com/infra.git",
	})
	require.NoError(t, err)
	assert.Equal(t, raw, git.Spec)
}
