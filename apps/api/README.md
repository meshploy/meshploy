# apps/api

The Meshploy REST API. Built with Go, Chi router, and [Huma](https://huma.rocks/) for automatic OpenAPI 3.1 spec generation.

---

## Stack

| | |
|---|---|
| Language | Go 1.25+ |
| Router | Chi |
| OpenAPI | Huma v2 (OpenAPI 3.1, automatic schema + docs) |
| Database | GORM + PostgreSQL (via `packages/db`) |
| Auth | JWT (HS256, 24h expiry) + TOTP 2FA; `magt-` agent tokens for automation |

---

## Directory structure

`apps/api/` is a thin entrypoint — its `main.go` just calls `server.Main()`. The actual API core (config, HTTP handlers, business logic) lives in `packages/server`, which `apps/api` imports via a `go.work` replace directive. This split lets the API core be reused (e.g. embedded as the in-gateway `/mcp` server) without depending on the `apps/api` binary.

```
apps/api/
├── main.go        # Calls server.Main() — no business logic here
├── Dockerfile
├── go.mod
└── tools/

packages/server/
├── server.go       # HTTP server setup, route registration
├── entrypoint.go   # Main() — config load, DB connect, server start
├── config/         # Typed env config — Load() from environment
├── middleware/     # Auth() resolves a JWT or magt- agent token; RequireAuth() 401s anything off the public allowlist
├── handler/        # HTTP layer only — thin, delegates to service
│   ├── handler.go          # Handler struct, Register(), RegisterRaw()
│   ├── access.go           # checkAccess(), checkOrgAdminAccess(), checkOrgMemberAccess() helpers
│   ├── auth.go             # /auth/register, /auth/login, /me, TOTP, 2FA
│   ├── agent.go            # Agent principals: create, list, token mint/rotate/revoke, delete
│   ├── mcp.go              # Remote MCP (Streamable HTTP) at /mcp — agent-token authed, permission-scoped
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
│   ├── template.go         # One-click template catalog
│   ├── config_file.go      # Config file CRUD + attach/detach
│   ├── entitlement.go      # Licence status + activation
│   ├── ondemand_tls.go     # Caddy ask endpoint for on-demand TLS
│   ├── extension.go        # Extension point: extra routes (EE)
│   ├── system.go           # Version, exposure notice, install/uninstall scripts
│   └── health.go           # GET /health
├── service/        # Business logic — one file per domain
│   ├── service.go          # Services aggregate struct + New()
│   ├── auth.go             # Register (user + default org in tx), Login, TOTP
│   ├── agent.go            # Agent principals + agent_tokens; ResolveToken() for the auth middleware
│   ├── org.go              # Org CRUD, member management, invitations
│   ├── project.go          # Project CRUD
│   ├── permission.go       # Resource permission grants
│   ├── node.go             # Node CRUD, registration/provisioning tokens, monitor
│   ├── node_exporter.go    # Live metrics scraping from node_exporter
│   ├── workload.go         # Service CRUD, env vars, build/db config
│   ├── stack.go            # Stack parse, apply, sync
│   ├── job.go              # Job CRUD, trigger, reconciler goroutine
│   ├── volume.go           # Volume CRUD, mounts, K8s PVC lifecycle
│   ├── route.go            # Route + target CRUD
│   ├── domain.go           # Domain CRUD + DNS verification
│   ├── deployment.go       # Deployment trigger, rollback, K8s Job lifecycle
│   ├── backup.go           # Backup schedule, trigger, restore, retention
│   ├── backup_executor.go  # Backup/restore K8s Job execution
│   ├── notification.go     # Notification dispatch (Slack, Discord, email, webhook)
│   ├── email_config.go     # Org SMTP config
│   ├── variable_group.go   # Variable group CRUD + service attachment
│   ├── git_integration.go  # Git provider connections + OAuth flows
│   ├── registry.go         # Registry integration CRUD
│   ├── storage.go          # Storage integration CRUD
│   ├── db_explorer.go      # Live DB query + schema via K8s exec
│   ├── system.go           # Version info, install/uninstall script serving
│   ├── template.go         # Template catalog fetch/cache + deploy
│   ├── config_file.go      # Config files projected into workloads via Secrets
│   ├── exposure.go         # Host-firewall exposure notice + dismissed notices
│   ├── entitlement.go      # Licence verification + entitlements
│   ├── extension.go        # Extension point: per-org quotas (EE)
│   ├── orphans.go          # Cluster workloads that no service owns
│   ├── workload_status.go  # Reconciles stored service status with the cluster
│   ├── volume_status.go    # Reconciles stored volume status with its claim
│   ├── wsticket.go         # Single-use tickets for WebSocket auth
│   └── headscale.go        # Headscale API client (list, get, delete, rename nodes)
├── k8s/            # Kubernetes client helpers
├── templates/      # Built-in template assets
└── version/        # Build/version metadata
```

---

## API routes

Routes are under `/api/v1` unless listed under **Outside /api/v1**. Most need `Authorization: Bearer <token>`, where the token is a user's JWT (24h) or an agent's `magt-` token. The server is fail-closed: a route that is not on its public allowlist returns `401` without a token.

This lists every route registered in `packages/server/handler`. Auth: ✓ needs a bearer token, `public` needs none, otherwise the credential the route checks instead.

The OpenAPI spec is at `/openapi.json` and Huma's docs page at `/docs`. Both need the same `Authorization` header, so fetch the spec with a token:

```bash
curl -H "Authorization: Bearer <token>" https://api.<your-domain>/openapi.json
```

### Auth & identity

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/auth/login` | public | Login and receive a JWT |
| POST | `/auth/recovery` | public | Complete login with a one-time recovery code |
| POST | `/auth/register` | public | Register a new user |
| GET | `/auth/status` | public | Check whether registration is open (no users exist yet) |
| POST | `/auth/totp` | public | Complete login with TOTP code |
| GET | `/me` | ✓ | Get current user |
| PATCH | `/me/password` | ✓ | Change current user password |
| POST | `/me/recovery-codes/regenerate` | ✓ | Regenerate 2FA recovery codes (requires current TOTP code) |
| DELETE | `/me/totp` | ✓ | Disable 2FA (requires current TOTP code) |
| POST | `/me/totp/enable` | ✓ | Verify TOTP code and enable 2FA |
| POST | `/me/totp/setup` | ✓ | Generate a new TOTP secret (not yet enabled) |

### Orgs, members & invitations

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/invitations/{token}` | invite token | Get invitation info by token (public) |
| POST | `/invitations/{token}/accept` | invite token | Accept an invitation and create an account (public) |
| GET | `/orgs` | ✓ | List organizations for the authenticated user |
| POST | `/orgs` | ✓ | Create an organization |
| GET | `/orgs/{orgId}` | ✓ | Get an organization |
| PATCH | `/orgs/{orgId}` | ✓ | Update an organization |
| DELETE | `/orgs/{orgId}` | ✓ | Delete an organization (owner only) |
| GET | `/orgs/{orgId}/invitations` | ✓ | List pending invitations |
| POST | `/orgs/{orgId}/invitations` | ✓ | Create an invite link for a new member |
| GET | `/orgs/{orgId}/members` | ✓ | List organization members |
| POST | `/orgs/{orgId}/members` | ✓ | Add a member to an organization |
| PATCH | `/orgs/{orgId}/members/{userId}` | ✓ | Update a member's role |
| DELETE | `/orgs/{orgId}/members/{userId}` | ✓ | Remove a member from an organization |

### Permissions

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/jobs/{resourceId}/permissions` | ✓ | List permissions on a job |
| GET | `/orgs/{orgId}/members/{userId}/permissions` | ✓ | List all permission grants for a member |
| POST | `/orgs/{orgId}/members/{userId}/permissions` | ✓ | Grant a permission to a member |
| DELETE | `/orgs/{orgId}/members/{userId}/permissions` | ✓ | Revoke a permission from a member |
| GET | `/orgs/{orgId}/projects/{resourceId}/permissions` | ✓ | List permissions on a project |
| GET | `/orgs/{orgId}/services/{resourceId}/permissions` | ✓ | List permissions on a service |
| GET | `/orgs/{orgId}/stacks/{resourceId}/permissions` | ✓ | List permissions on a stack |

### Agents

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/agents` | ✓ | List agent principals in an org |
| POST | `/orgs/{orgId}/agents` | ✓ | Create an agent principal and mint its first token |
| DELETE | `/orgs/{orgId}/agents/{agentId}` | ✓ | Delete an agent principal and all its tokens/grants |
| POST | `/orgs/{orgId}/agents/{agentId}/tokens` | ✓ | Mint an additional token for an agent (rotation) |
| DELETE | `/orgs/{orgId}/agents/{agentId}/tokens/{tokenId}` | ✓ | Revoke an agent token |

### Licence & entitlements

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/entitlements` | ✓ | Current license entitlements for this install |
| POST | `/entitlements/license` | ✓ | Install a license token |

### Projects

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/projects` | ✓ | List projects in an organization |
| POST | `/orgs/{orgId}/projects` | ✓ | Create a project |
| GET | `/orgs/{orgId}/projects/{projectId}` | ✓ | Get a project |
| PATCH | `/orgs/{orgId}/projects/{projectId}` | ✓ | Update a project |
| DELETE | `/orgs/{orgId}/projects/{projectId}` | ✓ | Delete a project |
| DELETE | `/orgs/{orgId}/projects/{projectId}/build-cache` | ✓ | Clear the buildah layer cache PVC for a project |

### Nodes & cluster

| Method | Path | Auth | Description |
|---|---|---|---|
| DELETE | `/nodes/self-deregister` | node token | Self-deregister a node using its registration token and node ID |
| POST | `/nodes/self-register` | registration token | Self-register a node using a registration token |
| GET | `/orgs/{orgId}/cluster/headscale-preauth-key` | ✓ | Get the most recent active Headscale preauth key |
| POST | `/orgs/{orgId}/cluster/headscale-preauth-key` | ✓ | Generate a new Headscale preauth key for joining the WireGuard mesh |
| GET | `/orgs/{orgId}/cluster/join-token` | ✓ | Get the k3s node token for joining the cluster |
| GET | `/orgs/{orgId}/cluster/mesh-health` | ✓ | Report whether the control plane can reach Headscale |
| GET | `/orgs/{orgId}/cluster/orphans` | ✓ | List cluster workloads that no service owns |
| DELETE | `/orgs/{orgId}/cluster/orphans/{namespace}/{name}` | ✓ | Remove a cluster workload that no service owns |
| POST | `/orgs/{orgId}/node-provisioning-tokens` | ✓ | Create a single-use node provisioning token |
| GET | `/orgs/{orgId}/node-registration-token` | ✓ | Get the node registration token |
| POST | `/orgs/{orgId}/node-registration-token` | ✓ | Generate (or rotate) the node registration token |
| GET | `/orgs/{orgId}/nodes` | ✓ | List nodes in an organization |
| POST | `/orgs/{orgId}/nodes` | ✓ | Register a new node |
| GET | `/orgs/{orgId}/nodes/{nodeId}` | ✓ | Get a node |
| PATCH | `/orgs/{orgId}/nodes/{nodeId}` | ✓ | Update a node |
| DELETE | `/orgs/{orgId}/nodes/{nodeId}` | ✓ | Remove a node |
| GET | `/orgs/{orgId}/nodes/{nodeId}/metrics` | ✓ | Get live resource metrics for a node (requires node_exporter) |

### Services

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/projects/{projectId}/services` | ✓ | List services in a project |
| POST | `/orgs/{orgId}/projects/{projectId}/services` | ✓ | Create a service |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}` | ✓ | Get a service |
| PATCH | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}` | ✓ | Update a service |
| DELETE | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}` | ✓ | Delete a service |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/build-config` | ✓ | Get build config for a service |
| PATCH | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/build-config` | ✓ | Create or update build config for a service |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/build-config/deploy-token` | ✓ | Regenerate the per-service webhook deploy token |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/build-config/env-vars` | ✓ | Get build-time environment variables for a service |
| PUT | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/build-config/env-vars` | ✓ | Set build-time environment variables for a service |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/database-config` | ✓ | Get database config for a database service |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/db/query` | ✓ | Execute a database query |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/db/schema` | ✓ | Introspect database schema |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/env-vars` | ✓ | Get decrypted env vars for a service |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/pods` | ✓ | List running pods for a service |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/pods/metrics` | ✓ | Live CPU and memory usage per pod (requires metrics-server) |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/reset` | ✓ | Wipe and re-provision a database (destructive) |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/start` | ✓ | Start a service |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/stop` | ✓ | Stop a service |

