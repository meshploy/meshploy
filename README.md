# Meshploy

[![ci](https://img.shields.io/github/actions/workflow/status/meshploy/meshploy/pr.yml?label=ci)](https://github.com/meshploy/meshploy/actions/workflows/pr.yml)
[![release](https://img.shields.io/github/v/release/meshploy/meshploy)](https://github.com/meshploy/meshploy/releases)
[![docs](https://img.shields.io/badge/docs-site-blue)](https://docs.meshploy.com)

**Your servers. Private by default. PaaS simplicity.**

Meshploy is a self-hosted PaaS that orchestrates multi-node deployments across a WireGuard mesh network, powered by K3s. Worker nodes are completely dark to the public internet — no open ports, no exposed services. The only public-facing component is the Meshploy Edge Gateway.

Deploy apps, provision managed databases, and ship to a global distributed cluster with a Vercel-like developer experience backed by enterprise-grade infrastructure.

---

## Documentation

### For users

| Document | Description |
|---|---|
| [How it works](./HOW_IT_WORKS.md) | Why NS delegation, why dark workers, how TLS works, CLI vs dashboard, MCP server — the questions that come up when you're setting up or evaluating Meshploy |
| [Self-hosting guide](#self-hosting) | Install, DNS setup, supported distros, managing your installation |
| [API reference](./apps/api/README.md) | All REST routes — useful when scripting against the API directly |
| [CLI reference](./apps/cli/README.md) | All CLI commands, config file, node workflows |

### For contributors & engineers

| Document | Description |
|---|---|
| [CONCEPTS.md](./CONCEPTS.md) | Architectural decisions — why each technical choice was made and what the alternative was |
| [CONTRIBUTING.md](./CONTRIBUTING.md) | Dev setup, coding guidelines, local vs VPS testing, PR process |
| [CLAUDE.md](./CLAUDE.md) | Coding standards, repo layout, safety guardrails — read before making changes |
| [packages/db/README.md](./packages/db/README.md) | Shared DB models — schema, migrations, encryption |
| [apps/proxy/README.md](./apps/proxy/README.md) | Edge proxy internals — route cache, "Ask & Resolve" pattern |
| [apps/web/AGENTS.md](./apps/web/AGENTS.md) | Frontend coding rules — @base-ui/react patterns, TanStack Router conventions |

---

## Stack

| Component | Technology |
|---|---|
| `apps/api` | Go · Chi · Huma (OpenAPI 3.1) |
| `apps/proxy` | Go · `net/http` |
| `apps/web` | Vite · React 19 · TanStack Router · Tailwind · shadcn/ui |
| `apps/cli` | Go · Cobra — static binary for node & cluster management |
| `packages/db` | Go · GORM · PostgreSQL |
| Infrastructure | Headscale · K3s · CoreDNS · Caddy |

---

## CLI

The `meshploy` CLI lets you manage nodes and authenticate from the terminal without the web dashboard. It is installed automatically by `get.sh` and lives at `/usr/local/bin/meshploy`.

```bash
# Authenticate against your instance
meshploy auth login --api-url https://api.your-domain.com

# Manage nodes
meshploy node list
meshploy node delete <id>
meshploy node token get

# Install/uninstall a node (requires root, shells out to install.sh / uninstall.sh)
sudo meshploy node install
sudo meshploy node uninstall

# Print the setup token for creating the first account (on the gateway)
sudo meshploy setup-token show

# Update the CLI binary (preferred)
meshploy update

# Or re-fetch from the install script
sudo bash -c "$(curl -fsSL https://meshploy.com/install.sh)" _ --cli-only
```

See [**apps/cli/README.md**](./apps/cli/README.md) for the full command reference.

---

## Features

- **Application deployments**: Nixpacks, Railpack, or Dockerfile; pre-built images also supported; any language or framework
- **Managed databases**: PostgreSQL, MySQL, Redis, MongoDB, Dragonfly, ClickHouse as K8s workloads
- **Docker Compose**: native Compose file support via compose-go; lift-and-shift existing stacks
- **AI-native**: MCP server with 90+ tools — Claude Code can deploy, query, manage, and monitor your platform without leaving your editor
- **WireGuard mesh networking**: workers are dark to the public internet; all traffic routes over the mesh
- **Multi-node K3s cluster**: unlimited workers; builds and jobs run as ephemeral K8s Jobs
- **Git integrations**: GitHub (App), GitLab, and Gitea; auto-detect build context
- **Jobs & cron**: one-off and scheduled jobs with full run history
- **Automated backups**: scheduled to any S3-compatible storage (R2, MinIO, AWS); restore from dashboard
- **Real-time monitoring**: node and container CPU / memory / network metrics
- **Web terminal**: SSH into any node or exec into any pod from the browser
- **DB Explorer**: run live queries and browse schema from the dashboard
- **Notifications**: Slack, Discord, email, or generic webhooks on deploy events
- **RBAC**: organizations, projects, Owner / Admin / Member roles, per-resource permissions
- **CLI**: manage nodes, deployments, and services from the terminal

---

## Who is this for?

- **Solo developers and small teams** who want Render or Railway-level simplicity but on their own servers — no per-seat pricing, no vendor lock-in
- **Teams with compliance or data residency requirements** — every workload runs on your infrastructure, nothing leaves it
- **Engineers running multi-cloud or bare-metal** — mix Hetzner, AWS spot instances, and home servers in one cluster without cloud VPC complexity

---

## Repository Structure

```
meshploy/
├── apps/
│   ├── api/          # API entrypoint: main.go calls server.Main() from packages/server
│   ├── proxy/        # Edge proxy: "Ask & Resolve" L7 routing over the WireGuard mesh
│   ├── cli/          # meshploy CLI: static binary for nodes, the cluster and the platform
│   ├── web/          # Dashboard: Vite + React 19 + TanStack Router
│   ├── builder/      # Builder image: git clone + Nixpacks / Railpack / Dockerfile builds
│   └── docs/         # Documentation site (Astro Starlight), generated from these READMEs
├── packages/
│   ├── server/       # API core: config, handlers, services, middleware, k8s
│   ├── db/           # Shared GORM models: all 41 tables, migrations, encryption
│   ├── client/       # Typed Go client for the API, used by the CLI and the MCP server
│   ├── mcpserver/    # MCP tool definitions: stdio through the CLI, remote at /mcp
│   └── license/      # Enterprise licence verification
├── deploy/
│   ├── install.sh           # The installer
│   ├── docker-compose.yml   # Production: pulls images from GHCR
│   ├── caddy/               # Custom Caddy build + a Caddyfile for each DNS mode
│   ├── headscale/           # Headscale config
│   └── coredns/             # CoreDNS zones + Corefile
├── get.sh                   # Bootstrap served at meshploy.com/install.sh; fetches deploy/
├── go.work                  # Go workspace: links all Go modules
└── .env.example             # Environment variables for local development
```

---

## Self-Hosting

### Supported operating systems

| Distro | Versions | Container runtime |
|---|---|---|
| Ubuntu | 20.04+ | Docker (auto-installed) or Podman |
| Debian | 11+ | Docker (auto-installed) or Podman |
| Fedora | 38+ | Docker or Podman (auto-installed) |
| RHEL / Rocky / AlmaLinux | 8+ | Docker or Podman (auto-installed) |
| CentOS Stream | 9+ | Docker or Podman (auto-installed) |
| openSUSE Leap / Tumbleweed | latest | Docker or Podman (auto-installed) |
| Arch Linux | rolling | Docker or Podman (auto-installed) |

> **Requirements:** systemd, x86_64 or arm64, kernel ≥ 5.4. Alpine and non-systemd distros are not supported.

### Prerequisites

- A supported Linux distro (see above)
- At least **5 GB** free disk space (images + k3s + data)
- A domain you control, with DNS records pointing at this server. Which records depends on the DNS mode; see [DNS setup](#dns-setup)
- Ports **80** and **443** open in your firewall and not in use by other services on the host, plus **53** (TCP+UDP) in the default NS-delegation mode. Port 53 conflicts with `systemd-resolved` on Ubuntu 22.04+; the installer will warn you. UDP **41641** is optional: it lets nodes reach the gateway directly instead of through a relay
- A host or provider firewall keeping everything else closed. The API (4000), the built-in registry (5000), the k3s API (6443), node_exporter (9100) and the kubelet (10250) listen on every interface so the mesh can reach them, and the registry has no authentication of its own. The dashboard warns you when the installer found no host firewall
- Root / sudo access

### Install

```bash
sudo bash -c "$(curl -fsSL https://meshploy.com/install.sh)"
```

The script installs Docker (if needed), downloads Meshploy to `/opt/meshploy`, walks you through an interactive setup (domain, IP, secrets), and starts the full stack. Select **Master** for the gateway node or **Worker** to join an existing mesh.

Before the first question, the installer offers to continue in a browser instead. Say yes and it prints a setup token and an address on port 9000, where the same setup runs as a web page that checks your DNS as you go and streams the install. Port 9000 has to be reachable from wherever your browser is.

### DNS setup

Pick a domain for Meshploy, ideally a dedicated subdomain such as `meshploy.example.com`. The console, the API and every app you deploy get hostnames under it. There are two ways to point it at the server; the installer asks which one when it cannot see a delegation already in place.

**NS delegation (default).** The gateway runs its own authoritative DNS (CoreDNS) for the domain, which lets it obtain one wildcard certificate. Add two records where the *parent* zone is hosted (for `meshploy.example.com`, that is `example.com`), the A record first:

```
ns1.meshploy.example.com   A    <gateway-public-ip>
meshploy.example.com       NS   ns1.meshploy.example.com
```

An NS record names a nameserver rather than an address, which is why the `ns1` A record has to exist for the delegation to resolve. Verify once it has propagated:

```bash
dig @<gateway-public-ip> console.meshploy.example.com A
```

**Self-managed DNS (`--dns-mode=ondemand`).** For providers that cannot delegate a subdomain (Hostinger, for example). Keep DNS with your provider and add two A records; Caddy then issues a certificate per hostname the first time each one is requested:

```
*.meshploy.example.com   A   <gateway-public-ip>
meshploy.example.com     A   <gateway-public-ip>
```

Some providers call the second one the root or `@` record. Verify:

```bash
dig +short console.meshploy.example.com A
```

Re-running the installer keeps whichever mode the server was installed with.

### Creating the owner account

When the install finishes, open `https://console.<your-domain>` and register. The first account owns the instance, so the form asks for the one-time **setup token** the installer printed. If you no longer have it, print it again on the gateway:

```bash
sudo meshploy setup-token show
```

The token is only accepted until that first account exists. If someone else may have seen it before then, issue a new one with `sudo meshploy setup-token rotate` and restart the API (`cd /opt/meshploy && docker compose up -d api`).

### Managing your installation

Installing, reinstalling and removing all go through the install script:

| Command | What it does |
|---|---|
| `sudo bash -c "$(curl -fsSL URL)"` | Fresh install |
| `sudo bash -c "$(curl -fsSL URL)" _ --reinstall` | Update images and config, **preserve** database and TLS certs |
| `sudo bash -c "$(curl -fsSL URL)" _ --reinstall --wipe-data` | Full reinstall from scratch, wipes database and TLS cert cache |
| `sudo bash -c "$(curl -fsSL URL)" _ --uninstall` | Remove Meshploy (interactive) |
| `sudo bash -c "$(curl -fsSL URL)" _ --cli-only` | Install or update the `meshploy` CLI binary only — safe on existing nodes |
| `sudo bash -c "$(curl -fsSL URL)" _ --dns-mode=ondemand` | Install without NS delegation — you add a wildcard A record and Caddy issues a certificate per hostname |
| `sudo bash -c "$(curl -fsSL URL)" _ --edge` | Install edge builds from `main` instead of the latest stable release |

> Replace `URL` with `https://meshploy.com/install.sh`

> **Release channel**: by default the installer tracks the latest **stable** release: `:latest` images and the `deploy/` config from the newest release tag. Pass `--edge` to track `main` instead, which pulls `:main` images and branch config. Edge carries work that has not been released yet, so prefer stable unless you need something that has just landed. The choice is not remembered: a later re-run or `sudo meshploy server-upgrade` without `--edge` writes `MESHPLOY_CHANNEL=latest` and moves the install back to stable, so pass `--edge` every time if you mean to stay on it. Upgrades from the console keep the channel the server is on. To change it there, the server's owner uses **Settings → Server**, which shows both channels and where the server sits on them. The console only switches forward: edge to stable becomes available once a release includes the build the server runs, so it never installs older code.

> **TLS cert cache**: Caddy stores Let's Encrypt certificates in a Docker volume. `--reinstall` always preserves this volume to avoid hitting rate limits (5 certs per domain per week). Use `--wipe-data` only when you genuinely need a clean slate.

> **Upgrading**: `sudo meshploy server-upgrade` on the gateway downloads the configuration and images for the latest release, then installs them and restarts the services, keeping your data. If the services do not come back, it puts the previous version back. The server's owner can also upgrade from the console: **Update available** in the sidebar opens an **Upgrade now** button. New installs are set up for that; on an older server, run `sudo meshploy updater start` once after upgrading. Add `--edge` to upgrade to `main`. Run `sudo meshploy update` first, so the upgrade runs with the latest CLI. See the [CLI reference](./apps/cli/README.md#meshploy-server-upgrade).

---

## Enterprise

Community is complete and stays free: everything above is MIT, with one organization per server. Enterprise adds organizational features under a licence (the console's **Settings → Licence → Compare editions** lists them) and runs as a separate pair of private images, `ghcr.io/meshploy/api-ee` and `ghcr.io/meshploy/web-ee`, built from the same source and version as each Community release.

Moving a server to Enterprise keeps its data and settings:

1. **Get a licence** at [meshploy.com/enterprise](https://meshploy.com/enterprise). It is bound to your domain and names the image it grants.
2. **Activate it** on the Community server: an admin pastes it into **Settings → Licence**, or runs `meshploy license activate <token>`. It is verified and stored, and grants nothing until the switch, since Community has no Enterprise features built in.
3. **Give the gateway registry access** once, as root, with a GitHub token that can read packages, for the account the licence was granted to: `echo <token> | sudo docker login ghcr.io -u <github-user> --password-stdin` (or `podman`). Credentials never pass through the console.
4. **Switch.** The server's owner presses **Switch to Enterprise** in the licence section, which runs through the same updater as an upgrade, or runs `sudo meshploy server-upgrade --ee` on the gateway. Either points the API and the console at the Enterprise images and restarts them; if the images cannot be pulled, nothing changes.

Upgrades then work as before, on either channel. The Enterprise images for a release appear a few minutes after it, and an upgrade started before they do stops without changing anything. `meshploy license status` shows the edition and the licence.

To go back to Community, remove `MESHPLOY_API_IMAGE` and `MESHPLOY_WEB_IMAGE` from `/opt/meshploy/.env` and run `sudo meshploy server-upgrade`. The licence and any Enterprise data stay in the database, unused.

---

## Local Development

### Prerequisites

- Go 1.25+
- Node.js 20+
- PostgreSQL 15+
- Docker

### 1. Clone and configure

```bash
git clone https://github.com/meshploy/meshploy
cd meshploy
cp .env.example .env
```

Edit `.env`:

```bash
DATABASE_URL=postgres://user:password@localhost:5432/meshploy?sslmode=disable
JWT_SECRET=your-long-random-secret
ENCRYPTION_KEY=0123456789abcdef0123456789abcdef   # exactly 32 characters: openssl rand -hex 16
```

### 2. Start PostgreSQL

```bash
docker compose -f deploy/docker-compose.dev.yml up -d
```

This starts only PostgreSQL on port 5432. Headscale, CoreDNS, Caddy, and the registry are **not needed** for local development — the API and frontend work without them.

### 3. Run the API

```bash
cd apps/api && go run main.go
```

API at `http://localhost:4000`. The OpenAPI spec is at `/openapi.json`; it needs a bearer token, like every non-public route.

### 4. Run the Proxy

```bash
cd apps/proxy && go run main.go
```

### 5. Run the Web dashboard

```bash
cd apps/web && npm install && npm run dev
```

Dashboard at `http://localhost:5173`

---

## API

Meshploy exposes an OpenAPI 3.1 REST API. The spec is served at `/openapi.json` and Huma's docs page at `/docs`, both behind the same bearer-token authentication as the API.

See [**apps/api/README.md**](./apps/api/README.md) for the full route reference.

---

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feat/my-feature`
3. Follow the coding standards in `CLAUDE.md`
4. Open a pull request

---

## License

[MIT](LICENSE)

---

## Acknowledgements

Inspired by Northflank, Tailscale, and Dokploy.

---

## AI Usage Disclosure

This project was built with AI assistance. Architecture decisions, code generation, and debugging were done with the help of Claude Code and Gemini. Every generated output was reviewed, tested, and adapted by the author.
