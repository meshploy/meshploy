# Meshploy CLI

The `meshploy` CLI manages your Meshploy installation from the terminal — nodes, services, deployments, stacks, volumes, secrets, integrations, and more.

---

## Installation

```bash
sudo bash -c "$(curl -fsSL https://meshploy.com/get.sh)"
```

To install or update the CLI only (skips node setup):

```bash
sudo bash -c "$(curl -fsSL https://meshploy.com/get.sh)" _ --cli-only
```

The binary is installed to `/usr/local/bin/meshploy`.

---

## First-time setup

```bash
meshploy auth login --api-url https://app.your-domain.com
```

Prompts for email and password, saves credentials to `~/.meshploy/config.json`. The org ID is resolved automatically — no `--org` flag needed on subsequent commands.

---

## Project context

Commands that operate on a project accept `-p <id|slug>`. To avoid passing it every time, link your working directory once:

```bash
cd ~/myapp
meshploy link myproject     # writes .meshploy in the current directory
```

After that, all commands in that directory pick up the project automatically. Use `meshploy link --unlink` to remove it.

---

## Commands

### `meshploy version`

```bash
meshploy version
# meshploy 0.1.0+abc1234
```

---

### `meshploy auth`

| Command | Description |
|---|---|
| `auth login --api-url <url>` | Log in and save credentials |
| `auth logout` | Remove saved credentials |
| `auth whoami` | Print the saved API URL and token preview |

If your account has 2FA enabled, `auth login` prompts for a 6-digit TOTP code after the password step. Use a recovery code instead if you've lost access to your authenticator app.

---

### `meshploy node`

| Command | Description |
|---|---|
| `node list` | List all nodes in the cluster |
| `node status` | Show this machine's node identity (`/etc/meshploy/node.conf`) |
| `node delete <id>` | Remove a node from Headscale, k3s, and the DB |
| `node remove <host>` | Cleanly uninstall a remote node over SSH |
| `node init <host>` | Prepare a remote machine over SSH (installs prerequisites) |
| `node add <host>` | Bootstrap a remote machine as a worker node over SSH |
| `node install` | Run `install.sh` on this machine — requires root |
| `node uninstall` | Run `uninstall.sh` on this machine — requires root |
| `node token get` | Print the current node registration token |
| `node token rotate` | Generate a new registration token (invalidates the old one) |

SSH commands (`remove`, `init`, `add`) accept `--identity-file` and `--port`.

---

### `meshploy project`

| Command | Description |
|---|---|
| `project list` | List all projects in the org |
| `project create <name>` | Create a new project |
| `project delete <name\|id>` | Delete a project |

---

### `meshploy service`

All service commands accept `-p <project>` or use the linked project from `.meshploy`.

**Lifecycle**

| Command | Description |
|---|---|
| `service list` | List services in the project |
| `service create` | Interactive wizard — generates a `meshploy.toml` manifest |
| `service deploy <name\|id>` | Trigger a new deployment |
| `service start <name\|id>` | Start a stopped service |
| `service stop <name\|id>` | Stop a running service |
| `service logs <name\|id>` | Stream live container logs |
| `service delete <name\|id>` | Delete a service |

`service logs` flags: `--tail <n>`, `--since <1h\|6h\|24h\|7d>`, `--follow` (default true).

**Deployments**

| Command | Description |
|---|---|
| `service deployments <name\|id>` | List deployment history |
| `service rollback <name\|id>` | Roll back to the previous successful deployment |
| `service rollback <name\|id> --to <deploy-id>` | Roll back to a specific deployment |
| `service cancel <name\|id>` | Cancel the active deployment |
| `service retry <name\|id>` | Retry the latest failed deployment |
| `service retry <name\|id> <deploy-id>` | Retry a specific deployment |

---

### `meshploy stack`

| Command | Description |
|---|---|
| `stack list` | List stacks in the project |
| `stack get <name\|id>` | Show stack details and spec |
| `stack services <name\|id>` | List services managed by a stack |
| `stack apply <name\|id>` | Apply the stack spec — create or update services |
| `stack delete <name\|id>` | Delete a stack |

