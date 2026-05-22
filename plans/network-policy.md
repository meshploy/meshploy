# K8s NetworkPolicy — Design & Implementation Plan

Zero-trust pod-to-pod isolation for Meshploy's K3s cluster.
Every change described here is derived from decisions made during the design session on 2026-05-22.

---

## Why we are adding this

Currently all pods in the K3s cluster can freely reach any other pod, across all projects and organizations. A compromised pod in org A can open a TCP connection to a database pod in org B with no restriction. Meshploy's project model maps directly to K8s namespaces, so the isolation boundary already exists structurally — we just have no NetworkPolicy enforcing it.

The WireGuard mesh (Headscale) encrypts traffic between nodes at the transport layer, but that does not help with pod-to-pod traffic that stays within the same node, and it provides no application-layer identity verification. NetworkPolicy fills the pod isolation gap.

---

## How K8s NetworkPolicy works (brief primer)

A `NetworkPolicy` resource selects a set of pods via `podSelector` and defines what ingress (incoming) and egress (outgoing) traffic is allowed for those pods. The rules are **additive** — you can only allow, never explicitly deny with a rule. The way you achieve "deny-all" is by creating a policy whose `podSelector` matches pods but whose `ingress`/`egress` arrays are empty. Once any NetworkPolicy selects a pod, all traffic not covered by an allow rule is dropped.

**Important**: NetworkPolicy is enforced by the CNI plugin, not by kube-proxy or the kernel. K3s ships with Flannel by default, and Flannel silently ignores NetworkPolicy objects. Calico or Cilium must be installed for policies to have any effect.

---

## Infrastructure prerequisite — CNI replacement

### The problem with Flannel

K3s bundles Flannel as its default CNI. Flannel handles pod networking (IP allocation, VXLAN overlay) but does not implement the K8s NetworkPolicy spec. If you create a NetworkPolicy object on a Flannel cluster, the API server accepts it but Flannel ignores it — no traffic is ever blocked. This is a silent failure; nothing errors, the policies just have no effect.

### Solution — Calico (preferred) or Cilium

Both Calico and Cilium are OS-level CNI plugins that fully implement K8s NetworkPolicy. They run as DaemonSets on every node and install eBPF or iptables rules to enforce policy at the kernel level. Calico is simpler to operate; Cilium offers more advanced features (layer-7 policy, DNS-aware rules) but is heavier.

**Calico is the default choice** for Meshploy CE. It is well-tested with K3s, lightweight, and the NetworkPolicy spec coverage is complete.

### Install strategy

**New nodes (install.sh)**
Add Calico installation to `install.sh` after K3s is set up. On the server node, apply the Calico manifest (`kubectl apply -f calico.yaml`) before any workloads are scheduled. On agent nodes, Calico's DaemonSet propagates automatically — no extra step needed on agents.

**Existing nodes (CLI subcommand)**
Add a `meshploy install calico` subcommand to `apps/cli/`, following the same pattern as `meshploy install node-exporter`. The command:
1. Connects to the gateway node over the WireGuard mesh
2. Checks if a NetworkPolicy-capable CNI is already running
3. Applies the Calico manifest to the cluster
4. Waits for the DaemonSet to be Ready on all nodes

**Warning to surface**: Swapping CNI on a live cluster causes a brief pod network interruption (seconds to low minutes) while Flannel is evicted and Calico takes over. The CLI must print this warning and prompt for confirmation before proceeding. K3s itself and the Headscale mesh are not affected — only pod-to-pod connectivity is briefly interrupted.

---

## Policy model

### Core principle — derive rules from existing data

We do not introduce a separate "network rules" configuration surface. All allow rules are derived from data that already exists in the database:

- Which services are in the same project (namespace)
- Which variable groups a service has attached (`service_variable_groups` table)
- Whether a group is system-managed and which service generated it (`variable_groups.service_id`)
- Whether a service has public ports (`service_ports.is_public`)
- The gateway node's host IP (`config.HostGatewayIP`)

This means: when a developer attaches service B's system-generated variable group to service A, they are implicitly declaring that A needs to call B. The NetworkPolicy egress rule allowing A → B writes itself from that declaration.

