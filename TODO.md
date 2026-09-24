# Meshploy roadmap

Meshploy connects your servers into one application platform, operated through the console, CLI, API, and AI agents. This roadmap describes the next work toward that goal.

**Status reviewed: September 24, 2026.** In-progress work may already have usable parts; it does not mean the entire workflow is complete or available in every release. Planned work has no committed release date. See [releases](https://github.com/meshploy/meshploy/releases) for what is available in a published version.

Some of this work may be released in Meshploy Enterprise or Meshploy Cloud rather than the Community edition.

## In progress

| Area | Current state and remaining work |
|---|---|
| Network exposure hardening | Published workload ports are restricted to the mesh, and TCP routes publish a port deliberately. The gateway's own service ports still listen on every interface; restricting them to the mesh and loopback remains. Host firewall configuration remains an operator responsibility. |
| Existing-service discovery | Endpoint discovery and route actions are built. Importing discovered workloads into platform management remains deferred. |
| Migration from Dokploy | Core migration stages have been implemented and exercised on real servers. Broader coverage, including Compose applications and additional edge configurations, remains. |

## Planned

| Area | Direction |
|---|---|
| Agent connections | OAuth support for remote MCP clients that require it. Scoped agent tokens and local CLI-based MCP already exist. |
| CLI workflows | Browser-assisted authentication and improved coverage of platform operations. |
| Workload isolation | Kubernetes network policies for explicit communication boundaries between workloads. |
| Gateway availability | Redundant gateway operation and failover. This is future work, not a current high-availability guarantee. |
| CI runners | Ephemeral GitHub Actions runners on your own infrastructure. |
| Cluster validation | Broader end-to-end tests across installation, deployment, networking, and recovery. |
| Node role management | Console workflows for changing mesh-only nodes into workload or build nodes, with the required runtime installation. |

## Under consideration

These directions need further design, prioritization, or validation before becoming delivery commitments.

- **Notifications:** clearer event coverage, delivery behavior, and user controls.
- **Hosted playground:** a live infrastructure-backed trial alongside the existing browser demo.
- **Node identity:** periodic attestation and re-verification after registration.
- **Upstream identity:** mutual TLS between the gateway proxy and upstream services, in addition to mesh encryption.
- **Build isolation:** stronger network boundaries between builds belonging to different organizations.
- **Audit trails:** structured records of sensitive operations and access.
- **Egress controls:** policies for outbound connections from deployed workloads.
- **Secret rotation:** managed rotation workflows and transition periods for dependent services.

## Already implemented foundations

The current codebase includes:

- browser-based installation, and console-driven upgrades run by a host agent on the gateway that also reports firewall and port exposure;
- domains: several base domains served side by side, each with its own DNS mode, a primary that can be moved, a retirement flow, and ownership proof for custom hostnames;
- one-command node joining with a single-use provisioning token, and mesh-only nodes that join the mesh without running workloads;
- scoped agent identities and MCP access;
- configuration files and configuration references between services;
- templates, and rollout on configuration changes;
- route target types, TCP routes and TCP exposure zones, and route actions on discovered endpoints.

These remain subject to the version you install; they should not be read as upcoming features simply because related improvements appear above.

## Follow or contribute

Use [GitHub Issues](https://github.com/meshploy/meshploy/issues) to describe a use case, report a problem, or discuss a proposed change. The [contributing guide](./CONTRIBUTING.md) explains how to work on the project. For vulnerability reports, follow the [security policy](./SECURITY.md).

This file is also published as the documentation site's roadmap, so both views share the same source.
