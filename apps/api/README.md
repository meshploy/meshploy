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
| Auth | JWT (HS256, 24h expiry) + TOTP 2FA |

---

## Directory structure

```
apps/api/
├── main.go
└── internal/
    ├── config/       # Typed env config — Load() from environment
    ├── middleware/   # Auth() — soft JWT middleware, sets user in ctx
    ├── handler/      # HTTP layer only — thin, delegates to service
    │   ├── handler.go          # Handler struct, Register(), RegisterRaw()
    │   ├── access.go           # checkAccess(), checkOrgAdminAccess(), checkOrgMemberAccess() helpers
    │   ├── auth.go             # /auth/register, /auth/login, /me, TOTP, 2FA
    │   ├── org.go              # Org CRUD, member management, invitations
    │   ├── project.go          # Project CRUD
    │   ├── permission.go       # Per-resource permission grants
    │   ├── node.go             # Node CRUD, self-register, self-deregister, metrics
    │   ├── workload.go         # Service CRUD, env vars, build/db config, pods
    │   ├── stack.go            # Stack CRUD, apply, sync
    │   ├── job.go              # Job CRUD, trigger, run history
    │   ├── volume.go           # Volume CRUD, mounts, backup config
    │   ├── route.go            # Route CRUD, targets, hostname verify
    │   ├── domain.go           # Domain CRUD + DNS verification
    │   ├── deployment.go       # Deployment list, trigger, rollback, SSE logs
    │   ├── backup.go           # Service backups + system backup
    │   ├── notification.go     # Notification channels
    │   ├── email_config.go     # Org SMTP config
    │   ├── variable_group.go   # Variable group CRUD + service attach/detach
    │   ├── git_integration.go  # Git provider integrations + OAuth callbacks
    │   ├── registry.go         # Registry integration CRUD
    │   ├── storage.go          # Storage integration CRUD
    │   ├── terminal.go         # WebSocket: node terminal + pod terminal
    │   ├── webhook.go          # Inbound webhooks (GitHub push, deploy token)
    │   ├── system.go           # System version, install/uninstall scripts
    │   └── health.go           # GET /health
    └── service/      # Business logic — one file per domain
        ├── service.go          # Services aggregate struct + New()
        ├── auth.go             # Register (user + default org in tx), Login, TOTP
        ├── org.go              # Org CRUD, member management, invitations
        ├── project.go          # Project CRUD
        ├── permission.go       # Resource permission grants
        ├── node.go             # Node CRUD, registration/provisioning tokens, monitor
        ├── node_exporter.go    # Live metrics scraping from node_exporter
        ├── workload.go         # Service CRUD, env vars, build/db config
        ├── stack.go            # Stack parse, apply, sync
        ├── job.go              # Job CRUD, trigger, reconciler goroutine
        ├── volume.go           # Volume CRUD, mounts, K8s PVC lifecycle
        ├── route.go            # Route + target CRUD
        ├── domain.go           # Domain CRUD + DNS verification
        ├── deployment.go       # Deployment trigger, rollback, K8s Job lifecycle
        ├── backup.go           # Backup schedule, trigger, restore, retention
        ├── backup_executor.go  # Backup/restore K8s Job execution
        ├── notification.go     # Notification dispatch (Slack, Discord, email, webhook)
        ├── email_config.go     # Org SMTP config
        ├── variable_group.go   # Variable group CRUD + service attachment
        ├── git_integration.go  # Git provider connections + OAuth flows
        ├── registry.go         # Registry integration CRUD
        ├── storage.go          # Storage integration CRUD
        ├── db_explorer.go      # Live DB query + schema via K8s exec
        ├── system.go           # Version info, install/uninstall script serving
        └── headscale.go        # Headscale API client (list, get, delete, rename nodes)
```

---

## API routes

All routes are under `/api/v1`. Authenticated routes require `Authorization: Bearer <jwt>`. The interactive OpenAPI docs are at `GET /docs`.