### Deployments & logs

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments` | ✓ | List deployments for a service |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments` | ✓ | Trigger a new deployment |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}` | ✓ | Get a deployment |
| DELETE | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}` | ✓ | Cancel an active deployment |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}/logs/stream` | ✓ | Stream a deployment's build log (SSE) |
| DELETE | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}/record` | ✓ | Delete a deployment record |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/deployments/{deploymentId}/rollback` | ✓ | Roll back to a previous successful deployment |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/logs` | ✓ | Snapshot of a service's container logs |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/logs/stream` | ✓ | Stream a service's container logs (SSE) |

### Stacks

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/orgs/{orgId}/projects/{projectId}/apply` | ✓ | Upsert a stack from an inline compose manifest and reconcile it |
| GET | `/orgs/{orgId}/projects/{projectId}/stacks` | ✓ | List stacks for a project |
| POST | `/orgs/{orgId}/projects/{projectId}/stacks` | ✓ | Create a new stack |
| GET | `/orgs/{orgId}/projects/{projectId}/stacks/{stackId}` | ✓ | Get a stack |
| PUT | `/orgs/{orgId}/projects/{projectId}/stacks/{stackId}` | ✓ | Update a stack's spec and variables |
| DELETE | `/orgs/{orgId}/projects/{projectId}/stacks/{stackId}` | ✓ | Delete a stack |
| POST | `/orgs/{orgId}/projects/{projectId}/stacks/{stackId}/apply` | ✓ | Apply the stack spec - reconcile services |
| POST | `/orgs/{orgId}/projects/{projectId}/stacks/{stackId}/destroy` | ✓ | Destroy the services this stack created, keeping the stack |
| GET | `/orgs/{orgId}/projects/{projectId}/stacks/{stackId}/services` | ✓ | List services belonging to a stack |
| POST | `/orgs/{orgId}/projects/{projectId}/stacks/{stackId}/sync` | ✓ | Fetch spec from git source and re-apply |

### Templates

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/orgs/{orgId}/projects/{projectId}/templates/{templateId}/deploy` | ✓ | Deploy a template into a project as a stack |
| GET | `/templates` | ✓ | List one-click templates |
| POST | `/templates/refresh` | ✓ | Re-read the template catalog from its source |
| GET | `/templates/{templateId}` | ✓ | Get a template (manifest + compose) |
| GET | `/templates/{templateId}/icon` | public | Template icon image |

