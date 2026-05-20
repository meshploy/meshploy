# Meshploy — Zero-Trust TODO

## Critical

- [x] **Enforce org membership on every protected route**
  Implemented as a Chi middleware in `internal/middleware/orgmember.go`. Extracts the org UUID from `/api/v1/orgs/{orgId}/...` paths, checks membership via `svc.Orgs.MemberRole`, caches results for 30s per user+org pair, fails closed (403) on any error.

## High

- [x] **Harden auth middleware — fail closed by default**
  `RequireAuth` middleware added in `internal/middleware/auth.go`. Blocks unauthenticated requests globally with 401; allowlist covers login, register, self-register/deregister, terminal WebSocket paths, and OpenAPI schema.

- [x] **Per-node registration secrets**
  Added `NodeProvisioningToken` model (`mprov-<hex>`, single-use, hashed, optional TTL). `RegisterWithProvisioningToken` stamps `used_at` on first use and issues a per-node `mnode-<hex>` secret (hash stored on `Node.NodeSecretHash`). `SelfDeregisterNode` accepts `node_secret` (new) or legacy `mreg-` token. Admin CRUD at `GET/POST /orgs/{id}/node-provisioning-tokens` and `DELETE /orgs/{id}/node-provisioning-tokens/{id}`.

- [x] **Rate limiting on auth endpoints**
  Implemented in `internal/middleware/ratelimit.go` + `internal/server/server.go`: login 5 req burst / 1 per 12s, register 3 per hour.

- [ ] **JWT key rotation**
  All tokens are signed with a single static `JWT_SECRET`. Add support for key rotation: sign with the new key, accept both old and new keys during a transition window, then retire the old key.

## Medium

- [ ] **mTLS between proxy and upstream services**
  The proxy forwards traffic over plain HTTP (`http://<tailscale_ip>:<nodeport>`). WireGuard encrypts the transport layer but there is no mutual identity verification at the application layer. Add mTLS so the proxy and upstream services authenticate each other on every connection.

- [ ] **K8s NetworkPolicy — pod-to-pod isolation**
  No `NetworkPolicy` resources are applied in the K3s cluster. All pods can reach each other across projects and orgs. Apply namespace-level `NetworkPolicy` to default-deny all ingress/egress within a namespace except explicitly allowed paths (service → DB, ingress → service).

- [ ] **Node attestation / continuous re-verification**
  Nodes are trusted indefinitely after initial registration. A compromised node stays in the mesh with no mechanism to detect or revoke it. Add periodic re-attestation (heartbeat + token challenge) and automatic Headscale node expiry if a node goes silent.

- [ ] **Build isolation between orgs**
  K8s build Jobs from different orgs run on shared builder nodes with no `NetworkPolicy` between them. Pin each org's builds to a dedicated namespace with network isolation, or enforce a `NetworkPolicy` that blocks pod-to-pod traffic within the builder namespace.

- [ ] **Audit logging**
  No record of who accessed or mutated what. Add structured audit log entries (user, org, action, resource ID, timestamp) for all write operations and sensitive reads (secrets, credentials). Store in a dedicated append-only table or ship to an external sink.

## Low

- [ ] **Egress filtering for deployed services**
  Services can reach the internet freely after deployment. Add an egress `NetworkPolicy` to restrict outbound traffic to explicitly allow-listed destinations, or route egress through a controlled gateway.

- [ ] **Secret rotation**
  Secrets are encrypted at rest but there is no rotation mechanism. Add a `POST /secrets/{id}/rotate` endpoint that re-encrypts the value and bumps a version counter, with a grace period before the old version is invalidated.

- [ ] **MFA (Multi-Factor Authentication)**
  User login is single-factor (password + JWT). Add TOTP-based MFA as an opt-in (or org-enforced) second factor on login.