### Auth & identity

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/auth/register` | — | Create user + default org |
| POST | `/auth/login` | — | Return signed JWT (prompts TOTP if enabled) |
| POST | `/auth/totp` | — | Complete TOTP step during login |
| POST | `/auth/recovery` | — | Login with a recovery code |
| GET | `/auth/status` | — | Check if any users exist (onboarding gate) |
| GET | `/me` | ✓ | Current user profile |
| PUT | `/me/password` | ✓ | Change password |
| GET/POST | `/me/totp/setup` | ✓ | Begin TOTP enrollment (returns QR seed) |
| POST | `/me/totp/enable` | ✓ | Confirm and activate TOTP |
| DELETE | `/me/totp` | ✓ | Disable TOTP |
| POST | `/me/recovery-codes/regenerate` | ✓ | Regenerate recovery codes |

### Orgs & members

| Method | Path | Auth | Description |
|---|---|---|---|
| GET/POST | `/orgs` | ✓ | List / create orgs |
| GET/PATCH/DELETE | `/orgs/{orgId}` | ✓ | Get / update / delete org |
| GET/POST | `/orgs/{orgId}/members` | ✓ | List / add members |
| PATCH/DELETE | `/orgs/{orgId}/members/{userId}` | ✓ | Update role / remove member |
| POST | `/orgs/{orgId}/invitations` | ✓ | Send email invitation |
| GET | `/orgs/{orgId}/invitations` | ✓ | List pending invitations |
| GET | `/invitations/{token}` | — | Look up invitation by token |
| POST | `/invitations/{token}/accept` | ✓ | Accept an invitation |
| GET/POST/DELETE | `/orgs/{orgId}/members/{userId}/permissions` | ✓ | List / grant / revoke resource permissions |

### Projects

| Method | Path | Auth | Description |
|---|---|---|---|
| GET/POST | `/orgs/{orgId}/projects` | ✓ | List / create projects |
| GET/PUT/DELETE | `/orgs/{orgId}/projects/{projectId}` | ✓ | Project CRUD |
| DELETE | `/orgs/{orgId}/projects/{projectId}/build-cache` | ✓ | Purge build cache for project |
| GET | `/orgs/{orgId}/projects/{resourceId}/permissions` | ✓ | Project-level permissions |

### Nodes

| Method | Path | Auth | Description |
|---|---|---|---|
| GET/POST | `/orgs/{orgId}/nodes` | ✓ | List / register nodes |
| GET/PUT/DELETE | `/orgs/{orgId}/nodes/{nodeId}` | ✓ | Node CRUD |
| GET | `/orgs/{orgId}/nodes/{nodeId}/metrics` | ✓ | Live CPU / memory / disk metrics |
| POST | `/nodes/self-register` | — | Worker self-registration (`mreg-` or `mprov-` token) |
| DELETE | `/nodes/self-deregister` | — | Worker self-removal |
| GET/POST | `/orgs/{orgId}/node-registration-token` | ✓ | Get / rotate registration token |
| GET/POST | `/orgs/{orgId}/node-provisioning-tokens` | ✓ | List / create provisioning tokens |
| GET | `/cluster/headscale-preauth-key` | ✓ admin | Generate Headscale pre-auth key |
| GET | `/cluster/join-token` | ✓ admin | Get K3s join token |

### Services

| Method | Path | Auth | Description |
|---|---|---|---|
| GET/POST | `/orgs/{orgId}/projects/{projectId}/services` | ✓ | List / create services |
| GET/PUT/DELETE | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}` | ✓ | Service CRUD |
| GET/PUT | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/env-vars` | ✓ | Get / update env vars |
| GET/PUT | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/build-config` | ✓ | Build config |
| PATCH | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/build-config/env-vars` | ✓ | Build-time env vars |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/build-config/deploy-token` | ✓ | Regenerate deploy webhook token |
| GET/PUT | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/database-config` | ✓ | Database config |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/start` | ✓ | Scale up |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/stop` | ✓ | Scale to zero |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/reset` | ✓ | Reset database (destructive) |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/pods` | ✓ | List running pods |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/pods/metrics` | ✓ | Pod CPU/memory |
| GET | `/orgs/{orgId}/services/{resourceId}/permissions` | ✓ | Service-level permissions |

### Deployments

