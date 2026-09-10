# Contributing to Meshploy

Thanks for your interest. This guide gets you from zero to a working dev environment and covers what to keep in mind when submitting changes.

---

## Prerequisites

| Tool | Version |
|---|---|
| Go | 1.25+ |
| Node.js | 20+ |
| PostgreSQL | 15+ |
| Docker or Podman | any recent version |

**You only need PostgreSQL to get started.** Headscale, K3s, CoreDNS, and Caddy are all optional — the API and frontend work without any of them running.

---

## Local setup

```bash
git clone https://github.com/meshploy/meshploy.git
cd meshploy

# Copy the env template and fill in your local values
cp .env.example .env
```

Minimum env vars to start locally:

```
DATABASE_URL=postgres://meshploy:meshploy@localhost:5432/meshploy?sslmode=disable
JWT_SECRET=any-long-random-string
ENCRYPTION_KEY=0123456789abcdef0123456789abcdef
```

### Start PostgreSQL

```bash
docker compose -f deploy/docker-compose.dev.yml up -d
```

This starts only PostgreSQL on port 5432 with default credentials (`meshploy/meshploy`). No Headscale, no CoreDNS, no Caddy needed.

### Start the API

```bash
cd apps/api && go run main.go
# Runs on :4000. DB migrations run automatically on startup.
```

### Start the web dev server

```bash
cd apps/web && npm install && npm run dev
# Runs on :5173. Route tree is auto-generated.
```

### Build the CLI

```bash
cd apps/cli && go build -o meshploy .
```

### What works locally vs what needs a VPS

The API and UI are fully usable locally without any deploy infrastructure.
Mesh and cluster features degrade gracefully — they don't crash, they just
return empty data or skip the infra step.

| Area | Local (Postgres only) | Needs a VPS |
|---|---|---|
| Auth, RBAC, permissions, invitations | ✅ full | |
| Orgs, projects, services, stacks, jobs | ✅ full | |
| Secrets, variable groups, routes | ✅ full | |
| Frontend UI — all pages and flows | ✅ full | |
| CLI commands (service, stack, job, secret) | ✅ full | |
| Node list / registration API | ⚠️ API works, no real nodes | ✅ |
| Deployments | ⚠️ triggers, fails at K8s step | ✅ |
| Build jobs | ⚠️ triggers, fails at K8s step | ✅ |
| WireGuard mesh, Headscale | ❌ no-ops silently | ✅ |
| Edge proxy routing (`apps/proxy`) | ❌ no routes to resolve | ✅ |
| CoreDNS wildcard DNS | ❌ not running | ✅ |
| Worker node install/uninstall | ❌ needs real servers | ✅ |

If your change is in the API, frontend, CLI, or service layer — local dev
is all you need. Only reach for a VPS when your change touches the mesh,
the proxy, node registration, or the actual build/deploy execution path.

---

## Project layout

```
apps/api/            Thin CE entrypoint (main.go calls server.Main()). Chi + Huma REST API core lives in packages/server — business logic in service/, HTTP in handler/.
apps/proxy/          Edge reverse proxy. Reads Host header → WireGuard mesh → upstream.
apps/cli/            Cobra CLI binary. Wraps API calls; node install/uninstall shells out to scripts.
apps/web/            Vite + React 19 + TanStack Router frontend.
packages/db/         Shared GORM models imported by api and proxy.
packages/server/     API core — config, service, handler, middleware, k8s, templates. Imported by apps/api.
packages/client/     Typed Go REST client for the API. Imported by cli and mcpserver.
packages/mcpserver/  MCP tool definitions. Imported by cli (stdio) and by packages/server (remote /mcp).
```

---

## Guidelines

### Go (api, proxy, cli)

- **Never put business logic in handlers.** Handlers call the service layer and return results. Logic belongs in `packages/server/service/`.
- **Use GORM for all DB access.** No raw SQL — use `applyConstraints()` in `packages/db/db.go` for DDL.
- **Schema changes go on the models.** Add fields to the structs in `packages/db/models.go` and new models to the `AutoMigrate` list in `db.go`; indexes GORM can't express go in `applyConstraints()`. `db.RegisterMigration()` is only for schema that lives outside `packages/db`, such as the Enterprise module.
- **Secrets stay encrypted.** Use `db.EncryptedString` for any sensitive column. Never store plaintext.
- **Error responses** use `huma.Error4xx()` helpers — don't write raw JSON.

### TypeScript / React (web)

