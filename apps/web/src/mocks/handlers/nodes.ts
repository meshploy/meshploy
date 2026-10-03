import { http, HttpResponse } from "msw"
import { demoNodeGateway, demoNodeWorker, demoLaptop, demoNodeMetrics, demoHostContainers, demoDiscovery, DEMO_ORG_ID, DEMO_MEMBER_ID, DEMO_USER_ID, DEMO_SVC_DB, DEMO_PROJECT_ID } from "../data"
import { db } from "../state"

// The demo database's reach switch for ravi; undefined until switched.
let dbReach: boolean | undefined
// Whether the demo's mesh "enforces" its policy: nothing is enforced in a demo.
let meshEnforced = false
// Network rules added on the demo's Access page.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
let networkRules: any[] = []

const nodes = [demoNodeGateway, demoNodeWorker, demoLaptop]

// The mesh access report, worked out as the server would for the demo's
// three machines.
function meshAccess() {
  const owner = (db.nodes.find((n) => n.id === demoLaptop.id) ?? demoLaptop).owner_id as string | undefined
  const laptop = {
    id: demoLaptop.id, name: demoLaptop.name, ip: demoLaptop.tailscale_ip, kind: "connected",
    owner_id: owner, owner_name: owner === DEMO_MEMBER_ID ? "ravi" : owner === DEMO_USER_ID ? "Demo User" : undefined,
    everything: owner === DEMO_USER_ID,
    reaches: owner === DEMO_USER_ID
      ? [{ to: "every machine", why: "Demo User is an owner of the organisation" }]
      : [
          { to: "gateway", ports: [53, 4000], why: "Every machine resolves names and registers through the gateway" },
          ...(owner === DEMO_MEMBER_ID ? [
            { to: "gateway", ports: [80, 443], why: "Internal web routes, checked per person at the proxy" },
            { to: "gateway, worker-1", ports: [31432], why: "ravi may use postgres in Demo Project" },
          ] : []),
        ],
  }
  return {
    enforced: meshEnforced,
    applied_at: meshEnforced ? new Date().toISOString() : undefined,
    can_enforce: true,
    unknown: [],
    // What the server's owner sees on the Policy tab: a short sample.
    policy: JSON.stringify({
      hosts: { "gateway-0004": "100.64.0.1/32", "worker-1-0005": "100.64.0.2/32", "ravi-laptop-0013": "100.64.0.7/32" },
      acls: [
        { action: "accept", src: ["gateway-0004", "worker-1-0005"], dst: ["gateway-0004:*", "worker-1-0005:*"] },
        { action: "accept", src: ["*"], dst: ["gateway-0004:53", "gateway-0004:4000"] },
        ...networkRules.map((r) => ({ action: "accept", src: ["ravi-laptop-0013"], dst: (r.ports.length ? r.ports : ["*"]).map((p: number | string) => `${r.to.name}-0005:${p}`) })),
      ],
    }, null, 2),
    machines: [
      { id: demoNodeGateway.id, name: demoNodeGateway.name, ip: demoNodeGateway.tailscale_ip, kind: "gateway", everything: true,
        reaches: [{ to: "every machine", why: "The gateway's proxy, metrics and discovery reach every machine" }] },
      { id: demoNodeWorker.id, name: demoNodeWorker.name, ip: demoNodeWorker.tailscale_ip, kind: "cluster", everything: false,
        reaches: [{ to: "gateway", why: "Kubernetes runs across the cluster's machines" }, { to: "ravi-laptop", why: "Apps reach data on connected machines, on any port until a machine says which it offers" }] },
      laptop,
    ],
  }
}

// What a demo grant opens from its person's machines: the database's port
// once switched on, and an app's port and internal route.
function opensOf(type: string, database: boolean, member: boolean) {
  if (!member || type === "job") return { ports: [] }
  if (database) return dbReach ? { ports: [31432], opens: [{ kind: "port", port: 31432, on: "cluster" }] } : { ports: [], off: 1 }
  return {
    ports: [30080],
    opens: [{ kind: "port", port: 30080, on: "cluster" }, { kind: "route", hostname: "grafana.internal.demo.meshploy.dev" }],
    ...(type === "project" ? { off: dbReach ? 0 : 1 } : {}),
  }
}

