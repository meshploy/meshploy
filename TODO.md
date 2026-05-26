# Meshploy — Upcoming

- **JWT key rotation**
  Support seamless key rotation for signed tokens — issue new tokens with an updated key while honoring existing ones during a configurable transition window.

- **Node attestation / continuous re-verification**
  Periodically re-verify node identity after initial registration via heartbeat challenges, with automatic Headscale expiry for nodes that go silent.

- **mTLS between proxy and upstream services**
  Mutual TLS between the edge proxy and upstream services for application-layer identity verification on top of the WireGuard mesh.

- **K8s NetworkPolicy — pod-to-pod isolation**
  Namespace-level default-deny network policies with explicit allow paths (service → DB, ingress → service) for stronger workload isolation.

- **Build isolation between orgs**
  Dedicated build namespaces per org with inter-pod network policies, so build jobs from different orgs are fully isolated at the network layer.

- **Audit logging**
  Structured audit trail (user, org, action, resource, timestamp) for write operations and sensitive reads, stored in an append-only table or shipped to an external sink.

- **Egress filtering for deployed services**
  Allow-list based egress policies so services can only reach explicitly permitted external destinations.

- **Secret rotation**
  First-class secret rotation via `POST /secrets/{id}/rotate` — re-encrypts the value, bumps a version counter, and supports a grace period before the old version is invalidated.
