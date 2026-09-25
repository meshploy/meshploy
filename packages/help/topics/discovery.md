---
id: discovery
title: Discovery
summary: What is already running on your machines outside Meshploy, and how to route it without moving it.
pages: [discovery]
---

## Discovery {#discovery}

**Discovery** shows everything running on your nodes that Meshploy does not manage: the **endpoints** listening on each machine (a port and the process behind it) and the **containers** running there, such as an app started by hand, another platform's containers, or a service installed from a package.

It is read from each machine by the host agent, and **nothing is touched**. A node that is not reporting is listed separately, with how to start its agent.

## Routing an existing service {#routing}

An endpoint can be given a route without moving it: **Create route** makes a hostname for it, served over HTTPS and forwarded over the mesh to that machine's port. The service keeps running where it is. It is the gentlest way to bring an existing machine under Meshploy.

## Terms {#terms}

- **Endpoint** {#endpoint -> discovery}: A port listening on a node, and the process behind it.
- **Host agent** {#host-agent -> discovery}: The small program on each node that reports what is running there. It reads; it changes nothing.
