---
id: cloud-link
title: Meshploy Cloud link
summary: Linking a self-hosted server to a Meshploy account, so it appears beside the account's other workspaces, and what it sends.
pages: [cloud-link]
---

## Linking a server {#link}

A self-hosted server can be **linked** to a Meshploy account at app.meshploy.com, where it appears in the account's Spaces beside the workspaces Meshploy hosts, marked self-hosted. Only the server's owner can link it, under **Settings, Meshploy Cloud**: make a link token in the account with **Link self-hosted**, and paste it here. A token works once, within a day.

The server **connects out** to Meshploy Cloud and keeps that one connection open. No port is opened for it, so it works behind a router or on a server reachable only over its mesh, and Meshploy Cloud has no way to reach the server except through that connection. The server works the same whether the link is up or not.

## What it reports {#report}

While **Report to Meshploy Cloud** is on, the server sends its state once a minute: its version, how many nodes are online, how many services are running, how many backups are scheduled or failing, how many databases have none, and whether an update is available. **No names, addresses or secrets** leave the server: no project or service names, no domains, no variables.

Turned off, the link stands and nothing about the server is sent; the account shows it as connected but not reporting.

## Ending the link {#unlink}

**Unlink server** ends the link on the server and in the account. Removing it from the account's Spaces ends it too: the server is told and forgets the link. Either way nothing else on the server changes, and it can be linked again with a new token.

## Terms {#terms}

- **Link token** {#token -> link}: A one-time `mflt-` token made in the Meshploy account, which links one server to it.
- **Reporting** {#reporting -> report}: The server's state, counts only, sent to the linked account once a minute while the owner allows it.