- File-based routing in `src/routes/`. Every route file exports `Route = createFileRoute(...)`.
- Use shadcn/ui components from `src/components/ui/` — don't reach for native HTML elements for UI.
- shadcn/ui uses `@base-ui/react` (not Radix UI). Use the `render` prop instead of `asChild`.
- Tailwind v4 — no `tailwind.config` file. All tokens live in `src/index.css`.
- State via Zustand in `src/store/`. API calls go through `src/lib/api/`.

### Safety rules

- Never modify files inside `deploy/headscale/data/`.
- Never commit `.env`, `.db`, `.db-shm`, or `.db-wal` files.
- Never expose worker container ports to public interfaces.
- Never delete a gateway node (`k3s_role=server`) via the API — block at handler level.

---

## Commit convention

```
feat:     new user-visible feature
fix:      bug fix
refactor: code change with no behaviour change
test:     adding or updating tests
docs:     documentation only
chore:    build, deps, config, release tooling
perf:     performance improvement
ci:       CI/CD changes
```

One subject line, no trailing period. Keep it short: around 100 characters is a good ceiling, but a clear subject matters more than hitting a count.

---

## Pull requests

- **One concern per PR.** A refactor and a bug fix are two PRs.
- **Tests for service-layer changes.** The `packages/server/service/` package has integration tests — add coverage for new service methods.
- **Build must pass.** Run `go build ./...` before pushing.
- **Type-check the frontend.** Run `npm run build` in `apps/web/` to catch TypeScript errors.

---

## Testing on a staging VPS

If your change touches anything in the "Needs a VPS" column above, you need
a real Linux server with a public IP. A $5/month VPS is enough for a
single-node test setup.

### Required open ports (gateway only)

Configure your firewall or cloud security group to allow inbound traffic on:

| Port | Protocol | Purpose |
|---|---|---|
| 80 | TCP | Caddy: ACME HTTP-01 challenges + HTTP→HTTPS redirect |
| 443 | TCP | Caddy: dashboard, API, proxy routing, Headscale control plane |
| 53 | TCP + UDP | CoreDNS: authoritative DNS for the domain. NS-delegation mode only |
| 41641 | UDP | Optional. Lets nodes reach the gateway directly instead of through a relay |

> **Worker nodes do not need open ports.** They only make outbound connections
> to the gateway. When two nodes cannot connect directly, WireGuard traffic is
> relayed through Tailscale's public DERP servers; Meshploy's Headscale does not
> run its own relay.

Keep everything else closed. The API (4000), the built-in registry (5000), the
k3s API (6443), node_exporter (9100) and the kubelet (10250) listen on every
interface so the mesh can reach them, and the registry has no authentication,
so they rely on the firewall to stay off the internet.

### Gateway setup

```bash
# Install Meshploy on the gateway server
sudo bash -c "$(curl -fsSL https://meshploy.com/install.sh)"
```

Set up DNS for the domain you will give the installer. The default mode
delegates it to the gateway with an NS record; if your provider can't do that,
install with `--dns-mode=ondemand` and add wildcard A records instead. The exact
records for both modes are in the README's [DNS setup](./README.md#dns-setup).

CoreDNS handles internal mesh DNS (`*.internal.yourdomain.com`) automatically
once it's running — you don't need to configure those records manually.

### Adding a worker node

In the dashboard, open **Cluster** and choose **Add a worker node**. It creates a
single-use provisioning token and shows the command to run on the worker. Or,
from any machine where the CLI is logged in, let Meshploy do it over SSH:

```bash
meshploy node add ubuntu@<worker-ip>
```

The worker registers with Headscale, joins the WireGuard mesh and the k3s
cluster, and appears in the dashboard within a few seconds. Worker nodes don't
need a domain or any open firewall ports.

### Iterating without a full reinstall

The gateway runs prebuilt images from GHCR, and `/opt/meshploy` is an unpacked
copy of `deploy/`, not a git checkout, so there is nothing to `git pull` or
rebuild there. Once a change is on `main`, CI publishes `:main` images; move the
gateway onto them with:

```bash
sudo meshploy update --edge
sudo meshploy server-upgrade --edge
```

To try an API change before merging, push your own image and set
`MESHPLOY_API_IMAGE` in `/opt/meshploy/.env` to its repository (the tag still
comes from `MESHPLOY_CHANNEL`), then run `docker compose up -d api` in
`/opt/meshploy`.

The database, certificates and Headscale state are preserved across upgrades.

---

## Reporting bugs

Open a [GitHub Issue](https://github.com/meshploy/meshploy/issues). Include the Meshploy version (`meshploy version`), OS, and steps to reproduce.

For security vulnerabilities, **do not open a public issue** — see [SECURITY.md](./SECURITY.md).
