# apps/api

The Meshploy REST API. Built with Go, Chi router, and [Huma](https://huma.rocks/) for automatic OpenAPI 3.1 spec generation.

---

## Stack

| | |
|---|---|
| Language | Go 1.22+ |
| Router | Chi |
| OpenAPI | Huma v2 (OpenAPI 3.1, automatic schema + docs) |
| Database | GORM + PostgreSQL (via `packages/db`) |
| Auth | JWT (HS256, 24h expiry) |

---

## Directory structure

```
apps/api/
├── main.go
└── internal/
    ├── config/       # Typed env config — Load() from environment
    ├── middleware/   # Auth() — soft JWT middleware, sets user in ctx
    ├── handler/      # HTTP layer only — thin, delegates to service
    │   ├── handler.go          # Handler struct, Register(), requireUser()
    │   ├── auth.go             # /auth/register, /auth/login
    │   ├── org.go              # Org CRUD + member management
    │   ├── project.go          # Project CRUD
    │   ├── node.go             # Node CRUD, self-register, self-deregister, enrichment
    │   ├── workload.go         # Service CRUD
    │   ├── route.go            # Route CRUD
    │   ├── deployment.go       # Deployment list + trigger
    │   ├── domain.go           # Domain CRUD + verification
    │   └── git_integration.go  # Git provider integrations
    └── service/      # Business logic — one file per domain
        ├── service.go          # Services aggregate struct
        ├── auth.go             # Register (user + default org in tx), Login (JWT)
        ├── org.go
        ├── project.go
        ├── node.go             # Node CRUD, registration token, headscale ID
        ├── workload.go
        ├── route.go
        ├── deployment.go
        └── headscale.go        # Headscale API client (list, get, delete, rename nodes)
```

---

## API routes

All routes are under `/api/v1`. Authenticated routes require `Authorization: Bearer <jwt>`.

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/auth/register` | — | Create user + default org |
| POST | `/auth/login` | — | Return signed JWT |
| GET/POST | `/orgs` | ✓ | List / create orgs |
| GET/PUT/DELETE | `/orgs/{id}` | ✓ | Get / update / delete org |
| GET/POST | `/orgs/{id}/members` | ✓ | List / add members |
| DELETE | `/orgs/{id}/members/{userId}` | ✓ | Remove member |
| GET/POST | `/orgs/{id}/projects` | ✓ | List / create projects |
| GET/PUT/DELETE | `/orgs/{id}/projects/{id}` | ✓ | Project CRUD |
| GET/POST | `/orgs/{id}/nodes` | ✓ | List / register nodes |
| GET/PUT/DELETE | `/orgs/{id}/nodes/{id}` | ✓ | Node CRUD |
| POST | `/orgs/{id}/nodes/self-register` | — | Worker node self-registration (uses `mreg-` token) |
| DELETE | `/orgs/{id}/nodes/self-deregister` | — | Worker node self-removal (uses `mreg-` token + node ID) |
| GET/POST | `/orgs/{id}/node-registration-token` | ✓ | Get / rotate registration token |
| GET/POST | `/orgs/{id}/projects/{id}/services` | ✓ | Service CRUD |
| GET/PUT/DELETE | `/orgs/{id}/projects/{id}/services/{id}` | ✓ | Service CRUD |
| GET/POST | `/orgs/{id}/projects/{id}/routes` | ✓ | Route CRUD |
| DELETE | `/orgs/{id}/projects/{id}/routes/{id}` | ✓ | Delete route |
| GET/POST | `/orgs/{id}/projects/{id}/services/{id}/deployments` | ✓ | List / trigger deployments |
| GET | `/orgs/{id}/domains` | ✓ | List domains |
| POST/GET/PATCH/DELETE | `/orgs/{id}/domains/{id}` | ✓ | Domain CRUD |
| POST | `/orgs/{id}/domains/{id}/verify` | ✓ | Verify domain DNS |

Interactive docs are served at `GET /docs` (Huma built-in).

---

## Node enrichment

`GET /nodes` enriches each node with live Headscale peer data (online status, last seen, FQDN, tags). When a node has a stored `headscale_id` the lookup is O(1) via `GET /api/v1/node/{id}`. Nodes without an ID fall back to a linear IP scan and store the ID as a side-effect for future calls.

## Self-register / self-deregister

Worker nodes authenticate with a `mreg-<hex>` registration token rather than a user JWT:

- **Self-register** — called by `install.sh` during worker setup. Creates the node record and returns the node ID + token saved to `/etc/meshploy/node.conf`.
- **Self-deregister** — called by `uninstall.sh`. Validates the token + node ID pair, removes the node from Headscale, the k3s cluster, and the database.

---

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `DATABASE_URL` | Yes | PostgreSQL DSN |
| `JWT_SECRET` | Yes | Secret for signing JWTs |
| `ENCRYPTION_KEY` | Yes | Exactly 32 characters — AES-256-GCM field encryption |
| `API_PORT` | No | Listen port (default: `4000`) |
| `HEADSCALE_URL` | No | Headscale API URL (default: `http://localhost:8080`) |
| `HEADSCALE_API_KEY` | No | Headscale API key |

---

## Running locally

```bash
cd apps/api
go run main.go
```

API at `http://localhost:4000` · Docs at `http://localhost:4000/docs`

Database migrations run automatically on startup via `db.Migrate()`.
