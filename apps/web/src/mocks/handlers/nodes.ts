import { http, HttpResponse } from "msw"
import { demoNodeGateway, demoNodeWorker, demoNodeMetrics, DEMO_NODE_GW, DEMO_NODE_W1 } from "../data"

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
