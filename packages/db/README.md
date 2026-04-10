# packages/db

Shared GORM models and database utilities. Imported by `apps/api` and `apps/proxy` via the Go workspace `replace` directive.

---

## Contents

| File | Purpose |
|---|---|
| `db.go` | `Open()`, `Migrate()`, `FromEnv()`, `RegisterMigration()` (EE hook) |
| `models.go` | All 18 CE table definitions |
| `types.go` | Custom JSONB types: `EnvVarsMap`, `JSONObject`, `StringArray` |
| `crypto.go` | `EncryptedString` — AES-256-GCM GORM type |

---

## Schema (18 CE tables)

| Table | Purpose |
|---|---|
| `users` | Identity |
| `organizations` | Tenancy root |
| `organization_members` | User ↔ Org join (roles: owner / admin / member) |
| `resource_permissions` | Per-resource ACL (service, route) |
| `projects` | K8s namespace — slug becomes the namespace name |
| `nodes` | Mesh worker nodes + K3s + Headscale metadata |
| `node_registration_tokens` | `mreg-<hex>` tokens for worker self-registration |
| `secrets` | AES-encrypted project-scoped secrets |
| `service_secrets` | Service ↔ Secret join (mirrors `secretKeyRef`) |
| `services` | Polymorphic workload: application or database |
| `build_configs` | Git source, builder type, registry target (1:1 with service) |
| `database_configs` | Engine, version, storage size (1:1 with service) |
| `routes` | Hostname → mesh IP + port (proxy hot-path) |
| `deployments` | Deployment history + K8s artefacts + log |
| `storage_integrations` | S3-compatible storage credentials (org-scoped) |
| `registry_integrations` | Container registry credentials (org-scoped) |
| `backup_configs` | Scheduled DB backup configuration |
| `notification_channels` | Slack / webhook / email event routing |
| `templates` | 1-click deployment blueprints (official + user) |

---

## Migrations

`db.Migrate()` runs GORM `AutoMigrate` for all models followed by `applyConstraints()` which creates partial unique indexes that GORM cannot express as struct tags:

| Index | Constraint |
|---|---|
| `idx_one_owner_per_org` | Exactly one owner per organisation |
| `idx_secrets_project_name` | Secret names unique within a project |
| `idx_service_secrets_env_key` | No duplicate env var keys per service |

Migrations run automatically on API startup — no migration CLI needed.

---

## Encryption

`EncryptedString` is a custom GORM type that transparently encrypts on write and decrypts on read using AES-256-GCM. Call `db.SetEncryptionKey(key)` before any DB operation — the key must be exactly 32 characters.

Fields using this type (e.g. registry credentials, storage keys) are stored as base64-encoded ciphertext and are never readable as plaintext in the database.

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