### Jobs

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/projects/{projectId}/jobs` | ✓ | List jobs in a project |
| POST | `/orgs/{orgId}/projects/{projectId}/jobs` | ✓ | Create a job or cron job |
| GET | `/orgs/{orgId}/projects/{projectId}/jobs/{jobId}` | ✓ | Get a job |
| PATCH | `/orgs/{orgId}/projects/{projectId}/jobs/{jobId}` | ✓ | Update a job |
| DELETE | `/orgs/{orgId}/projects/{projectId}/jobs/{jobId}` | ✓ | Delete a job |
| GET | `/orgs/{orgId}/projects/{projectId}/jobs/{jobId}/runs` | ✓ | List run history for a job |
| DELETE | `/orgs/{orgId}/projects/{projectId}/jobs/{jobId}/runs/{runId}` | ✓ | Delete a job run record |
| POST | `/orgs/{orgId}/projects/{projectId}/jobs/{jobId}/trigger` | ✓ | Manually trigger a job run |

### Volumes

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/mounts` | ✓ | List a service's volume mounts |
| GET | `/orgs/{orgId}/projects/{projectId}/volumes` | ✓ | List volumes |
| POST | `/orgs/{orgId}/projects/{projectId}/volumes` | ✓ | Create a volume |
| GET | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}` | ✓ | Get a volume |
| DELETE | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}` | ✓ | Delete a volume (must be unattached) |
| GET | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/backup` | ✓ | Get a volume's backup config |
| PUT | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/backup` | ✓ | Set a volume's backup config |
| DELETE | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/backup` | ✓ | Remove a volume's backup config |
| POST | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/mounts` | ✓ | Attach a volume to a service |
| DELETE | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/mounts/{mountId}` | ✓ | Detach a volume mount |
| PUT | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/node` | ✓ | Pin a volume to a node, or clear the pin to auto-schedule |
| GET | `/orgs/{orgId}/projects/{projectId}/volumes/{volumeId}/placement` | ✓ | Where the volume's claim is actually bound or pinned |

