# Meshploy CLI

The `meshploy` CLI is a static Go binary that lets you manage your Meshploy installation from the terminal — authenticate, inspect nodes, rotate tokens, and install or uninstall nodes — without touching the web dashboard.

---

## Installation

### Fresh install (gateway or worker node)

The standard `get.sh` installs the CLI automatically before running `install.sh`:

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh)"
```

### Existing node — CLI only

To install or update the CLI on a machine that is already set up (worker or gateway), use `--cli-only`. This downloads the binary and exits — it does **not** run `install.sh` or touch any node configuration:

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh)" _ --cli-only
```

The binary is installed to `/usr/local/bin/meshploy` and is immediately available system-wide.

### Private repo (while in development)

```bash
export GITHUB_PAT=ghp_xxxx
sudo -E bash -c "$(curl -fsSL "https://${GITHUB_PAT}@raw.githubusercontent.com/meshploy/meshploy/main/get.sh")" _ --cli-only
```

---

## First-time setup

After installation, authenticate once against your Meshploy instance:

```bash
meshploy auth login --api-url https://app.your-domain.com
```

This prompts for your email and password, then saves credentials to `~/.meshploy/config.json` (mode `0600`). The org ID is resolved automatically — no `--org` flag needed on subsequent commands.

---

## Commands

### `meshploy auth`

| Command | Description |
|---|---|
| `auth login --api-url <url>` | Log in and save credentials |
| `auth logout` | Remove saved credentials |
| `auth whoami` | Print the saved API URL and token preview |

### `meshploy node`

| Command | Description |
|---|---|
| `node list` | List all nodes in the cluster |
| `node delete <node-id>` | Remove a node from Headscale, k3s, and the DB |
| `node delete -y <node-id>` | Same, skip confirmation |
| `node status` | Show this machine's node identity (`/etc/meshploy/node.conf`) |
| `node install` | Run `install.sh` — join this machine as a node (requires root) |
| `node uninstall` | Run `uninstall.sh` — deregister and clean up (requires root) |
| `node token get` | Print the current node registration token |
| `node token rotate` | Generate a new registration token (invalidates the old one) |

> `node install` and `node uninstall` require root and shell out to `/opt/meshploy/install.sh` and `/opt/meshploy/uninstall.sh` respectively. They are interactive — prompts work normally.

### `meshploy project`

| Command | Description |
|---|---|
| `project list` | List projects in the org |
| `project create <name>` | Create a new project |
| `project delete <name\|id>` | Delete a project |

### `meshploy service`

| Command | Description |
|---|---|
| `service list -p <project>` | List services in a project |
| `service deploy <name\|id>` | Trigger a new deployment |
| `service start <name\|id>` | Start a stopped service |
| `service stop <name\|id>` | Stop a running service |
| `service logs <name\|id>` | Stream live container logs |
| `service delete <name\|id>` | Delete a service |
| `service create` | Interactive wizard — generates a `meshploy.toml` manifest |

Use `--project <id|slug>` or `meshploy link <project>` to set the project context.

### `meshploy job`

| Command | Description |
|---|---|
| `job list -p <project>` | List jobs |
| `job get <name\|id>` | Show job details |
| `job create --image <img> [flags]` | Create a job (`--command`, `--schedule`, `--concurrency`, `--history-limit`) |
| `job update <name\|id> [flags]` | Update job settings |
| `job run <name\|id>` | Trigger a job run now |
| `job delete <name\|id>` | Delete a job |
| `job runs list <name\|id>` | List run history |
| `job runs delete <job> <run-id>` | Delete a run record |

### `meshploy stack`

| Command | Description |
|---|---|
| `stack list -p <project>` | List stacks |
| `stack get <name\|id>` | Show stack details and spec |
| `stack services <name\|id>` | List services managed by a stack |
| `stack apply <name\|id>` | Apply the stack spec (create/update services) |
| `stack delete <name\|id>` | Delete a stack |

### `meshploy volume`

