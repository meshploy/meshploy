---
status: pending-implementation
delete-when: headscale_id column added to nodes table and enrichNodes updated
---

# Context: Storing `headscale_id` on the Node Model

**THIS FILE IS TEMPORARY.** Delete it once the implementation described below is complete.

---

## Background

Meshploy has two data stores that both know about mesh nodes:

| Store | What it knows |
|---|---|
| Meshploy PostgreSQL (`nodes` table) | Name, TailscaleIP, K3s role, mesh role, status |
| Headscale SQLite (`/var/lib/headscale/db.sqlite`) | Peer ID, online status, last seen, expiry, tags, given name |

The join between them is currently done by **IP address**: `enrichNodes()` in
`apps/api/internal/handler/node.go` calls `GET /api/v1/node` (full node list),
then matches each DB node to a Headscale peer by `TailscaleIP == IPAddresses[0]`.

---

## The Problem

### 1. IP-based join is fragile

If a worker is deleted from Headscale and re-registers (e.g. after re-running the
install script), Headscale assigns a new sequential IP. The DB node still has the
old `TailscaleIP`. The join silently produces no match — Headscale peer data
disappears from the UI with no error.

### 2. Full list scan on every request

Every call to `GET /api/v1/orgs/{orgId}/nodes` or `GET /api/v1/orgs/{orgId}/nodes/{id}`
triggers `ListNodes` on Headscale (full list), even if you only need one node's data.
This adds latency proportional to the total number of Headscale peers.

---

## Decision: What to Store vs What to Fetch Live

| Field | Store in DB? | Reason |
|---|---|---|
| `headscale_id` | **Yes** | Stable, assigned once. Enables direct lookups and delete |
| `headscale_online` | No | Changes every few minutes — always stale if cached |
| `headscale_last_seen` | No | Updates on every heartbeat |
| `headscale_expiry` | No | Semi-static but not Meshploy's concern to manage |
| `headscale_tags` | No | Managed in Headscale, Meshploy has no write path |
| `headscale_fqdn` | No | Fully deterministic: `{givenName}.mesh.{DOMAIN}` — compute it |

---

## What to Implement

### 1. Add `HeadscaleID` to the `Node` model (`packages/db/models.go`)

```go
type Node struct {
    Base
    OrganizationID uuid.UUID  `gorm:"type:uuid;not null;index"                    json:"organization_id"`
    Name           string     `gorm:"not null"                                    json:"name"`
    TailscaleIP    string     `gorm:"not null"                                    json:"tailscale_ip"`
    HeadscaleID    string     `gorm:"default:''"                                  json:"headscale_id"` // ADD THIS
    // ... rest unchanged
}
```

No unique index needed — it can be empty for nodes registered before this change.

### 2. Populate it during self-registration (`apps/api/internal/handler/node.go`)

In `SelfRegisterNode`, after the node is created, look it up in Headscale by IP
and store the peer ID:

```go
func (h *Handler) SelfRegisterNode(ctx context.Context, input *SelfRegisterNodeInput) (*RegisterNodeOutput, error) {
    node, err := h.svc.Nodes.RegisterWithToken(...)
    if err != nil { ... }

    // Backfill HeadscaleID if Headscale is configured
    if h.svc.Headscale != nil {
        if hsID := lookupHeadscaleIDByIP(ctx, h.svc.Headscale, input.Body.TailscaleIP); hsID != "" {
            h.svc.Nodes.SetHeadscaleID(ctx, node.ID, hsID)
            node.HeadscaleID = hsID
        }
    }
    // ...
}
```

Also backfill during `enrichNodes` when a match is found via IP (for nodes that
existed before this change):

```go
if hs, ok := hsByIP[n.TailscaleIP]; ok {
    // ... existing enrichment ...
    if n.HeadscaleID == "" && h.svc != nil {
        go h.svc.Nodes.SetHeadscaleID(context.Background(), n.ID, hs.node.ID)
    }
}
```

### 3. Use stored ID for single-node enrichment

In `enrichNode()` (called by `GetNode`, `UpdateNode`, `DeleteNode`), when
`node.HeadscaleID != ""`, call `GET /api/v1/node/{id}` directly instead of
listing all nodes and scanning:

```go
// headscale.go — add this method
func (h *HeadscaleService) GetNode(ctx context.Context, id string) (*HeadscaleNode, error) {
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.url+"/api/v1/node/"+id, nil)
    req.Header.Set("Authorization", "Bearer "+h.key)
    // ... decode single HeadscaleNode response
}
```

### 4. Use stored ID for Headscale cleanup on node delete

In `DeleteNode` handler, if `node.HeadscaleID != ""`, call
`DELETE /api/v1/node/{id}` on Headscale to deregister the peer:

```go
func (h *Handler) DeleteNode(ctx context.Context, input *NodePathInput) (*struct{}, error) {
    node, _ := h.svc.Nodes.Get(ctx, nodeID)
    if h.svc.Headscale != nil && node.HeadscaleID != "" {
        if err := h.svc.Headscale.DeleteNode(ctx, node.HeadscaleID); err != nil {
            log.Printf("warning: deregister headscale peer %s: %v", node.HeadscaleID, err)
            // non-fatal — still delete from Meshploy DB
        }
    }
    return nil, h.svc.Nodes.Delete(ctx, nodeID)
}
```

---

## Files to Touch

| File | Change |
|---|---|
| `packages/db/models.go` | Add `HeadscaleID string` field to `Node` |
| `apps/api/internal/service/headscale.go` | Add `GetNode(ctx, id)` and `DeleteNode(ctx, id)` methods |
| `apps/api/internal/service/node.go` | Add `SetHeadscaleID(ctx, nodeID, headscaleID)` method |
| `apps/api/internal/handler/node.go` | Backfill in `SelfRegisterNode`, use ID in `enrichNode`, cleanup in `DeleteNode` |

`AutoMigrate` will add the column automatically on next API start — no manual migration needed.

---

## What Does NOT Change

- `headscale_online`, `headscale_last_seen`, `headscale_fqdn`, `headscale_tags`,
  `headscale_expiry` remain live-fetched from Headscale on each request. This is correct.
- The full list scan in `enrichNodes` (called by `ListNodes`) stays as-is — you
  still need the full list to enrich multiple nodes in one shot.
- No changes to the frontend or API response shape.
