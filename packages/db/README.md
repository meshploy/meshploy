# packages/db

Shared GORM models and database utilities. Imported by `apps/api` and `apps/proxy` via the Go workspace `replace` directive.

---

## Contents

| File | Purpose |
|---|---|
| `db.go` | `Open()`, `Migrate()`, `FromEnv()`, `RegisterMigration()` |
| `models.go` | All 41 CE table definitions |
| `types.go` | Custom JSONB types: `EnvVarsMap`, `JSONObject`, `StringArray` |
| `crypto.go` | `EncryptedString` — AES-256-GCM GORM type |

---

## Schema

### Identity & Access

| Table | Purpose |
|---|---|
| `users` | Identity |
| `trusted_devices` | Remembered devices — skips 2FA prompt on re-login |
| `recovery_codes` | One-time 2FA recovery codes (hashed) |
| `dismissed_notices` | Console advisories a user has dismissed — per-user, keyed by a stable slug |
| `agent_tokens` | `magt-` tokens for agent principals (SHA-256 hashed, shown once) |
| `installed_licenses` | Enterprise licence tokens activated on this install |
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
| `services` | Polymorphic workload: application or database. `slug` is the Kubernetes object name, fixed at creation and suffixed when the plain name is taken in the project; empty on pre-slug rows, which fall back to the display name |
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
| `config_files` | Files projected into a workload at an absolute path; body is `EncryptedString` and never read back out (project-scoped) |
| `service_config_files` | Service ↔ ConfigFile join |

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

`db.Migrate()` runs GORM `AutoMigrate` for all models, then `applyConstraints()`, which creates the unique indexes GORM cannot express as struct tags and runs a few idempotent data migrations (column cleanups and backfills):

| Index | Constraint |
|---|---|
| `idx_one_owner_per_org` | Exactly one owner per organisation (partial: `WHERE role = 'owner'`) |
| `idx_users_email_unique` | Email unique among humans only (partial: `WHERE email <> ''`); agents carry an empty email |
| `idx_variable_group_service` | At most one system-managed variable group per service (partial) |
| `idx_variable_group_item_key` | Item keys unique within a variable group |
| `idx_service_variable_group` | A service attaches a given group at most once |
| `idx_jobs_project_name` | Job names unique within a project |
| `idx_route_target_path` | One path rule per route |
| `idx_resource_permission_grant` | No duplicate permission grants |

Domain names are unique across all organisations through the `uniqueIndex` tag on `domains.base_domain`, so one org cannot claim another's domain.

Migrations run automatically on API startup; no migration CLI is needed.

---

## Encryption

`EncryptedString` is a custom GORM type that transparently encrypts on write and decrypts on read using AES-256-GCM. Call `db.SetEncryptionKey(key)` before any DB operation — the key must be exactly 32 characters.

Fields using this type (registry credentials, storage keys, git tokens) are stored as base64-encoded ciphertext and are never readable as plaintext in the database.

---

## Extension Registry

`db.RegisterMigration(fn)` registers additional schema migrations that run after `AutoMigrate` and `applyConstraints`. Call it from any package's `init()` to extend the schema without modifying `packages/db` directly.

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