| Command | Description |
|---|---|
| `volume list -p <project>` | List volumes |
| `volume get <name\|id>` | Show volume details |
| `volume create <name> --size <gb>` | Create a persistent volume |
| `volume attach <vol> --service <svc> --mount <path>` | Attach to a service |
| `volume detach <vol> --mount <mount-id>` | Detach from its service |
| `volume delete <name\|id>` | Delete a volume (must be unattached) |

### `meshploy route`

| Command | Description |
|---|---|
| `route list -p <project>` | List routes |
| `route create --hostname <host> --service <svc>` | Map a hostname to a service |
| `route delete <route-id>` | Remove a route |

### `meshploy secret`

| Command | Description |
|---|---|
| `secret list -p <project>` | List secrets (names only) |
| `secret set <key> <value>` | Create or update a secret |
| `secret delete <key>` | Delete a secret |

### `meshploy mcp`

Starts an MCP (Model Context Protocol) server over stdio, exposing all Meshploy operations as structured tools for Claude Code or any other MCP-compatible AI agent.

```bash
meshploy mcp
```

The server reads credentials from `~/.meshploy/config.json` — the same file written by `meshploy auth login`. No extra setup is needed once you're logged in.

**Claude Code setup** — add to `.claude/settings.json`:

```json
{
  "mcpServers": {
    "meshploy": {
      "command": "meshploy",
      "args": ["mcp"]
    }
  }
}
```

Claude Code will spawn `meshploy mcp` automatically at session start. You can then ask Claude to list projects, deploy services, apply stacks, manage volumes, and more — directly against your live platform.

**Available tools:**

| Tool | What it does |
|---|---|
| `list_resources` | List any resource type (services, jobs, volumes, stacks, routes, projects, nodes) |
| `get_resource` | Get a single resource; volumes include attached mount IDs |
| `deploy_service` | Trigger a deployment |
| `start_service` / `stop_service` | Lifecycle control |
| `delete_service` | Delete service and history |
| `create_stack` / `update_stack` / `apply_stack` / `delete_stack` | Full stack lifecycle |
| `trigger_job` | Run a job now |
| `create_volume` / `attach_volume` / `detach_volume` / `delete_volume` | Volume management |
| `create_route` / `delete_route` | Route management |

Destructive tools (`delete_*`) include explicit warnings in their descriptions so the AI confirms with you before acting.

---

## Config file

Credentials are stored at `~/.meshploy/config.json`:

```json
{
  "api_url": "https://app.your-domain.com",
  "token": "<jwt>",
  "org_id": "<uuid>"
}
```

The file is created with mode `0600` (owner read/write only). Delete it or run `meshploy auth logout` to clear saved credentials.

The `--api-url` flag on any command overrides the saved value without modifying the file.

---

## Typical workflows

### Managing the cluster from any machine

```bash
meshploy auth login --api-url https://app.your-domain.com

meshploy node list
# ID      NAME       STATUS   ROLE    IP
# abc...  gateway    online   server  100.64.0.1
# def...  worker-1   online   agent   100.64.0.2

meshploy node delete def...
```

### Adding a new worker node

```bash
# On the gateway — get the registration token
meshploy node token get

# On the new machine — run the install script
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh)"
# → enter the registration token when prompted
```

### Removing a worker node cleanly

```bash
# On the worker node itself (preferred — auto-deregisters via API)
sudo meshploy node uninstall

# Or remotely from any authenticated machine
meshploy node delete <node-id>
```

---

## Building from source

```bash
cd apps/cli
go build -o meshploy .
```

The binary has no runtime dependencies (`CGO_ENABLED=0`) and runs on any Linux x86_64 or arm64 machine.

---

## Release builds

The CLI is built and published automatically by `.github/workflows/cli.yml` on every push to `main` that touches `apps/cli/`. Binaries are attached to the `cli-latest` GitHub release:

| File | Platform |
|---|---|
| `meshploy-linux-amd64` | x86_64 servers |
| `meshploy-linux-arm64` | ARM64 (Graviton, Raspberry Pi, etc.) |
