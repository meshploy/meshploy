# Documentation content review

Reviewed against the documentation sources and current resource model on 2026-09-21. This is an editorial backlog, not a statement that planned features are shipped.

## Current structure

The public site contains a curated home page and ten synced documents. Self-hosting comes from the root README; the introduction uses its own curated source. Architecture pages come from HOW_IT_WORKS.md, CONCEPTS.md, and the database package README. CLI and API references come from their application READMEs. Contributing, security, and roadmap pages mirror root documents.

The introduction now has a dedicated source at `apps/docs/content/introduction.md`, still processed by `sync-docs.mjs`. It explains the product without importing repository badges, the technology inventory, or duplicate command reference material.

## Priority 1: complete user workflows

| Proposed guide | Coverage and completion criteria |
| --- | --- |
| Deploy your first application | Prerequisites, Git connection or image, project, service port, environment, first deploy, route, verification, failed-build diagnosis. Include one tested sample app. |
| Node roles and placement | Gateway, workload, builder, combined, and mesh-only roles; supported OS; join flow; build pinning; offline builders; removal and draining. |
| Route an existing service | Mesh-only node, reachable listen address, node-and-port target, hostname, TLS, validation, pause and removal. Distinguish public, mesh, and local access. |
| Domains and certificates | Installation DNS modes, custom route hostnames, verification, DNS propagation, certificate issuance, common failures. Keep future DNS-provider integrations separate. |
| Connect an AI agent | Local user credentials versus remote agent tokens, actual client configuration, permission setup, read-only first checks, token rotation/revocation, tool availability differences. |
| Database backup and restore | Supported engines, storage integration, schedule, retention behavior, manual backup, restore target and consequences, verification of restored data. Separate platform backup from application backup. |

## Priority 2: operating the platform

- Services: build context, build variables versus runtime variables, health checks, logs, rollout failures, rollback limitations, replicas and placement.
- Storage: volume lifecycle, node affinity and rescheduling limitations, migration, capacity, and deletion behavior.
- Projects and access: organization roles, project/resource permissions, agent identities, examples of allowed and denied operations.
- Jobs: scheduled versus one-off runs, timezone behavior, concurrency, retries, logs, and run history.
- Integrations: provider-specific Git setup, webhook validation, registry credentials, backup storage, notification testing and failed deliveries.
- Operations: upgrades, platform backups, gateway loss, certificate problems, disconnected nodes, registry access, disk pressure, and recovery exercises.
- Migration: document the existing Dokploy CLI workflow with supported inputs, preview/apply boundaries, unsupported resources, and recovery expectations.

## Existing pages: corrections and depth

| Page | Findings and proposed action |
| --- | --- |
| Introduction | Replaced the README extract with the connected-infrastructure story, resource model, interfaces, and realistic ownership boundaries. |
| Self-hosting | Contains useful OS, DNS, firewall, and first-account material. Add an explicit successful-install checklist and troubleshooting tree; verify prerequisites and resource sizing on a fresh installation. |
| How it works | Useful explanations, but mixes architecture, tutorials, and client claims. Remove absolute claims that workers cannot be reached or containers cannot bind public ports. The same document notes listeners on all interfaces. Review guarantees about instant rollback, retained data, and recovery under a minute. |
| Concepts | Valuable engineering detail. Node lifecycle still defines all nodes as K3s members despite mesh-only support. Cross-check builder labels and scheduling descriptions against current combined node roles. Clarify availability and rollout assumptions. |
| CLI reference | Broad command inventory, including migration, apply, and MCP. Add task-oriented examples, expected output, error cases, and links to guides. Compare older `node add` examples in How it works with the current command definitions. |
| API reference | Primarily route tables and repository internals; its generated description promises request/response shapes that are not provided for each endpoint. Add authentication, pagination, errors, streaming, and executable examples; derive endpoint schemas from the API's OpenAPI output where possible. |
| Database schema | Contributor-focused; move under an engineering/contributing group as user architecture expands. Generated description mentions an open-core boundary while the source includes the extension registry; verify terminology. |
| Contributing | Keep the development and testing workflow, and add docs authoring instructions: sources, sync, links, preview, and production search verification. |
| Security | This is a vulnerability disclosure policy. Add a separate operator security guide rather than mixing host/network guidance into disclosure instructions. |
| Roadmap | Updated from the security-heavy list into in-progress, planned, and under-consideration sections, based on the internal plan statuses. Keep it aligned with implementation and published releases; avoid timelines inferred from plans. |

## Claims to verify before republishing deeper guides

- HOW_IT_WORKS.md promises every MCP call is approved and destructive labels cause confirmation. Tool descriptions do not enforce client approval policy. Describe server authorization separately from client confirmation settings.
- Tool counts differ between documents (90+ versus 100+). Prefer capabilities and actual local/remote tool availability over hard-coded counts.
- The README no longer claims nothing leaves the infrastructure. Continue to distinguish encrypted transport, possible public DERP relay use, and third-party integrations when expanding the architecture docs.
- NS delegation is described as required early in How it works, followed by an alternative DNS mode. Scope that requirement to wildcard issuance in delegation mode.
- Replica count is not a guarantee of service or platform availability. Document health checks, storage constraints, capacity, and the gateway dependency.

## Proposed navigation

Getting started → Guides (deploy, connect, operate) → Networking → AI agents → Reference (CLI, API) → Architecture → Contributing.

Add navigation groups when their pages exist. Preserve existing public URLs, using redirects if content is later split.

## Planned capabilities

Multiple managed domains, DNS-provider integrations, and further agent workflows require implementation-backed docs when available. The plans live in the sibling `../internal-docs/plans` directory. Its index and relevant closed, open, and upcoming plans were reviewed for the six guides, with implementation checks for roles, build placement, routes, agent permissions, TLS, and database backup/restore.

Closed plans confirm agent identities, declarative apply rollouts, and route targets/TCP zones. Open plans include shipped mesh-only support with role switching still pending, and partially implemented discovery/network access. Upcoming domain management, DNS-provider integration, MCP OAuth, HA gateways, and network isolation must not be presented as released capabilities. Existing custom route hostnames are distinct from future managed domains.

## Guide implementation

The six Priority 1 guides now exist under `src/content/docs/guides/` and are linked from the sidebar, home, and introduction. They are authored pages, not sync output. The initial walkthrough includes a standard Nginx image and a small Dockerfile example. Documentation builds and links are checked locally; deploying that walkthrough and exercising a real backup/restore remain live-environment validation work.
