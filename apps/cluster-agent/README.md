# apps/cluster-agent

The cluster agent, `meshploy-cluster-agent`. It runs on a server whose
Meshploy API runs somewhere else, holds that server's cluster credential, and
answers the API over the mesh. The API holds no credential to the cluster: every Kubernetes
request it sends is checked here, then sent on with the agent's own.

```
Meshploy API ──(mesh, TLS, pinned certificate, token)──► meshploy-cluster-agent ──► K3s API server
 AgentDriver                                               policy, dry run         the agent's credential
```

---

## What the API may do

Reads are allowed, except secrets outside Meshploy's namespaces. Writes are
judged by the object the API server would store: the agent sends each write as
a server-side dry run first and decides on the result, so a patch, a default
or an admission webhook cannot carry something past the check.

- **In a namespace labelled `managed-by=meshploy`:** pods, deployments and
  their scale, jobs, cron jobs, services, secrets, volume claims, network
  policies, resource quotas and limit ranges. Nothing else: a kind not on the
  list can make pods or grant rights no check sees (k3s's `HelmChart` runs an
  installer as cluster-admin; a `RoleBinding` hands a pod the API server).
- **Every pod**, and every pod template, must meet Kubernetes' baseline Pod
  Security standard, and beyond it: no added capabilities, no security
  profile from a file on the node, and only volumes that hold what the pod is
  given (no host paths, NFS, CSI or image volumes).
- **Services** may not be load balancers, external names or claim external
  addresses. **Volume claims** may not name the volume they bind to.
- **Cluster-wide:** creating a namespace labelled `managed-by=meshploy`,
  deleting one, the `gvisor` runtime class for sandboxed projects, and on a
  node only Meshploy's build role label and taint. Nodes are never added or
  removed by the API.
- **kube-system:** nothing, except exec into a node terminal the agent made,
  while the machine's owner has approved it.

## What the agent makes itself

The pods that reach the host are asked for by intent, under `/meshploy/v1/`,
and made here from Meshploy's own templates. `packages/server/k8s.AgentDriver`
sends them.

| Intent | |
|---|---|
| `POST builds` | a build from the API's inputs, run with the agent's builder image (whatever the API names), rootless: uid 1000, unprivileged, its cache in a folder of the claim (`buildkit-rootless`) no root builder wrote; a build that cannot use the cache works in a folder on the node's disk (`build-scratch`), where layers stay layers |
| `POST nodes/<node>/prune` | the image cleanup on a node |
| `POST nodes/<node>/shells/<name>` | the node terminal, only while the owner has approved it |
| `DELETE shells/<name>` | closes a node terminal |
| `DELETE nodes/<node>` | takes a worker out of the cluster once its machine has left the mesh; never the cluster's server, and a node already gone is no error |
| `POST k3s/join-tokens` | a token for one machine to join the cluster (`k3s token create`, `-k3s` names the binary), named for the node and lasting an hour, whatever the API would like: the API never holds the server's own token |

## What the agent carries to the host agent

The host agent (`meshploy host serve`) on the agent's machine takes requests
from a folder, `/var/lib/meshploy/host` (`-host-dir`; empty refuses them): on
a gateway the API shares it, and through the agent the API reaches it as
`host/files/<name>` and `host/list/<name>` under `/meshploy/v1/`
(`hostagent.Folder`, `AgentCluster.HostFolder`). The host agent is the same
either way. Each name is one the agent knows, and each body is checked before
it is left:

| | |
|---|---|
| written into `inbox/` | a `domain.apply` or `registry.gc` request, the edge to serve (`edge.json`, which must validate) and the DNS providers' tokens it names (`dns-credentials.json`, always root-only). A migration is refused: it acts through an API on the same machine |
| read | the host agent's reports in `state/` (with `facts.json`, what the install recorded: the public address, where node ports are bound), each request's status, the results of `domain.apply` and `registry.gc`, and the requests still waiting. Nothing else: not the migration's credential or plan, not the tokens the API left, not a link out of the folder |

## Commands

`install.sh` sets all of this up for an edge: `meshploy install cluster-agent` puts the binary in place, from the CLI's own release (`--from <file>` for one built here), then `init`, a `meshploy-cluster-agent.service` running `serve`, and `register` with the provisioning token the install was given.

```bash
meshploy-cluster-agent init                    # make the certificate, key and token in /var/lib/meshploy/cluster-agent
meshploy-cluster-agent register -api-url http://100.64.0.1:4000 -token mprov-… -name edge-1 -mesh-ip 100.64.0.2
meshploy-cluster-agent serve -listen 100.64.0.2:7443 -api 100.64.0.1   # -builder-image overrides ghcr.io/meshploy/builder, -host-dir the host agent's folder
meshploy-cluster-agent approve-terminal 30m    # let the API open a node terminal until then; "off" ends it
```

`register` sends the agent's certificate and token to an API started with
`CLUSTER_AGENT=true`, spending a provisioning token minted for the agent (an
ordinary or `mreg-` token is refused), and keeps the node secret it gets back.
The API stores the identity on the node and reaches the cluster through the
agent from then on, without restarting; revoking it in the API cuts what is
open to the agent at once.

The agent answers only the API's mesh addresses (`-api`), presenting the
token. Its certificate names `cluster-agent.meshploy.internal` and is its own
trust anchor; the API pins it, so the agent's mesh address can change.

## Tests

`go test ./...` runs the policy and the agent against a stand-in API server,
and the API's `AgentDriver` against the agent over TLS.

`e2e/` drives a real cluster through a running agent the way the API does,
Meshploy's own Kubernetes functions over an `AgentDriver`, then the writes the
agent must refuse, and, with `AGENT_HOST_DIR`, the host agent's requests
carried to a real host agent. It is built with the `agente2e` tag and run on a machine on
the agent's mesh; its header lists the variables it reads.
