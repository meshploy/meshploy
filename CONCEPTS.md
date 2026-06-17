# Meshploy — Architectural Concepts

How Meshploy works, and why it diverges from the conventional way of doing things.
Each section shows the standard approach first, then what Meshploy does instead, and why.

---

## 1. Network Connectivity — WireGuard Mesh instead of VPC/Firewall Rules

**Standard approach:**
Workers are placed in a private cloud network (AWS VPC, GCP VPC). The control plane and workers talk to each other over private IPs within that network. You're locked to one cloud provider, or you run a site-to-site VPN if you span multiple clouds/regions.

```
Cloud VPC
├── Control node  (10.0.0.1)
├── Worker 1      (10.0.0.2)
└── Worker 2      (10.0.0.3)
All on the same private network — easy to reach each other, but tied to one provider.
```

**What Meshploy does:**
Every node — regardless of where it's hosted — joins a WireGuard mesh managed by Headscale. Headscale is a self-hosted control plane for the Tailscale protocol. Each node gets a stable mesh IP in the `100.64.0.0/10` range.

```
Internet (any cloud, any datacenter, any home server)
├── Gateway / Control plane  (mesh IP: 100.64.0.1)
├── Hetzner worker            (mesh IP: 100.64.0.2)
├── AWS spot instance         (mesh IP: 100.64.0.3)
└── Someone's home server     (mesh IP: 100.64.0.4)
All encrypted, all reachable at stable IPs regardless of physical location.
```

**Why:**
You can add any machine to the cluster — any cloud, any provider, any location. There are no firewall rules to open except the WireGuard UDP port. All traffic between nodes is encrypted by default. The gateway is the only machine exposed to the internet; everything else is dark.

---

## 2. HTTP Routing — Proxy + DB instead of Ingress Controller

**Standard approach:**
Kubernetes has a dedicated resource called an Ingress. You write an Ingress manifest describing which hostname routes to which Service. An Ingress Controller (Nginx, Traefik, etc.) watches for Ingress resources and reconfigures itself accordingly.

```
Internet
  → LoadBalancer (cloud)
  → Ingress Controller (reads Ingress manifests from k8s API)
  → Service (ClusterIP)
  → Pods
```

Routing config lives as YAML files in the cluster. Adding a new route means applying a new Ingress manifest.

**What Meshploy does:**
There is no Ingress Controller. Caddy sits on the gateway and terminates TLS. `apps/proxy` maintains an in-memory cache of the routes table (refreshed every 30s) and forwards requests over the WireGuard mesh.

```
Internet
  → Caddy (TLS termination, gateway)
  → apps/proxy (reads routes table from DB)
  → WireGuard mesh (to the worker node's mesh IP)
  → Pod (running on a worker)
```

The `routes` table stores:
```
hostname                        → target_ip      port
myapp.user.meshploy.com         → 100.64.0.3     3000
api.user.meshploy.com           → 100.64.0.3     4000
other.user.meshploy.com         → 100.64.0.4     8080
```

When a request arrives, the proxy looks up the `Host` header in the DB, gets the mesh IP and port, and streams the connection over WireGuard.

**Why:**
The routes table is just a database table — you can add, remove, and update routes with a simple SQL upsert. No kubectl, no YAML, no cluster access needed. The proxy is stateless; any number of proxy replicas read from the same DB. The routing config is also the product's data model.

**What about load balancing across replicas?**
If a customer's app is scaled to 3 pods, the `target_ip` in the routes table points to the Kubernetes Service ClusterIP (not a pod IP directly). k8s handles the round-robin to actual pods internally via iptables rules — Meshploy's proxy doesn't need to know about individual pod IPs.

```
apps/proxy → Service ClusterIP (10.43.x.x, stable) → Pod 1 / Pod 2 / Pod 3
```

---

## 3. TLS Certificates — CertMagic + Self-hosted DNS instead of cert-manager

**Standard approach:**
`cert-manager` is a Kubernetes add-on that manages TLS certificates. It watches for Certificate resources, talks to Let's Encrypt, and stores certs as k8s Secrets. Requires installing a CRD-heavy operator.

For wildcard certificates (`*.user.meshploy.com`), you need DNS-01 ACME challenge, which requires a DNS provider plugin (usually Cloudflare).

