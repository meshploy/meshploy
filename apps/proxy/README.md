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

1. Caddy terminates TLS and forwards all `*.internal.<domain>` traffic to the proxy
2. The proxy strips the port from the `Host` header and looks up the route
3. On a cache hit, it creates a `httputil.ReverseProxy` targeting `http://<target_ip>:<target_port>`
4. The original `Host` is preserved as `X-Forwarded-Host` for host-aware upstreams
5. On a cache miss (route not found), returns `404`; upstream errors return `502`

---

## Directory structure

```
apps/proxy/
├── main.go
└── internal/
    ├── cache/
    │   └── cache.go    # In-memory route table, refreshed from DB every 30s
    └── proxy/
        └── handler.go  # ServeHTTP — Host lookup + reverse proxy
```

---

## Route cache

The cache holds a `map[hostname]Entry` where `Entry` is `{TargetIP, TargetPort}`. It polls the `routes` table every 30 seconds via a background goroutine. All reads use a `sync.RWMutex` so hot-path lookups never block on refresh.

The 30-second refresh means new routes are live within half a minute of being created via the API.

---

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `DATABASE_URL` | Yes | PostgreSQL DSN — same as the API |
| `ENCRYPTION_KEY` | No | Required only if encrypted columns are read |
| `PROXY_PORT` | No | Listen port (default: `8081`) |

---

## Running locally

```bash
cd apps/proxy
go run main.go
```

Proxy at `http://localhost:8081`. Requires a running PostgreSQL instance with at least one row in the `routes` table to test routing.
