---
id: discovery
title: Discovery
summary: What is already running on your machines outside Meshploy, and how to route it without moving it.
pages: [discovery]
---

## Discovery {#discovery}

**Discovery** shows everything running on your nodes that Meshploy does not manage: the **endpoints** listening on each machine (a port and the process behind it) and the **containers** running there, such as an app started by hand, another platform's containers, or a service installed from a package.

Discovery is for owners and admins: it lists every port, process and container on the machines, which a project member does not need to see. It is read from each machine by the host agent, and **nothing is touched**. A node that is not reporting is listed separately, with how to start its agent.

## Routing an existing service {#routing}

An endpoint can be given a route without moving it: **Add route** makes a hostname for it, served over HTTPS, or a port on the gateway, and forwards it over the mesh to that machine's port. The service keeps running where it is. It is the gentlest way to bring an existing machine under Meshploy.

A row offers a route only where one adds something. Something bound to the machine alone or to the mesh gets a way in; a web app already open on every interface gets a hostname, and its port stays open as it is. Anything else already open on every interface, such as SSH or a published database, offers none: a route would add a second way in, not close the first. The machine's own DNS resolver and mail relay offer none either, since published they would answer anyone.

## From the internet {#internet}

On the gateway, each endpoint bound where the internet could reach it says what the host firewall does with it: open, firewalled, or allowed only from some addresses. A container's published port is marked as open around the firewall, because the container runtime forwards it before UFW or firewalld sees it; publish it on `127.0.0.1` instead and route it. A firewall at the hosting provider can still stop a port, and the gateway cannot see that one.

## Terms {#terms}

- **Endpoint** {#endpoint -> discovery}: A port listening on a node, and the process behind it.
- **Host agent** {#host-agent -> discovery}: The small program on each node that reports what is running there. It reads; it changes nothing.
