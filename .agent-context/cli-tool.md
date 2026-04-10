---
status: pending-implementation
delete-when: meshploy CLI tool is implemented and shipped
---

# Context: Meshploy CLI Tool (`meshploy` or `mctl`)

**THIS FILE IS TEMPORARY.** Delete it once the CLI is implemented.

---

## Background

Currently all node lifecycle operations are driven by bash scripts:

| Script | Purpose |
|---|---|
| `deploy/install.sh` | Interactive installer for master and worker nodes |
| `deploy/uninstall.sh` | Interactive uninstaller for master and worker nodes |

These scripts work but have significant limitations:
- Interactive-only (hard to automate or script)
- No way to run individual operations (e.g. just register a node, just delete from Headscale)
- Error messages are scattered across bash `warn`/`die` calls
- No machine-readable output
- Difficult to maintain as feature surface grows

---

## Decision: Build a CLI Tool

Replace the bash scripts with a proper CLI binary (`meshploy` or `mctl`) that:
- Wraps every operation the scripts perform
- Supports both interactive (human) and non-interactive (CI/automation) modes
- Produces structured output (JSON with `--json` flag)
- Calls the Meshploy REST API directly where possible (no SSH needed)

---

## Commands to Implement

### `meshploy node` — Node lifecycle

| Command | What it does | Script equivalent |
|---|---|---|
| `meshploy node install master` | Full master install (interactive) | `install.sh` → master path |
| `meshploy node install worker` | Full worker install (interactive) | `install.sh` → worker path |
| `meshploy node uninstall` | Auto-detect role and uninstall | `uninstall.sh` |
| `meshploy node uninstall --worker` | Force worker uninstall path | `uninstall.sh --worker` |
| `meshploy node register` | Self-register this machine with the API | curl to `/api/v1/nodes/self-register` |
| `meshploy node list` | List all nodes in the org | `GET /api/v1/orgs/{id}/nodes` |
| `meshploy node delete <name-or-id>` | Delete node from DB + Headscale | `DELETE /api/v1/orgs/{id}/nodes/{id}` |
| `meshploy node status` | Show this node's mesh + k3s status | `tailscale status` + k8s node status |

### `meshploy mesh` — Headscale / WireGuard mesh

| Command | What it does |
|---|---|
| `meshploy mesh join` | Run `tailscale up` with the right flags |
| `meshploy mesh leave` | `tailscale logout` + deregister from Headscale |
| `meshploy mesh key generate` | `POST /api/v1/cluster/headscale-preauth-key` |
| `meshploy mesh key status` | `GET /api/v1/cluster/headscale-preauth-key` |
| `meshploy mesh nodes` | List Headscale nodes (via gateway API) |

### `meshploy cluster` — k3s cluster

| Command | What it does |
|---|---|
| `meshploy cluster join` | Install k3s agent and join the cluster |
| `meshploy cluster leave` | Drain + remove this node from k3s |
| `meshploy cluster token` | `GET /api/v1/cluster/join-token` |
| `meshploy cluster status` | Show k3s node status |

### `meshploy auth` — Authentication

| Command | What it does |
|---|---|
| `meshploy auth login` | Prompt for email/password, store JWT in `~/.meshploy/config` |
| `meshploy auth logout` | Delete stored credentials |
| `meshploy auth token` | Print the current JWT (for scripting) |

### `meshploy org` — Organisation

| Command | What it does |
|---|---|
| `meshploy org list` | List orgs the current user belongs to |
| `meshploy org use <slug>` | Set default org in `~/.meshploy/config` |

---

## Config File

Store credentials and defaults in `~/.meshploy/config` (YAML or JSON):

```yaml
api_url: https://api.meshp.pnath.com
token: eyJhbGci...
org_id: uuid-here
org_slug: meshploy
```

All commands read from this file. Override with:
- `--api-url` flag
- `MESHPLOY_API_URL` env var
- `--token` flag
- `MESHPLOY_TOKEN` env var

---

## Implementation Language

**Go** — same as the API. Reasons:
- Single static binary, no runtime deps (critical for install scripts)
- Cross-compile for linux/amd64, linux/arm64, darwin/arm64 easily
- `cobra` + `viper` are the standard CLI stack in Go
- Can share types with the API (via `packages/db` or a new `packages/client` SDK package)

### Suggested package layout

```
apps/
  cli/               # new module: github.com/meshploy/apps/cli
    main.go
    cmd/
      root.go        # cobra root, persistent flags, config loading
      auth.go        # auth login/logout/token
      node.go        # node install/uninstall/register/list/delete/status
      mesh.go        # mesh join/leave/key
      cluster.go     # cluster join/leave/token/status
      org.go         # org list/use
    internal/
      client/        # thin wrapper around the Meshploy REST API
        client.go    # http client, auth header injection, error parsing
        nodes.go
        cluster.go
        orgs.go
      install/       # node install logic (ported from install.sh)
        master.go
        worker.go
      uninstall/     # uninstall logic (ported from uninstall.sh)
        master.go
        worker.go
```

Add `apps/cli` to `go.work`.

---

## What the install.sh / uninstall.sh scripts should do long-term

Once the CLI exists, the bash scripts become thin wrappers:

```bash
# get.sh downloads the CLI binary and runs:
meshploy node install master   # or worker
```

The bash scripts handle only:
1. Downloading the CLI binary for the right platform
2. Exec'ing into the CLI

---

## Key behaviours to preserve from the bash scripts

- `tailscale up` must always pass `--force-reauth` when using `--authkey`
- `tailscale up` must use `--hostname`, `--accept-routes`
- After joining Headscale, poll up to 60 s for API reachability at `{MESHPLOY_API_URL}/api/v1/auth/login` before self-registering
- k3s agent install via `curl -sfL https://get.k3s.io | K3S_URL=... K3S_TOKEN=... sh -s - agent`
- k3s uninstall via `k3s-agent-uninstall.sh`
- Worker uninstall: k3s-agent first, then `tailscale logout`
- Print human-readable progress (coloured, indented) by default; `--json` for machine output
- Non-interactive mode via `--yes` / env vars for all prompts

---

## Files to Create

| File | Notes |
|---|---|
| `apps/cli/main.go` | Entry point |
| `apps/cli/go.mod` | New module `github.com/meshploy/apps/cli` |
| `go.work` | Add `apps/cli` |
| `apps/cli/cmd/*.go` | One file per subcommand group |
| `apps/cli/internal/client/*.go` | REST API client |
| `apps/cli/internal/install/*.go` | Install logic |
| `apps/cli/internal/uninstall/*.go` | Uninstall logic |
| `.github/workflows/cli.yml` | Build + release workflow (linux/amd64, linux/arm64, darwin/arm64) |

---

## What Does NOT Change (yet)

- `deploy/install.sh` and `deploy/uninstall.sh` stay as-is — they are the fallback
  until the CLI binary is available via a release URL
- The REST API does not change — the CLI is a consumer, not a replacement
- The web dashboard continues to work independently