export const nodesHandlers = [
  http.get("/api/v1/orgs/:orgId/nodes", () => HttpResponse.json(nodes)),

  http.get("/api/v1/orgs/:orgId/nodes/:nodeId", ({ params }) => {
    const node = nodes.find((n) => n.id === params.nodeId)
    if (!node) return new HttpResponse(null, { status: 404 })
    return HttpResponse.json(node)
  }),

  http.patch("/api/v1/orgs/:orgId/nodes/:nodeId", ({ params }) => {
    const node = nodes.find((n) => n.id === params.nodeId)
    return HttpResponse.json(node ?? demoNodeGateway)
  }),

  http.get("/api/v1/orgs/:orgId/mesh/access", () => HttpResponse.json(meshAccess())),

  // Who the demo's internal route answers.
  http.get("/api/v1/orgs/:orgId/projects/:projectId/routes/:routeId/openers", () => {
    const person = (m: (typeof db.members)[number]) => ({ user_id: m.user_id, name: m.user_name, email: m.user_email, role: m.role, machines: [] as string[] })
    return HttpResponse.json({
      internal: true, enforced: meshEnforced,
      admins: db.members.filter((m) => m.role !== "member").map((m) => ({ ...person(m), reach: true })),
      members: db.members.filter((m) => m.role === "member").slice(0, 1).map((m) => ({ ...person(m), via: "Demo Project", reach: true, machines: ["ravi-laptop"] })),
      rules: networkRules.filter((r) => !r.ports.length || r.ports.includes(443)).map((r) => r.from.name),
    })
  }),

  // The Access page's rules: the demo's grants, and network rules added in
  // this session.
  http.get("/api/v1/orgs/:orgId/access/rules", () => {
    const members = db.members
    type Grant = { user: string; type: string; id: string; actions: string[] }
    const grants = new Map<string, Grant>()
    for (const p of db.permissions) {
      const k = `${p.user_id}:${p.resource_type}:${p.resource_id}`
      const g: Grant = grants.get(k) ?? { user: p.user_id, type: p.resource_type, id: p.resource_id, actions: [] }
      g.actions.push(p.action)
      grants.set(k, g)
    }
    const nameOf = (type: string, id: string) => {
      const list = (db as Record<string, { id: string; name?: string; project_id?: string; type?: string }[]>)[type === "service" ? "services" : `${type}s`] ?? []
      return list.find((r) => r.id === id)
    }
    const rows = [...grants.values()].map((g) => {
      const r = nameOf(g.type, g.id)
      return {
        id: `grant:${g.user}:${g.type}:${g.id}`, kind: "grant",
        from: { kind: "person", id: g.user, name: members.find((m) => m.user_id === g.user)?.user_name ?? "", email: members.find((m) => m.user_id === g.user)?.user_email, member: members.some((m) => m.user_id === g.user) },
        to: { kind: g.type, id: g.id, name: r?.name ?? (g.id === DEMO_SVC_DB ? "postgres" : g.type), project_id: r?.project_id ?? DEMO_PROJECT_ID, database: g.id === DEMO_SVC_DB || r?.type === "database" },
        ...opensOf(g.type, g.id === DEMO_SVC_DB, members.some((m) => m.user_id === g.user)), actions: g.actions,
        ...(g.id === DEMO_SVC_DB && dbReach !== undefined ? { reach: dbReach } : g.id === DEMO_SVC_DB ? { reach: false } : {}),
      }
    })
    return HttpResponse.json([...rows, ...networkRules])
  }),
  http.post("/api/v1/orgs/:orgId/access/rules/preview", async ({ request }) => {
    const b = (await request.json()) as { from_kind: string; from_id?: string; to_node_id: string; ports: string }
    const host = (name: string, id: string) => `${name}-${id.slice(-4)}`
    const to = db.nodes.find((n) => n.id === b.to_node_id)
    const ports = b.ports.split(/[ ,]+/).filter(Boolean)
    const dst = (ports.length ? ports : ["*"]).flatMap((p) => [`${host(to?.name ?? "machine", b.to_node_id)}:${p}`, `${host(to?.name ?? "machine", b.to_node_id)}-v6:${p}`])
    // The machines the source names, as the server would: a person's
    // connected machines, the one machine, or every connected machine.
    const connected = db.nodes.filter((n) => n.mesh_role === "mesh")
    const from = b.from_kind === "all" ? connected : b.from_kind === "machine" ? connected.filter((n) => n.id === b.from_id)
      : connected.filter((n) => n.owner_id === b.from_id)
    if (from.length === 0) return HttpResponse.json({ message: "this rule would allow nothing yet: the source has no machine on the mesh" }, { status: 400 })
    const src = from.flatMap((n) => [host(n.name, n.id), `${host(n.name, n.id)}-v6`])
    return HttpResponse.json({ acls: JSON.stringify([{ action: "accept", src, dst }], null, 2) })
  }),
  http.post("/api/v1/orgs/:orgId/access/rules", async ({ request }) => {
    const b = (await request.json()) as { from_kind: string; from_id?: string; to_node_id: string; ports: string; note: string }
    const nodes = db.nodes
    const members = db.members
    const fromName = b.from_kind === "all" ? "Every machine" : b.from_kind === "person"
      ? members.find((m) => m.user_id === b.from_id)?.user_name ?? "" : nodes.find((n) => n.id === b.from_id)?.name ?? ""
    networkRules.push({
      id: crypto.randomUUID(), kind: "network", from: { kind: b.from_kind, id: b.from_id, name: fromName },
      to: { kind: "machine", id: b.to_node_id, name: nodes.find((n) => n.id === b.to_node_id)?.name ?? "" },
      ports: b.ports.split(/[ ,]+/).filter(Boolean).map(Number).sort((x, y) => x - y), note: b.note,
    })
    return new HttpResponse(null, { status: 201 })
  }),
  http.put("/api/v1/orgs/:orgId/access/rules/:ruleId", async ({ params, request }) => {
    const b = (await request.json()) as { from_kind: string; from_id?: string; to_node_id: string; ports: string; note: string }
    const r = networkRules.find((x) => x.id === params.ruleId)
    if (!r) return new HttpResponse(null, { status: 404 })
    const nodes = db.nodes
    const members = db.members
    r.from = { kind: b.from_kind, id: b.from_id, name: b.from_kind === "all" ? "Every connected machine" : b.from_kind === "person"
      ? members.find((m) => m.user_id === b.from_id)?.user_name ?? "" : nodes.find((n) => n.id === b.from_id)?.name ?? "" }
    r.to = { kind: "machine", id: b.to_node_id, name: nodes.find((n) => n.id === b.to_node_id)?.name ?? "" }
    r.ports = b.ports.split(/[ ,]+/).filter(Boolean).map(Number).sort((x: number, y: number) => x - y)
    r.note = b.note
    return new HttpResponse(null, { status: 204 })
  }),
  http.delete("/api/v1/orgs/:orgId/access/rules/:ruleId", ({ params }) => {
    networkRules = networkRules.filter((r) => r.id !== params.ruleId)
    return new HttpResponse(null, { status: 204 })
  }),
  http.put("/api/v1/orgs/:orgId/mesh/enforced", async ({ request }) => {
    meshEnforced = ((await request.json()) as { enforced: boolean }).enforced
    return new HttpResponse(null, { status: 204 })
  }),

  // ravi may view the demo database (a grant seeded in the demo store);
  // whether his laptop reaches it is off until switched on, as for any database.
  http.get("/api/v1/orgs/:orgId/mesh/reach", ({ request }) => {
    if (new URL(request.url).searchParams.get("resource_id") !== DEMO_SVC_DB) return HttpResponse.json([])
    return HttpResponse.json([{
      user_id: DEMO_MEMBER_ID, reach: dbReach ?? false, chosen: dbReach !== undefined, machines: ["ravi-laptop"],
      services: [{ service_id: DEMO_SVC_DB, name: "postgres", database: true, reach: dbReach ?? false,
        ports: [{ name: "postgres", port: 5432, mesh_port: 30003, on: "cluster" }] }],
    }])
  }),
  http.put("/api/v1/orgs/:orgId/mesh/reach", async ({ request }) => {
    dbReach = ((await request.json()) as { reach: boolean }).reach
    return new HttpResponse(null, { status: 204 })
  }),

  http.put("/api/v1/orgs/:orgId/nodes/:nodeId/owner", async ({ params, request }) => {
    const { owner_id } = (await request.json()) as { owner_id: string }
    const node = db.nodes.find((n) => n.id === params.nodeId)
    if (!node) return new HttpResponse(null, { status: 404 })
    node.owner_id = owner_id || undefined
    return HttpResponse.json(node)
  }),

  http.delete("/api/v1/orgs/:orgId/nodes/:nodeId", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.get("/api/v1/orgs/:orgId/nodes/:nodeId/metrics", () =>
    HttpResponse.json(demoNodeMetrics)
  ),

  // Gateway-only, like the real one: the host agent reports there.
  http.get("/api/v1/orgs/:orgId/nodes/:nodeId/containers", ({ params }) =>
    HttpResponse.json(
      params.nodeId === demoNodeGateway.id
        ? demoHostContainers
        : { available: false, containers: [], groups: [], stale: false, mine: 0 }
    )
  ),

  http.get("/api/v1/orgs/:orgId/discovery", () => HttpResponse.json(demoDiscovery)),

  http.post("/api/v1/orgs/:orgId/discovery/ignores", async ({ request }) => {
    const body = await request.json() as Record<string, unknown>
    return HttpResponse.json({ id: crypto.randomUUID(), organization_id: DEMO_ORG_ID, note: "", ...body })
  }),

  http.delete("/api/v1/orgs/:orgId/discovery/ignores/:ignoreId", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.get("/api/v1/orgs/:orgId/nodes/registration-token", () =>
    HttpResponse.json({ token: "mreg-demo0000000000000000000000000" })
  ),

  http.post("/api/v1/orgs/:orgId/nodes/registration-token", () =>
    HttpResponse.json({ token: "mreg-demo0000000000000000000000001" })
  ),

  http.post("/api/v1/orgs/:orgId/nodes/provisioning-tokens", () =>
    HttpResponse.json({ token: "mprov-demo000000000000000000000000" })
  ),
]
