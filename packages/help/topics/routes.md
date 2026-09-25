---
id: routes
title: Routes
summary: How a hostname or a port reaches a service, the zones a route can live in, and how certificates are issued.
pages: [routes]
---

## Routes {#routes}

A **route** is how traffic reaches a service. There are two kinds:

- A **domain route** is a hostname served over HTTPS, such as `app.example.com`, for anything that speaks HTTP.
- A **TCP route** is a port on the gateway, forwarded over the mesh as it is, for Postgres, Redis, SSH and anything else that does not.

A route can be **paused**, which stops it serving without deleting it.

## Zones {#zones}

A domain route lives in one of three zones of a base domain:

- **Public**: `app.example.com`, on the internet.
- **Internal**: `grafana.internal.example.com`, answered only on your mesh. For tools and dashboards nobody outside should reach.
- **Preview**: `app.preview.example.com`, public, kept apart from your real names.

A TCP route binds the gateway's **public** interfaces (subject to its allowlist and the host firewall), its **mesh** address only, or **local** loopback only, reached through an SSH tunnel to the gateway.

## Targets {#targets}

A domain route sends each path to a **target**: a service's port, an address on a node, or a redirect to another route. Several paths can go to different services, and **strip path** removes the path prefix before the request reaches the target.

## Certificates {#tls}

Certificates are automatic. A base domain whose DNS is **delegated** to the gateway gets one wildcard certificate that covers every name under it. A domain in **on-demand** mode gets a certificate per hostname the first time it is asked for. An internal route on an on-demand domain uses a certificate from the gateway's own authority, which browsers on your machines need to trust once.

## Your own hostnames {#custom}

A route can use a hostname outside your base domains, such as `shop.customer.com`. Point it at the gateway, then prove it is yours with a TXT record at `_meshploy-verify.<hostname>`; the route shows the value to add.

## In environment levels {#levels}

A route in a level takes the level's name: `app` in staging is served as `app-staging`. See [Projects & environments](/concepts/environments#routes).

## Terms {#terms}

- **Internal zone** {#internal -> zones}: Names under internal.<domain>, answered only on your mesh.
- **Preview zone** {#preview -> zones}: Names under preview.<domain>: public, and kept apart from your real names.
- **Paused** {#paused -> routes}: Not serving, and not deleted. Publish it again to serve.
- **TCP route** {#tcp -> routes}: A port on the gateway forwarded over the mesh as it is, for anything that does not speak HTTP.
- **Strip path** {#strip-path -> targets}: Removes the path prefix before the request reaches the target.
