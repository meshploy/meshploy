---
title: Route an existing service through the mesh
description: Reach an application you already run using node or address targets and deliberate route exposure.
---

You can route traffic to an application without moving it into a Meshploy deployment. This guide assumes an HTTP application already listens on port `8080` on a machine you control.

## 1. Connect the host

Join a supported host as **Mesh only** using **Cluster → Add a node**, or:

```bash
meshploy node init user@host.example.com --role mesh
```

A mesh-only node joins the private network without running Kubernetes workloads. Verify that it appears online.

## 2. Verify the listener from the gateway

A **Node + port** target uses the node's mesh IP. The application must accept connections at that address and port.

From the gateway, check the upstream, replacing this example IP:

```bash
curl --fail --show-error http://100.64.0.20:8080/
```

A service listening only on the remote machine's `127.0.0.1` cannot be reached through that machine's mesh IP. Configure the application to listen on an appropriate reachable address and keep unintended interfaces protected by a firewall.

## 3. Select the right target

| Target | Use when |
| --- | --- |
| Service | Meshploy manages the application workload. |
| Node + port | The application runs on a connected machine at its mesh address. |
| Address + port | You need an explicit address reachable from the gateway, such as a LAN address or gateway loopback. |

For this example, create an HTTP route in the project's **Routes**, select **Node + port**, choose the node, set port `8080`, and use path `/`.

An address target of `127.0.0.1` means **the gateway's loopback**, because the proxy connects from the gateway. It never means your browser's machine or another node's loopback.

## 4. Configure the hostname and publish

Choose the route's intended zone and hostname. For public access, configure DNS and TLS as described in [domains and TLS](/guides/domains-and-tls/), then publish the route.

```text
Browser → gateway HTTPS route → node mesh address:8080 → existing app
```

Check the hostname from the intended client network. Verify the response and the application's logs.

Routing provides connectivity; it does not add application login or authorization. Keep the existing service's authentication in place.

## Plain TCP services

For a non-HTTP application, use a TCP route instead of an HTTP hostname route.

| TCP zone | Gateway listener | Intended access |
| --- | --- | --- |
| Public | All interfaces | Clients able to reach the gateway port, subject to firewall/access rules. |
| Mesh | Gateway mesh address | Clients on the mesh. |
| Local | Gateway loopback | Gateway processes or an SSH tunnel. |

TCP routing forwards the connection; it does not automatically add HTTPS or database TLS. Configure protocol security in the application where required.

For mesh/local routes, the route can use the target's port by default. Listener ports must remain unique across TCP routes, regardless of zone.

A service already reachable directly on the mesh may not need another TCP listener. A useful mesh-zone example is forwarding the gateway's loopback-only service to its mesh address.

## Troubleshoot and remove access

- **Upstream connection refused:** check the process, listen address, port, and firewall.
- **Upstream timeout:** check node availability and connectivity from the gateway.
- **Wrong application:** check the hostname/path and chosen target.
- **DNS or TLS failure:** follow the domain guide before changing the upstream.
- **TCP listener rejected:** check conflicts with other routes and gateway services.

Pause a route to stop serving it while retaining its configuration. Delete it when no longer needed. Neither action uninstalls or stops the existing application.
