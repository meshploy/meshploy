# Meshploy roadmap

Meshploy connects your servers into one application platform, operated through the console, CLI, API, and AI agents. This roadmap describes the next work toward that goal.

**Status reviewed: September 21, 2026.** In-progress work may already have usable parts; it does not mean the entire workflow is complete or available in every release. Planned work has no committed release date. See [releases](https://github.com/meshploy/meshploy/releases) for what is available in a published version.

## In progress

| Area | Current state and remaining work |
|---|---|
| Browser-based installation | Browser setup is implemented; onboarding refinements and validation remain. |
| Console-driven upgrades | Updater, installer, API, and console support are implemented. End-to-end upgrade and recovery validation on a real gateway remains. |
| Host health and exposure reporting | A host agent is being developed to report gateway firewall and port exposure state. |
| Network exposure hardening | Restricting published workload ports to the mesh and improving exposure checks are in progress. Host firewall configuration remains an operator responsibility. |
| Existing-service discovery | Endpoint discovery and route actions are built. Importing discovered workloads into platform management remains deferred. |
| Migration from Dokploy | Core migration stages have been implemented and exercised on real servers. Broader coverage, including Compose applications and additional edge configurations, remains. |
| Configuration references | Initial variable-reference implementation is built; live validation remains. |

## Planned

| Area | Direction |
|---|---|
| Domains | Manage multiple platform domains and expand DNS and certificate configuration. Custom hostnames on routes already exist; this is broader domain management. |
| Installation domain changes | A supported workflow for changing an installation's base domain. |
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

The current codebase includes scoped agent identities and MCP access, configuration files, templates, rollout on configuration changes, route target types, and TCP exposure zones. These remain subject to the version you install; they should not be read as upcoming features simply because related improvements appear above.

## Follow or contribute

Use [GitHub Issues](https://github.com/meshploy/meshploy/issues) to describe a use case, report a problem, or discuss a proposed change. The [contributing guide](./CONTRIBUTING.md) explains how to work on the project. For vulnerability reports, follow the [security policy](./SECURITY.md).

This file is also published as the documentation site's roadmap, so both views share the same source.
