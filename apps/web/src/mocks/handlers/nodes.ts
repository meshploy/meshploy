import { http, HttpResponse } from "msw"
import { demoNodeGateway, demoNodeWorker, demoNodeMetrics, demoHostContainers, demoDiscovery, DEMO_ORG_ID } from "../data"

const nodes = [demoNodeGateway, demoNodeWorker]

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
