# Meshploy

**The open-source, zero-trust Internal Developer Platform.**

Meshploy is a self-hosted PaaS that orchestrates multi-node deployments across a WireGuard mesh network. Worker nodes are completely dark to the public internet — no open ports, no exposed services. The only public-facing component is the Meshploy Edge Gateway.

Deploy apps, provision managed databases, and ship to a global distributed cluster with a Vercel-like developer experience backed by enterprise-grade infrastructure.

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
| `apps/web` | Next.js 15 · App Router · Tailwind · shadcn/ui |
| `packages/db` | Go · GORM · PostgreSQL |
| Infrastructure | Headscale · K3s · CoreDNS · Caddy |

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
│   ├── api/              # REST API — Chi + Huma, OpenAPI 3.1
│   │   ├── main.go
│   │   └── internal/
│   │       ├── config/   # Typed env config
│   │       ├── handler/  # Huma operation handlers (thin HTTP layer)
│   │       ├── middleware/  # JWT auth
│   │       ├── server/   # Chi router + Huma wiring
│   │       └── service/  # Business logic
│   ├── proxy/            # Edge router — "Ask & Resolve" L7 proxy
│   └── web/              # Next.js dashboard + setup wizard
├── packages/
│   └── db/               # Shared GORM models — imported by api and proxy
│       ├── models.go     # All 18 CE table definitions
│       ├── db.go         # Open(), Migrate(), RegisterMigration() (EE hook)
│       ├── types.go      # Custom JSONB types (EnvVarsMap, JSONObject, StringArray)
│       └── crypto.go     # AES-256-GCM EncryptedString type
├── deploy/
│   ├── docker-compose.yml   # Production: pulls images from GHCR
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
curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh \
  -o /tmp/get.sh && sudo bash /tmp/get.sh
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
| `sudo bash /tmp/get.sh` | Fresh install |
| `sudo bash /tmp/get.sh --reinstall` | Update images and config, **preserve** database and TLS certs |
| `sudo bash /tmp/get.sh --reinstall --wipe-data` | Full reinstall from scratch, wipes database and TLS cert cache |
| `sudo bash /tmp/get.sh --uninstall` | Remove Meshploy (interactive) |

> **TLS cert cache**: Caddy stores Let's Encrypt certificates in a Docker volume. `--reinstall` always preserves this volume to avoid hitting rate limits (5 certs per domain per week). Use `--wipe-data` only when you genuinely need a clean slate.

### Private repo (while in development)

```bash
export GITHUB_PAT=ghp_xxxx
curl -fsSL "https://${GITHUB_PAT}@raw.githubusercontent.com/meshploy/meshploy/main/get.sh" \
  -o /tmp/get.sh && GITHUB_PAT=$GITHUB_PAT sudo -E bash /tmp/get.sh
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

Meshploy exposes a fully documented OpenAPI 3.1 REST API.

| Resource | Base path |
|---|---|
| Auth | `/api/v1/auth` |
| Organizations | `/api/v1/orgs` |
| Projects | `/api/v1/orgs/{orgId}/projects` |
| Nodes | `/api/v1/orgs/{orgId}/nodes` |
| Services | `/api/v1/orgs/{orgId}/projects/{projectId}/services` |
| Routes | `/api/v1/orgs/{orgId}/projects/{projectId}/routes` |
| Deployments | `/api/v1/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments` |

Interactive docs: `GET /docs` — served automatically by Huma.

---

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `DATABASE_URL` | Yes | PostgreSQL DSN |
| `JWT_SECRET` | Yes | Secret for signing JWTs |
| `ENCRYPTION_KEY` | Yes | 32-char key for AES-256 field encryption |
| `API_PORT` | No | API listen port (default: `4000`) |
| `HEADSCALE_URL` | No | Headscale API URL (default: `http://localhost:8080`) |
| `HEADSCALE_API_KEY` | No | Headscale API key |

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
