---
id: agents
title: AI assistants, agents & MCP
summary: Connecting an AI assistant to Meshploy over MCP, as yourself or as an agent, and giving automation its own scoped keys.
pages: [agents]
---

## Connecting an assistant {#assistants}

Claude, Cursor, VS Code and other MCP clients connect to your gateway's MCP address, `https://console.<your-domain>/mcp`, and **sign in**: the application sends you to the console to approve it, and it then works with **your** access, apart from server administration. An owner or admin can instead let it act as one of the workspace's agents. Each connection is listed under **Connected sessions** in your settings (with your signed-in CLIs), on your page for admins, and for everyone on the **Access** page's Sessions tab; it can be disconnected from any of them, and one unused for 90 days stops working.

Claude on the web connects from the internet, so the console has to be reachable from it over HTTPS.

## Agents {#agents}

An **agent** is a member of your workspace that is not a person: an AI assistant, a CI job, a script. It is added like a member and granted access the same way, per project or per resource, so it can do exactly what it was given and nothing more. It signs in with a **token** instead of a password.

## Tokens {#tokens}

An agent's key (its token) starts with `magt-` and is shown **once**, when it is made. Keep it like a password. Keys can be rotated, which replaces the old one, or revoked. They are listed on the **API keys** page. A key suits CI, scripts and MCP clients that take a pasted token; a client that signs in needs none.

## MCP {#mcp}

Meshploy speaks **MCP**, the protocol AI assistants use to call tools. An assistant with an agent's token can list and deploy services, read logs, check the environments board, and more, limited to what that agent was granted.

- **Remote**: the gateway serves MCP at `/mcp`. An assistant signs in to it, or uses an agent's key.
- **Local**: `meshploy mcp` runs the same tools on your machine, as the CLI's login.

Tools that belong to an operator, such as node tokens, system backups and querying databases directly, are left out of the remote server.

## Terms {#terms}

- **Agent** {#agent -> agents}: A workspace member that is not a person, granted access like one and signing in with a token.
- **Agent token** {#token -> tokens}: The agent's credential, starting magt-. Shown once; rotate or revoke it from the agent's page.
- **MCP** {#mcp -> mcp}: The protocol AI assistants use to call tools. Meshploy serves it at /mcp.
- **Connected assistant** {#connection -> assistants}: An MCP client someone signed in, acting as them or as an agent they chose, until it is disconnected.
