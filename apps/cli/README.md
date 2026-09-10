# Meshploy CLI

The `meshploy` CLI manages your Meshploy installation from the terminal — nodes, services, deployments, stacks, volumes, secrets, integrations, and more.

Commands are grouped by what they act on:

| Group | Commands |
|---|---|
| [First-time setup](#first-time-setup) | `auth`, `link` |
| [Server management](#server-management) | `node install`, `node uninstall`, `node status`, `server-upgrade`, `setup serve`, `setup-token`, `install node-exporter`, `license` |
| [Cluster and nodes](#cluster-and-nodes) | `node list`, `node add`, `node init`, `node remove`, `node delete`, `node token` |
| [Projects and workloads](#projects-and-workloads) | `project`, `service`, `stack`, `apply`, `job`, `secret`, `volume`, `route` |
| [Integrations](#integrations) | `integration git`, `integration registry`, `integration storage` |
| [The CLI itself](#the-cli-itself) | `version`, `update`, `alias` |
| [AI agents](#ai-agents) | `mcp` |

---

## Installation

```bash
sudo bash -c "$(curl -fsSL https://meshploy.com/install.sh)"
```

To install or update the CLI only (skips node setup):

```bash
sudo bash -c "$(curl -fsSL https://meshploy.com/install.sh)" _ --cli-only
```

To install the edge build from `main` rather than the latest stable release:

```bash
sudo bash -c "$(curl -fsSL https://meshploy.com/install.sh)" _ --cli-only --edge
```

The binary is installed to `/usr/local/bin/meshploy`. Once installed, `meshploy update` and `meshploy update --edge` switch between the two channels.

---

## First-time setup

Log in once, then link the directories you work in to a project.

### `meshploy auth`

```bash
meshploy auth login --api-url https://api.your-domain.com
```

Prompts for email and password, saves credentials to `~/.meshploy/config.json`. The org ID is resolved automatically — no `--org` flag needed on subsequent commands.

| Command | Description |
|---|---|
| `auth login --api-url <url>` | Log in and save credentials |
| `auth logout` | Remove saved credentials |
| `auth whoami` | Print the saved API URL and token preview |

If your account has 2FA enabled, `auth login` prompts for a 6-digit TOTP code after the password step. Use a recovery code instead if you've lost access to your authenticator app.

### `meshploy link`

Commands that operate on a project accept `-p <id|slug>`. To avoid passing it every time, link your working directory once:

```bash
cd ~/myapp
meshploy link myproject     # writes .meshploy in the current directory
meshploy link --unlink      # removes it
```

After that, all commands in that directory pick up the project automatically.

---

## Server management

These install, upgrade and recover Meshploy on the machine you run them on, so run them there, as root. Most only make sense on the **gateway server**; `node install`, `node uninstall`, `node status` and `install node-exporter` apply to a worker too. `license` is the exception: it goes through the API, so it works from any machine you are logged in on.

### `meshploy node` on this machine

| Command | Description |
|---|---|
| `node install` | Run `install.sh` on this machine — requires root |
| `node uninstall` | Run `uninstall.sh` on this machine — requires root |
| `node status` | Show this machine's node identity (`/etc/meshploy/node.conf`) |

To add, remove or list nodes across the cluster, see [Cluster and nodes](#cluster-and-nodes).

---

### `meshploy server-upgrade`

```bash
sudo meshploy server-upgrade           # stable — latest release configs + images
sudo meshploy server-upgrade --edge    # edge — main branch configs + images
```

Syncs the `deploy/` configuration directory from GitHub and pulls the latest container images, then restarts all services. Equivalent to what the CI deploy job does for your own server.

Must be run as root on the **gateway server**.

Protected files are never overwritten: `.env`, DNS zone files, and Headscale config keep their runtime-rendered values. `coredns/Corefile` is replaced and re-rendered from `.env`. On a gateway using self-managed DNS (`DNS_MODE=ondemand`), the on-demand Caddyfile is put back after the sync, and Caddy is recreated whenever its configuration changed.

The upgrade runs with the CLI you have installed, so run `sudo meshploy update` (or `update --edge`) first.

| Flag | Description |
|---|---|
| `--edge` | Sync from the `main` branch and pull edge images instead of the latest stable release |
| `--token <pat>` | GitHub personal access token (or set `GITHUB_PAT`) — required if the repo is private |

---

### `meshploy setup serve`

```bash
sudo meshploy setup serve
```

Serves the browser installer: a page on port 9000 that collects the domain and DNS mode, runs the installer, and streams its output. `install.sh` starts it for you when you choose to continue setup in a browser. Run it yourself to reopen setup, for example to change the domain; it starts from the current configuration in `.env`. Requires root, and access is gated on the setup token from `/opt/meshploy/.env`. It serves plain HTTP, because no certificate exists yet.

| Flag | Description |
|---|---|
| `--addr` | Address to serve on (default: `0.0.0.0:9000`) |
| `--public-ip` | Public IP to pre-fill and to check DNS against (default: `PUBLIC_IP` from `.env`) |

---

### `meshploy setup-token`

```bash
sudo meshploy setup-token show      # print the token for creating the first account
sudo meshploy setup-token rotate    # issue a new one, invalidating the old
```

The first account registered on a Meshploy server owns it, so until that account exists, registration asks for a one-time setup token. The installer prints it once; these commands recover it. Both must be run as root on the **gateway server**, where the token is stored in `/opt/meshploy/.env`.

| Subcommand | Description |
|---|---|
| `show` | Prints the current token while the instance has no owner. Once an owner exists, registration is closed and the token is no longer accepted, so it says that instead of printing it. |
| `rotate` | Issues a new token and invalidates the previous one, for when it may have been seen by someone else. Restart the API afterwards: `cd /opt/meshploy && docker compose up -d api`. |

---

### `meshploy install`

| Command | Description |
|---|---|
| `install node-exporter` | Install Prometheus node_exporter as a systemd service |

---

### `meshploy license`

| Command | Description |
|---|---|
| `license status` | Show the licence status and entitlements of this install |
| `license activate <token>` | Install a licence token. The server verifies its signature, expiry and domain binding before storing it |

Only the Enterprise image can verify a licence; on a Community install, `activate` reports what to run instead.

---

## Cluster and nodes

Adding and removing worker machines. These go through the API, so they work from any machine you are logged in on; `init`, `add` and `remove` also reach the target machine over SSH.

### `meshploy node`

| Command | Description |
|---|---|
| `node list` | List all nodes in the cluster |
| `node init <host>` | Prepare a remote machine over SSH (installs prerequisites) |
| `node add <host>` | Bootstrap a remote machine as a worker node over SSH |
| `node remove <host>` | Cleanly uninstall a remote node over SSH |
| `node delete <id>` | Remove a node from Headscale, k3s, and the DB |
| `node token get` | Print the current node registration token |
| `node token rotate` | Generate a new registration token (invalidates the old one) |

SSH commands (`remove`, `init`, `add`) accept `--identity-file` and `--port`.

For `install`, `uninstall` and `status`, which act on the machine you run them on, see [Server management](#server-management).

---

## Projects and workloads

These act on a project: pass it with `-p` (or `--project`), or [link the directory](#meshploy-link) once.

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

### `meshploy apply`

```bash
meshploy apply -f compose.yml --project my-project
```

Applies a Docker Compose manifest, with `x-meshploy` extensions, to a project in one call: it is upserted as a stack and reconciled into live services. Idempotent: run it again to converge on the same spec.

| Flag | Description |
|---|---|
| `-f, --file` | Path to the compose manifest (required) |
| `--name` | Stack name (default: the manifest file's base name) |
| `--project` | Project name or ID |

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

## Integrations

Credentials the organisation shares across projects: git providers to build from, registries to push to and pull from, and S3-compatible storage for backups.

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

## The CLI itself

### `meshploy version`

```bash
meshploy version
# meshploy 0.9.0          ← stable build
# meshploy 0.9.0+abc1234 (edge)  ← edge build
```

---

### `meshploy update`

```bash
meshploy update           # latest stable binary
meshploy update --edge    # edge build from main
```

Downloads the latest CLI binary from GitHub and replaces the running binary in-place. Defaults to the latest stable release. Pass `--token <pat>` or set `GITHUB_PAT` if the repo is private.

On the gateway, run it before [`server-upgrade`](#meshploy-server-upgrade), which upgrades the server with whichever CLI is installed.

---

### `meshploy alias`

| Command | Description |
|---|---|
| `alias install` | Create a shell alias symlink for the meshploy binary |
| `alias remove` | Remove alias symlinks |

---

## AI agents

### `meshploy mcp`

```bash
meshploy mcp
```

Starts an MCP (Model Context Protocol) server over stdio, exposing Meshploy operations as tools for Claude Code or any MCP-compatible agent. It uses the credentials saved by `meshploy auth login` and acts as that user.

**Claude Code setup**: add it to the project's `.mcp.json`:

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

For an agent that shouldn't act as you, use the gateway's remote MCP endpoint at `https://console.<your-domain>/mcp` with an agent token instead. Create the agent in the dashboard under **Agents**, which shows the configuration for each client.

---

## Config file

Credentials are stored at `~/.meshploy/config.json` (mode `0600`):

```json
{
  "api_url": "https://api.your-domain.com",
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
