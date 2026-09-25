---
id: nodes
title: Nodes & the mesh
summary: The gateway and worker nodes, the private mesh that connects them, what each node runs, and how a machine joins.
pages: [nodes]
---

## The gateway and workers {#nodes}

The **gateway** is the one machine facing the internet. It runs the control plane, the console and API, and the edge that serves your routes. Every other node is a **worker**: it has no public ports at all, and everything reaches it over the mesh.

## The mesh {#mesh}

Nodes are joined by a private **WireGuard mesh**. Each node gets a mesh address, and all traffic between nodes, and from the gateway to your services, travels over it encrypted. That is why a worker can run anywhere, a home server or another cloud, with nothing exposed.

## What a node runs {#roles}

A node's role decides what is scheduled on it:

- **Workloads and builds**: services, and image builds. The default.
- **Workloads**: services only.
- **Builds**: image builds only; services are kept off it.
- **Mesh only**: on the mesh, not in the cluster. Nothing is scheduled on it, and routes can reach its ports. A Mac or Windows machine joins this way.

The gateway is a build node too unless you turn **Act as build node** off.

## Adding a node {#joining}

**Add node** gives a one-line install command with a token in it. A **registration token** can be used again for many machines; a **provisioning token** works once. Mac and Windows machines have their own join scripts and join as mesh-only nodes.

A node that stops reporting is marked **offline** after a short while. After a few minutes more, the cluster moves its services to other nodes that can take them.

## Terms {#terms}

- **Gateway** {#gateway -> nodes}: The one machine facing the internet: the control plane, the console and API, and the edge.
- **Mesh address** {#mesh-address -> mesh}: A node's address on the private WireGuard mesh, which all traffic between nodes uses.
- **Build node** {#build-node -> roles}: A node that runs image builds.
- **Mesh-only** {#mesh-only -> roles}: On the mesh but not in the cluster: nothing is scheduled on it, and routes can reach its ports.
- **Provisioning token** {#provisioning-token -> joining}: A join token that works once. A registration token can be used again.