---

### `meshploy job`

| Command | Description |
|---|---|
| `job list` | List jobs in the project |
| `job get <name\|id>` | Show job details |
| `job create --image <img>` | Create a job (`--command`, `--schedule`, `--concurrency`, `--history-limit`) |
| `job update <name\|id>` | Update job settings |
| `job run <name\|id>` | Trigger a job run immediately |
| `job delete <name\|id>` | Delete a job |
| `job runs list <name\|id>` | List run history |
| `job runs delete <job> <run-id>` | Delete a specific run record |

---

### `meshploy secret`

| Command | Description |
|---|---|
| `secret list` | List secret names in the project |
| `secret set <key> <value>` | Create or update a secret |
| `secret set <key>` | Create or update — reads value from stdin |
| `secret delete <key>` | Delete a secret |

---

### `meshploy volume`

| Command | Description |
|---|---|
| `volume list` | List volumes in the project |
| `volume get <name\|id>` | Show volume details |
| `volume create <name> --size <gb>` | Create a persistent volume |
| `volume attach <vol> --service <svc> --mount <path>` | Attach to a service |
| `volume detach <vol> --mount <mount-id>` | Detach from its service |
| `volume delete <name\|id>` | Delete a volume — must be unattached |

---

### `meshploy route`

| Command | Description |
|---|---|
| `route list` | List HTTP routes in the project |
| `route create --hostname <host> --service <svc>` | Map a hostname to a service |
| `route delete <route-id>` | Remove a route |

---

### `meshploy integration`

**Git**

| Command | Description |
|---|---|
| `integration git list` | List git integrations |
| `integration git add` | Interactive wizard (GitHub App, GitLab, Gitea) |
| `integration git delete <name\|id>` | Remove a git integration |

**Registry**

| Command | Description |
|---|---|
| `integration registry list` | List registry integrations |
| `integration registry add` | Interactive wizard (GHCR, DockerHub, ECR, GCR, custom) |
| `integration registry delete <name\|id>` | Remove a registry integration |

**Storage**

| Command | Description |
|---|---|
| `integration storage list` | List storage integrations |
| `integration storage add` | Interactive wizard (S3, Cloudflare R2, MinIO, Backblaze B2) |
| `integration storage delete <name\|id>` | Remove a storage integration |

---

### `meshploy install`

| Command | Description |
|---|---|
| `install node-exporter` | Install Prometheus node_exporter as a systemd service |

---

### `meshploy link`

```bash
meshploy link <project-id|slug>   # link current directory to a project
meshploy link --unlink            # remove the .meshploy file
```

---

### `meshploy update`

```bash
meshploy update
```

Downloads the latest CLI binary from GitHub and replaces the running binary in-place. Pass `--token <pat>` or set `GITHUB_PAT` if the repo is private.

---

### `meshploy alias`

| Command | Description |
|---|---|
| `alias install` | Create a shell alias symlink for the meshploy binary |
| `alias remove` | Remove alias symlinks |

---

### `meshploy mcp`

```bash
meshploy mcp
```

Starts an MCP (Model Context Protocol) server over stdio, exposing all Meshploy operations as structured tools for Claude Code or any MCP-compatible AI agent. Reads credentials from `~/.meshploy/config.json` — no extra setup beyond `meshploy auth login`.

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

---

## Config file

Credentials are stored at `~/.meshploy/config.json` (mode `0600`):

```json
{
  "api_url": "https://app.your-domain.com",
  "token": "<jwt>",
  "org_id": "<uuid>"
}
```

Run `meshploy auth logout` or delete the file to clear credentials. The `--api-url` flag on any command overrides the saved value without modifying the file.

---

## Building from source

```bash
cd apps/cli
go build -o meshploy .
```

No runtime dependencies (`CGO_ENABLED=0`). Locally built binaries report `meshploy dev` — CI injects the version at build time via `-ldflags`.
