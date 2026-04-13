# Meshploy

**The open-source, zero-trust Internal Developer Platform.**

Meshploy is a self-hosted PaaS that orchestrates multi-node deployments across a WireGuard mesh network. Worker nodes are completely dark to the public internet — no open ports, no exposed services. The only public-facing component is the Meshploy Edge Gateway.

Deploy apps, provision managed databases, and ship to a global distributed cluster with a Vercel-like developer experience backed by enterprise-grade infrastructure.

---

## Documentation

| Document | Description |
|---|---|
| [CLAUDE.md](./CLAUDE.md) | Coding standards, architecture overview, safety guardrails — read this first |
| [CONCEPTS.md](./CONCEPTS.md) | Architectural decisions — what Meshploy does differently and why |
| [apps/api/README.md](./apps/api/README.md) | REST API — routes, node enrichment, self-register/deregister |
| [apps/proxy/README.md](./apps/proxy/README.md) | Edge proxy — "Ask & Resolve" routing, route cache |
| [apps/web/README.md](./apps/web/README.md) | Web dashboard — stack, API client, production build |
| [apps/web/AGENTS.md](./apps/web/AGENTS.md) | Web coding rules — @base-ui/react patterns, TanStack Router conventions |
| [apps/cli/README.md](./apps/cli/README.md) | CLI — installation, all commands, config file, workflows |
| [packages/db/README.md](./packages/db/README.md) | Shared DB models — schema, migrations, encryption, CE/EE boundary |

---

## How Meshploy Differs

Meshploy makes deliberate architectural choices that differ from how most platforms approach the same problems — no Ingress controller, no cert-manager, no external CI runners, encrypted DB columns instead of K8s Secrets, and a WireGuard mesh instead of cloud VPC lock-in.

[**CONCEPTS.md**](./CONCEPTS.md) walks through each of these decisions: what the standard approach is, what Meshploy does instead, and why.

---

## How It Works

```
User → Caddy (TLS) → Go Proxy → WireGuard Mesh → K3s Worker Node
                         ↑
                    "Ask & Resolve"
                  reads Host header,
                  looks up route in DB,
                  finds Headscale IP + port
```

**Every request:**
1. Caddy terminates TLS and blindly forwards to the Meshploy Proxy
2. The Proxy reads the `Host` header and queries the database for the matching route
3. The request is streamed over the WireGuard mesh to the target node's K3s ingress
4. Worker nodes never bind to public interfaces — all traffic flows through the mesh

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

# Update the CLI on an existing node without re-running install
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh)" _ --cli-only
```

See [**apps/cli/README.md**](./apps/cli/README.md) for the full command reference.

---

## Features (Community Edition)

- **Application deployments** — Node.js, Go, Python, Ruby, and any Dockerfile
- **Build systems** — Nixpacks, Heroku Buildpacks, Dockerfile, or pre-built images
- **Managed databases** — PostgreSQL, MySQL, Redis, MongoDB provisioned as K3s workloads
- **Docker Compose support** — Lift-and-shift existing compose files
- **1-Click Templates** — Ghost, WordPress, Outline, Meilisearch, and more
- **Multi-node K3s cluster** — Unlimited worker nodes joining over the Headscale mesh
- **Automated backups** — Scheduled dumps to any S3-compatible storage (R2, MinIO, AWS)
- **Webhook notifications** — Slack, Discord, email, or generic webhooks on deploy events
- **Real-time monitoring** — Node and container CPU / memory / network metrics
- **Multi-tenant RBAC** — Organizations, projects, Owner / Admin / Member roles, per-resource permissions

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
│       ├── models.go     # All 19 CE table definitions
│       ├── db.go         # Open(), Migrate(), RegisterMigration() (EE hook)
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
- A public domain with NS records pointing to this server
- Ports **80**, **443**, **53** (TCP+UDP) open on the gateway
- Root / sudo access

### Install

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh)"
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

> Replace `URL` with `https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh`

> **TLS cert cache**: Caddy stores Let's Encrypt certificates in a Docker volume. `--reinstall` always preserves this volume to avoid hitting rate limits (5 certs per domain per week). Use `--wipe-data` only when you genuinely need a clean slate.

### Private repo (while in development)

```bash
export GITHUB_PAT=ghp_xxxx
sudo -E bash -c "$(curl -fsSL "https://${GITHUB_PAT}@raw.githubusercontent.com/meshploy/meshploy/main/get.sh")"
```

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

### 2. Start the infrastructure

```bash
cd deploy && docker compose up -d
```

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

## Open-Core Model

Meshploy is open-core. The Community Edition (this repository) is MIT-licensed and fully functional. An Enterprise Edition adds multi-tailnet isolation, SSO/SAML, audit logs, and multi-cluster fleet management via a separate private module that extends CE via Go's `init()` side-effect pattern — the CE codebase has zero awareness of EE.

---

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feat/my-feature`
3. Follow the coding standards in `CLAUDE.md`
4. Open a pull request

---

## License

Community Edition — [MIT](LICENSE)

---

## AI Usage Disclosure

This project was built with AI assistance. Architecture decisions, code generation, and debugging were done with the help of Claude Code and Gemini. Every generated output was reviewed, tested, and adapted by the author.