### Namespace default-deny

On every project create, apply two NetworkPolicy resources to the project's K8s namespace:

```
deny-all-ingress  — podSelector: {} (all pods), ingress: []
deny-all-egress   — podSelector: {} (all pods), egress: []
```

Empty arrays mean "allow nothing". Once these are in place, every new pod in the namespace starts with zero connectivity. Explicit allow rules then carve out only what is needed.

### Allow rules — what gets permitted and why

**1. DNS egress (always, every service)**
Without DNS, a pod cannot resolve any hostname. This breaks everything — `http://postgres-service:5432`, `https://api.github.com`, all of it. Always allow egress to kube-dns (namespace `kube-system`, port 53 UDP+TCP). This is applied as a namespace-wide policy, not per-service.

**2. Proxy ingress (every service with a public port)**
The Meshploy proxy (`apps/proxy`) runs as a process on the gateway node, not as a K8s pod. Traffic from the proxy arrives at a service's NodePort, is DNAT'd by kube-proxy to the pod's container port, and the pod sees the source as the gateway node's host IP. The allow rule uses `ipBlock` with `config.HostGatewayIP/32`.

Only services that have at least one `is_public = true` port get this ingress rule. Database services with no public ports do not receive proxy ingress traffic and do not need this rule.

*Known limitation*: The source IP a pod sees after kube-proxy DNAT/SNAT depends on the CNI and kube-proxy configuration. On most Calico + K3s setups the source IP is the gateway node's host IP, making the `ipBlock` approach reliable. If this breaks in a specific CNI configuration, the fix is to add `externalTrafficPolicy: Local` to the NodePort service or configure Calico to preserve source IPs. This is noted as a known limitation and will be revisited alongside the mTLS work.

**3. Cross-service egress (derived from group attachments)**
When service A has service B's system-generated variable group attached, A needs to talk to B. At policy reconcile time, query:

```sql
SELECT vg.service_id AS target_service_id
FROM service_variable_groups svg
JOIN variable_groups vg ON svg.group_id = vg.id
WHERE svg.service_id = <A's ID>
  AND vg.system_managed = true
  AND vg.service_id IS NOT NULL
```

For each `target_service_id` result, add an egress rule from A's pod to B's pod on B's container ports. The pod selector uses the service's slug label (which is already applied to pods at deploy time as `app: <service-slug>`).

**4. Internet egress (configurable per service)**
A new boolean field `InternetEgress` on the `Service` model controls whether the pod can reach addresses outside the cluster. Default:
- `true` for `type = application`
- `false` for `type = database`

When `InternetEgress = true`, an egress rule allows all traffic to `0.0.0.0/0` excluding the cluster pod CIDR (so pods still go through the explicit cross-service rules for in-cluster traffic rather than hitting the catch-all). When `false`, no internet egress rule is added — the default-deny covers it.

### When policies are applied

Policies are reconciled on:
- **Project create** — apply namespace default-deny + DNS egress
- **Service deploy** — apply/update the service's ingress and egress rules
- **Variable group attach or detach** — re-reconcile the attaching service's egress rules immediately, without requiring a redeploy

The reconcile for a single service rewrites only that service's named NetworkPolicy objects (named by service slug, e.g. `netpol-<service-slug>-ingress`, `netpol-<service-slug>-egress`). It is idempotent — safe to call on every config change.

---

## Build pod namespace

Build jobs run as ephemeral K8s Jobs with the `meshploy.com/role=builder` node selector. They run in a dedicated `meshploy-builds` namespace, completely separate from project namespaces.

The policy for the builds namespace is the inverse of service namespaces:
- **Deny all ingress** — nothing needs to reach a build pod
- **Allow all egress** — builds need to git clone, run package managers (npm, pip, cargo), push images to the internal or external registry, and pull base images

Applying a restrictive egress policy to builds is deferred. Build traffic patterns are too varied (different registries, different package managers, different git hosts) to enumerate safely without risking broken builds. The `meshploy-builds` namespace gets only the default-deny-ingress policy; egress is left fully open.

---

## New model field

Add to `Service` in `packages/db/models.go`:

```go
InternetEgress bool `gorm:"not null;default:true" json:"internet_egress"`
```

