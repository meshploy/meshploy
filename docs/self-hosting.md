# Self-Hosting Meshploy

## Requirements

- A Linux server (Ubuntu 22.04+ or Debian 12+ recommended) with a public IP
- A domain name you control (e.g. `meshploy.example.com`)
- Ports **80**, **443**, and **53** (UDP+TCP) open in your firewall

---

## Install

Run this on your server as root:

```bash
curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh | sudo bash
```

The script will:
1. Install Docker (if not present)
2. Clone Meshploy to `/opt/meshploy`
3. Walk you through an interactive setup — domain, IP, auto-generated secrets
4. Start the full stack via Docker Compose
5. Join the server to its own WireGuard mesh via Headscale

---

## DNS setup (one-time)

After the installer finishes, point your domain to Meshploy's CoreDNS by adding an **NS record** at your registrar:

| Type | Name | Value |
|------|------|-------|
| `A` | `ns1.your-domain.com` | `<your server IP>` |
| `NS` | `your-domain.com` | `ns1.your-domain.com` |

> If you're using a subdomain (e.g. `mesh.example.com`), add the NS record under that subdomain at your registrar instead.

Once DNS propagates, Meshploy's CoreDNS becomes authoritative and all subdomains (`app`, `api`, `headscale`, and any app routes you create) are managed automatically — no further DNS changes needed.

Verify with:
```bash
dig @<your server IP> app.your-domain.com A
```

---

## Services

| URL | Service |
|-----|---------|
| `https://app.your-domain.com` | Meshploy dashboard |
| `https://api.your-domain.com` | REST API |
| `https://headscale.your-domain.com` | Headscale control plane |

TLS certificates are provisioned automatically by Caddy via Let's Encrypt.

---

## Adding worker nodes

On each worker machine, run:

```bash
curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh | sudo bash
```

Select **Worker** when prompted, then enter the Headscale URL and pre-auth key shown at the end of the master install.

Once joined, register the node in the Meshploy dashboard under **Nodes**.

---

## Updating

```bash
cd /opt/meshploy/deploy
docker compose pull
docker compose up -d
```

Or re-run the installer — it updates the repo in place before re-running setup.

---

## Useful commands

```bash
# View logs
docker compose -f /opt/meshploy/deploy/docker-compose.yml logs -f

# Restart a single service
docker compose -f /opt/meshploy/deploy/docker-compose.yml restart api

# Generate a new Headscale pre-auth key
docker compose -f /opt/meshploy/deploy/docker-compose.yml exec headscale \
  headscale preauthkeys create --user meshploy --expiration 1h
```
