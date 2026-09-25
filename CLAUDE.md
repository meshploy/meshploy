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
│   ├── web/          # Vite + React 19 + TanStack Router frontend
│   ├── builder/      # Builder image (meshploy-build): git clone + Nixpacks / Railpack / Dockerfile
│   └── docs/         # Astro Starlight docs site, generated from the repo's READMEs by sync-docs.mjs
├── packages/
│   ├── db/           # Shared GORM + PostgreSQL models (imported by api and proxy)
│   ├── client/       # Typed Go REST client for the API (imported by cli and mcpserver)
│   ├── mcpserver/    # MCP tool definitions (imported by cli for stdio, api for remote /mcp)
│   ├── license/      # Enterprise licence verification: claims, keys, features
│   ├── hostagent/    # Contract between the gateway's host agent (CLI) and the API: reports, firewall parsing, port verdicts
│   ├── help/         # Meshploy explaining itself: topics/*.md, read by the console's help drawer, the docs' Concepts and (later) MCP
│   └── server/       # API core — config, service, handler, middleware, k8s (imported by apps/api)
├── deploy/           # Headscale, CoreDNS, Docker Compose infra
├── go.work           # Go Workspaces: ties apps/* + packages/*
└── .env              # Local secrets (never committed)
```

---

## Architecture overview

- **apps/api** — Thin CE entrypoint: `main.go` calls `server.Main()`. The API itself lives in `packages/server` — business logic in `service/`, HTTP concerns in `handler/`, config in `config/`.
- **apps/proxy** — Minimal L7 reverse proxy. Reads the `Host` header → in-memory route cache (backed by PostgreSQL, refreshed every 30s) → streams over WireGuard mesh to target node. Listens on port 8081.
- **apps/cli** — Static Go binary (`/usr/local/bin/meshploy`). Wraps API calls and shells out to `install.sh` / `uninstall.sh` for node operations. Built with Cobra.
- **packages/db** — Shared GORM models backed by **PostgreSQL**. `AutoMigrate` + supplementary partial unique indexes run on API startup via `db.Migrate()`. Exports an Extensible Migration Registry (`RegisterMigration`) for the EE open-core pattern. Imported by both `apps/api` and `apps/proxy`.
- **packages/client** — Typed Go REST client for the Meshploy API. Imported by `apps/cli` (every command) and by `packages/mcpserver`. Lives in `packages/` so no app depends on another app.
- **packages/mcpserver** — The MCP tool definitions (~100 tools) built on `packages/client`. Imported by `apps/cli` for the local stdio server (`meshploy mcp`) and by `packages/server` for the gateway-served remote `/mcp` endpoint.
- **apps/web** — Vite + React 19 + TanStack Router frontend. Dark-only, Tailwind CSS v4 (CSS-first via `@tailwindcss/vite`, no config file), shadcn/ui Nova preset, `@base-ui/react` primitives.
- **deploy/** — Headscale (WireGuard mesh), CoreDNS, Docker Compose. The gateway node is the only public-internet-facing machine; all workers are dark.

### Help is one text

`packages/help/topics/*.md` is the only place a feature is explained to users. The console bundles it (the `virtual:help-topics` Vite plugin; the web image gets the folder as a second build context, `--build-context help=packages/help`) for its help drawer (a page's "?" or the `?` key), the ⓘ beside a term, and empty states; `apps/docs/sync-docs.mjs` publishes it as the docs' **Concepts**; the MCP server can serve it. A topic is a header (`id`, `title`, `summary`, `pages`), sections `## Title {#id}`, and a `## Terms {#terms}` glossary of `- **Label** {#term -> section}: text` that the console's tooltips show. `packages/help`'s test fails a term pointing at a missing section, or an em-dash. `CONCEPTS.md` is the architecture's design decisions, not this.

### Edge configuration is generated

`caddy/Caddyfile`, `coredns/Corefile`, the zone files and `headscale/config/config.yaml` are **output**, rendered by `meshploy domain apply` from an edge snapshot at `$HOST_DIR/state/edge.json`. Headscale's is generated because its split DNS must name every base domain's internal zone and its `server_url` follows the primary. They are not shipped in the deploy tarball and editing them is pointless - the next apply overwrites them. An operator's own Caddy configuration goes in `caddy/conf.d/*.caddy`, which the generated file imports and nothing writes.

Templates live in `apps/cli/internal/edgeconfig/templates/`, with golden fixtures in its `testdata/`. A change to a template is therefore a change to a checked-in file a reviewer can read, and it reaches servers through `server-upgrade`, which re-renders. `install.sh` seeds the first snapshot with `--from-env`.

Two snapshots, separated by who writes them: `inbox/edge.json` is what the API wants served (the one place the API can write, and leaving one there changes nothing by itself), and `state/edge.json` is what is actually installed. A `domain.apply` host-agent request is what puts the first into service and writes the second, so "the database changed" and "the gateway's edge changed" stay two visible events.

The primary is a pointer. Moving it (`make-primary`) starts the platform's names on the new domain and keeps them serving on the old one, marked `former_primary`, until that domain is removed - the console in use and every worker's control URL depend on them. MagicDNS's `mesh.<domain>` stays pinned to the install domain so a switch renames no node. The platform's public URLs (the Headscale URL handed to a joining machine, the API base in the install script) come from the primary via `DomainService.PlatformURL`, never from `HEADSCALE_URL`, which is the API's in-network address for Headscale. Each node records the control URL it joined through (`nodes.control_url`), and each git integration the API address it gave its provider (`git_integrations.registered_api_base`); a former primary cannot be removed while nodes still use it or a provider still calls it. A service's CI deploy webhook is pasted into someone's CI where Meshploy cannot see it, so the webhook handler records the `Host` each authenticated call arrived through (`build_configs.deploy_hook_host`); calls still arriving through a former primary hold it too, and clear themselves once the job is updated. The console gives that URL on the primary's API, not `window.location.origin`. Repository push hooks are identified by path (`/api/v1/webhooks/git/<provider>/<id>`), not full URL, so a hook made under an earlier primary is recognised rather than duplicated. Links in notifications and the browser's return after an OAuth callback follow the primary too; the callback return honours the request's `Host` only for a domain that serves the platform.

Each base domain carries its own DNS mode, so one gateway can serve a delegated domain and an on-demand one at once. A delegated domain gets its public zone plus two ACME challenge zones; an on-demand domain gets only its mesh zone, because nothing delegates here for it. **The `_acme-challenge.*` zones are seeded once and then never rewritten** - Caddy's DNS module writes challenge records straight into them.

### Mesh routing

```
Internet → Caddy (TLS) → apps/proxy (:8081) → WireGuard mesh → K3s worker node
                              ↑
                        reads Host header
                        cache: hostname → (mesh_ip, port)
```

`apps/proxy` reads the `Host` header → route cache lookup → `httputil.ReverseProxy` to `http://<mesh_ip>:<port>`. Caddy's `handle /api/*` block routes API traffic to port 4000; `*.internal.<domain>` goes to port 8081.

### K3s cluster
Single K3s cluster spanning all mesh nodes. Control plane on gateway (`k3s_role=server`), workers join as agents. Builds run as ephemeral K8s Jobs with `meshploy.com/role=builder` node selector. The gateway is a build node by default (`mesh_role` defaults to `workload_builder` when unset); turning "Act as build node" off stores `workload`, which is kept. A deploy fails at once when no online node can build, and after a few minutes when the scheduler cannot place the pod.

### Environment levels
A project is its own **production** level. Each level below it (staging, dev, ...) is a **project row of its own** pointing at it through `parent_project_id`, with its own namespace (`<project slug>-<level>`), so everything that scopes by project works inside a level unchanged. `env_level` orders them: 0 is production. The projects list shows projects only; a grant on a project covers its levels; a project cannot be deleted while it has levels.

A level holds only what has been put in it. Across levels a service is one **lineage** and several rows (`services.lineage_id`). **Promotion groups** move services up together along a path of their own: only the lowest level on the path builds (auto-deploy is off above it), and promotion deploys the image the level below last ran, as it is, for each service whose image there is newer than the level above's (the rest are skipped with a reason) (`DeploymentService.DeployImage`, shared with rollback). A service copied down with **Copy to** is a single-service group (`single`), which a named group absorbs when the service joins it. Removing a level's copy (`PromotionService.RemoveFromLevel`) deletes it and the level's routes to it, and takes it out of a group that builds in that level. Image cleanup keeps any image another service has deployed, so staging never deletes what production was promoted to. Each deployment records where its image came from (`deployments.source`, branch, commit and subject; `from_level` and `from_deployment_id` for one moved between levels), which the board cards and the service page show. A build reads its commit from the builder's `Commit:` log line, falling back to the clone line an older builder image prints, which has the hash only.

Whatever a level does not have it **borrows** from the nearest level above: a service's published variable group is resolved at every deploy to the nearest copy at or above the consumer's level (`VariableGroupService.nearestCopies`). Databases are never promoted; a level either borrows the one above (writes reach its data, which the console says) or gets its own copy, empty or cloned from the source's latest backup (`PromotionService.OwnDatabase`, `BackupService.RestoreInto`). A level never uses anything from **below** it: a shared group belonging to a lower level, or a published one whose service has no running copy at or above, is refused with `ErrBorrowFromBelow` - at deploy, and by Promote, which skips the service (`from_below`) and says in the dialog what the target needs first (`PromotionService.Preflight`). A service leaving a group, or a group deleted, hands the copies above its entry back the entry's auto-deploy (`releaseLineages`). Promote orders images by when they were **built** (`DeploymentService.ImageOrigin`, following `from_deployment_id` through redeploys, rollbacks and promotions), so a rollback or redeploy above makes nothing newer. A level above a group's entry that runs something built there (a hotfix: `arrival` build or image on the board) is replaced only by an explicit Overwrite (`?overwrite=true`); Deploy there asks first, offering a plain redeploy (`POST .../redeploy`) instead of a build. Deleting a project or a level takes its workloads and namespace out of the cluster before its rows (`PromotionService.DeleteProject`/`DeleteLevel`). A service's delete confirmation lists who reads its published variables at any level (`VariableGroupService.Dependents`), and a job whose variables cannot be read fails its run instead of running without them. A route in a level derives its hostname (`app` in staging is `app-staging`); a subdomain ending in a level's name is refused; renaming a level re-derives its hostnames but not its namespace.

### Node lifecycle
Workers self-register via `POST /api/v1/nodes/self-register` using an `mreg-<hex>` registration token or a single-use `mprov-<hex>` provisioning token. The node ID is saved to `/etc/meshploy/node.conf`, with the per-node secret (`mnode-`) a provisioning-token registration hands back; the spent `mprov-` token proves nothing later. On uninstall, `DELETE /api/v1/nodes/self-deregister` removes the node from Headscale, the k3s cluster, and the database.

A Mac or a Windows machine joins as a **mesh-only** node through `deploy/join/macos.sh` or `deploy/join/windows.ps1`, served anonymously at `/join/macos.sh` and `/join/windows.ps1` with the gateway's address filled in, like `install.sh`. `nodes.os` records `linux`, `darwin` or `windows` (absent means Linux), and registration refuses any cluster role for a non-Linux machine before the token is spent. Metrics are read from `node_exporter` on Linux and macOS; Windows reports none yet.

---

## Go workspace

`go.work` uses `replace` so local modules resolve from the filesystem. When adding new local modules, add them to `go.work` — do **not** use pseudo-versions.

Adding a module to `go.work` also means adding a `COPY <module>/go.mod` line to every Dockerfile that runs `go work sync` (`apps/api`, `apps/proxy`), because that command reads the manifest of **every** workspace module — including ones the image never builds. `go build`/`go vet`/`go test` resolve from the filesystem and stay green, so the omission only shows up in a Docker build. `scripts/check-workspace-dockerfiles.sh` enforces this and runs in CI.

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
| `PROXY_BIND` | Addresses the proxy listens on, comma-separated (default: `127.0.0.1`). It also listens on `MESH_IP`, so a container on the host - a migration's old edge - can reach it |
| `HEADSCALE_URL` | The API's own address for Headscale, inside the compose network (`http://headscale:8080`). Never hand it to a machine outside the gateway - the public URL comes from the primary domain |
| `HEADSCALE_API_KEY` | Headscale API key |
| `KUBECONFIG` | Path to kubeconfig file (empty = in-cluster) |
| `K3S_SERVER_URL` | Override K3s API server URL (needed when API runs in Docker) |
| `K3S_TLS_SERVER_NAME` | Name the cluster certificate is verified against when `K3S_SERVER_URL` rewrites the address (default: `kubernetes.default.svc.cluster.local`) |
| `K3S_SKIP_TLS_VERIFY` | Escape hatch — disables authentication of the cluster connection. Leave unset |
| `K3S_TOKEN` | Node token for workers joining the cluster |
| `DOMAIN` | Base domain — seeds the org domain record |
| `MESH_IP` | WireGuard IP of the gateway node |
| `PUBLIC_IP` | Public internet IP — backfilled on the gateway node record |
| `GATEWAY_HOSTNAME` | Gateway server hostname |
| `HOST_GATEWAY_IP` | Docker bridge gateway IP — used to reach node_exporter from inside the API container |
| `FIREWALL_STATE` | What `install.sh` saw on the host: `none`, `ufw` or `firewalld`. Read-only record — the API container cannot inspect the host firewall itself. Drives the console's exposure notice |
| `FIREWALL_CHECKED_AT` | RFC3339 UTC timestamp of that check, so the notice never reads as live state |
| `DNS_MODE` | `delegation` (the gateway runs authoritative DNS and holds a wildcard) or `ondemand` (DNS stays with the operator's provider). Seeds the **primary** domain's mode in the edge snapshot; every base domain carries its own from then on. An internal route on an `ondemand` domain is served with a certificate from Caddy's own CA, which the console says on the route form |
| `NODEPORT_ADDRESSES` | The CIDR kube-proxy binds published ports to, e.g. `100.64.0.0/10` for the mesh. Empty means every interface, which the console warns about when a database is published |
| `BUILTIN_REGISTRY_ENDPOINT` | Seeds a built-in registry row per org (format: `<host>:<port>`) |
| `TEMPLATE_DIR` | Local one-click template catalog dir (`<dir>/<id>/...`). Set = offline/air-gapped source; overrides the remote repo |
| `TEMPLATE_REPO` | GitHub `owner/repo` the catalog is fetched from when `TEMPLATE_DIR` is unset (default: `meshploy/meshploy-templates`) |
| `TEMPLATE_REPO_REF` | Git ref for the catalog repo (default: `main`) |
| `TEMPLATE_REFRESH_INTERVAL` | How often the in-memory catalog cache refreshes (Go duration, default: `1h`) |
| `SETUP_TOKEN` | Gates the first registration; set by `install.sh`. Empty disables the check |
| `API_BASE_URL` | Public base URL of the API (default: `http://localhost:4000`) |
| `FRONTEND_URL` | Console URL (default: `http://localhost:5173`) |
| `HEADSCALE_USER` | Headscale user pre-auth keys are created under (default: `meshploy`) |
| `BUILDER_IMAGE` | Override the builder container image (default: `ghcr.io/meshploy/builder:latest`, or `:main` on an edge API) |
| `HOST_DIR` | Where the host agent (`meshploy host serve`, run by `meshployd.service` on the gateway) reports (default: `/var/lib/meshploy/host`). docker-compose mounts its `state/` read-only and its `inbox/` read-write for requests (Dokploy detect and plan); missing or stale reports make firewall verdicts `unknown` |
| `UPGRADE_DIR` | Where the console and the host-side updater meet (default: `/var/lib/meshploy/upgrade`). docker-compose mounts its `inbox/` read-write and `state/` read-only, so the API can queue an upgrade but never run one |

---

## packages/db — schema (47 CE tables)

Full schema documented in `packages/db/README.md`. Key groups:

| Group | Tables |
|---|---|
| Identity & Access | `users`, `trusted_devices`, `recovery_codes`, `dismissed_notices`, `agent_tokens`, `installed_licenses`, `organizations`, `organization_members`, `resource_permissions`, `org_invitations` |
| Projects & Infra | `projects`, `nodes`, `node_registration_tokens`, `node_provisioning_tokens`, `domains` |
| Environments | `promotion_groups`, `promotion_group_members` |
| Workloads | `stacks`, `services`, `service_ports`, `build_configs`, `database_configs`, `volumes`, `volume_mounts`, `volume_backup_configs` |
| Variable Groups | `variable_groups`, `variable_group_items`, `service_variable_groups`, `job_variable_groups` |
| Config Files | `config_files`, `service_config_files` |
| Traffic | `routes`, `route_targets`, `tcp_routes` |
| Discovery | `ignored_endpoints` |
| History | `deployments`, `jobs`, `job_runs` |
| Integrations | `storage_integrations`, `registry_integrations`, `git_integrations` |
| Operations | `backup_configs`, `system_backup_configs`, `notification_channels`, `notification_deliveries`, `org_email_configs` |
| Templates | `templates` |

**Partial unique indexes** (in `applyConstraints`):
- `idx_one_owner_per_org` — exactly one owner per org
- `idx_variable_group_service`: `variable_groups(service_id) WHERE service_id IS NOT NULL`, at most one system-managed group per service
- `idx_one_primary_domain_per_org` — `domains(organization_id) WHERE is_primary` — every base domain routes; primary only decides whose platform subdomains serve and what a new route defaults to
- `idx_users_email_unique` — `users(email) WHERE email <> ''` — email unique among humans only; agents (`users.kind = 'agent'`) carry an empty email so many can coexist

`applyConstraints` also creates plain unique indexes (variable group item keys, job names per project, route target paths, permission grants) and runs idempotent data migrations. Domain names are unique across all orgs through the `uniqueIndex` tag on `domains.base_domain`, so one org cannot claim another's domain.

**Agent principals**: an agent is a `users` row with `kind = 'agent'` (empty email, no password/TOTP) that reuses `organization_members` + `resource_permissions` unchanged — it differs from a human only in auth (a `magt-` token in `agent_tokens`, SHA-256 hashed, shown once). `requireUser`/`checkAccess` are untouched. Remote MCP is served at `/mcp` (Streamable HTTP) under an agent token and is permission-scoped by construction; operator tools (node registration token, system backups, member/permission enumeration, `db_query`/`db_schema`) are stripped from the remote surface. The MCP tool code lives in `packages/{client,mcpserver}` — shared modules imported by both `apps/cli` (stdio) and `packages/server` (remote `/mcp`), so no app depends on another app.

**Encryption**: `EncryptedString` GORM type uses AES-256-GCM. Call `db.SetEncryptionKey()` before any DB operation. Never stored as plaintext.

**Open-core CE/EE boundary**: `db.RegisterMigration(fn)` is called from the EE module's `init()`. The CE binary never imports the EE module so `eeHooks` stays empty in CE builds.

---

## packages/server — directory structure

```
packages/server/
├── server.go     # Router assembly, middleware chain, Huma config
├── entrypoint.go # Main() — shared by the CE and EE binaries
├── config/       # Config struct + Load() from env
├── middleware/   # Auth() — soft principal middleware: resolves a JWT (human) OR a magt- agent token to the same user-id in ctx; RequireAuth() is fail-closed and 401s anything off the publicRules allowlist
├── handler/      # HTTP layer only — thin, delegates to service layer
│   ├── handler.go          # Handler struct + Register() + RegisterRaw()
│   ├── access.go           # checkAccess(), checkOrgAdminAccess(), checkOrgMemberAccess()
│   ├── auth.go             # /auth/*, /me, TOTP, 2FA
│   ├── agent.go            # Agent principals: create, list, token mint/rotate/revoke, delete
│   ├── mcp.go              # Remote MCP (Streamable HTTP) at /mcp — agent-token authed, permission-scoped
│   ├── org.go              # Org CRUD, members, invitations
│   ├── project.go          # Project CRUD
│   ├── environment.go      # Environment levels: list, create above/below, rename
│   ├── promotion.go        # Promotion groups, promote, board, own database, copy down, bring down
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
│   ├── system.go           # Version, exposure notice, upgrade requests, install/uninstall scripts
│   ├── config_file.go      # Config file CRUD + attach/detach
│   ├── template.go         # One-click template catalog + deploy
│   ├── entitlement.go      # Licence status + activation
│   ├── ondemand_tls.go     # Caddy ask endpoint for on-demand TLS
│   ├── extension.go        # Extension point: extra routes (EE)
│   └── health.go           # GET /health
├── service/      # Business logic
│   ├── service.go          # Services aggregate struct + New()
│   ├── auth.go             # Register (user + default org in tx), Login, TOTP
│   ├── agent.go            # Agent principals + agent_tokens; ResolveToken() for the auth middleware
│   ├── org.go              # Org CRUD, members, invitations
│   ├── project.go          # Project CRUD
│   ├── environment.go      # Environment levels: order, create, rename (hostnames re-derived)
│   ├── promotion.go        # Groups, lineages across levels, promotion, board, own databases
│   ├── permission.go       # Resource permission grants
│   ├── node.go             # Node CRUD, registration/provisioning tokens, offline monitor
│   ├── node_exporter.go    # Live metrics scraping from node_exporter
│   ├── workload.go         # Service CRUD, env vars, build/db config
│   ├── stack.go            # Stack parse, apply, sync
│   ├── job.go              # Job CRUD, trigger, K8s Job reconciler goroutine
│   ├── volume.go           # Volume CRUD, mounts, K8s PVC lifecycle
│   ├── route.go            # Route + target CRUD
│   ├── domain.go           # Domain CRUD + DNS verification
│   ├── deployment.go       # Deployment trigger, rollback, K8s Job lifecycle
│   ├── backup.go           # Backup schedule, trigger, restore, retention reaper
│   ├── backup_executor.go  # Backup/restore K8s Job execution
│   ├── notification.go     # Dispatch: Slack, Discord, email, HMAC webhook
│   ├── email_config.go     # Org SMTP config
│   ├── variable_group.go   # Variable group CRUD + service attachment
│   ├── git_integration.go  # Git provider connections + OAuth flows
│   ├── registry.go         # Registry integration CRUD
│   ├── storage.go          # Storage integration CRUD
│   ├── db_explorer.go      # Live DB query + schema via K8s exec
│   ├── system.go           # Version info, install/uninstall script serving
│   ├── exposure.go         # Host-firewall exposure notice + dismissed notices
│   ├── upgrade.go          # Console upgrades: queue a request, read the host updater's progress
│   ├── config_file.go      # Config files projected into workloads via Secrets
│   ├── template.go         # One-click template deploy: resolve, stack, routes
│   ├── entitlement.go      # Licence verification + entitlements
│   ├── extension.go        # Extension point: per-org quotas (EE)
│   ├── orphans.go          # Cluster workloads that no service owns
│   ├── workload_status.go  # Reconciles stored service status with the cluster
│   ├── volume_status.go    # Reconciles stored volume status with its claim
│   ├── wsticket.go         # Single-use tickets for WebSocket auth
│   └── headscale.go        # Headscale API client: list, get, delete, rename nodes
├── k8s/          # Kubernetes client, exec, terminal helpers
├── templates/    # One-click template catalog (embedded + remote)
└── version/      # Current — overridden at build time via -ldflags
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
