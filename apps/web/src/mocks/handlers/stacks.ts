import { http, HttpResponse } from "msw"
import { demoStack } from "../data"

export const stacksHandlers = [
  http.get("/api/v1/orgs/:orgId/projects/:projectId/stacks", () =>
    HttpResponse.json([demoStack])
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/stacks/:stackId", () =>
    HttpResponse.json(demoStack)
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/stacks", async ({ request }) => {
    const body = await request.json() as { name: string }
    return HttpResponse.json({ ...demoStack, id: crypto.randomUUID(), name: body.name })
  }),

  http.put("/api/v1/orgs/:orgId/projects/:projectId/stacks/:stackId", () =>
    HttpResponse.json(demoStack)
  ),

  http.delete("/api/v1/orgs/:orgId/projects/:projectId/stacks/:stackId", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/stacks/:stackId/services", () =>
    HttpResponse.json([])
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/stacks/:stackId/apply", () =>
    HttpResponse.json({
      stack: demoStack,
      created: [],
      updated: [],
      deleted: [],
      errors: [],
    })
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/stacks/:stackId/sync", () =>
    HttpResponse.json({
      stack: demoStack,
      created: [],
      updated: [],
      deleted: [],
      errors: [],
      suggested_mode: "",
      warning: "",
    })
  ),
]