**What Meshploy does:**
Caddy uses CertMagic (its built-in ACME client) to automatically obtain and renew certificates. Caddy handles TLS with zero config — you just point a domain at it and it gets a cert.

For wildcards, Meshploy runs its own authoritative DNS server (CoreDNS) and uses a custom Caddy DNS provider plugin (`caddy-dns/meshploy`) that writes the ACME TXT challenge records directly to CoreDNS zone files.

```
Let's Encrypt
  → DNS-01 challenge: needs _acme-challenge.user.meshploy.com TXT record
  → Custom Caddy plugin writes record to CoreDNS zone file
  → CoreDNS answers the challenge
  → Wildcard cert issued: *.user.meshploy.com
```

**Why:**
No cert-manager operator, no CRDs, no extra cluster components. Caddy handles everything — ACME negotiation, cert storage, auto-renewal. Running your own authoritative DNS server means you control the entire ACME flow without depending on a third-party DNS provider.

One thing to know: authoritative DNS servers don't chase CNAMEs and recursive resolvers cache negative responses (NXDOMAIN) for the SOA TTL. CertMagic's `resolvers` config points directly at the authoritative CoreDNS server, bypassing recursive resolver caches entirely.

---

## 4. Build System — K8s Jobs instead of separate CI runners

**Standard approach:**
Build pipelines run on external CI services (GitHub Actions, GitLab CI, CircleCI) or self-hosted runner pools (GitHub Actions self-hosted, Jenkins). These are separate from the deployment cluster — you build externally, push an image, then deploy.

**What Meshploy does:**
Builds run as ephemeral Kubernetes Jobs inside the cluster itself.

```
User triggers deployment
  → API creates a K8s Job in the project's namespace
  → Job runs on a node labelled meshploy.com/role=builder
  → Builder container: clones repo, builds image, pushes to registry
  → Job completes and is cleaned up (TTLSecondsAfterFinished)
  → API creates/updates the Deployment with the new image
```

The builder runs as a container with the `meshploy-build` binary, which handles git clone, Dockerfile build, and registry push. Logs stream back to the API in real time.

**Why:**
No external CI service needed. Builds run on infrastructure you control. You can dedicate specific nodes to builds (label them `meshploy.com/role=builder`) so build workloads don't compete with customer workloads for resources. The build is just another pod — it gets resource limits, it has access to secrets, and it's scheduled by the same scheduler as everything else.

**Node roles for builds:**

| Role | Accepts builds | Accepts customer workloads |
|---|---|---|
| `workload_builder` (default) | Yes | Yes |
| `workload` | No | Yes |
| `builder` | Yes | No (tainted) |

The `meshploy.com/role=builder` label is what makes a node eligible for builds. The taint on `builder`-only nodes ensures nothing else lands there.

---

## 5. Secret Storage — Encrypted DB column instead of K8s Secrets

**Standard approach:**
Sensitive values (registry credentials, git tokens, API keys) are stored as Kubernetes Secrets, which are base64-encoded etcd entries. They're mounted into pods as environment variables or files. Access control is via RBAC.

Base64 is not encryption. etcd encryption at rest requires explicit cluster configuration.

**What Meshploy does:**
Secrets are stored in Postgres using a custom GORM type (`EncryptedString`) that transparently applies AES-256-GCM encryption on every write and decryption on every read.

```go
// In the DB model — looks like a regular string field
type Secret struct {
    Value db.EncryptedString  // encrypted in Postgres, decrypted on read
}

// Service code never touches crypto — just uses it as a string
secret.Value = "my-token"        // stored as AES-256-GCM ciphertext
fmt.Println(secret.Value)        // prints "my-token" — decrypted automatically
```

The field is also tagged `json:"-"` so it can never be accidentally included in an API response.

**Why:**
Encryption happens at the application layer, not the infrastructure layer — it doesn't depend on etcd encryption config or cloud KMS integration. The encryption key is a single `ENCRYPTION_KEY` env var. Any code that reads the field gets the plaintext automatically; there's no separate decrypt call to forget.

---

## 6. Multi-tenancy — Organizations as the Tenancy Root

**Standard approach:**
Most platforms use project-level or workspace-level isolation. Users belong to projects; resources belong to projects.

