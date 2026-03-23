# Meshploy: Architecture & Context

## 1. The Vision & The Problem We Solve
Meshploy is an open-source Internal Developer Platform (IDP) and PaaS. 
Most existing PaaS solutions (like Dokploy, CapRover, or Coolify) treat containers like "pets" on a single server, exposing public ports directly to the internet. 

**The Meshploy Difference (Zero-Trust Edge):**
Meshploy distributes compute across a global WireGuard mesh network using **Headscale**. 
Worker nodes are completely dark to the public internet. The only public-facing component is the Meshploy Edge Gateway (Caddy). We provide a Vercel-like developer experience (1-click deploys) with enterprise-grade, zero-trust network security.

## 2. The Networking Flow (The "Magic")
Claude, when you are writing routing or proxy code, you must understand how a packet travels through this platform. We use an **"Ask and Resolve"** dynamic routing pattern, completely avoiding static Caddyfiles for user workloads.

1. **The Request:** A user visits `api.internal.cs.example.com`.
2. **The Edge (Caddy):** Caddy receives the request. It automatically handles Let's Encrypt Wildcard TLS using our custom CoreDNS DNS-01 plugin. It unwraps the HTTPS and blindly forwards ALL traffic to our Go `proxy` app.
3. **The Brain (Go Proxy):** The `apps/proxy` service reads the `Host` header (`api.internal...`). It queries the `packages/db` database: *"Which Tailnet node and port owns this route?"*
4. **The Handoff:** The Go proxy dynamically proxies the HTTP request over the Headscale mesh network (e.g., to `100.64.0.5:3000`).
5. **The Worker:** The Docker container (or K8s K3s Ingress) on the remote node receives the request and serves the app.

## 3. The Data Mental Model (Loose Coupling)
Unlike other platforms, **Domains are NOT tightly coupled to Services.** - **Project:** A logical namespace (e.g., "Production").
- **Service:** The actual compute workload (e.g., a Docker container or K3s deployment). It only knows *what* it is and *where* it lives on the mesh.
- **Route:** The traffic cop. It maps an incoming subdomain to a target node/port. 

*Why?* If a worker node crashes, a user can deploy the Service to a new node, update the Route's target IP, and traffic shifts instantly with zero DNS propagation delay.

## 4. The Monorepo Structure
We use Go Workspaces to share code without publishing packages.
- `apps/web`: Next.js frontend (The UI & Setup Wizard).
- `apps/api`: Fiber Go REST API (Handles UI requests, talks to Docker/Headscale).
- `apps/proxy`: Standard `net/http` Go reverse proxy (The Edge Router).
- `packages/db`: Shared GORM SQLite schema. **(Source of truth for data).**
- `deploy/`: Infrastructure control plane (Headscale, CoreDNS, docker-compose).

## 5. AI Engineering Directives
When contributing to this codebase, Claude MUST adhere to these principles:
1. **Never break the Mesh:** Never expose a user's Docker container to `0.0.0.0`. Containers should only bind to the internal Docker network or the `tailscale0` interface.
2. **Keep the Edge Dumb:** Do not write Go code that modifies Caddy files via the Caddy API. Caddy is just a TLS terminator. All Layer 7 routing logic belongs in `apps/proxy`.
3. **Think Distributed:** Always assume Services might be running on a completely different machine than the API/Web dashboard. Rely on Headscale IPs (`100.64.x.x`), not `localhost`.