Override the default in the service layer when creating a database service: set `InternetEgress = false`.

No other model changes are needed. All cross-service allow rules are derived from the existing `service_variable_groups` and `variable_groups` tables.

---

## New and changed files

### `internal/k8s/netpol.go` (new)

Policy builder functions. None of these talk to the database — they take plain structs and return `*networkingv1.NetworkPolicy` objects:

- `NamespaceDefaultDenyIngress(namespace string) *NetworkPolicy`
- `NamespaceDefaultDenyEgress(namespace string) *NetworkPolicy`
- `NamespaceDNSEgressPolicy(namespace string) *NetworkPolicy`
- `ServiceIngressPolicy(namespace, serviceSlug, gatewayIP string, ports []int) *NetworkPolicy`
- `ServiceEgressPolicy(namespace, serviceSlug string, targets []EgressTarget, internetEgress bool, clusterCIDR string) *NetworkPolicy`

`EgressTarget` is a struct carrying the target pod label and its container ports.

### `internal/k8s/netpol_reconciler.go` (new)

`ReconcileServicePolicy(ctx, db, k8sClient, serviceID)` — the single entry point for policy reconcile. It:
1. Loads the service, its ports, and its variable group attachments from the database
2. Resolves which services are reachable from this service (the cross-service egress targets)
3. Calls the builder functions
4. Applies the resulting NetworkPolicy objects to K8s via `Apply` (server-side apply, idempotent)

### `internal/service/variable_group.go` (changed)

Call `ReconcileServicePolicy` at the end of `Attach` and `Detach`. No deploy needed — policy takes effect immediately.

### `internal/service/deployment.go` (changed)

Call `ReconcileServicePolicy` after the K8s Deployment is applied, to ensure the policy reflects the latest state at deploy time as well.

### `internal/service/workload.go` (changed)

When creating a database service, set `InternetEgress = false` before inserting.

### `internal/service/project.go` (changed)

After creating the K8s namespace, apply the namespace default-deny and DNS egress policies.

### `apps/cli/cmd/install.go` (changed)

Add `meshploy install calico` subcommand. Applies the Calico manifest to the cluster, waits for the DaemonSet to become Ready, prints the interruption warning before proceeding.

### `install.sh` (changed)

On server node setup, apply Calico manifest after K3s is initialized and before returning. Flannel disabled via `--flannel-backend=none` K3s flag when Calico is used.

### `apps/web/src/routes/_app/projects/$id/services/$serviceId/config.tsx` (changed)

Add a **Network** section to the config tab. It is read-only except for the internet egress toggle:

- **Inbound**: "Gateway proxy" (always listed if the service has public ports) + any services that have attached this service's system group (query `GET /services/{id}/network` endpoint)
- **Outbound**: kube-dns (always), internet (toggle tied to `InternetEgress`), + list of services this service's groups point to
- The internet egress toggle calls `PATCH /services/{id}` with `{ "internet_egress": bool }`

The section is informational for most users — the toggle is the only user action. The inbound/outbound lists give visibility into what the policy actually permits, which is also useful as a dependency map.

---

## Deferred items

**Ingress rule precision (SNAT/ipBlock)**
The current `ipBlock` approach for the proxy ingress rule works in the common Calico + K3s configuration but is not guaranteed across all setups. The correct fix is to preserve the original client source IP through kube-proxy (`externalTrafficPolicy: Local`) or use Cilium's identity-based policy instead of IP-based. Deferred to the mTLS implementation phase, where per-service identity is addressed properly.

**Stacks — intra-stack communication**
Services belonging to the same stack (same `StackID`) almost certainly need to talk to each other, but the current policy model requires explicit variable group attachments to generate cross-service allow rules. Without this, deploying a stack breaks inter-service communication within it. The fix is to add an implicit allow rule between all services sharing a `StackID`. Deferred — stacks need to be updated as part of the network policy work before it ships.

**Egress filtering for builds**
The `meshploy-builds` namespace currently gets only ingress deny; all egress is open. A future improvement is to enumerate required egress targets (known registry IPs, git hosts) and restrict to those. Not done now because build traffic is too varied to enumerate safely without risking broken builds.
