# Meshploy — Architectural Concepts

For contributors and engineers who want to understand how Meshploy is built and why each technical decision was made. Assumes you've read the README and are comfortable with Go, containers, and basic Kubernetes concepts.

For user-facing questions — why NS delegation is required, how traffic reaches an app, what happens if the gateway goes down — see [HOW_IT_WORKS.md](./HOW_IT_WORKS.md).

---

## System overview

One machine is public. Everything else is dark.

```
Internet
  └── Gateway (public IP)
        ├── Caddy          — TLS termination, reverse proxy
        ├── apps/proxy     — L7 routing over WireGuard mesh
        ├── apps/api       — REST API, business logic
        ├── Headscale      — WireGuard control plane
        ├── CoreDNS        — authoritative DNS + internal mesh DNS
        ├── PostgreSQL     — source of truth for API and proxy
        └── Built-in registry — image store (mesh-only, no public access)

  └── Worker nodes (no public IP, WireGuard mesh IP: 100.64.x.x)
        ├── k3s agent      — joins the K3s cluster via mesh
        ├── Workloads      — pods scheduled by K3s
        └── node_exporter  — metrics (scraped by API over mesh)
```

The K3s control plane runs on the gateway; workers join as agents. The proxy routes inbound HTTP to the right worker by reading a `routes` table from PostgreSQL — no Ingress controller, no kubectl, no cluster components on the routing path. Everything in the system — nodes, services, routes, deployments, build jobs — is a row in PostgreSQL, owned by an organization.

---

## Node connectivity

WireGuard is a kernel-level VPN protocol. Each peer has a public/private key pair and a static IP within the tunnel network. Encrypted packets travel over UDP — WireGuard handles NAT traversal, but not key distribution or peer discovery.

Headscale is the self-hosted implementation of the Tailscale control plane. It distributes WireGuard public keys to all peers, assigns each node a stable IP in `100.64.0.0/10` (CGNAT range, routable only inside the mesh), and handles the DERP relay fallback for nodes that can't reach each other directly via NAT.

**Conventional approach:** cloud platforms tie all nodes to a VPC within one provider. Adding nodes from a different provider requires site-to-site VPNs, provider-specific peering, and manually managed firewall rules on both ends. Multi-cloud is an advanced networking project.

