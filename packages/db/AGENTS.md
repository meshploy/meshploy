# packages/db — Agent rules

Shared GORM + PostgreSQL data layer. Imported by `apps/api` via Go Workspaces `replace` directive.

## Package structure

| File | Purpose |
|---|---|
| `db.go` | `Open()`, `Migrate()`, Extensible Migration Registry |
| `models.go` | All 18 CE GORM models + enum constants |
| `types.go` | Custom JSONB types: `EnvVarsMap`, `JSONObject`, `StringArray` |
| `crypto.go` | `EncryptedString` GORM type (AES-256-GCM), `SetEncryptionKey()` |

## Adding a new model

1. Define the struct in `models.go` — embed `Base`, use `uuid.UUID` PK.
2. Add it to `AutoMigrate(...)` in `db.go`.
3. If the model needs a partial unique index or constraint that GORM can't express via struct tags, add it to `applyConstraints()` in `db.go`.
4. Run `AutoMigrate` — it will update the schema automatically.

## Encryption

Use `EncryptedString` for any field that must not be stored in plaintext (tokens, passwords, keys):

```go
type MyModel struct {
    Base
    Token EncryptedString `gorm:"not null" json:"-"`
}
```

`json:"-"` is mandatory — encrypted values must never be serialized to API responses.

## Custom JSONB types

- `EnvVarsMap` — `map[string]string`, for env vars.
- `JSONObject` — `map[string]any`, for freeform config blobs (labels, notification configs, manifests).
- `StringArray` — `[]string`, for event lists.

All implement `driver.Valuer` and `sql.Scanner` for Postgres JSONB round-tripping.

## Open-core CE/EE boundary

`RegisterMigration(fn)` is the only extension point. Call it from an EE module's `init()`:

```go
// In EE module — NEVER import this from CE code
func init() {
    db.RegisterMigration(func(d *gorm.DB) error {
        return d.AutoMigrate(&MyEEModel{})
    })
}
```

The CE binary never imports the EE module. `eeHooks` stays nil in CE builds.

## Rules

- Never use SQLite — this package is Postgres-only.
- All PKs are `uuid.UUID` with `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`.
- All models embed `Base` (ID, CreatedAt, UpdatedAt, DeletedAt with soft-delete).
- Never write raw SQL directly in models — put it in `applyConstraints()` in `db.go`.