### Variable groups

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/variable-groups` | ✓ | List variable groups attached to service |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/variable-groups` | ✓ | Attach variable group to service |
| DELETE | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/variable-groups/{groupId}` | ✓ | Detach variable group from service |
| GET | `/orgs/{orgId}/projects/{projectId}/variable-groups` | ✓ | List variable groups |
| POST | `/orgs/{orgId}/projects/{projectId}/variable-groups` | ✓ | Create variable group |
| GET | `/orgs/{orgId}/projects/{projectId}/variable-groups/{groupId}` | ✓ | Get variable group |
| PATCH | `/orgs/{orgId}/projects/{projectId}/variable-groups/{groupId}` | ✓ | Update variable group |
| DELETE | `/orgs/{orgId}/projects/{projectId}/variable-groups/{groupId}` | ✓ | Delete variable group |
| PUT | `/orgs/{orgId}/projects/{projectId}/variable-groups/{groupId}/items` | ✓ | Upsert variable group item |
| DELETE | `/orgs/{orgId}/projects/{projectId}/variable-groups/{groupId}/items/{itemId}` | ✓ | Delete variable group item |

### Config files

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/projects/{projectId}/config-files` | ✓ | List the project's config files |
| POST | `/orgs/{orgId}/projects/{projectId}/config-files` | ✓ | Create a config file |
| GET | `/orgs/{orgId}/projects/{projectId}/config-files/{fileId}` | ✓ | Get a config file and the services mounting it |
| PATCH | `/orgs/{orgId}/projects/{projectId}/config-files/{fileId}` | ✓ | Replace a config file's content, re-applying every service using it |
| DELETE | `/orgs/{orgId}/projects/{projectId}/config-files/{fileId}` | ✓ | Delete a config file that no service mounts |
| POST | `/orgs/{orgId}/projects/{projectId}/config-files/{fileId}/attach/{serviceId}` | ✓ | Mount a config file into a service |
| DELETE | `/orgs/{orgId}/projects/{projectId}/config-files/{fileId}/attach/{serviceId}` | ✓ | Unmount a config file from a service |

