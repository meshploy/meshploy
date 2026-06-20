# packages/db

Shared GORM models and database utilities. Imported by `apps/api` and `apps/proxy` via the Go workspace `replace` directive.

---

## Contents

| File | Purpose |
|---|---|
| `db.go` | `Open()`, `Migrate()`, `FromEnv()`, `RegisterMigration()` (EE hook) |
| `models.go` | All 36 CE table definitions |
| `types.go` | Custom JSONB types: `EnvVarsMap`, `JSONObject`, `StringArray` |
| `crypto.go` | `EncryptedString` — AES-256-GCM GORM type |

---

## Schema (36 CE tables)

### Identity & Access

| Table | Purpose |
|---|---|
| `users` | Identity |
| `trusted_devices` | Remembered devices — skips 2FA prompt on re-login |
| `recovery_codes` | One-time 2FA recovery codes (hashed) |
| `organizations` | Tenancy root |
| `organization_members` | User ↔ Org join (roles: owner / admin / member) |
| `resource_permissions` | Per-resource ACL grants (service, stack, job, project) |
| `org_invitations` | Email invitations to join an org |

### Projects & Infrastructure

| Table | Purpose |
|---|---|
| `projects` | K8s namespace — slug becomes the namespace name |
| `nodes` | Mesh worker nodes + K3s + Headscale metadata |
| `node_registration_tokens` | `mreg-<hex>` tokens for legacy worker self-registration |
| `node_provisioning_tokens` | `mprov-<hex>` single-use provisioning tokens (hashed, with expiry) |
| `domains` | Custom domains attached to an org |

### Workloads

| Table | Purpose |
|---|---|
| `stacks` | Docker Compose stacks — parsed spec + services |
| `services` | Polymorphic workload: application or database |
| `service_ports` | Exposed ports per service |
| `build_configs` | Git source, builder type, registry target (1:1 with service) |
| `database_configs` | Engine, version, storage size (1:1 with service) |
| `volumes` | Persistent volumes |
| `volume_mounts` | Volume ↔ Service mount (path + read-only flag) |
| `volume_backup_configs` | Backup schedule for individual volumes |

### Variable Groups

| Table | Purpose |
|---|---|
| `variable_groups` | Named collections of key/value variables (project-scoped) |
| `variable_group_items` | Individual variable items within a group |
| `service_variable_groups` | Service ↔ VariableGroup join |

### Traffic

| Table | Purpose |
|---|---|
| `routes` | Hostname → service routing rule |
| `route_targets` | Target service + path-strip config per route |

### Deployment History

| Table | Purpose |
|---|---|
| `deployments` | Deployment history + K8s artefacts + build log |

### Jobs & Cron

| Table | Purpose |
|---|---|
| `jobs` | Job definition (image, command, schedule, concurrency) |
| `job_runs` | Individual run records (status, logs, started/finished at) |

### Integrations

| Table | Purpose |
|---|---|
| `storage_integrations` | S3-compatible storage credentials (org-scoped) |
| `registry_integrations` | Container registry credentials (org-scoped) |
| `git_integrations` | Git provider connections (GitHub App, GitLab OAuth, Gitea OAuth) |

### Operations

| Table | Purpose |
|---|---|
| `backup_configs` | Scheduled DB backup config (service-scoped) |
| `system_backup_configs` | Org-wide system backup config |
| `notification_channels` | Slack / Discord / webhook / email event routing |
| `org_email_configs` | SMTP credentials per org |

### Templates

| Table | Purpose |
|---|---|
| `templates` | 1-click deployment blueprints (official + user-created) |

---

## Migrations

`db.Migrate()` runs GORM `AutoMigrate` for all models followed by `applyConstraints()` which creates partial unique indexes that GORM cannot express as struct tags:

| Index | Constraint |
|---|---|
| `idx_one_owner_per_org` | Exactly one owner per organisation |
| `idx_unique_domain_per_org` | Domain names unique within an org |

Migrations run automatically on API startup — no migration CLI needed.

---

## Encryption

`EncryptedString` is a custom GORM type that transparently encrypts on write and decrypts on read using AES-256-GCM. Call `db.SetEncryptionKey(key)` before any DB operation — the key must be exactly 32 characters.

Fields using this type (registry credentials, storage keys, git tokens) are stored as base64-encoded ciphertext and are never readable as plaintext in the database.

---

## Open-core CE/EE boundary

`db.RegisterMigration(fn)` allows the EE module to register additional schema migrations via Go's `init()` side-effect import pattern. The CE binary never imports the EE module so `eeHooks` remains empty in all CE builds. The hook runs after `AutoMigrate` and `applyConstraints`.

---

## Usage

```go
import dbpkg "github.com/meshploy/packages/db"

// Open from DATABASE_URL env var
db, err := dbpkg.FromEnv()

// Or explicit DSN
db, err := dbpkg.Open(dsn)

// Run migrations
dbpkg.Migrate(db)

// Set encryption key before any encrypted field access
dbpkg.SetEncryptionKey(os.Getenv("ENCRYPTION_KEY"))
```
