import { http, HttpResponse } from "msw"
import { demoRoute } from "../data"

export const routesHandlers = [
  http.get("/api/v1/orgs/:orgId/projects/:projectId/routes", () =>
    HttpResponse.json([demoRoute])
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/routes/:routeId", () =>
    HttpResponse.json(demoRoute)
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/routes", async ({ request }) => {
    const body = await request.json() as { hostname: string }
    return HttpResponse.json({ ...demoRoute, id: crypto.randomUUID(), hostname: body.hostname })
  }),

  http.patch("/api/v1/orgs/:orgId/projects/:projectId/routes/:routeId", () =>
    HttpResponse.json(demoRoute)
  ),

  http.delete("/api/v1/orgs/:orgId/projects/:projectId/routes/:routeId", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/routes/:routeId/targets", () =>
    HttpResponse.json(demoRoute.targets)
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/routes/:routeId/targets", () =>
    HttpResponse.json(demoRoute.targets[0])
  ),

  http.delete("/api/v1/orgs/:orgId/projects/:projectId/routes/:routeId/targets/:targetId", () =>
    new HttpResponse(null, { status: 204 })
  ),

  // Domains
  http.get("/api/v1/orgs/:orgId/domains", () => HttpResponse.json([])),

  // Variable groups
  http.get("/api/v1/orgs/:orgId/projects/:projectId/variable-groups", () =>
    HttpResponse.json([])
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/variable-groups", () =>
    HttpResponse.json([])
  ),

  // Permissions
  http.get("/api/v1/orgs/:orgId/resources/:resourceType/:resourceId/permissions", () =>
    HttpResponse.json([])
  ),

  // Notifications
  http.get("/api/v1/orgs/:orgId/notification-channels", () =>
    HttpResponse.json([])
  ),

  // Integrations
  http.get("/api/v1/orgs/:orgId/git-integrations", () => HttpResponse.json([])),
  http.get("/api/v1/orgs/:orgId/registry-integrations", () => HttpResponse.json([])),
  http.get("/api/v1/orgs/:orgId/storage-integrations", () => HttpResponse.json([])),

  // Backups
  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/backups", () =>
    HttpResponse.json([])
  ),

  // Email config
  http.get("/api/v1/orgs/:orgId/email-config", () =>
    new HttpResponse(null, { status: 404 })
  ),
]
