# Stack Feature — Full Docker Compose Support via compose-go

## Why we're doing this

The current stack parser is hand-rolled `yaml.v3`. It cannot handle the real-world Docker Compose files we need to support:

- **YAML anchors + merge keys** (`&common-env`, `*common-env`, `<<: [*db-env, *redis-env]`) — `yaml.v3` cannot merge across `x-` fragments
- **`${VAR:-default}` interpolation** — no substitution at all today
- **`depends_on` with conditions** — ignored, services created in undefined map iteration order
- **Named volumes** (`volumes:` section + per-service mounts) — completely ignored
- **Native `build.args`** — ignored (only x-meshploy build is parsed)

Replacing the parser with `github.com/compose-spec/compose-go/v2` (the same library Docker Compose CLI uses) fixes all of the above at once.

---

## Target compose format

Full Compose spec 3.9, including:

```yaml
version: '3.9'

x-common-env: &common-env
  NODE_ENV: production
  JWT_SECRET: ${JWT_SECRET}

x-db-env: &db-env
  <<: *common-env
  DATABASE_URL: postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}

services:
  postgres:
    image: postgres:16-alpine
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-admin}"]
      interval: 5s
      timeout: 5s
      retries: 10

  auth-service:
    build:
      context: .
      dockerfile: Dockerfile.service
      args:
        SERVICE_PATH: services/auth-service
    environment:
      <<: [*db-env]
      PORT: "3001"
    depends_on:
      postgres: { condition: service_healthy }

volumes:
  postgres_data:
  redis_data:
```

---

## Architecture

### Variable interpolation (`${VAR:-default}`)

The `Stack` model gains a `Variables JSONObject` field (persisted as `jsonb`). On apply, the caller can pass one-shot `env_overrides` too. These are merged into a `map[string]string` and injected into compose-go's `ConfigDetails.Environment` — compose-go does the full `${VAR}`, `${VAR:-default}`, `${VAR:?error}` resolution from there.

```
stack.Variables  +  apply.EnvOverrides  →  envMap  →  compose-go interpolation
```

### Named volumes → PVCs

The compose `volumes:` top-level section maps to Meshploy `Volume` records (backed by K8s PVCs). Per-service volume mounts (`volumes: - postgres_data:/var/lib/postgresql/data`) become `VolumeMount` records via `VolumeService.Attach()`. Both the `Volume` and `VolumeMount` schema are already in place.

Volume naming: `{stackName}-{composeVolumeName}` to avoid cross-stack collisions within a project. Idempotent: lookup by `project_id + name` before creating.

VolumeMount idempotency: query for existing `(volume_id, service_id, mount_path)` before calling `Attach`.

### `depends_on` ordering

Kahn's topological sort over the service dependency graph before creating services. Ensures infra services (postgres, redis) are created before the app services that depend on them — matters for DB seeding and deployment ordering.

### `x-meshploy` extension

Still supported on top of compose-go. compose-go stores unknown `x-` keys in `service.Extensions["x-meshploy"]` as `map[string]interface{}`. We decode it into the existing `meshployExt` struct using `json.Marshal` + `json.Unmarshal`. Used for Meshploy-specific config not expressible in standard compose: `node`, `resource limits`, `builder`, `source`, `rollback`.

### Native `build.args`

`types.BuildConfig.Args` is `map[string]*string`. These are converted to `KEY=VALUE\n` and stored in `BuildConfig.BuildEnvVars` (field already exists).

---

## Files to change

### `packages/db/models.go`

Add `Variables` to `Stack`:

```go
type Stack struct {
    Base
    ProjectID     uuid.UUID   `gorm:"type:uuid;not null;index"                      json:"project_id"`
    Name          string      `gorm:"not null"                                      json:"name"`
    Spec          string      `gorm:"type:text;not null;default:''"                 json:"spec"`
    Variables     JSONObject  `gorm:"type:jsonb;not null;default:'{}'"              json:"variables"`
    Status        StackStatus `gorm:"type:varchar(10);not null;default:'idle'"      json:"status"`
    LastAppliedAt *time.Time  `json:"last_applied_at"`

    Project  Project   `gorm:"foreignKey:ProjectID"                            json:"-"`
    Services []Service `gorm:"foreignKey:StackID;constraint:OnDelete:SET NULL" json:"-"`
}
```

AutoMigrate picks this up automatically — no manual migration needed.

---

### `apps/api/go.mod`

Add:
```
github.com/compose-spec/compose-go/v2 vX.X.X
```

Run: `go get github.com/compose-spec/compose-go/v2@latest`

---

### `apps/api/internal/service/service.go`

Create `VolumeService` before the `svc` struct so it can be wired into `StackService`:

```go
volumes := &VolumeService{db: db, k8s: k8sClient, deployment: deployments}

svc := &Services{
    ...
    Stacks:  &StackService{db: db, workload: workloads, volumes: volumes},
    Volumes: volumes,
    ...
}
```

---

### `apps/api/internal/service/stack.go`

