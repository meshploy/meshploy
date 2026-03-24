# Meshploy

**The open-source, zero-trust Internal Developer Platform.**

Meshploy is a self-hosted PaaS that orchestrates multi-node deployments across a WireGuard mesh network. Worker nodes are completely dark to the public internet — no open ports, no exposed services. The only public-facing component is the Meshploy Edge Gateway.

Deploy apps, provision managed databases, and ship to a global distributed cluster with a Vercel-like developer experience backed by enterprise-grade infrastructure.

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

## Getting Started

### Prerequisites

- Go 1.22+
- Node.js 20+
- PostgreSQL 15+
- Docker (for infra)

### 1. Clone and configure

```bash
git clone https://github.com/meshploy/meshploy
cd meshploy
cp .env.example .env
```

Edit `.env` with your values:

```bash
DATABASE_URL=postgres://user:password@localhost:5432/meshploy?sslmode=disable
JWT_SECRET=your-long-random-secret
ENCRYPTION_KEY=exactly-32-characters-here!!!!!   # openssl rand -hex 16
```

### 2. Start the infrastructure

```bash
cd deploy && docker compose up -d
```

This starts Headscale (WireGuard mesh) and CoreDNS.

### 3. Run the API

The API runs database migrations automatically on startup.

```bash
cd apps/api && go run main.go
```

API available at `http://localhost:4000`
OpenAPI docs at `http://localhost:4000/docs`

### 4. Run the Proxy

```bash
cd apps/proxy && go run main.go
```

### 5. Run the Web dashboard

```bash
cd apps/web && npm install && npm run dev
```

Dashboard at `http://localhost:3000`

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
