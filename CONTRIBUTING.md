# Contributing to Meshploy

Thanks for your interest. This guide gets you from zero to a working dev environment and covers what to keep in mind when submitting changes.

---

## Prerequisites

| Tool | Version |
|---|---|
| Go | 1.22+ |
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
DATABASE_URL=postgres://user:pass@localhost:5432/meshploy?sslmode=disable
JWT_SECRET=any-long-random-string
ENCRYPTION_KEY=exactly-32-characters-here!!!!!
```

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
apps/api/     Chi + Huma REST API. Business logic in internal/service/, HTTP in internal/handler/.
apps/proxy/   Edge reverse proxy. Reads Host header → WireGuard mesh → upstream.
apps/cli/     Cobra CLI binary. Wraps API calls; node install/uninstall shells out to scripts.
apps/web/     Vite + React 19 + TanStack Router frontend.
packages/db/  Shared GORM models imported by api and proxy.
```

---

## Guidelines

### Go (api, proxy, cli)

- **Never put business logic in handlers.** Handlers call the service layer and return results. Logic belongs in `internal/service/`.
- **Use GORM for all DB access.** No raw SQL — use `applyConstraints()` in `packages/db/db.go` for DDL.
- **Register DB migrations** via `db.RegisterMigration()` — don't add columns directly to `AutoMigrate`.
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

One subject line, no trailing period. Keep it under 72 characters.

---

## Pull requests

- **One concern per PR.** A refactor and a bug fix are two PRs.
- **Tests for service-layer changes.** The `apps/api/internal/service/` package has integration tests — add coverage for new service methods.
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
| 80 | TCP | Caddy — ACME challenge + HTTP→HTTPS redirect |
| 443 | TCP | Caddy — dashboard, API, proxy routing, Headscale control plane |
| 53 | TCP + UDP | CoreDNS — wildcard DNS (`*.yourdomain.com → gateway IP`) |
| 3478 | UDP | STUN — WireGuard NAT traversal for connecting worker nodes |

> **Worker nodes do not need open ports.** They only make outbound connections
> to the gateway. If your test environment blocks outbound UDP or has strict
> egress rules, WireGuard will fall back to the STUN relay on port 3478.

All other services (Headscale API, the traffic proxy, the built-in registry)
run behind Caddy and are only reachable through port 443. Exposing any
additional ports is not required and not recommended.

### Gateway setup

```bash
# Install Meshploy on the gateway server
curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh | sudo bash
```

Point a wildcard DNS record at the gateway's public IP before installing,
or configure it afterwards in your registrar:

```
*.yourdomain.com  →  <gateway public IP>   (A record)
yourdomain.com    →  <gateway public IP>   (A record)
```

CoreDNS handles internal mesh DNS (`*.internal.yourdomain.com`) automatically
once it's running — you don't need to configure those records manually.

### Adding a worker node

From the dashboard go to **Cluster → Nodes → Add Node**, copy the install
command, and run it on the worker server:

```bash
sudo meshploy node install --gateway <gateway-ip> --key <preauth-key>
```

The worker registers with Headscale, joins the WireGuard mesh, and appears
in the dashboard within a few seconds. Worker nodes don't need a domain or
any open firewall ports.

### Iterating without a full reinstall

Once the gateway is up, you can redeploy individual components as you work:

```bash
# On the gateway — rebuild and restart only what changed
cd /opt/meshploy && git pull
docker compose up -d --build api      # API changes
docker compose up -d --build proxy    # proxy changes
docker compose up -d --build web      # frontend changes
```

The database and Headscale state are preserved across restarts.

---

## Reporting bugs

Open a [GitHub Issue](https://github.com/meshploy/meshploy/issues). Include the Meshploy version (`meshploy version`), OS, and steps to reproduce.

For security vulnerabilities, **do not open a public issue** — see [SECURITY.md](./SECURITY.md).
