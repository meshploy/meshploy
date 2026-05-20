# Meshploy — Zero-Trust TODO

## Critical

- [ ] **Enforce org membership on every protected route**
  Currently `requireUser()` only checks "are you logged in?" — it does not verify the caller is a member of the org in the URL path. A user from Org A can read and write Org B's projects, services, routes, secrets, and deployments. Every handler that takes an `orgId` path param must call `h.svc.Orgs.MemberRole(ctx, orgID, userID)` and reject non-members.
  - `handler/org.go` — GetOrg, UpdateOrg (missing membership check)
  - `handler/project.go` — all handlers
  - `handler/workload.go` — all handlers
  - `handler/route.go` — all handlers
  - `handler/deployment.go` — all handlers
  - `handler/secret.go` — all handlers
  - `handler/node.go` — all handlers (terminal handler has `// future: verify org membership`)
  - `handler/backup.go` — all handlers
  - `handler/domain.go` — all handlers
  - `handler/git_integration.go` — all handlers
  - `handler/registry.go`, `handler/storage.go`, `handler/notification.go` — all handlers

## High

- [ ] **Harden auth middleware — fail closed by default**
  `Auth()` is soft — it never blocks, requiring every handler to manually call `requireUser()`. A missed call leaves an endpoint open. Consider a separate strict middleware that blocks unauthenticated requests on all routes except the explicitly public ones (`/auth/register`, `/auth/login`, node self-register/deregister).

- [ ] **Per-node registration secrets**
  A single `mreg-<hex>` token registers all nodes for an org. If it leaks, any machine can join the mesh. Rotate to per-node one-time-use provisioning tokens that are invalidated immediately after the node's first successful registration.

- [ ] **Rate limiting on auth endpoints**
  `POST /auth/login` and `POST /auth/register` have no rate limiting. Add a per-IP rate limiter (e.g. `golang.org/x/time/rate`) on these two endpoints to prevent brute-force and credential-stuffing attacks.

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
