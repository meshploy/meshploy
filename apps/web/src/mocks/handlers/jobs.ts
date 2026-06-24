import { http, HttpResponse } from "msw"
import { demoJob, demoJobRun, DEMO_JOB_ID } from "../data"

export const jobsHandlers = [
  http.get("/api/v1/orgs/:orgId/projects/:projectId/jobs", () =>
    HttpResponse.json([demoJob])
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/jobs/:jobId", () =>
    HttpResponse.json(demoJob)
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/jobs", async ({ request }) => {
    const body = await request.json() as { name: string }
    return HttpResponse.json({ ...demoJob, id: crypto.randomUUID(), name: body.name })
  }),

  http.patch("/api/v1/orgs/:orgId/projects/:projectId/jobs/:jobId", () =>
    HttpResponse.json(demoJob)
  ),

  http.delete("/api/v1/orgs/:orgId/projects/:projectId/jobs/:jobId", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/jobs/:jobId/runs", () =>
    HttpResponse.json([demoJobRun])
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/jobs/:jobId/trigger", () =>
    HttpResponse.json({ ...demoJobRun, id: crypto.randomUUID(), status: "pending" })
  ),

  http.delete("/api/v1/orgs/:orgId/projects/:projectId/jobs/:jobId/runs/:runId", () =>
    new HttpResponse(null, { status: 204 })
  ),
]