**Meshploy's decision:** every node — regardless of provider, datacenter, or network — joins the same Headscale-managed mesh by making a single outbound `tailscale up` call to the gateway. Workers have no inbound ports. There's no provider lock-in at the network layer, no manual key distribution, and no firewall rules to punch on the worker side. The entire mesh topology is visible and managed in one place (Headscale's API, mirrored into the `nodes` table).

---

## HTTP routing

`apps/proxy` is a small Go process that reads a `routes` table from PostgreSQL and acts as an HTTP reverse proxy over the WireGuard mesh.

On startup, it loads all routes into an in-memory map (`hostname → []TargetEntry`) and starts a background goroutine that re-reads the database every 30 seconds. On a cache miss, it also falls back to a live DB query for that hostname, then warm that hostname in the cache for subsequent requests. Route lookups are O(1) for exact hostname matches; path-prefix matching (longest-prefix wins) handles services mounted at a sub-path. The proxy itself is stateless — it holds no authoritative state, just a copy of what's in PostgreSQL.

```
routes table:
  hostname                  → target_ip (mesh)   target_port (NodePort)
  myapp.example.com         → 100.64.0.3          31234
  api.example.com           → 100.64.0.3          31235
```

Traffic flows: `apps/proxy → 100.64.0.x:NodePort → kube-proxy → Pod`

Load balancing across pods is handled by kube-proxy on the receiving node via the NodePort. The proxy never needs to know about individual pod IPs or watch the Kubernetes API.

**Conventional approach:** Kubernetes Ingress controllers (Nginx, Traefik) watch the k8s API for Ingress manifests, reconcile their internal config, and reload on changes. Routing configuration lives as YAML in the cluster and requires `kubectl` or cluster API access to update.

**Meshploy's decision:** routing is the product's data model, stored in PostgreSQL like everything else. Adding a route is a DB upsert via the API — no YAML, no kubectl, no cluster access required to change traffic routing. The proxy being stateless means it can scale horizontally with no coordination; the 30-second cache window is the only staleness window.

---

## TLS automation

Caddy handles TLS using its built-in CertMagic library, which negotiates ACME challenges, stores certificates, and renews them automatically. Meshploy uses three certificate strategies to cover all traffic types:

```
Named subdomains (api, console, headscale, registry)
  → DNS-01 via CNAME to _acme-challenge.{domain}
  → resolved by CoreDNS

*.internal.{domain}  (mesh-only, bound to WireGuard IP)
  → DNS-01 via NS delegation so Let's Encrypt queries
    CoreDNS directly, bypassing recursive resolver caching

Custom user domains (myapp.com)
  → On-Demand TLS (HTTP-01): Caddy calls the Meshploy
    API to verify the domain before issuing any cert
```

A custom Caddy DNS plugin (`github.com/meshploy/caddy-dns-meshploy`) writes DNS-01 challenge TXT records directly to CoreDNS zone files on disk, and deletes them after the challenge completes. No external DNS API is involved.

**Conventional approach:** cert-manager is the standard Kubernetes TLS operator. It introduces CRDs (`Certificate`, `ClusterIssuer`, `CertificateRequest`), requires a DNS provider plugin for each registrar, and is tied to the cluster lifecycle. External DNS providers (Cloudflare, Route53) need API keys and impose rate limits.

**Meshploy's decision:** Caddy + CoreDNS eliminates the operator entirely. CertMagic handles ACME storage and renewal; CoreDNS handles TXT record writes without any external DNS API. The three-strategy approach covers named subdomains, wildcard mesh-internal hostnames, and user-supplied custom domains with a single Caddy process. No CRDs, no cluster dependency, no external provider.

---

## The build system

Builds run as ephemeral Kubernetes Jobs inside the cluster. The API creates a Job in the project's namespace; it runs on a node labelled `meshploy.com/role=builder` using a dedicated builder image that contains git, Nixpacks, Railpack, and buildah (for Dockerfile builds).

```
deployment trigger
  → API creates K8s Job in project namespace
  → Job scheduled on builder node
  → meshploy-build binary:
      git clone → detect or use builder type
      Nixpacks / Railpack / Dockerfile / pre-built image
      push → built-in registry (gateway mesh_ip:5000)
  → Job completes, cleaned up after 1h (TTLSecondsAfterFinished)
  → API updates K8s Deployment to pull new image → rolling update
```

The built-in registry runs on the gateway as a Docker container, bound to the gateway's mesh IP (`mesh_ip:5000`). It is only reachable from within the WireGuard mesh — worker nodes pull images directly, no image ever touches a public registry unless you configure one.

Builder node roles:

| Role | Builds | Customer workloads |
|---|---|---|
| `workload_builder` (default) | Yes | Yes |
| `workload` | No | Yes |
| `builder` | Yes | No (tainted) |

**Conventional approach:** external CI systems (GitHub Actions, Jenkins, Buildkite) run builds outside the deployment cluster. Build artifacts are pushed to a public or private registry, then deployed by a separate process. Two systems — CI and the platform — to configure, monitor, and reason about.

**Meshploy's decision:** builds are just pods. They get K8s resource limits, cluster-internal registry access, and are scheduled by the same scheduler as all other workloads. There's no separate CI system to operate, no credentials to share between systems, and no external registry required by default. Build logs stream through the same deployment log infrastructure as everything else.

---

## The deployment pipeline

A deployment represents one attempt to bring a service to a specific image. Each deployment is a row in the `deployments` table, recording the image reference, K8s artifact names, build log, and status.

When a deployment is triggered:

1. The API creates a `Deployment` row and a K8s `Job` (for source builds) or directly creates/updates the K8s `Deployment` (for pre-built images)
2. A reconciler goroutine watches the Job in the background, appending log lines to the deployment record until the Job completes
3. On success, the API patches the K8s `Deployment` with the new image — K3s performs a rolling update, keeping old pods alive until new pods pass health checks
4. On failure, the deployment row records the error; the K8s Deployment is not touched, so the previous version stays live

Rollback works by finding a previous successful deployment's image reference and repeating step 3 with the old image. The image already exists in the built-in registry, so rollback is instant.

**Why this matters for contributors:** the deployment goroutine in `service/deployment.go` is long-running and manages its own lifecycle. Handlers trigger deployments and return immediately; status polling and log streaming are separate endpoints backed by the deployment row's stored state plus SSE streaming from the live Job logs.

---

## Node lifecycle

A node is any machine that has joined the WireGuard mesh and the K3s cluster. Its database row in the `nodes` table holds its Headscale node ID, mesh IP, K3s role (`server` or `agent`), and a `node_secret` hash used to authenticate subsequent API calls from that node.

Nodes join via two token types:

- **Registration token** (`mreg-<hex>`) — an org-wide token stored unhashed. Any machine that presents it can join. Used for legacy/manual installs where you paste a token into the install script.
- **Provisioning token** (`mprov-<hex>`) — a single-use token created by an admin per node, stored as a bcrypt hash with an expiry time. When used, the API destroys the token and issues the node a `node_secret` for future calls. Used by `meshploy node add` and the dashboard's "Add Node" flow.

On first use, the node saves its assigned ID and secret to `/etc/meshploy/node.conf`. Subsequent calls (self-deregister, metrics ping) present this ID + secret rather than the one-time token. Self-deregistration removes the node from Headscale, drains and removes it from the K3s cluster, and deletes the database row.

**Why this matters for contributors:** the handler in `handler/node.go:SelfRegisterNode` accepts both token prefixes (`mprov-` vs `mreg-`) and branches on them. The provisioning path issues a `node_secret`; the registration path does not. This asymmetry shows up in the `nodes` table: `node_secret` is NULL for nodes registered with `mreg-` tokens.

---

## The API layer

`apps/api` uses two libraries on top of Go's `net/http`:

- **Chi** — a lightweight router. Handles URL parameter extraction, middleware chaining, and grouping. No reflection, no magic.
- **Huma** — generates OpenAPI 3.1 schemas from Go function signatures. Each handler is a function with typed input and output structs; Huma validates the request, deserializes it, calls the function, and serializes the response. Interactive docs are served at `GET /docs`.

The handler layer is strictly thin: no business logic, no direct DB access. Handlers call services, return results. The service layer (`internal/service/`) owns all business logic and talks to PostgreSQL via GORM. This boundary is enforced by convention rather than Go's type system — `Handler` holds a `*service.Services` aggregate, not individual DB connections.

```
HTTP request
  → Chi router matches path
  → Huma validates + deserializes input struct
  → handler/foo.go calls h.svc.Foo(ctx, ...)
  → service/foo.go runs business logic, calls GORM
  → handler returns output struct
  → Huma serializes response + validates against schema
```

There are two registration paths:
- `Register(api huma.API)` — all OpenAPI-documented endpoints. Huma handles validation, schema generation, and docs.
- `RegisterRaw(r chi.Router)` — endpoints that need raw `http.Handler` access: WebSocket upgrades (terminals), SSE (deployment logs), and inbound webhooks (GitHub push, deploy tokens).

**Why this matters for contributors:** if you add a new endpoint, it almost always belongs in `Register` with typed input/output structs. Only add to `RegisterRaw` if you need to take over the response writer directly (streaming, WebSocket). Business logic goes in a new method on the relevant service — never in the handler.

---

## Credential storage

The `packages/db` package provides an `EncryptedString` type — a custom GORM scanner/valuer that transparently encrypts on write and decrypts on read using AES-256-GCM.

```go
type GitIntegration struct {
    AccessToken db.EncryptedString  // AES-256-GCM ciphertext in PostgreSQL
}

// Service code uses it as a plain string
integration.AccessToken = "ghp_xxxx"   // stored as ciphertext
fmt.Println(integration.AccessToken)   // decrypted on read
```

Fields using this type are also tagged `json:"-"` — they cannot appear in any API response, regardless of how the struct is serialized.

The encryption key (`ENCRYPTION_KEY` env var, exactly 32 characters) is set once at startup via `db.SetEncryptionKey()`. All encrypted fields share this key; there is no per-field or per-org key rotation in the current implementation.

**Conventional approach:** Kubernetes Secrets are base64-encoded (not encrypted). Encryption at rest requires etcd-level configuration that most self-hosted clusters never enable. K8s Secrets are also scoped to the cluster — they can't cover credentials stored before a workload is deployed (registry credentials, git tokens, OAuth tokens), which all live in PostgreSQL.

**Meshploy's decision:** encrypt at the application layer, independent of cluster configuration. `EncryptedString` covers every sensitive field in PostgreSQL regardless of where the API runs. A database dump — from a backup leak, a misconfigured PostgreSQL, or a SQL injection — reveals only ciphertext.

---

## Resource ownership and isolation

Every resource in Meshploy belongs to an `Organization`. Nodes, integrations, notification channels, and variable groups are org-level — shared across all projects in the org. Projects are scoped within the org.

```
Organization  (one per Meshploy install)
├── Members          (roles: owner / admin / member)
├── Nodes            (shared across all projects)
├── Integrations     (registry, storage, git, notification channels)
└── Projects         (= K8s namespaces)
    ├── Services     (deployments or managed databases)
    ├── Stacks       (Docker Compose files)
    ├── Jobs         (one-off and cron)
    ├── Variable groups
    └── Routes
```

The isolation boundary that actually matters at runtime is the **project**. Each project maps directly to a Kubernetes namespace — the project slug becomes the namespace name. Workloads in one project cannot access resources in another project's namespace at the K8s level.

`resource_permissions` allows finer-grained access below the org-member role: a member can be granted explicit access to a specific service, stack, or project without being promoted to admin. `checkAccess` in `handler/access.go` checks the org-member role first, then falls back to the per-resource grant table.

**Current state:** each Meshploy install is effectively single-org — the installer creates one default organization, and there's no UI for creating or switching between multiple organizations. The schema and all API code are org-scoped from the ground up, so the data model is already correct; multi-org support (multiple independent teams sharing one install) is a planned extension.

**Why the org layer exists now:** nodes and integrations are inherently shared infrastructure. Putting them at the project level would mean duplicating node registration and registry credentials across every project — which is wrong even in a single-team setup. The org layer gives shared resources a home without making the project model do double duty.

---

## Auth model

`apps/api/internal/middleware/auth.go` runs on every route. It attempts to parse the JWT from the `Authorization` header; if valid, it stores the user in the request context. If the token is missing or invalid, it does nothing — the request continues to the handler unauthenticated.

Each handler that requires a logged-in user calls `requireUser(ctx)` explicitly:

```go
func (h *Handler) GetService(ctx context.Context, input *GetServiceInput) (*GetServiceOutput, error) {
    user, err := requireUser(ctx)   // 401 if no user in context
    if err != nil {
        return nil, err
    }
    if err := h.checkAccess(ctx, user, ResourceService, input.ServiceID); err != nil {
        return nil, err             // 403 if no permission
    }
    // ...
}
```

Public endpoints (node self-register, health check, install script, inbound webhooks) omit `requireUser` — they have their own token or signature verification inline.

**Conventional approach:** split handlers into public and protected router groups, with auth middleware only on the protected group. The group a handler is in determines whether it's protected.

**Meshploy's decision:** one middleware on all routes, explicit `requireUser` in every handler that needs it. The group-based model has a silent failure mode: accidentally registering a handler in the wrong group either silently exposes a protected endpoint or silently blocks a public one. With the soft middleware approach, auth intent is visible in the handler body — you can read any handler and know exactly what it checks, without tracing router group membership.

---

## The extension registry

`db.RegisterMigration(fn)` registers additional schema migrations that run after `AutoMigrate` and `applyConstraints`. Registered functions are collected in a slice and called in registration order by `db.Migrate()`.

```go
// Any package's init() can extend the schema:
func init() {
    db.RegisterMigration(func(d *gorm.DB) error {
        return d.AutoMigrate(&MyExtraModel{})
    })
}
```

The pattern allows external packages to extend the database schema without modifying `packages/db` directly. `packages/db` never imports the package that calls `RegisterMigration` — dependency flows one way.

**Why this matters for contributors:** this is where schema extensions belong if you're building a feature that lives outside the core `packages/db` module. Call `RegisterMigration` from your package's `init()`, and it runs automatically on API startup as long as your package is imported.

---

## The constraint behind all decisions

Every architectural choice above trades operator simplicity for engineering convention. The question asked at each decision point: *can one person understand, debug, and operate this at 2am?*

cert-manager is more powerful than CertMagic + CoreDNS. An Ingress controller is more conventional than a DB-backed proxy. External CI has more features than ephemeral K8s Jobs. Each conventional choice adds a system boundary, a new failure mode, and another component to learn.

Meshploy's architecture is a single coherent stack — WireGuard, K3s, CoreDNS, Caddy, PostgreSQL — where each component is well-understood on its own. The whole thing fits in one person's head. That constraint is intentional: the platform is for teams who shouldn't need a platform team to run it.