| Method | Path | Auth | Description |
|---|---|---|---|
| GET/POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments` | ✓ | List / trigger deployment |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}` | ✓ | Get deployment |
| DELETE | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}` | ✓ | Delete record |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}/rollback` | ✓ | Roll back |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/logs` | ✓ | Container log snapshot |
| GET | `…/deployments/{deploymentId}/logs/stream` | ✓ | SSE build log stream |
| GET | `…/services/{serviceId}/logs/stream` | ✓ | SSE live container log stream |

### Stacks

| Method | Path | Auth | Description |
|---|---|---|---|
| GET/POST | `/orgs/{orgId}/projects/{projectId}/stacks` | ✓ | List / create stacks |
| GET/PUT/DELETE | `/orgs/{orgId}/projects/{projectId}/stacks/{stackId}` | ✓ | Stack CRUD |
| POST | `/orgs/{orgId}/projects/{projectId}/stacks/{stackId}/apply` | ✓ | Apply — create/update services from spec |
| POST | `/orgs/{orgId}/projects/{projectId}/stacks/{stackId}/sync` | ✓ | Sync spec from git |
| GET | `/orgs/{orgId}/projects/{projectId}/stacks/{stackId}/services` | ✓ | List stack-owned services |
| GET | `/orgs/{orgId}/stacks/{resourceId}/permissions` | ✓ | Stack-level permissions |

### Jobs

| Method | Path | Auth | Description |
|---|---|---|---|
| GET/POST | `/orgs/{orgId}/projects/{projectId}/jobs` | ✓ | List / create jobs |
| GET/PUT/DELETE | `/orgs/{orgId}/projects/{projectId}/jobs/{jobId}` | ✓ | Job CRUD |
| POST | `/orgs/{orgId}/projects/{projectId}/jobs/{jobId}/trigger` | ✓ | Trigger a run |
| GET | `/orgs/{orgId}/projects/{projectId}/jobs/{jobId}/runs` | ✓ | Run history |
| DELETE | `/orgs/{orgId}/projects/{projectId}/jobs/{jobId}/runs/{runId}` | ✓ | Delete run record |
| GET | `/orgs/{orgId}/jobs/{resourceId}/permissions` | ✓ | Job-level permissions |

### Volumes

| Method | Path | Auth | Description |
|---|---|---|---|
| GET/POST | `/orgs/{orgId}/projects/{projectId}/volumes` | ✓ | List / create volumes |
| GET/DELETE | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}` | ✓ | Get / delete volume |
| POST | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/mounts` | ✓ | Attach to service |
| DELETE | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/mounts/{mountId}` | ✓ | Detach mount |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/mounts` | ✓ | List mounts for service |
| GET/PUT/DELETE | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/backup` | ✓ | Volume backup config |

### Variable groups

| Method | Path | Auth | Description |
|---|---|---|---|
| GET/POST | `/orgs/{orgId}/projects/{projectId}/variable-groups` | ✓ | List / create groups |
| GET/PATCH/DELETE | `/orgs/{orgId}/projects/{projectId}/variable-groups/{groupId}` | ✓ | Group CRUD |
| PUT | `/orgs/{orgId}/projects/{projectId}/variable-groups/{groupId}/items` | ✓ | Upsert item |
| DELETE | `/orgs/{orgId}/projects/{projectId}/variable-groups/{groupId}/items/{itemId}` | ✓ | Delete item |
| GET/POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/variable-groups` | ✓ | List / attach group |
| DELETE | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/variable-groups/{groupId}` | ✓ | Detach group |

### Routes & domains

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/routes` | ✓ | List all routes across projects |
| GET/POST | `/orgs/{orgId}/projects/{projectId}/routes` | ✓ | List / create routes |
| GET/DELETE | `/orgs/{orgId}/projects/{projectId}/routes/{routeId}` | ✓ | Get / delete route |
| POST | `/orgs/{orgId}/projects/{projectId}/routes/{routeId}/verify-hostname` | ✓ | Verify DNS record |
| POST | `/orgs/{orgId}/projects/{projectId}/routes/{routeId}/targets` | ✓ | Add route target |
| PATCH/DELETE | `/orgs/{orgId}/projects/{projectId}/routes/{routeId}/targets/{targetId}` | ✓ | Update / remove target |
| GET | `/orgs/{orgId}/domains` | ✓ | List org domains |
| GET | `/orgs/{orgId}/domains/{domainId}` | ✓ | Get domain |

### Backups

| Method | Path | Auth | Description |
|---|---|---|---|
| GET/POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups` | ✓ | List / create backup config |
| PATCH/DELETE | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}` | ✓ | Update / delete config |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}/trigger` | ✓ | Trigger now |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}/objects` | ✓ | List backup objects in storage |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}/restore` | ✓ | Restore from object |
| GET/PUT/DELETE | `/orgs/{orgId}/system-backup` | ✓ | System backup config |
| POST | `/orgs/{orgId}/system-backup/trigger` | ✓ | Trigger system backup |
| GET | `/orgs/{orgId}/system-backup/objects` | ✓ | List system backup objects |

