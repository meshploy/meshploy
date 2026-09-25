---
id: domains
title: Domains
summary: Base domains, the primary domain that serves the platform, DNS modes, and how to move to a new domain without breaking anything.
pages: [domains]
---

## Base domains {#domains}

A **base domain**, such as `example.com`, is a domain routes are made under. Each one also has an **internal** name (`internal.example.com`, answered only on your mesh) and a **preview** name. A domain must be **verified** before routes can use it: add the TXT record `_meshploy-verify.<domain>` the domain's page shows, then check.

Every verified base domain serves routes. One gateway can serve several.

## The primary domain {#primary}

One base domain is **primary**. It decides where the platform itself answers: the console, the API, and the address new machines join the mesh through. It is also what a new route defaults to.

**Make primary** moves the platform to another domain without breaking anything. The platform keeps answering on the old one, marked **former primary**, until you remove it, because the console you are using and every node's control address depend on it. A former primary cannot be removed while a node or a git connection still uses it; its page says which.

## DNS modes {#dns-modes}

Each base domain has its own DNS mode:

- **Delegation**: the domain's name servers point at the gateway, which answers for it and holds one wildcard certificate for every name. Nothing to add per route.
- **On-demand**: DNS stays with your provider. Each hostname needs a record pointing at the gateway, and gets its own certificate the first time it is asked for.

## Retiring a domain {#retiring}

A domain that has served routes is removed in two steps: **retire** it, which stops new routes choosing it while existing ones keep serving, then move those routes and remove it.

## Terms {#terms}

- **Primary** {#primary -> primary}: The domain the platform itself answers on: the console, the API, and where nodes join.
- **Former primary** {#former-primary -> primary}: The domain the primary moved away from. It keeps serving the platform until nothing uses it, then can be removed.
- **Delegation** {#delegation -> dns-modes}: The domain's name servers point at the gateway, which answers for it and covers every name with one wildcard certificate.
- **On-demand** {#on-demand -> dns-modes}: DNS stays with your provider; each hostname needs its own record and gets its own certificate.
