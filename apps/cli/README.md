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