### Routes

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/projects/{projectId}/routes` | ✓ | List routes in a project |
| POST | `/orgs/{orgId}/projects/{projectId}/routes` | ✓ | Create a route |
| GET | `/orgs/{orgId}/projects/{projectId}/routes/{routeId}` | ✓ | Get a route |
| DELETE | `/orgs/{orgId}/projects/{projectId}/routes/{routeId}` | ✓ | Delete a route |
| POST | `/orgs/{orgId}/projects/{projectId}/routes/{routeId}/targets` | ✓ | Add a path target to a route |
| PATCH | `/orgs/{orgId}/projects/{projectId}/routes/{routeId}/targets/{targetId}` | ✓ | Update a route target |
| DELETE | `/orgs/{orgId}/projects/{projectId}/routes/{routeId}/targets/{targetId}` | ✓ | Delete a route target |
| POST | `/orgs/{orgId}/projects/{projectId}/routes/{routeId}/verify-hostname` | ✓ | Verify DNS ownership of a custom-domain route via TXT record |
| GET | `/orgs/{orgId}/routes` | ✓ | List all routes in an organization |

### Domains & TLS

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/internal/domain-check` | public (Caddy) | Caddy ask endpoint for On-Demand TLS |
| GET | `/internal/ondemand-tls-check` | public (Caddy) | Caddy ask endpoint for On-Demand TLS (self-managed DNS mode) |
| GET | `/orgs/{orgId}/domains` | ✓ | List domains for an organization |
| GET | `/orgs/{orgId}/domains/{domainId}` | ✓ | Get a domain |

