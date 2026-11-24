# Meshploy server template

A provider's ready-made image, "Ubuntu 24.04 with Meshploy": a customer creates
a server from it, opens a page in a browser, gives it a domain, and has their
own Meshploy.

## Making the image

On a fresh Ubuntu 24.04 server, as root:

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/get.sh)" _ --template
```

This installs Docker, the `meshploy` CLI and the deploy files in
`/opt/meshploy`, pulls the container images, and arranges for setup to start on
first boot. It creates nothing that must differ between servers: no setup
token, no secrets, no k3s, no mesh. Clean the machine the way your platform asks
(cloud-init state, SSH host keys, logs, machine ID), then save it as the image.

## What a customer sees

On each server's first boot, `meshploy-firstboot.service`:

1. creates the server's own setup token and records its public address in
   `/opt/meshploy/.env` (mode 600),
2. starts the setup page, `meshploy-setup.service`, on port 9000.

The login message, and `/root/meshploy-setup.txt`, say where to go:

```
  Meshploy is ready to set up.

  Open   http://203.0.113.10:9000
  Token  sudo meshploy setup-token show
```

The page asks for the token, then a domain and how its DNS is managed, shows
the exact records to add, checks them, and runs the installation, streaming
its progress. When the console is ready the page stops, the port it opened in
the firewall is closed again, and the login message goes away.

The same token then creates the first account, which owns the instance.

## Requirements

- Ubuntu 24.04 (22.04 works), x86_64 or arm64, with systemd
- 2 vCPU and 4 GB of memory or more; 40 GB of disk
- Ports 80 and 443 reachable, and 9000 during setup
- A domain, or a subdomain, whose DNS the customer can change
