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
meshploy auth login --api-url https://app.your-domain.com

# Manage nodes
meshploy node list
meshploy node delete <id>
meshploy node token get

# Install/uninstall a node (requires root, shells out to install.sh / uninstall.sh)
sudo meshploy node install
sudo meshploy node uninstall

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
│   ├── api/          # REST API — Chi + Huma, OpenAPI 3.1
│   ├── proxy/        # Edge proxy — "Ask & Resolve" L7 routing over WireGuard mesh
│   ├── cli/          # meshploy CLI — static binary for node & cluster management
│   └── web/          # Dashboard — Vite + React 19 + TanStack Router
├── packages/
│   └── db/           # Shared GORM models — imported by api and proxy
│       ├── models.go     # All 36 table definitions
│       ├── db.go         # Open(), Migrate(), RegisterMigration()
│       ├── types.go      # Custom JSONB types (EnvVarsMap, JSONObject, StringArray)
│       └── crypto.go     # AES-256-GCM EncryptedString type
├── deploy/
│   ├── docker-compose.yml   # Production: pulls images from GHCR
│   ├── caddy/               # Custom Caddy build with CoreDNS DNS-01 plugin
│   ├── headscale/           # Headscale config
│   └── coredns/             # CoreDNS zones + Corefile
├── go.work                  # Go workspace — links all Go modules
└── .env.example             # Required environment variables
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
- A public domain with NS records pointing to this server's public IP (required before TLS certificates can be issued)
- Ports **80**, **443**, **53** (TCP+UDP), and **3478/UDP** open in your firewall *and* not in use by other services on the host (port 53 conflicts with `systemd-resolved` on Ubuntu 22.04+ — the installer will warn you)
- Root / sudo access

### Install

```bash
sudo bash -c "$(curl -fsSL https://meshploy.com/install.sh)"
```

The script installs Docker (if needed), downloads Meshploy to `/opt/meshploy`, walks you through an interactive setup (domain, IP, secrets), and starts the full stack. Select **Master** for the gateway node or **Worker** to join an existing mesh.

### DNS setup

Point your domain's NS records to the gateway's public IP before running the install. Meshploy runs its own CoreDNS authoritative server — no third-party DNS provider needed.

```
# At your registrar, delegate a subdomain to the gateway
meshploy.example.com  NS  <gateway-public-ip>
```

After install, verify:

```bash
dig @<gateway-public-ip> app.meshploy.example.com A
```

### Managing your installation

All operations go through `get.sh` — no Docker Compose commands needed.

| Command | What it does |
|---|---|
| `sudo bash -c "$(curl -fsSL URL)"` | Fresh install |
| `sudo bash -c "$(curl -fsSL URL)" _ --reinstall` | Update images and config, **preserve** database and TLS certs |
| `sudo bash -c "$(curl -fsSL URL)" _ --reinstall --wipe-data` | Full reinstall from scratch, wipes database and TLS cert cache |
| `sudo bash -c "$(curl -fsSL URL)" _ --uninstall` | Remove Meshploy (interactive) |
| `sudo bash -c "$(curl -fsSL URL)" _ --cli-only` | Install or update the `meshploy` CLI binary only — safe on existing nodes |

> Replace `URL` with `https://meshploy.com/install.sh`

> **TLS cert cache**: Caddy stores Let's Encrypt certificates in a Docker volume. `--reinstall` always preserves this volume to avoid hitting rate limits (5 certs per domain per week). Use `--wipe-data` only when you genuinely need a clean slate.

---

## Local Development

### Prerequisites

- Go 1.22+
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
ENCRYPTION_KEY=exactly-32-characters-here!!!!!   # openssl rand -hex 16
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

API at `http://localhost:4000` · OpenAPI docs at `http://localhost:4000/docs`

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

Meshploy exposes a fully documented OpenAPI 3.1 REST API. Interactive docs are served at `GET /docs`.

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
