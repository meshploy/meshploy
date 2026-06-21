# How Meshploy Works

Answers to the technical questions that come up when you first run Meshploy or want to understand what it's doing under the hood. No assumed knowledge of WireGuard, Kubernetes internals, or ACME.

---

## Why do I have to delegate NS records to my own server?

Because Meshploy issues **wildcard TLS certificates** (`*.yourdomain.com`), and the only ACME challenge type that supports wildcards is DNS-01.

Let's Encrypt verifies domain ownership differently depending on the certificate type:

- **Single hostname** (`api.example.com`) — HTTP-01: Let's Encrypt requests a specific file from your server and checks it's there.
- **Wildcard** (`*.example.com`) — DNS-01: Let's Encrypt asks you to create a TXT record at `_acme-challenge.example.com`. The value changes every renewal, so it has to be automated.

Meshploy automates DNS-01 using a custom Caddy plugin (`caddy-dns-meshploy`) that writes TXT records directly to CoreDNS zone files on your gateway. But for Let's Encrypt to *find* those records, it has to query the authoritative nameserver for your domain — which needs to be CoreDNS on your gateway.

That's why NS delegation is required: you're telling the world "DNS queries for this domain go to my server." Without it, Let's Encrypt queries your registrar's nameservers, which know nothing about the TXT record Caddy just wrote, and the certificate challenge fails.

**Practical note:** use a dedicated subdomain (`meshploy.example.com`, not `example.com`). Delegating NS for a subdomain is safe and doesn't affect your root domain's email or other services.

---

## Why are worker nodes dark to the internet?

Workers have **no open inbound ports**. They join the mesh by making an outbound connection to Headscale (the WireGuard control plane running on the gateway). Once connected, the gateway can reach them over the encrypted WireGuard mesh, but nothing on the public internet can reach them directly.

This means:

- No SSH port exposed to the internet — node access goes through the gateway's web terminal
- Containers can't accidentally bind a public port — there's no public interface to bind to
- A compromised application can't receive inbound connections from attackers

The gateway is the only public surface. Three ports: 80, 443, 53.

---

## What is Headscale and why does Meshploy need it?

WireGuard is a VPN protocol — it creates encrypted tunnels between machines. But WireGuard alone requires you to manually distribute public keys and configure which machines can talk to which. This doesn't scale past a handful of nodes.

**Tailscale** solves this: every machine gets a stable IP in the `100.64.0.0/10` range and can reach every other machine in the network automatically, including through NAT.

**Headscale** is the self-hosted version of the Tailscale control plane. Instead of depending on Tailscale's cloud, your gateway runs Headscale. Workers register with `tailscale up --login-server=https://headscale.yourdomain.com`, get a mesh IP, and are immediately reachable from the gateway over WireGuard.

The mesh is entirely under your control. Nothing leaves your infrastructure.

---

## Can I add nodes from different cloud providers, or even a home server?

Yes — that's the point. Because every node connects to the mesh over WireGuard, it doesn't matter where a node physically lives. A Hetzner VPS, an AWS spot instance, a machine in your office, and a Raspberry Pi on your home network all get a stable mesh IP and are treated identically by the scheduler and the proxy.

The only requirement is that the node can make an outbound connection to the gateway (to join Headscale). No inbound ports needed.

---

## Why K3s and not just Docker?

Docker Compose works well for a single machine. Once you have multiple machines, you need something that handles:

- **Scheduling** — deciding which node a workload runs on based on available resources
- **Self-healing** — restarting failed containers and rescheduling them if a node goes down
- **Rolling deployments** — updating an app with zero downtime
- **Resource enforcement** — CPU and memory limits per container
- **Build isolation** — build jobs run on nodes labelled `meshploy.com/role=builder` and don't compete with running apps

K3s is specifically designed to be lightweight — it runs on a $5 VPS, uses roughly 500 MB of RAM at idle, and installs with a single curl command. It replaces etcd with SQLite, drops cloud-provider components, and keeps everything that matters for running workloads.

---

## How does a request actually reach my app?

```
Browser
  → Caddy (TLS termination, gateway :443)
  → apps/proxy (:8081)
  → route cache lookup (Host header → mesh IP + NodePort)
  → WireGuard mesh
  → kube-proxy on worker (NodePort → pod)
  → your app
```

