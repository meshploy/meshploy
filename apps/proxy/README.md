# apps/proxy

The Meshploy Edge Proxy. A minimal L7 reverse proxy that implements the "Ask & Resolve" routing pattern — reads the `Host` header, looks up the route in an in-memory cache backed by the database, and streams the request over the WireGuard mesh to the target node.

---

## How it works

```
Caddy (TLS) → Proxy (:8081) → WireGuard mesh → K3s worker node
                  ↑
            reads Host header
            cache lookup: hostname → (target_ip, target_port)
            reverse_proxy to http://<mesh-ip>:<port>
```

1. Caddy terminates TLS and forwards workload traffic to the proxy: app hostnames under `*.<domain>`, `*.internal.<domain>` hostnames reachable over the mesh, and verified custom domains
2. The proxy strips the port from the `Host` header and looks up the route
3. On a cache hit, it creates a `httputil.ReverseProxy` targeting `http://<target_ip>:<target_port>`
4. The original `Host` is preserved as `X-Forwarded-Host` for host-aware upstreams
5. A hostname missing from the cache is looked up in the database once and cached; if there is still no route it returns `404`. Upstream errors return `502`

---

## Directory structure

The proxy's code is the `packages/proxy` module, so an edition can build its
own proxy on it; this app is the image's entrypoint, one call to `proxy.Main()`.

```
apps/proxy/
└── main.go             # proxy.Main()

packages/proxy/
├── main.go             # Main: route cache, TCP routes, HTTP listeners
├── handler.go          # ServeHTTP: Host lookup + reverse proxy
├── cache/
│   └── cache.go        # In-memory route table, reloaded within a second of a change
├── meshgate/
│   └── meshgate.go     # Who on the mesh may open an internal route
└── tcp/
    └── forwarder.go    # Published TCP ports, one listener each
```

---

## Route cache

The cache maps each hostname to a list of `TargetEntry` values (mesh IP, port and path prefix), sorted longest path first so a service mounted at a sub-path wins over one at `/`. Lookups try the exact hostname, then a wildcard (`*.parent`), then the database. It reloads within a second of any change to the routes (the `routes` count in `edge_versions`, which every write to a route table moves) and every 30 seconds regardless. All reads use a `sync.RWMutex` so hot-path lookups never block on refresh.

---

## Internal routes

An internal route is reached over the mesh, and every internal route shares the gateway's 443, so the mesh policy cannot tell them apart; the proxy does (`meshgate/`). Caddy hands it the caller's mesh address, which is a machine, which has an owner, who has grants. While the mesh policy is enforced, an internal route answers:

- the gateway itself and the cluster's machines;
- a machine whose owner is an owner or admin of the route's organisation;
- a member's machine when the member is granted what the route leads to (the route, its service, the service's stack, or its project or the project above its level) and has not switched off reaching it from their machines; a database is off until switched on;
- a machine a network rule opens the gateway's port 443 to.

Anyone else gets a 403 "Not shared with you". It follows the `mesh` count, so a grant or a new owner takes effect within a second. Until the policy is enforced, every caller passes.

---

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `DATABASE_URL` | Yes | PostgreSQL DSN — same as the API |
| `ENCRYPTION_KEY` | No | Required only if encrypted columns are read |
| `PROXY_PORT` | No | Listen port (default: `8081`) |
| `PROXY_BIND` | No | Comma-separated addresses to listen on (default: `127.0.0.1`, where Caddy reaches it). Empty means every interface |
| `MESH_IP` | No | The node's mesh address. The proxy listens there as well, so callers outside this host's network namespace - a container, which during a migration is the edge still holding 443 - can reach it |

---

## Running locally

```bash
cd apps/proxy
go run main.go
```

Proxy at `http://localhost:8081`. Requires a running PostgreSQL instance with at least one row in the `routes` table to test routing.