### Backups

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups` | ✓ | List backup configs for a service |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups` | ✓ | Add a backup config for a service |
| PATCH | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}` | ✓ | Update a backup config |
| DELETE | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}` | ✓ | Delete a backup config |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}/objects` | ✓ | List restore points for a backup config |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}/restore` | ✓ | Restore a database from a backup object |
| POST | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/backups/{id}/trigger` | ✓ | Manually trigger a backup |
| GET | `/orgs/{orgId}/system-backup` | ✓ | Get system backup config for an org |
| PUT | `/orgs/{orgId}/system-backup` | ✓ | Create or update system backup config |
| DELETE | `/orgs/{orgId}/system-backup` | ✓ | Delete system backup config |
| GET | `/orgs/{orgId}/system-backup/objects` | ✓ | List restore points for the system backup |
| POST | `/orgs/{orgId}/system-backup/restore` | ✓ | Restore system database from a backup object |
| POST | `/orgs/{orgId}/system-backup/trigger` | ✓ | Manually trigger the system backup |

### Notifications & email

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/email-config` | ✓ | Get the org SMTP configuration |
| PUT | `/orgs/{orgId}/email-config` | ✓ | Create or update the org SMTP configuration |
| DELETE | `/orgs/{orgId}/email-config` | ✓ | Remove the org SMTP configuration |
| GET | `/orgs/{orgId}/notification-channels` | ✓ | List notification channels |
| POST | `/orgs/{orgId}/notification-channels` | ✓ | Create a notification channel |
| PUT | `/orgs/{orgId}/notification-channels/{id}` | ✓ | Update a notification channel |
| DELETE | `/orgs/{orgId}/notification-channels/{id}` | ✓ | Delete a notification channel |

### Integrations

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/gitea/callback` | OAuth state | Gitea OAuth callback |
| GET | `/github/app-callback` | OAuth state | GitHub App installation callback |
| GET | `/github/callback` | OAuth state | GitHub OAuth callback |
| GET | `/gitlab/callback` | OAuth state | GitLab OAuth callback |
| GET | `/orgs/{orgId}/git-integrations` | ✓ | List git integrations |
| POST | `/orgs/{orgId}/git-integrations` | ✓ | Create a GitLab or Gitea integration via personal access token |
| POST | `/orgs/{orgId}/git-integrations/github` | ✓ | Start a GitHub App integration (manifest flow) |
| POST | `/orgs/{orgId}/git-integrations/oauth` | ✓ | Start a GitLab or Gitea OAuth App connection |
| DELETE | `/orgs/{orgId}/git-integrations/{id}` | ✓ | Delete a git integration |
| GET | `/orgs/{orgId}/git-integrations/{id}/branches` | ✓ | List branches for a repository |
| GET | `/orgs/{orgId}/git-integrations/{id}/install-url` | ✓ | Get GitHub App install URL for a specific integration |
| GET | `/orgs/{orgId}/git-integrations/{id}/oauth-reconnect` | ✓ | Re-generate OAuth authorization URL for a pending integration |
| GET | `/orgs/{orgId}/git-integrations/{id}/repos` | ✓ | List repositories for a git integration |
| GET | `/orgs/{orgId}/registry-integrations` | ✓ | List container registry integrations |
| POST | `/orgs/{orgId}/registry-integrations` | ✓ | Add a container registry integration |
| DELETE | `/orgs/{orgId}/registry-integrations/{id}` | ✓ | Remove a container registry integration |
| GET | `/orgs/{orgId}/storage-integrations` | ✓ | List object storage integrations |
| POST | `/orgs/{orgId}/storage-integrations` | ✓ | Add an object storage integration |
| DELETE | `/orgs/{orgId}/storage-integrations/{id}` | ✓ | Remove an object storage integration |

### Terminals

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/orgs/{orgId}/nodes/{nodeId}/terminal` | ticket | WebSocket: shell on a node |
| GET | `/orgs/{orgId}/projects/{projectId}/services/{serviceId}/pods/{podName}/terminal` | ticket | WebSocket: shell in a pod |
| POST | `/terminal/ticket` | ✓ | Mint a single-use ticket for a terminal WebSocket |

