---
id: users
title: Users & access
summary: Workspace members, their roles, and access granted per project or per resource.
pages: [users]
---

## Members and roles {#roles}

Everyone in a workspace is a **member** with a role:

- **Owner**: one per workspace, with every right, including over admins.
- **Admin**: manages the workspace: members, nodes, domains, integrations and every project.
- **Member**: sees and changes only what they are granted.

People join by **invitation**, which an admin sends from this page.

## Granting access {#grants}

A member's access is granted **per project** or **per resource** (a service, a stack, a job), each at a level: **view**, **deploy**, **create**, **update** or **delete**. A grant on a project covers its environment levels too. A member sees only the projects they were given; the overview and activity feed show nothing else.

## Agents {#agents}

An **agent**, such as an AI assistant or a CI job, is a member that signs in with a token instead of a password, and is granted access the same way. See [Agents & MCP](/concepts/agents).

## Terms {#terms}

- **Admin** {#admin -> roles}: Manages the workspace and every project in it.
- **Member** {#member -> roles}: Sees and changes only what they were granted.
- **Grant** {#grant -> grants}: Access to a project or a resource at a level: view, deploy, create, update or delete.