Complete rewrite of `Apply`. CRUD methods are unchanged.

**`StackService` struct:**
```go
type StackService struct {
    db       *gorm.DB
    workload *WorkloadService
    volumes  *VolumeService   // NEW — for PVC management
}
```

**`Apply` signature change:**
```go
func (s *StackService) Apply(
    ctx context.Context,
    stackID uuid.UUID,
    triggeredBy uuid.UUID,
    envOverrides map[string]string,  // NEW — one-shot variable values
) (*ApplyResult, error)
```

**`Apply` logic:**
1. Load stack from DB, mark `applying`
2. Build `envMap` from `stack.Variables` + `envOverrides`
3. Call `loader.Load(ConfigDetails{ConfigFiles: [{Content: spec}], Environment: envMap})` with `SkipValidation: true`
4. Process top-level `project.Volumes` → create/find `Volume` records (PVCs)
5. Topological sort `project.Services` respecting `depends_on`
6. For each service in order: create/update `Service` + `BuildConfig` records
7. For each service's volume mounts: attach volumes
8. Unlink services no longer in spec
9. Mark `idle` or `failed`

**Removed:** all hand-rolled compose types (`composeSpec`, `composeService`, `composeEnv`, `composeHealthcheck`). These are replaced by `composetypes.ServiceConfig`, `composetypes.HealthCheckConfig`, etc.

**Kept:** `meshployExt`, `meshploySource`, `meshployBuild`, `meshployDeploy`, `meshployRollback`, `meshployDatabase` — still decoded from `service.Extensions["x-meshploy"]`.

**New helpers:**
- `topoSortServices(services map[string]composetypes.ServiceConfig) []string` — Kahn's topo sort
- `healthcheckFromCompose(hc *composetypes.HealthCheckConfig) (cmd string, interval, timeout, retries, startPeriod int32)`
- `envFromMapping(m composetypes.MappingWithEquals) string` — converts `map[string]*string` to `KEY=VALUE\n` string
- `buildArgsToEnvStr(args composetypes.MappingWithEquals) string` — same for build args

**Volume handling in Apply:**
```go
// 1. Resolve named volumes
volumesByName := map[string]*meshdb.Volume{}
for volName := range project.Volumes {
    storedName := stack.Name + "-" + volName
    var vol meshdb.Volume
    if s.db.Where("project_id = ? AND name = ?", stack.ProjectID, storedName).First(&vol).Error != nil {
        created, err := s.volumes.Create(ctx, stack.ProjectID, storedName, 5)
        if err != nil { /* log */ continue }
        vol = *created
    }
    volumesByName[volName] = &vol
}

// 2. After creating each service, attach its mounts
for _, mount := range svc.Volumes {
    if mount.Type != "volume" || mount.Source == "" { continue }
    vol, ok := volumesByName[mount.Source]
    if !ok { continue }
    var existing meshdb.VolumeMount
    if s.db.Where("volume_id = ? AND service_id = ?", vol.ID, createdSvc.ID).First(&existing).Error != nil {
        s.volumes.Attach(ctx, vol.ID, createdSvc.ID, mount.Target)
    }
}
```

---

### `apps/api/internal/handler/stack.go`

**`CreateStackBody` and `UpdateStackBody`** — add `Variables`:
```go
type CreateStackBody struct {
    Name      string            `json:"name"`
    Spec      string            `json:"spec"`
    Variables map[string]string `json:"variables,omitempty"`
}
```

**`ApplyStackInput`** — add body with env overrides:
```go
type ApplyStackBody struct {
    EnvOverrides map[string]string `json:"env_overrides,omitempty"`
}

type ApplyStackInput struct {
    OrgID     string `path:"orgId"`
    ProjectID string `path:"projectId"`
    StackID   string `path:"stackId"`
    Body      ApplyStackBody
}
```

**`ApplyStack` handler** — pass `EnvOverrides` to service:
```go
result, err := h.svc.Stacks.Apply(ctx, stackID, userID, input.Body.EnvOverrides)
```

---

## Deferred / Out of scope for now

- **Stacks NetworkPolicy**: when NetworkPolicy is implemented, services sharing a `StackID` must auto-allow intra-stack traffic. Noted in `plans/network-policy.md`.
- **`bind` volume mounts**: compose bind mounts (`type: bind`) have no equivalent in K8s without hostPath — skip silently on K8s, log a warning.
- **`networks:`**: compose networks are no-ops in Meshploy's K8s model (all pods in the same namespace share a flat network). Parse and ignore.
- **`deploy.resources`** (standard compose deploy limits): compose-go parses `deploy.resources.limits.cpus` / `.memory`. These overlap with `x-meshploy.deploy` limits — pick `x-meshploy` if present, fall back to compose `deploy.resources` values if not.
- **`profiles:`**: not needed for initial implementation — `loader.WithProfiles(nil)` (no filtering).
- **Multiple compose files / `extends:`**: single-file only for now.
- **Stack-level variable UI**: the frontend needs a variables editor on the stack detail page — separate frontend task.