### Webhooks

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/webhooks/deploy/{serviceId}` | deploy token | Inbound deploy webhook |
| POST | `/webhooks/github/{integrationId}` | HMAC | Inbound GitHub push webhook |

### System

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/system/exposure` | ✓ | Report whether this gateway runs without a host firewall |
| POST | `/system/notices/{key}/dismiss` | ✓ | Dismiss a console advisory for the current user |
| GET | `/system/channels` | ✓ | Describe the stable and edge channels, where this server is, and whether it may switch |
| GET | `/system/upgrade` | ✓ | Report whether this server can be upgraded from the console, and the last upgrade |
| POST | `/system/upgrade` | ✓ | Queue an upgrade of this server to the latest build on its channel, a switch to the other channel (`{"channel": "edge"}`), or a switch to the Enterprise images the active licence grants (`{"edition": "enterprise"}`). Instance owner only |
| GET | `/system/version` | ✓ | Get current and latest platform version |

### Outside /api/v1

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/health` | public | Health check |
| GET | `/install.sh` | ✓ | Install script |
| GET/POST | `/mcp` | agent token | Remote MCP (Streamable HTTP), permission-scoped |
| GET | `/uninstall.sh` | ✓ | Uninstall script |

---

## Node enrichment

`GET /orgs/{orgId}/nodes` enriches each node with live Headscale peer data (online status, last seen, FQDN). When a node has a stored `headscale_id` the lookup is O(1). Nodes without an ID fall back to an IP scan and store the ID as a side-effect for future calls.

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
| `ENCRYPTION_KEY` | Yes | Exactly 32 characters: AES-256-GCM field encryption |
| `API_PORT` | No | Listen port (default: `4000`) |
| `API_BASE_URL` | No | Public base URL of the API (default: `http://localhost:4000`) |
| `FRONTEND_URL` | No | Console URL (default: `http://localhost:5173`) |
| `SETUP_TOKEN` | No | Gates the first registration; set by install.sh. Empty disables the check |
| `HEADSCALE_URL` | No | Headscale API URL |
| `HEADSCALE_API_KEY` | No | Headscale API key |
| `HEADSCALE_USER` | No | Headscale user pre-auth keys are created under (default: `meshploy`) |
| `KUBECONFIG` | No | Path to kubeconfig; empty = in-cluster |
| `K3S_SERVER_URL` | No | Override the k3s API URL (needed when the API runs in Docker) |
| `K3S_TLS_SERVER_NAME` | No | Name the cluster certificate is verified against when `K3S_SERVER_URL` rewrites the address |
| `K3S_SKIP_TLS_VERIFY` | No | Escape hatch: disables authentication of the cluster connection. Leave unset |
| `K3S_TOKEN` | No | Node token for workers joining the cluster |
| `BUILDER_IMAGE` | No | Override the builder container image |
| `DOMAIN` | No | Base domain; seeds the org domain record |
| `MESH_IP` | No | Gateway's WireGuard mesh IP; seeds the gateway node |
| `GATEWAY_HOSTNAME` | No | Gateway hostname, used for gateway node seeding |
| `PUBLIC_IP` | No | Gateway public IP, backfilled on the gateway node record |
| `HOST_GATEWAY_IP` | No | Docker bridge IP, used when the API runs in Docker to reach the gateway's node_exporter |
| `FIREWALL_STATE` | No | What install.sh saw on the host: `none`, `ufw` or `firewalld`. Drives the console's exposure notice |
| `FIREWALL_CHECKED_AT` | No | When install.sh checked the firewall (RFC3339) |
| `BUILTIN_REGISTRY_ENDPOINT` | No | Seed a built-in registry row per org (`<host>:<port>`) |
| `TEMPLATE_DIR` | No | Local template catalog directory; overrides the remote repo |
| `TEMPLATE_REPO` | No | GitHub `owner/repo` the catalog is fetched from (default: `meshploy/meshploy-templates`) |
| `TEMPLATE_REPO_REF` | No | Git ref for the catalog repo (default: `main`) |
| `TEMPLATE_REFRESH_INTERVAL` | No | How often the catalog cache refreshes (default: `1h`) |

---

## Running locally

```bash
cd apps/api
go run main.go
```

API at `http://localhost:4000`. The OpenAPI spec is at `/openapi.json` and needs a bearer token, like every non-public route.

Database migrations run automatically on startup via `db.Migrate()`.
