---
title: Configure node roles and build placement
description: Choose which machines build images, run applications, or only join the private mesh.
---

A node's location and its role are separate choices. A cloud VM can build images; a supported office machine can run workloads; a mesh-only machine can expose an existing service without joining Kubernetes.

## Choose a role

| Role | Builds | Application workloads | Typical use |
| --- | --- | --- | --- |
| Workload + builder (`workload_builder`) | Yes | Yes | A small installation sharing capacity. |
| Workload (`workload`) | No | Yes | Application runtime capacity. |
| Builder (`builder`) | Yes | No | Dedicated build capacity. |
| Mesh only (`mesh`) | No | No | Existing services reachable over the mesh. |

The gateway also hosts central platform services and public routing. Treat its availability and available resources separately from the number of workers you add.

## 1. Prepare the machine

Use a supported Linux host from the [self-hosting requirements](/self-hosting/). Verify connectivity to the gateway and available disk, CPU, and memory.

Keep the host or provider firewall configured. Joining a private mesh does not automatically make every other listener on the machine private.

For a laptop or office build machine, it must run a supported node environment, remain awake, and stay reachable while builds execute. Installing only the CLI does not turn it into a builder.

## 2. Join with the intended role

Open **Cluster → Add a node**, select its role, and follow the generated installation instructions on that machine.

For an authenticated CLI with SSH access to the host, the role-aware setup command is:

```bash
meshploy node init user@builder.example.com --role builder
```

Use `workload`, `workload_builder`, or `mesh` for the other roles. Follow the command's prompts and check the resulting node in the console. See the [CLI reference](/cli/reference/) for SSH identity and port options.

Confirm the node is online. Workload and builder nodes must also be healthy Kubernetes members; a mesh-only node intentionally is not.

## 3. Choose build placement

Open the service's build settings and select the build node, or leave placement automatic to use an eligible builder.

Build placement selects where a source build runs. It does not select where the resulting application runs.

For example:

```text
Git repository
    → office build node
    → image registry
    → cloud workload node
```

Set build resource requests and limits for the repository's needs. A build can remain pending when its selected node lacks the requested capacity, even if the node is online.

## 4. Choose runtime placement

Use the service's target-node setting when it must run on a particular workload node. Otherwise, allow Kubernetes to schedule it on eligible capacity.

Before moving an application, review its volumes and other node-bound dependencies. Changing placement does not copy local persistent data to another machine.

Deploy and verify both the build location and the running workload's location.

## Offline nodes and role changes

| Situation | What to do |
| --- | --- |
| Pinned builder offline | Bring it online or select another eligible builder, then retry. |
| No eligible runtime capacity | Add capacity or correct placement and resource requests. |
| Mesh-only node missing from a service picker | Expected: it cannot run Kubernetes workloads. Use it as a route target instead. |
| Need to switch into or out of mesh-only | This requires installing/removing K3s; it is not a console role toggle. |

Before removing a node, move or stop affected workloads and protect its persistent data. Removing a node record and uninstalling software from a host are different actions; consult the CLI reference before choosing either.

Adding workers or replicas does not remove the gateway dependency or make local volumes highly available.