### Notifications & email

| Method | Path | Auth | Description |
|---|---|---|---|
| GET/POST | `/orgs/{orgId}/notification-channels` | ✓ | List / create channels |
| PUT/DELETE | `/orgs/{orgId}/notification-channels/{id}` | ✓ | Update / delete channel |
| GET/PUT/DELETE | `/orgs/{orgId}/email-config` | ✓ | Org SMTP config |

### Integrations

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/git-integrations` | ✓ | List git integrations |
| POST | `/orgs/{orgId}/git-integrations/github` | ✓ | Connect GitHub App |
| POST | `/orgs/{orgId}/git-integrations/oauth` | ✓ | Connect GitLab / Gitea via OAuth |
| GET/DELETE | `/orgs/{orgId}/git-integrations/{id}` | ✓ | Get / delete integration |
| GET | `/orgs/{orgId}/git-integrations/{id}/repos` | ✓ | List accessible repos |
| GET | `/orgs/{orgId}/git-integrations/{id}/branches` | ✓ | List branches for a repo |
| GET/POST | `/orgs/{orgId}/registry-integrations` | ✓ | List / add registry |
| DELETE | `/orgs/{orgId}/registry-integrations/{id}` | ✓ | Remove registry |
| GET/POST | `/orgs/{orgId}/storage-integrations` | ✓ | List / add storage |
| DELETE | `/orgs/{orgId}/storage-integrations/{id}` | ✓ | Remove storage |

### DB Explorer

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/db/schema` | ✓ | Live schema |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/db/query` | ✓ | Execute SQL query |

### WebSocket & system

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/nodes/{nodeId}/terminal` | ✓ | WebSocket — node shell |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/pods/{podName}/terminal` | ✓ | WebSocket — pod shell |
| POST | `/webhooks/github/{integrationId}` | HMAC | Inbound GitHub push webhook |
| POST | `/webhooks/deploy/{serviceId}` | token | Inbound deploy webhook |
| GET | `/system/version` | — | API version |
| GET | `/health` | — | Health check |

---

## Node enrichment

`GET /nodes` enriches each node with live Headscale peer data (online status, last seen, FQDN). When a node has a stored `headscale_id` the lookup is O(1). Nodes without an ID fall back to an IP scan and store the ID as a side-effect for future calls.

## Self-register / self-deregister

Worker nodes authenticate with a registration or provisioning token rather than a user JWT:

- **Self-register** — called by `install.sh`. Accepts either a `mreg-<hex>` registration token (reusable, org-wide) or a `mprov-<hex>` provisioning token (single-use, with expiry). Creates the node record and returns the node ID.
- **Self-deregister** — called by `uninstall.sh`. Removes the node from Headscale, the k3s cluster, and the database.

---

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `DATABASE_URL` | Yes | PostgreSQL DSN |
| `JWT_SECRET` | Yes | Secret for signing JWTs |
| `ENCRYPTION_KEY` | Yes | Exactly 32 characters — AES-256-GCM field encryption |
| `API_PORT` | No | Listen port (default: `4000`) |
| `HEADSCALE_URL` | No | Headscale API URL |
| `HEADSCALE_API_KEY` | No | Headscale API key |
| `GATEWAY_IP` | No | Gateway mesh IP — seeds the gateway node on first boot |
| `GATEWAY_HOSTNAME` | No | Gateway hostname — used for gateway node seeding |
| `HOST_GATEWAY_IP` | No | Docker bridge IP — used when API runs in Docker to reach gateway node_exporter |
| `PUBLIC_IP` | No | Gateway public IP — backfilled on the gateway node record |
| `DOMAIN` | No | Root domain — seeds the domain record on first org |
| `K3S_SERVER_URL` | No | K3s API URL (default: in-cluster) |
| `KUBECONFIG` | No | Path to kubeconfig (for out-of-cluster dev) |
| `BUILTIN_REGISTRY_ENDPOINT` | No | Seed a built-in registry row on org creation |

---

## Running locally

```bash
cd apps/api
go run main.go
```

API at `http://localhost:4000` · Docs at `http://localhost:4000/docs`

Database migrations run automatically on startup via `db.Migrate()`.
