---
id: agents
title: Agents & MCP
summary: Giving an AI assistant or an automation its own scoped access to Meshploy, over MCP or the API.
pages: [agents]
---

## Agents {#agents}

An **agent** is a member of your workspace that is not a person: an AI assistant, a CI job, a script. It is added like a member and granted access the same way, per project or per resource, so it can do exactly what it was given and nothing more. It signs in with a **token** instead of a password.

## Tokens {#tokens}

An agent's token starts with `magt-` and is shown **once**, when it is made. Keep it like a password. Tokens can be rotated, which replaces the old one, or revoked.

## MCP {#mcp}

Meshploy speaks **MCP**, the protocol AI assistants use to call tools. An assistant with an agent's token can list and deploy services, read logs, check the environments board, and more, limited to what that agent was granted.

- **Remote**: the gateway serves MCP at `/mcp`. Point an assistant at it with the agent's token.
- **Local**: `meshploy mcp` runs the same tools on your machine, as the CLI's login.

Tools that belong to an operator, such as node tokens, system backups and querying databases directly, are left out of the remote server.

## Terms {#terms}

- **Agent** {#agent -> agents}: A workspace member that is not a person, granted access like one and signing in with a token.
- **Agent token** {#token -> tokens}: The agent's credential, starting magt-. Shown once; rotate or revoke it from the agent's page.
- **MCP** {#mcp -> mcp}: The protocol AI assistants use to call tools. Meshploy serves it at /mcp for agents.
