---
title: Configure domains and TLS
description: Choose installation DNS, publish route hostnames, and understand public and internal certificates.
---

DNS directs a hostname to your gateway. A route selects the upstream application. TLS supplies the certificate for the hostname. Check these separately when publishing an application.

## Installation DNS modes

Use a dedicated installation subdomain, for example `meshploy.example.com`.

| Mode | DNS ownership | Public application certificates |
| --- | --- | --- |
| NS delegation | Delegate the installation zone to the gateway's CoreDNS. | Wildcard certificates using automated DNS-01. |
| Self-managed DNS (`ondemand`) | Keep the zone at your DNS provider. | Certificates per hostname using on-demand issuance. |

### NS delegation

In the parent zone, create:

```text
ns1.meshploy.example.com   A    <gateway-public-ip>
meshploy.example.com       NS   ns1.meshploy.example.com
```

Create the nameserver address record before the delegation. Keep port 53 reachable over TCP and UDP, in addition to the gateway's HTTP/HTTPS ports.

Verify the authoritative answer and public resolution:

```bash
dig @<gateway-public-ip> console.meshploy.example.com A
dig console.meshploy.example.com A
```

### Self-managed DNS

Use the installer's self-managed DNS mode and create these records at your provider:

```text
*.meshploy.example.com   A   <gateway-public-ip>
meshploy.example.com     A   <gateway-public-ip>
```

The base-domain record is often named `@` in the provider's zone editor. Gateway ports 80 and 443 must be reachable for public certificate validation.

```bash
dig +short console.meshploy.example.com A
```

See [self-hosting](/self-hosting/) for the full installation flow. Do not switch a running installation's DNS mode by changing records alone.

## Publish a hostname under the installation domain

1. Create or open the project's HTTP route.
2. Select its hostname and an HTTP-capable target.
3. Verify that the hostname resolves to the gateway.
4. Publish the route and open it over HTTPS.

In delegation mode, the appropriate wildcard covers installation subdomains. In self-managed mode, issuance happens for an authorized hostname when it is requested; the first request can take longer.

## Use a custom hostname

For a hostname such as `app.example.net`:

1. Create the custom-hostname route in the console.
2. Point the hostname at the gateway using your provider's DNS controls.
3. Copy the ownership TXT value shown by Meshploy to `_meshploy-verify.app.example.net`.
4. Wait for propagation, then run the route's hostname verification action.
5. Publish the route and request it over HTTPS.

Verify both records:

```bash
dig +short app.example.net A
dig +short TXT _meshploy-verify.app.example.net
```

Use the exact TXT value supplied by the route. A correct address record alone does not satisfy Meshploy's ownership check.

## Internal names and certificate trust

Internal routing uses names under `internal.<installation-domain>` and requires access to the mesh and its DNS.

- With NS delegation, Meshploy can automate DNS-01 for the internal wildcard.
- With self-managed DNS, the current Caddy configuration uses its local CA for internal names. Clients need to trust that CA to accept those certificates.

Public wildcard certificates only match one label: `*.example.com` does not cover `app.internal.example.com`.

Do not treat disabling certificate verification as the final client configuration. Establish the intended CA trust instead.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| NXDOMAIN or wrong IP | Correct zone, hostname, nameserver delegation, and propagation. |
| TXT verification fails | Exact TXT name/value and whether public resolvers can see it. |
| Public certificate fails | Correct gateway destination, published/verified route, ports 80/443, and Caddy logs. |
| Internal hostname fails | Client mesh connectivity and split DNS. |
| Internal certificate untrusted | Installation DNS mode and the client's trust of Caddy's internal CA. |

## Current domain-management boundary

Custom route hostnames are supported. Managing multiple installation domains and connecting DNS-provider APIs are separate capabilities still planned; this guide does not assume a domain-management page or automated provider record creation.

To connect the hostname to a service outside Meshploy, follow [routing existing services](/guides/route-existing-service/).
