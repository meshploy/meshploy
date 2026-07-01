# Meshploy — Monorepo Rules & Architecture

Internal Developer Platform. Go Workspaces monorepo + Vite/React frontend deployed via Docker Compose.

---

## Repository layout

```
meshploy/
├── apps/
│   ├── api/          # Chi + Huma REST API (Go, OpenAPI 3.1)
│   ├── proxy/        # Edge reverse proxy — "Ask & Resolve" L7 routing
│   ├── cli/          # Static Go binary — node & cluster management CLI
│   └── web/          # Vite + React 19 + TanStack Router frontend
├── packages/
│   └── db/           # Shared GORM + PostgreSQL models (imported by api and proxy)
├── deploy/           # Headscale, CoreDNS, Docker Compose infra
├── go.work           # Go Workspaces: ties apps/api + apps/proxy + apps/cli + packages/db
└── .env              # Local secrets (never committed)
```

---

## Architecture overview

- **apps/api** — Chi router + Huma (OpenAPI 3.1) REST API. All business logic lives in `internal/service/`, HTTP concerns in `internal/handler/`. Config loaded from env via `internal/config/`.
- **apps/proxy** — Minimal L7 reverse proxy. Reads the `Host` header → in-memory route cache (backed by PostgreSQL, refreshed every 30s) → streams over WireGuard mesh to target node. Listens on port 8081.
- **apps/cli** — Static Go binary (`/usr/local/bin/meshploy`). Wraps API calls and shells out to `install.sh` / `uninstall.sh` for node operations. Built with Cobra.
- **packages/db** — Shared GORM models backed by **PostgreSQL**. `AutoMigrate` + supplementary partial unique indexes run on API startup via `db.Migrate()`. Exports an Extensible Migration Registry (`RegisterMigration`) for the EE open-core pattern. Imported by both `apps/api` and `apps/proxy`.
- **apps/web** — Vite + React 19 + TanStack Router frontend. Dark-only, Tailwind CSS v4 (CSS-first via `@tailwindcss/vite`, no config file), shadcn/ui Nova preset, `@base-ui/react` primitives.
- **deploy/** — Headscale (WireGuard mesh), CoreDNS, Docker Compose. The gateway node is the only public-internet-facing machine; all workers are dark.

### Mesh routing

```
Internet → Caddy (TLS) → apps/proxy (:8081) → WireGuard mesh → K3s worker node
                              ↑
                        reads Host header
                        cache: hostname → (mesh_ip, port)
```

`apps/proxy` reads the `Host` header → route cache lookup → `httputil.ReverseProxy` to `http://<mesh_ip>:<port>`. Caddy's `handle /api/*` block routes API traffic to port 4000; `*.internal.<domain>` goes to port 8081.

### K3s cluster
Single K3s cluster spanning all mesh nodes. Control plane on gateway (`k3s_role=server`), workers join as agents. Builds run as ephemeral K8s Jobs with `meshploy.com/role=builder` node selector.

### Node lifecycle
Workers self-register via `POST /api/v1/nodes/self-register` using an `mreg-<hex>` registration token or a single-use `mprov-<hex>` provisioning token. The node ID is saved to `/etc/meshploy/node.conf`. On uninstall, `DELETE /api/v1/nodes/self-deregister` removes the node from Headscale, the k3s cluster, and the database.

---

## Go workspace

`go.work` uses `replace` so local modules resolve from the filesystem. When adding new local modules, add them to `go.work` — do **not** use pseudo-versions.

```
# apps/api/go.mod
replace github.com/meshploy/packages/db => ../../packages/db
```

---

## Dev commands

```bash
# Start PostgreSQL (only infra needed for local dev)
docker compose -f deploy/docker-compose.dev.yml up -d

# API
cd apps/api && go run main.go

# Proxy
cd apps/proxy && go run main.go

# CLI
cd apps/cli && go build -o meshploy .

# Web (Vite dev server + auto-generates TanStack Router route tree)
cd apps/web && npm run dev
```

Database migrations run automatically when the API starts. Headscale, K3s, CoreDNS, and Caddy are optional for local dev — the API and frontend work without them (mesh/node features are no-ops).

---

## Environment variables

Required in `.env` at the monorepo root:

**Required:**

| Variable | Description |
|---|---|
| `DATABASE_URL` | `postgres://user:pass@host:5432/db?sslmode=disable` |
| `JWT_SECRET` | Long random string for JWT signing |
| `ENCRYPTION_KEY` | Exactly 32 characters — used for AES-256-GCM at-rest encryption |

**Optional (infrastructure — set by `install.sh` on gateway):**

| Variable | Description |
|---|---|
| `API_PORT` | API listen port (default: `4000`) |
| `PROXY_PORT` | Proxy listen port (default: `8081`) |
| `HEADSCALE_URL` | Headscale server URL |
| `HEADSCALE_API_KEY` | Headscale API key |
| `KUBECONFIG` | Path to kubeconfig file (empty = in-cluster) |
| `K3S_SERVER_URL` | Override K3s API server URL (needed when API runs in Docker) |
| `K3S_TOKEN` | Node token for workers joining the cluster |
| `DOMAIN` | Base domain — seeds the org domain record |
| `MESH_IP` | WireGuard IP of the gateway node |
| `PUBLIC_IP` | Public internet IP — backfilled on the gateway node record |
| `GATEWAY_HOSTNAME` | Gateway server hostname |
| `HOST_GATEWAY_IP` | Docker bridge gateway IP — used to reach node_exporter from inside the API container |
| `BUILTIN_REGISTRY_ENDPOINT` | Seeds a built-in registry row per org (format: `<host>:<port>`) |
| `TEMPLATE_DIR` | Local one-click template catalog dir (`<dir>/<id>/...`). Set = offline/air-gapped source; overrides the remote repo |
| `TEMPLATE_REPO` | GitHub `owner/repo` the catalog is fetched from when `TEMPLATE_DIR` is unset (default: `meshploy/meshploy-templates`) |
| `TEMPLATE_REPO_REF` | Git ref for the catalog repo (default: `main`) |
| `TEMPLATE_REFRESH_INTERVAL` | How often the in-memory catalog cache refreshes (Go duration, default: `1h`) |

---

## packages/db — schema (36 CE tables)

Full schema documented in `packages/db/README.md`. Key groups:

| Group | Tables |
|---|---|
| Identity & Access | `users`, `trusted_devices`, `recovery_codes`, `organizations`, `organization_members`, `resource_permissions`, `org_invitations` |
| Projects & Infra | `projects`, `nodes`, `node_registration_tokens`, `node_provisioning_tokens`, `domains` |
| Workloads | `stacks`, `services`, `service_ports`, `build_configs`, `database_configs`, `volumes`, `volume_mounts`, `volume_backup_configs` |
| Variable Groups | `variable_groups`, `variable_group_items`, `service_variable_groups` |
| Traffic | `routes`, `route_targets` |
| History | `deployments`, `jobs`, `job_runs` |
| Integrations | `storage_integrations`, `registry_integrations`, `git_integrations` |
| Operations | `backup_configs`, `system_backup_configs`, `notification_channels`, `org_email_configs` |
| Templates | `templates` |

**Partial unique indexes** (in `applyConstraints`):
- `idx_one_owner_per_org` — exactly one owner per org
- `idx_unique_domain_per_org` — domain names unique within an org

**Encryption**: `EncryptedString` GORM type uses AES-256-GCM. Call `db.SetEncryptionKey()` before any DB operation. Never stored as plaintext.

**Open-core CE/EE boundary**: `db.RegisterMigration(fn)` is called from the EE module's `init()`. The CE binary never imports the EE module so `eeHooks` stays empty in CE builds.

---

## apps/api — internal directory structure

```
internal/
├── config/       # Config struct + Load() from env
├── middleware/   # Auth() — soft JWT middleware (sets user in ctx, doesn't block)
├── handler/      # HTTP layer only — thin, delegates to service layer
│   ├── handler.go          # Handler struct + Register() + RegisterRaw()
│   ├── access.go           # checkAccess(), checkOrgAdminAccess(), checkOrgMemberAccess()
│   ├── auth.go             # /auth/*, /me, TOTP, 2FA
│   ├── org.go              # Org CRUD, members, invitations
│   ├── project.go          # Project CRUD
│   ├── permission.go       # Per-resource permission grants
│   ├── node.go             # Node CRUD, self-register, self-deregister, metrics
│   ├── workload.go         # Service CRUD, env vars, build/db config, pods
│   ├── stack.go            # Stack CRUD, apply, sync
│   ├── job.go              # Job CRUD, trigger, run history
│   ├── volume.go           # Volume CRUD, mounts, backup config
│   ├── route.go            # Route CRUD, targets, hostname verify
│   ├── deployment.go       # List, trigger, rollback, SSE log streams
│   ├── backup.go           # Service backups + system backup
│   ├── notification.go     # Notification channels
│   ├── email_config.go     # Org SMTP config
│   ├── variable_group.go   # Variable group CRUD + service attach/detach
│   ├── git_integration.go  # Git provider integrations + OAuth callbacks
│   ├── registry.go         # Registry integration CRUD
│   ├── storage.go          # Storage integration CRUD
│   ├── terminal.go         # WebSocket: node terminal + pod terminal
│   ├── webhook.go          # Inbound webhooks (GitHub push, deploy token)
│   ├── domain.go           # Domain CRUD + DNS verification
│   ├── system.go           # Version, install/uninstall scripts
│   └── health.go           # GET /health
└── service/      # Business logic
    ├── service.go          # Services aggregate struct + New()
    ├── auth.go             # Register (user + default org in tx), Login, TOTP
    ├── org.go              # Org CRUD, members, invitations
    ├── project.go          # Project CRUD
    ├── permission.go       # Resource permission grants
    ├── node.go             # Node CRUD, registration/provisioning tokens, offline monitor
    ├── node_exporter.go    # Live metrics scraping from node_exporter
    ├── workload.go         # Service CRUD, env vars, build/db config
    ├── stack.go            # Stack parse, apply, sync
    ├── job.go              # Job CRUD, trigger, K8s Job reconciler goroutine
    ├── volume.go           # Volume CRUD, mounts, K8s PVC lifecycle
    ├── route.go            # Route + target CRUD
    ├── domain.go           # Domain CRUD + DNS verification
    ├── deployment.go       # Deployment trigger, rollback, K8s Job lifecycle
    ├── backup.go           # Backup schedule, trigger, restore, retention reaper
    ├── backup_executor.go  # Backup/restore K8s Job execution
    ├── notification.go     # Dispatch: Slack, Discord, email, HMAC webhook
    ├── email_config.go     # Org SMTP config
    ├── variable_group.go   # Variable group CRUD + service attachment
    ├── git_integration.go  # Git provider connections + OAuth flows
    ├── registry.go         # Registry integration CRUD
    ├── storage.go          # Storage integration CRUD
    ├── db_explorer.go      # Live DB query + schema via K8s exec
    ├── system.go           # Version info, install/uninstall script serving
    └── headscale.go        # Headscale API client: list, get, delete, rename nodes
```

Full API route reference: `apps/api/README.md`.

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
- API base URL is `""` in production (relative paths). Dev falls back to `http://localhost:4000`. Use `??` not `||` when checking the config value.

---

## Safety guardrails

- **NEVER** modify or delete files inside `deploy/headscale/data/`.
- **NEVER** commit `.db`, `.db-shm`, `.db-wal`, or `.env` files.
- **NEVER** write raw SQL in application code — use GORM or `applyConstraints()` in `packages/db/db.go`.
- **NEVER** store secrets as plaintext — use `EncryptedString` GORM type.
- **NEVER** delete a gateway node (`k3s_role=server`) via the API or UI — block at handler level.
- **NEVER** expose worker container ports to public interfaces — all traffic flows over the WireGuard mesh.
