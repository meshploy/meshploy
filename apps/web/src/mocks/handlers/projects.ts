import { http, HttpResponse } from "msw"
import { demoProject, DEMO_ORG_ID, DEMO_PROJECT_ID } from "../data"

export const projectsHandlers = [
  http.get("/api/v1/orgs/:orgId/projects", () =>
    HttpResponse.json([demoProject])
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId", () =>
    HttpResponse.json(demoProject)
  ),

  http.post("/api/v1/orgs/:orgId/projects", async ({ request }) => {
    const body = await request.json() as { name: string; slug: string }
    return HttpResponse.json({
      ...demoProject,
      id: crypto.randomUUID(),
      name: body.name,
      slug: body.slug,
    })
  }),

  http.patch("/api/v1/orgs/:orgId/projects/:projectId", async ({ request }) => {
    const body = await request.json() as { name: string }
    return HttpResponse.json({ ...demoProject, name: body.name })
  }),

  http.delete("/api/v1/orgs/:orgId/projects/:projectId", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.delete("/api/v1/orgs/:orgId/projects/:projectId/build-cache", () =>
    new HttpResponse(null, { status: 204 })
  ),
]