**What Meshploy does:**
The tenancy root is an `Organization`. Every resource (nodes, projects, secrets, routes, deployments) belongs to an org. Users can be members of multiple orgs with different roles (owner / admin / member). Projects are namespaces within an org.

```
Organization
├── Members (users with roles)
├── Nodes (worker machines in the mesh)
├── Projects (= K8s namespaces)
│   ├── Services (deployments or databases)
│   ├── Secrets
│   └── Routes
└── Integrations (registries, storage, notifications)
```

**Why:**
This maps naturally to how teams work — a company is an org, teams work in projects within that org, and nodes are shared infrastructure at the org level. A single Meshploy installation can serve multiple independent orgs without interference.

---

## 7. Authentication — Soft Middleware instead of Route Guards

**Standard approach:**
Protected routes are grouped under an auth middleware that blocks unauthenticated requests before they reach the handler. Public and private routes live in separate router groups.

```go
// Typical pattern
r.Group("/public", publicHandler)
r.Group("/protected", authMiddleware, protectedHandler)
```

Risk: if you accidentally register a handler in the wrong group, it's either publicly exposed or unnecessarily blocked.

**What Meshploy does:**
A single middleware runs on all routes. It attempts to parse the JWT from the Authorization header. If the token is valid, it sets the user in the request context. If not, it does nothing — it doesn't block or return 401.

Each handler that requires authentication calls `requireUser(ctx)` explicitly:

```go
func (h *Handler) GetNode(ctx context.Context, input *...) (...) {
    if _, err := requireUser(ctx); err != nil {
        return nil, err  // 401 if no user in context
    }
    // ...
}
```

Public endpoints (like `/api/v1/nodes/self-register`) simply don't call `requireUser`.

**Why:**
One middleware for the entire router. Authentication intent is explicit at the handler level — you can see at a glance whether a handler is protected by reading it, without tracing which router group it belongs to. No risk of accidentally exposing a handler because it was registered in the wrong group.

---

## 8. Open-core Boundary — Import Graph instead of Feature Flags

**Standard approach:**
Open-core products typically use feature flags: `if plan == "enterprise" { ... }`. This pollutes the community edition codebase with references to paid features.

**What Meshploy does:**
The boundary is enforced by Go's import graph. The Community Edition binary never imports the Enterprise Edition module. Paid features register themselves via an init hook:

```go
// CE code — packages/db
var eeHooks []func(*gorm.DB) error

func RegisterMigration(fn func(*gorm.DB) error) {
    eeHooks = append(eeHooks, fn)
}

// EE module — only compiled into EE binary
func init() {
    db.RegisterMigration(myEEMigration)
}
```

When the CE binary starts, `eeHooks` is empty. The same `Migrate()` function runs in both editions — it just has nothing extra to do in CE.

**Why:**
No `if EE` checks anywhere in the CE codebase. The CE binary literally cannot execute EE code because it doesn't link it. The boundary is a compile-time guarantee, not a runtime check.

---

## The Philosophy Behind All of This

Meshploy doesn't invent new technology. WireGuard, K3s, CoreDNS, Caddy, Headscale, PostgreSQL — none of these are new, and none of them were built for Meshploy. They're mature, well-understood tools that each solve one problem well.

What Meshploy does is assemble them into a coherent, opinionated stack with a clear contract: a single developer or small team should be able to run production-grade infrastructure without a dedicated DevOps function. The architecture patterns that large engineering teams take for granted — private mesh networking, encrypted secrets at rest, automated TLS with wildcard certificates, ephemeral build runners, multi-tenant RBAC — shouldn't require a platform team to operate.

The decisions documented here reflect that constraint. Every time there was a choice between a more powerful solution and a simpler one, the question was: *can one person understand, debug, and operate this at 2am when something breaks?* An Ingress controller with a CRD-heavy cert-manager setup is powerful. Caddy with a DNS plugin and a routes table in Postgres is something you can reason about end to end.

The other side of this is that the architecture doesn't cap out. A single node on a $5 VPS and a ten-node distributed cluster across three cloud providers use the exact same deployment model — WireGuard mesh, same API, same proxy, same build system. Growth doesn't require re-platforming.

That's the gap Meshploy is trying to close: the space between "deploy to a single server manually" and "hire a platform team." Robust by default, operable by one, scalable when you need it.
