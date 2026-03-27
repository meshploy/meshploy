# Meshploy — Monorepo Rules & Architecture

Zero-trust Internal Developer Platform. Go Workspaces monorepo + Next.js frontend deployed via Docker Compose.

---

## Repository layout

```
meshploy/
├── apps/
│   ├── api/          # Chi + Huma REST API (Go, OpenAPI 3.1)
│   └── web/          # Next.js 16 App Router frontend
├── packages/
│   └── db/           # Shared GORM + Postgres models (imported by api)
├── deploy/           # Headscale, CoreDNS, Docker Compose infra
├── go.work           # Go Workspaces: ties apps/api + packages/db
└── .env              # Local secrets (never committed)
```

---

## Architecture overview

- **apps/api** — Chi router + Huma (OpenAPI 3.1) REST API. All business logic lives in `internal/service/`, HTTP concerns in `internal/handler/`. Config loaded from env via `internal/config/`.
- **packages/db** — Shared GORM models backed by **PostgreSQL**. `AutoMigrate` + supplementary partial unique indexes run on API startup via `db.Migrate()`. Exports an Extensible Migration Registry (`RegisterMigration`) for the EE open-core pattern.
- **apps/web** — Vite + React 19 + TanStack Router frontend. Dark-only, Tailwind CSS v4 (CSS-first via `@tailwindcss/vite`, no config file), shadcn/ui Nova preset, `@base-ui/react` primitives.
- **deploy/** — Headscale (WireGuard mesh), CoreDNS, Docker Compose. The gateway node is the only public-internet-facing machine; all workers are dark.

### Zero-trust routing
The reverse proxy (future `apps/proxy`) reads the `Host` header → DB lookup for `routes.hostname` → Headscale mesh IP + port → streams over WireGuard mesh.

### K3s cluster
Single K3s cluster spanning all mesh nodes. Control plane on gateway (`k3s_role=server`), workers join as agents. Builds run as ephemeral K8s Jobs with `meshploy.com/role=builder` node selector.

---

## Go workspace

`go.work` uses `replace` so `apps/api` resolves `packages/db` from the local filesystem. When adding new local modules, add them to `go.work` — do **not** use pseudo-versions.

```
# apps/api/go.mod
replace github.com/meshploy/packages/db => ../../packages/db
```

---

## Dev commands

```bash
# API
cd apps/api && go run main.go

# Web (Vite dev server + auto-generates TanStack Router route tree)
cd apps/web && npm run dev

# Infra (Headscale + CoreDNS)
cd deploy && docker compose -f docker-compose.dev.yml up -d
```

Database migrations run automatically when the API starts.

---

## Environment variables

Required in `.env` at the monorepo root:

| Variable | Description |
|---|---|
| `DATABASE_URL` | `postgres://user:pass@host:5432/db?sslmode=disable` |
| `API_PORT` | API listen port (default: `4000`) |
| `JWT_SECRET` | Long random string for JWT signing |
| `ENCRYPTION_KEY` | Exactly 32 characters — used for AES-256-GCM at-rest encryption |
| `HEADSCALE_URL` | Headscale server URL (optional for dev) |
| `HEADSCALE_API_KEY` | Headscale API key (optional for dev) |

---

## packages/db — schema (18 CE tables)

| Table | Purpose |
|---|---|
| `users` | Identity |
| `organizations` | Tenancy root |
| `organization_members` | User ↔ Org join (roles: owner/admin/member) |
| `resource_permissions` | Per-resource ACL (service, route) |
| `projects` | K8s namespace (slug = namespace name) |
| `nodes` | Mesh worker nodes + K3s metadata |
| `secrets` | AES-encrypted project-scoped secrets |
| `service_secrets` | Service ↔ Secret join (mirrors `secretKeyRef`) |
| `services` | Polymorphic workload: application or database |
| `build_configs` | Git source, builder type, registry target (1:1 with service) |
| `database_configs` | Engine, version, storage (1:1 with service) |
| `routes` | Hostname → mesh IP + port (proxy hot-path) |
| `deployments` | Deployment history + K8s artefacts + log |
| `storage_integrations` | S3-compatible storage credentials (org-scoped) |
| `registry_integrations` | Container registry credentials (org-scoped) |
| `backup_configs` | Scheduled DB backup config |
| `notification_channels` | Slack/webhook/email event routing |
| `templates` | 1-click deployment blueprints (official + user) |

**Partial unique indexes** (in `applyConstraints`):
- `idx_one_owner_per_org` — exactly one owner per org
- `idx_secrets_project_name` — secret names unique within a project
- `idx_service_secrets_env_key` — no duplicate env keys per service

**Encryption**: `EncryptedString` GORM type uses AES-256-GCM. Call `db.SetEncryptionKey()` before any DB operation. Never stored as plaintext.

**Open-core CE/EE boundary**: `db.RegisterMigration(fn)` is called from the EE module's `init()`. The CE binary never imports the EE module so `eeHooks` stays empty in CE builds.

---

## apps/api — internal directory structure

```
internal/
├── config/       # Config struct + Load() from env
├── middleware/   # Auth() — soft JWT middleware (sets user in ctx, doesn't block)
├── handler/      # HTTP layer only — thin, delegates to service layer
│   ├── handler.go     # Handler struct + Register()
│   ├── auth.go        # POST /auth/register, POST /auth/login
│   ├── org.go         # CRUD + member management
│   ├── project.go     # CRUD
│   ├── node.go        # CRUD
│   ├── workload.go    # Service CRUD
│   ├── route.go       # CRUD
│   └── deployment.go  # List + trigger
└── service/      # Business logic
    ├── service.go     # Services aggregate struct
    ├── auth.go        # Register (user + default org in tx), Login (JWT)
    ├── org.go
    ├── project.go
    ├── node.go
    ├── workload.go
    ├── route.go
    └── deployment.go
```

### API routes (all under `/api/v1`)

| Method | Path | Description |
|---|---|---|
| POST | `/auth/register` | Create user + default org (transaction) |
| POST | `/auth/login` | Return signed JWT (24h) |
| GET/POST | `/orgs` | List / create orgs |
| GET/PUT/DELETE | `/orgs/{id}` | Get / update / delete org |
| GET/POST | `/orgs/{id}/members` | List / add members |
| DELETE | `/orgs/{id}/members/{userId}` | Remove member |
| GET/POST | `/projects` | List / create projects |
| GET/PUT/DELETE | `/projects/{id}` | CRUD |
| GET/POST | `/nodes` | List / register nodes |
| GET/PUT/DELETE | `/nodes/{id}` | CRUD |
| GET/POST | `/workloads` | List / create services |
| GET/PUT/DELETE | `/workloads/{id}` | CRUD |
| GET/POST | `/routes` | List / create routes |
| GET/PUT/DELETE | `/routes/{id}` | CRUD |
| GET/POST | `/deployments` | List / trigger deployments |
| GET | `/deployments/{id}` | Get deployment |

All authenticated routes require `Authorization: Bearer <jwt>`. Error responses follow RFC 7807 (Huma built-in).

---

## Coding standards

### Go
- Go 1.22+ syntax.
- **Never write business logic in HTTP handlers** — handler calls service, returns result.
- Use `github.com/google/uuid` for all PKs.
- Use `huma.Error4xx()` helpers for error responses — do not write raw JSON.
- `requireUser(ctx)` in handlers to enforce authentication on protected routes.

### TypeScript / React (Vite + TanStack Router)
- File-based routing in `src/routes/`. Every route file exports `Route = createFileRoute(...)`.
- All components are client-side React — no Server Components, no `'use client'` directives needed.
- Tailwind v4 via `@tailwindcss/vite` plugin — **no tailwind.config file**. Tokens in `src/index.css`.
- shadcn/ui components use `@base-ui/react` (not Radix UI). See `apps/web/AGENTS.md` for breaking changes.
- Shared types in `src/types/index.ts`. Mock data in `src/lib/mock-data.ts`.
- Global state (org switching) via Zustand in `src/store/`.

---

## Safety guardrails

- **NEVER** modify or delete files inside `deploy/headscale/data/`.
- **NEVER** commit `.db`, `.db-shm`, `.db-wal`, or `.env` files.
- **NEVER** write raw SQL in application code — use GORM or `applyConstraints()` in `packages/db/db.go`.
- **NEVER** store secrets as plaintext — use `EncryptedString` GORM type.
