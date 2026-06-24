import { http, HttpResponse } from "msw"
import { demoVolume } from "../data"

export const volumesHandlers = [
  http.get("/api/v1/orgs/:orgId/projects/:projectId/volumes", () =>
    HttpResponse.json([demoVolume])
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/volumes/:volumeId", () =>
    HttpResponse.json(demoVolume)
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/volumes", async ({ request }) => {
    const body = await request.json() as { name: string }
    return HttpResponse.json({ ...demoVolume, id: crypto.randomUUID(), name: body.name })
  }),

  http.delete("/api/v1/orgs/:orgId/projects/:projectId/volumes/:volumeId", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/volumes/:volumeId/mounts", () =>
    HttpResponse.json({
      id: crypto.randomUUID(),
      volume_id: demoVolume.id,
      service_id: "00000000-0000-0000-0000-000000000006",
      mount_path: "/data",
      created_at: demoVolume.created_at,
      updated_at: demoVolume.updated_at,
    })
  ),

  http.delete("/api/v1/orgs/:orgId/projects/:projectId/volumes/:volumeId/mounts/:mountId", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/volumes/:volumeId/backup", () =>
    new HttpResponse(null, { status: 404 })
  ),
]
