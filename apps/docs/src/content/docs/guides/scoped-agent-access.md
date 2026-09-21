---
title: Connect an agent with scoped permissions
description: Create a dedicated agent identity, configure remote MCP, and verify its allowed access.
---

Meshploy's remote MCP endpoint lets a compatible client operate as a dedicated agent identity. Use it when the agent should have a narrower scope than your own account.

## Choose local or remote MCP

| Connection | Identity | Configuration |
| --- | --- | --- |
| Local `meshploy mcp` | Your logged-in CLI user | Client launches the CLI over stdio. |
| Remote `/mcp` | Agent authenticated with a `magt-` token | Client connects over HTTP with bearer authentication. |

Remote MCP omits operator-only tools, including node registration, system backups, member management, and raw database queries. Resource permissions still apply to the tools that are available.

## 1. Create an agent

In the console, open **Agents** and create a named identity for the workflow, such as **staging-deployer**.

Choose **Member** for an identity whose access you intend to restrict with grants. An **Admin** role grants broad organization access; it is not made narrowly scoped simply by adding a project grant. Agents cannot be owners.

## 2. Grant the required access

Use the project or resource **Permissions** section to grant the agent only the actions needed by its task.

Start with read access to the intended resources. Add deployment or modification permissions when the workflow requires them. Project grants and resource grants affect the effective access together; inspect both rather than assuming a resource grant cancels broader access.

For example, a diagnostics agent should be able to inspect its application's status and logs without receiving general administrative access.

## 3. Create a token and configure the client

Create an agent token, give it a recognizable name, and set an expiry when appropriate. Copy the plaintext token when shown; stored token metadata cannot recover it later.

Use the client-specific configuration provided by the agent's connection panel. The remote endpoint is:

```text
https://console.<your-domain>/mcp
```

Authenticate using:

```text
Authorization: Bearer <agent-token>
```

Keep the token in the client's protected configuration rather than a committed project file.

The current connection requires a client that supports the endpoint's HTTP transport and bearer credentials. A client that only offers OAuth discovery cannot use this token flow; Meshploy's MCP OAuth connector support is planned separately.

## 4. Verify the boundary

Before allowing changes:

1. Ask the client to list the resources the agent can see.
2. Read the status or logs of an allowed service.
3. Confirm that an unrelated project is not accessible.
4. Verify that a change outside the granted permissions is rejected using a harmless request against a disposable test resource.

A missing tool and a denied resource are different failures: the first concerns the remote tool surface, the second authorization.

Only then add the write permissions required for the intended workflow.

## Client approvals and server permissions

Meshploy enforces access through the authenticated identity. Your client's approval settings determine when it asks before invoking a tool.

Destructive labels communicate intent; they do not guarantee a confirmation dialog. Configure the client to require review where appropriate, especially for deploys, deletion, and data changes.

## Rotate or revoke access

To rotate a token, create a replacement, update the client, confirm it connects, then revoke the old token. Revoke unused or compromised tokens immediately.

Revoking a token does not undo earlier deployments or data changes. Inspect the resources affected by the agent separately.

## Local MCP for your own workflow

After authenticating the CLI, a stdio-capable client can launch:

```json
{
  "mcpServers": {
    "meshploy": {
      "command": "meshploy",
      "args": ["mcp"]
    }
  }
}
```

This runs as your user. Use remote MCP with a member agent when you need a distinct permission boundary. See the [CLI reference](/cli/reference/) for authentication and command details.
