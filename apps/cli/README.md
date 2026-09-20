# Meshploy CLI

The `meshploy` CLI manages your Meshploy installation from the terminal — nodes, services, deployments, stacks, volumes, secrets, integrations, and more.

Commands are grouped by what they act on:

| Group | Commands |
|---|---|
| [First-time setup](#first-time-setup) | `auth`, `link` |
| [Server management](#server-management) | `node install`, `node uninstall`, `node status`, `server-upgrade`, `updater`, `setup serve`, `setup-token`, `install node-exporter`, `license` |
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

Downloads the `deploy/` configuration for the release from GitHub and pulls its container images, then installs the configuration, restarts the services, and checks that the API, console, proxy and Caddy answer.

Nothing on the server changes until the download and the pull have both succeeded, so a failure there leaves it as it was. If the restart or the check fails, the previous configuration and images are put back and the services restarted on them; `--no-rollback` leaves the failed state in place for inspection instead. The files an upgrade replaces are kept in `/opt/meshploy/.upgrade-previous` until the next one. Database migrations the new version already ran are not undone.

Must be run as root on the **gateway server**.

Protected files are never overwritten: `.env`, DNS zone files, and Headscale config keep their runtime-rendered values. `coredns/Corefile` is replaced and re-rendered from `.env`. On a gateway using self-managed DNS (`DNS_MODE=ondemand`), the on-demand Caddyfile is put back after the sync, and Caddy is recreated whenever its configuration changed.

`--ee` switches the install to the Enterprise images, API and console, named by the activated licence (it sets `MESHPLOY_API_IMAGE` and `MESHPLOY_WEB_IMAGE` in `.env`). The Enterprise images are built from each release shortly after it; until the ones for the release being installed are published, an Enterprise upgrade or switch stops after the pull with nothing changed.

The upgrade runs with the CLI you have installed, so run `sudo meshploy update` (or `update --edge`) first.

| Flag | Description |
|---|---|
| `--edge` | Sync from the `main` branch and pull edge images instead of the latest stable release |
| `--no-rollback` | On failure, leave the server as it is for inspection instead of putting the previous version back |
| `--ee` | Switch to the Enterprise images, API and console, named by the activated licence |
| `--ee-image <image>` | The Enterprise API image to switch to, instead of the licence's (`ghcr.io/meshploy/api-ee` or a vendor build of it). The console image pairs with it by name: `api-ee-acme` with `web-ee-acme` |
| `--token <pat>` | GitHub personal access token (or set `GITHUB_PAT`), required if the repo is private. With `--ee`, also used to log in to `ghcr.io` when the gateway cannot pull the Enterprise images yet (needs `read:packages`) |

---

### `meshploy migrate dokploy`

Plans moving a server that runs Dokploy into Meshploy. It only reads: Docker, Dokploy's database (through `docker exec`), the Traefik folder and the listening ports. Secrets are read only in memory and never printed or written. Run it on the Dokploy server, as root.

| Command | Description |
|---|---|
| `sudo meshploy migrate dokploy detect` | Whether Dokploy runs here, its version and schema level, what holds ports 80 and 443, resources, and how a move would run |
| `sudo meshploy migrate dokploy fixture` | Write a scrubbed reading of this server: every name, host, path, id and secret replaced, the structure kept. For adding a real server's shape to the repository's test fixtures without carrying anyone's data |
| `sudo meshploy migrate dokploy plan` | What each project, environment, application, compose app, database, domain and integration becomes in Meshploy: moves, needs you (with the choices and a default where one is safe), or not moved; plus containers Dokploy does not manage |

| Flag (`plan`) | Description |
|---|---|
| `--json` | Print the plan as JSON |
| `--out <file>` | Also write the plan as JSON, readable by root only |

The plan also lists **groups**: what moves together so no data lives in two places. A database groups with every application and compose app that names it by its Dokploy hostname (in its env, its environment's or project's variables, or its compose file), and apps that bind-mount the same host folder group together and copy it once into a shared volume. Each group shows its members, the data to copy (database volumes, compose and named volumes, bind-mounted folders), a downtime estimate, and whether it can move now; groups that can move and carry no data come first.

Supported: Dokploy schema levels 133 to 196 (v0.26.1 to v0.30.6); anything else is refused with the reason. Regex redirects that send one of an app's hostnames to another are worked out into route redirects. A bind mount another container also uses has no default choice, since copying it into one volume would split what was shared.

---

### `meshploy host`

The host agent: a systemd service on the gateway that reports what the API, in its container, cannot see. It listens on nothing; the API reads its reports from `/var/lib/meshploy/host/state`, mounted read-only. It reports the host firewall, so a TCP route can say whether the gateway blocks its port; reports the containers running on the machine, read through the container runtime's own API, and what is listening on its TCP ports, read from `ss` - one endpoint per port, with the container named where the port belongs to one, so the console can show what this server runs that Meshploy does not route; starts the upgrades the console asks for; and runs requests the API drops into `/var/lib/meshploy/host/inbox` - a Dokploy `detect`, `plan`, `prepare`, `move`, `cutover` or `rollback`, and the migration's `credential`, which it takes once and keeps where only root can read it - writing the result under its state directory. It only reads containers - anything that would change one is a named request, and none exist yet. `install.sh` and `server-upgrade` start it.

| Command | Description |
|---|---|
| `sudo meshploy host start` | Install the service and start it; running it again refreshes the unit |
| `sudo meshploy host stop` | Stop the service; firewall verdicts turn unknown |
| `meshploy host status` | Whether it runs, its last report, and the firewall it found |

---

### `meshploy updater`

```bash
sudo meshploy updater start     # let the console start upgrades; makes sure the host agent runs
sudo meshploy updater stop      # stop taking requests; an upgrade already running finishes
meshploy updater status         # whether it is on, and the last upgrade with its log
sudo meshploy updater run       # upgrade now: update, server-upgrade, health check
```

Upgrades the server on request from the console. The API cannot touch the host, so it only queues a request; the host agent (`meshploy host`) notices it and runs `meshploy updater run` as root, as a transient systemd unit, so restarting or replacing the agent never interrupts a run. Servers that ran upgrades through the older `meshploy-upgrade.path` unit have it removed by their next `server-upgrade`. That updates the CLI, runs `server-upgrade` with the new binary, and waits for the API to answer healthy. Progress and the log are kept in `/var/lib/meshploy/upgrade/state`, where the console and `updater status` read them.

`updater run` also works by hand, as a one-command upgrade. It stays on the channel the server is on now; pass `--edge` or `--stable` to switch. Unlike `server-upgrade`, leaving out `--edge` does not move an edge server to stable. The console's **Settings → Server** switches channels through the same runner, and only forward: it offers edge to stable once a release includes the running build. By hand, `--stable` does not check that. A switch to Enterprise from the licence section runs through it too: the API names the image from the verified licence, and the runner checks it is one of Meshploy's Enterprise API images before passing it to `server-upgrade --ee`.

Run it on the **gateway server**. `start`, `stop` and `run` need root. New installs turn the updater on; on a server installed before it existed, run `sudo meshploy updater start` once, after the upgrade that brings it. In the console, **Update available** in the sidebar then opens an **Upgrade now** button for the server's owner.

| Flag (`run`) | Description |
|---|---|
| `--edge` | Upgrade to the edge channel (builds from `main`) |
| `--stable` | Upgrade to the latest stable release |

---

### `meshploy setup serve`

```bash
sudo meshploy setup serve
```

Serves the browser installer: a page on port 9000 that collects the domain and DNS mode, offers to migrate another platform already on the machine, runs the installer, and streams its output. `install.sh` starts it for you when you choose to continue setup in a browser. Run it yourself to reopen setup, for example to change the domain; it starts from the current configuration in `.env`. Requires root, and access is gated on the setup token from `/opt/meshploy/.env`. It serves plain HTTP, because no certificate exists yet.

| Flag | Description |
|---|---|
| `--addr` | Address to serve on (default: `0.0.0.0:9000`) |
| `--public-ip` | Public IP to pre-fill and to check DNS against (default: `PUBLIC_IP` from `.env`) |

When another platform is on the machine, setup adds a **Migrate** step: it detects it, reads what moving it would take, and records what you decide. Nothing on the other platform is changed - the move itself runs later, from the console. Today that means Dokploy; see [`meshploy migrate dokploy`](#meshploy-migrate-dokploy).

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

A Community install can activate a licence before switching. Its features take effect once the server runs the Enterprise image (`sudo meshploy server-upgrade --ee`), and `license status` says so until then. On a build too old to verify a licence, `activate` reports what to run instead.

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
| `node delete <id>` | Remove a node from Headscale, k3s, and the DB. If Headscale cannot be reached, the node waits and is removed once it can, retried every minute. Software on the machine stays; `node remove` uninstalls it over SSH |
| `node token get` | Print the current node registration token |
| `node token rotate` | Generate a new registration token (invalidates the old one) |

SSH commands (`remove`, `init`, `add`) accept `--identity-file` and `--port`.

`init` accepts `--role`: `workload_builder` (the default), `workload`, `builder`, or `mesh`. A **mesh** node joins the mesh but not the cluster: nothing is scheduled on it, routes can reach its ports through a **Node + port** target, and it is not sent the k3s token. Moving a node into or out of `mesh` means installing or removing K3s on the machine.

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

Compose substitutes `${VAR}` from the stack's variables when the stack is applied. `--env-file` sends those values with the manifest, so a file that needs a password is one command rather than a visit to the console; they are stored on the stack, so a later apply without the flag keeps them. A service's env can also reference another variable at deploy time, such as a managed database's connection URL from an attached group; write that one as `$${PRIMARY_PG_DB_URL}` in the manifest, so it reaches the service as `${PRIMARY_PG_DB_URL}` and is resolved when it deploys.

Each compose service becomes a Meshploy service, read the way compose reads it:

- **Ports follow compose.** A port under `ports:` is published, so it is public: it gets a NodePort and a route can target it. One bound to a loopback address (`127.0.0.1:6379:6379`) is internal, and so is a port under `expose:`; an internal port is reachable from other services by name, not from outside. `x-meshploy.deploy.port` names the primary port, and is public when compose does not list it. A service that declares no port gets an internal port 3000.
- **HTTP or not.** A route can only target an HTTP port. Every port counts as HTTP except well-known non-HTTP ones (Postgres, pgbouncer, MySQL, Redis, MongoDB, SMTP, AMQP, Kafka and similar); the long syntax's `app_protocol` decides it explicitly, `http` or `ws` for HTTP and anything else for plain TCP.
- **TCP only.** UDP ports are left out, with a warning in the apply result.
- **`entrypoint:` and `command:`** replace the image's ENTRYPOINT and CMD, and reach the container exactly as written.
- **Files go in through `configs:` and `secrets:`.** A config lands at its `target`, `/<name>` by default, and a secret at `/run/secrets/<name>`; both are stored encrypted. `content:` and `environment:` work however the stack is applied. `file:` is read relative to the compose file: `meshploy apply` sends it along, a git stack reads it from the repository, and an apply that cannot read it keeps the copy an earlier one stored. Bind mounts, `extra_hosts` and values pointing at `host.docker.internal` are left out, with a warning.
- **Re-applying rolls out what it changes.** A changed image, command, environment, resource limit, port, config file or mount is deployed to the services that carry it, and the result lists them under `Rolled out`. A stopped service is never started, and a managed database is not re-provisioned; both are named in the warnings instead. `--no-deploy` writes the records and rolls nothing out. An apply that cannot roll a service out, because a deploy was already running, still owes it: the next apply rolls it out, rather than seeing a record that already matches and doing nothing. Ports are updated only when the compose definition declares some, so ports edited in the console are otherwise kept, and a public port that stays keeps its NodePort.
- **Settings are written out.** The stack is stored with every `x-meshploy` setting an apply would otherwise default written into each service: its primary port, replicas and resources, or a managed database's version and storage. The console then shows what runs. Your local file is not changed, and applying it again stores the same result.

| Flag | Description |
|---|---|
| `-f, --file` | Path to the compose manifest (required) |
| `--name` | Stack name (default: the manifest file's base name) |
| `--project` | Project name or ID |
| `--env-file` | File of `KEY=VALUE` lines the manifest interpolates as `${KEY}` |
| `--no-deploy` | Update the records without rolling anything out |

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
| `route pause <route-id>` | Stop serving a route and keep it: visitors get a 404, no certificate is issued |
| `route publish <route-id>` | Serve a paused route again |
| `route delete <route-id>` | Remove a route |
| `route tcp list` | List the TCP ports the gateway publishes |
| `route tcp create --service <svc> --port <n>` | Publish a port on the gateway and forward it over the mesh |
| `route tcp pause <route-id>` | Close a port and keep the route |
| `route tcp publish <route-id>` | Open a paused port again |
| `route tcp delete <route-id>` | Stop publishing a port |

A route carries a hostname through Caddy, with TLS, for anything that speaks HTTP. What does not - Postgres, Redis, SSH - takes a TCP route instead: the gateway listens on a port of its own and forwards it over the mesh. `--allow` restricts who may connect, by address or range, and an empty allow-list means anyone who can reach the gateway. The host firewall, and a cloud security group where there is one, must allow the port too: the gateway listens on it, but neither of those knows that. A managed database has to have mesh access before it can be published.

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

The download is checked against the release's `SHA256SUMS` before it replaces anything, and a mismatch leaves the installed CLI as it was. A release published before checksums existed is installed with a warning.

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

Deploying an app is one call: `apply_manifest` takes the compose, the values it interpolates as `${NAME}` and the contents of the files its `configs:` name. `list_templates`, `get_template` and `deploy_template` do the same from the one-click catalog, and `create_stack` can point a stack at a git repository for `sync_stack` to reconcile. Variables and prompt values are write-only: no tool reads them back, so a password an agent sets does not return through a transcript.

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
