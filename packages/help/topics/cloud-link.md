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

## Agent apps through Meshploy accounts {#agent-apps}

With **Allow agent apps through Meshploy accounts** on, a linked server lets agent apps such as Claude, Cursor or VS Code reach it through people's Meshploy accounts: one connection in the app reaches every workspace the account may use. It is off by default, and only the server's owner turns it on. Turning it on also links the owner: the account the server is linked to is theirs, so the agent apps they approved there act as them here at once.

For everyone else it is their own choice. While agent apps are allowed, the **Connectors** page offers each person **Link my account**: they approve it at app.meshploy.com, and from then on the agent apps they approved there act as them here, with **exactly their permissions** and nothing more. Some things stay out of reach of any agent app whatever its person may do: the server's link and upgrades, sign-in and accounts, members and invitations, node join tokens, the server's own backups, and running queries in a database.

An app connects once, to the Meshploy account: add `https://app.meshploy.com/mcp` as a custom connector or remote MCP server, and approve it there. It then reaches every workspace the account reaches, this server among them, and the account's **Agent apps** page lists the apps connected and can disconnect any of them.

Every call an app makes is recorded, with the app's name, on the person's Connectors page, and each app shows among their sign-ins as "*app* via Meshploy Cloud". Unlinking their account, or the owner turning agent apps off, stops every call at once.

## Ending the link {#unlink}

**Unlink server** ends the link on the server and in the account. Removing it from the account's Spaces ends it too: the server is told and forgets the link. Either way nothing else on the server changes, and it can be linked again with a new token.

## Terms {#terms}

- **Link token** {#token -> link}: A one-time `mflt-` token made in the Meshploy account, which links one server to it.
- **Reporting** {#reporting -> report}: The server's state, counts only, sent to the linked account once a minute while the owner allows it.
- **Agent apps** {#agent-apps -> agent-apps}: Claude, Cursor and other apps reaching this server through people's Meshploy accounts, each acting as the person who linked their account here, with exactly their permissions.
- **Your Meshploy account** {#my-account -> agent-apps}: Your account at app.meshploy.com, linked here by you, so the agent apps you approved there can work on this server as you.
