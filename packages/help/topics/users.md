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

A member's access is granted **per project** or **per resource** (a service, a stack, a job), each at a level: **view**, **deploy**, **create**, **update** or **delete**. A grant on a project covers its environment levels too. A member sees only the projects they were given; the overview and activity feed show nothing else. A grant can also come from somewhere other than this page, shown as **via** its source with a lock: it is given and removed there.

## From their machines {#mesh}

A machine joined to the mesh belongs to the person who made its token, its **owner**, and acts as them. Once an admin **enforces** the mesh policy on the **Access** page, each machine reaches only what its owner may use:

- An owner's or admin's machine reaches every machine of the workspace.
- A member's machine reaches the ports of what they were granted, and the internal routes that lead to it. Any other internal route answers it with "Not shared with you".
- A database is not reached from a member's machine until someone switches **From their machines** on for that database; switching on its project or stack does not. Anything else is reached unless switched off.
- The cluster's own machines always reach each other, and a machine with no owner reaches only what a **network rule** opens to it.

Until the policy is enforced, every machine reaches every other, and the Access page shows what each one would reach.

The Access page's **Sessions** tab lists everything signed in as the workspace's people: CLIs, and AI assistants connected to act as them or as an agent. An admin can disconnect an assistant, and log out a CLI, unless its person also belongs to another workspace, where the CLI acts as them too: then only they can. Its rules say what each grant opens, the ports where it answers on the mesh and its internal routes, and an internal route's page lists who can open it.

## Agents {#agents}

An **agent**, such as an AI assistant or a CI job, is a member that signs in with a token instead of a password, and is granted access the same way. See [Agents & MCP](/concepts/agents).

## Terms {#terms}

- **Admin** {#admin -> roles}: Manages the workspace and every project in it.
- **Member** {#member -> roles}: Sees and changes only what they were granted.
- **Machine owner** {#owner -> mesh}: The person a machine on the mesh acts as, who decides what it reaches.
- **Enforced** {#enforced -> mesh}: The mesh lets each machine reach only what its owner may use, internal routes included.
- **Grant** {#grant -> grants}: Access to a project or a resource at a level: view, deploy, create, update or delete.