1. Caddy terminates TLS and forwards plain HTTP to `apps/proxy` on port 8081
2. The proxy reads the `Host` header, looks it up in its in-memory route cache (backed by PostgreSQL, refreshed every 30s)
3. The cache returns a worker node's WireGuard IP (`100.64.0.x`) and a K8s NodePort
4. The proxy streams the connection over the WireGuard mesh to that IP and port
5. kube-proxy on the worker intercepts the NodePort and load-balances across the app's pods

Your app never has a public IP or an exposed port.

---

## How does TLS work for custom domains (myapp.com)?

For `*.yourdomain.com` subdomains, the wildcard cert Caddy already holds covers everything.

For a completely separate domain (`myapp.com`):

1. You add `myapp.com` as a custom domain in the dashboard
2. Meshploy asks you to set a DNS TXT record to prove you own it
3. Once verified, Caddy issues a certificate on the **first request** using On-Demand TLS (HTTP-01)

Before issuing any On-Demand cert, Caddy calls the Meshploy API to check if the domain is verified. If it isn't, no cert is issued — this prevents someone from pointing a domain they don't own at your gateway and stealing a certificate.

---

## How do builds work? Where does the built image go?

When you trigger a deployment from source:

1. Meshploy creates an ephemeral K8s Job in your project's namespace
2. The job runs on a node labelled `meshploy.com/role=builder`
3. The builder container clones your repo, builds the image using Nixpacks, Railpack, or your Dockerfile, and pushes it to the built-in private registry running on the gateway (`mesh_ip:5000`)
4. The job completes and is cleaned up automatically (TTL: 1 hour)
5. The API updates your K8s Deployment to pull the new image from the built-in registry
6. K3s performs a rolling update — old pods stay up until new pods pass health checks

The built-in registry is only reachable from within the WireGuard mesh. Worker nodes pull images directly from `mesh_ip:5000` — no image ever touches a public registry unless you configure one.

---

## How do I roll back a bad deployment?

Every deployment is recorded with its image reference. From the dashboard (or the CLI), you can trigger a rollback to any previous deployment — Meshploy updates the K8s Deployment to the previous image and K3s performs a rolling update in reverse.

The rollback is instant because the image already exists in the built-in registry. No rebuild needed.

---

## What happens if the gateway goes down?

Everything stops being reachable from the internet. The gateway is a **single point of failure** — all inbound traffic flows through it.

**What survives:**
- Worker nodes keep running — pods don't stop
- Worker-to-worker mesh communication keeps working
- Data in PostgreSQL is safe

**What breaks:**
- All inbound HTTP/HTTPS traffic
- The Meshploy dashboard and API
- Web terminal access to nodes

**Recovery:** restart the gateway. Docker Compose services start automatically on boot (`restart: unless-stopped`). Caddy reuses its cached TLS certificates. Recovery is typically under a minute.

**High availability** — running a redundant gateway (active/standby behind a floating IP) is a planned feature. The proxy is already stateless and reads from a shared PostgreSQL, so the architecture supports it.

---

## What data is encrypted at rest?

Sensitive fields — registry credentials, git tokens, API keys, OAuth tokens, storage keys — are stored using an `EncryptedString` GORM type that applies AES-256-GCM encryption on every write and decryption on every read. The encryption key is your `ENCRYPTION_KEY` environment variable.

This means:
- Even if your PostgreSQL database is dumped (backup leak, SQL injection), those fields are ciphertext
- Encryption happens at the application layer — it doesn't depend on PostgreSQL-level encryption config
- The fields are tagged `json:"-"` so they can never accidentally appear in an API response

Passwords (user accounts) are hashed with bcrypt and never stored as plaintext or ciphertext — they cannot be recovered, only reset.

---

## Why PostgreSQL? Can I use SQLite?

Two separate processes read the database simultaneously: the API and the proxy. Because both run as separate containers and could run on separate machines, they need a database that handles concurrent connections across processes. SQLite doesn't support this reliably.

PostgreSQL also handles the partial unique indexes (`idx_one_owner_per_org`, `idx_unique_domain_per_org`) and BRIN indexes used for time-series metrics data, neither of which SQLite supports.

---

## Why run CoreDNS instead of using an external DNS provider?

Two reasons:

**Wildcard TLS automation** — as described above, DNS-01 challenges require writing TXT records in real time. Running your own authoritative nameserver means Caddy can write and delete those records instantly without an API call to Cloudflare or Route53.

**Internal mesh DNS** — services inside the WireGuard mesh are reachable at `*.internal.yourdomain.com`. These hostnames only resolve inside the mesh (CoreDNS answers them; nothing public can resolve them). This lets services communicate by hostname without exposing anything to the internet.

---

## When should I use the CLI instead of the dashboard?

The dashboard and the CLI both talk to the same API. The CLI doesn't yet cover every operation available in the dashboard — things like variable groups, notification channels, registry integrations, and backup management are dashboard-only for now. For full coverage from the terminal, the MCP server (used via Claude Code) exposes all API operations.

For what the CLI does cover, use whichever fits your workflow:

The CLI is especially useful for:

- **Scripting and automation** — deploy on push, trigger jobs from CI, rotate tokens on a schedule
- **Node management from any machine** — `meshploy node add user@host` SSHes into a remote machine and runs the installer without you having to touch the target server
- **Headless environments** — servers without a browser, or when you're already in a terminal session
- **Quick reads** — `meshploy service list`, `meshploy job runs` are faster than navigating the UI when you know what you're looking for

```bash
# Authenticate once
meshploy auth login --api-url https://api.yourdomain.com

# Deploy a service, tail logs, trigger a job
meshploy service deploy my-api
meshploy deployment logs --follow
meshploy job trigger nightly-cleanup
```

---

## Can I add a worker node without SSH-ing into it manually?

Yes. `meshploy node add user@host` runs from your local machine or the gateway, connects to the remote server over SSH, and runs the entire node installer automatically — Headscale registration, k3s agent join, and all.

```bash
meshploy node add ubuntu@10.0.0.5
meshploy node add root@worker.example.com --identity-file ~/.ssh/id_ed25519
```

The command fetches a registration token from the API, generates a fresh Headscale preauth key, and passes everything to the installer in non-interactive mode. You don't need to touch the remote machine at all.

---

## What is the MCP server and how do I use it?

Meshploy ships an MCP (Model Context Protocol) server that lets Claude Code operate your platform natively — reading state, triggering deployments, managing services, running queries, and more, all without leaving your editor.

**How to connect:**

Add this to your Claude Code MCP config (`~/.claude/mcp.json` or project-level `.mcp.json`):

```json
{
  "mcpServers": {
    "meshploy": {
      "command": "meshploy",
      "args": ["mcp"],
      "env": {
        "MESHPLOY_API_URL": "https://api.yourdomain.com",
        "MESHPLOY_TOKEN": "your-api-token",
        "MESHPLOY_ORG_ID": "your-org-id"
      }
    }
  }
}
```

Once connected, Claude can answer questions like "which services are down?", "show me the last deployment logs for my-api", or "deploy the latest build of my-api to production" — and act on them.

---

## What can Claude actually do through the MCP server?

The MCP server exposes over 90 tools covering every API operation. A few examples of what you can ask Claude:

- *"List all services in the production project and show me which ones haven't been deployed in the last 7 days"*
- *"Check the logs for the last failed deployment of my-api"*
- *"Create a new service called worker with image my-registry/worker:latest and 2 replicas"*
- *"Run a query on the production database to check how many users signed up this week"*
- *"Add a Slack notification channel for the staging project"*
- *"Show me all nodes and their current CPU usage"*

Destructive operations (delete, rollback, restore) are labelled `DESTRUCTIVE` in the tool descriptions, which causes Claude to ask for confirmation in chat before calling them.

---

## Is it safe to give Claude access to my Meshploy instance?

A few things to keep in mind:

**Every tool call is shown for approval.** Claude Code's permission system displays every MCP tool call before executing it — you can review and deny anything that looks wrong.

**Destructive operations require explicit confirmation.** Tools like `delete_service`, `restore_backup`, and `cancel_deployment` are labelled as destructive. Claude will ask you in chat before calling them, in addition to the permission prompt.

**The MCP server uses a regular API token.** It has the same access as the user who generated the token. You can create a dedicated read-only user in Meshploy and use that token for Claude if you want to limit what it can do.

**DB Explorer is scoped to your org.** The `db_query` tool runs queries through the Meshploy API, which enforces RBAC. Claude cannot access databases in other orgs.
