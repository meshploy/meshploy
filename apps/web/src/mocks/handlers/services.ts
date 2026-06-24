import { http, HttpResponse } from "msw"
import {
  demoServiceApi, demoServiceWeb, demoServiceDb,
  demoDeployment, demoBuildConfig, demoPods,
  DEMO_SVC_API, DEMO_SVC_WEB, DEMO_SVC_DB,
} from "../data"

const serviceList = [demoServiceApi, demoServiceWeb, demoServiceDb]

function findService(id: string) {
  return serviceList.find((s) => s.id === id)
}

export const servicesHandlers = [
  http.get("/api/v1/orgs/:orgId/projects/:projectId/services", () =>
    HttpResponse.json(serviceList)
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId", ({ params }) => {
    const svc = findService(params.serviceId as string)
    if (!svc) return new HttpResponse(null, { status: 404 })
    return HttpResponse.json(svc)
  }),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/services", async ({ request }) => {
    const body = await request.json() as { name: string }
    return HttpResponse.json({ ...demoServiceApi, id: crypto.randomUUID(), name: body.name })
  }),

  http.patch("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId", ({ params }) => {
    const svc = findService(params.serviceId as string)
    return HttpResponse.json(svc ?? demoServiceApi)
  }),

  http.delete("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/start", ({ params }) =>
    HttpResponse.json({ ...findService(params.serviceId as string) ?? demoServiceApi, status: "running" })
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/stop", ({ params }) =>
    HttpResponse.json({ ...findService(params.serviceId as string) ?? demoServiceApi, status: "stopped" })
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/env-vars", () =>
    HttpResponse.json({ env_vars: "NODE_ENV=production\nPORT=4000\n" })
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/build-config", () =>
    HttpResponse.json(demoBuildConfig)
  ),

  http.patch("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/build-config", () =>
    HttpResponse.json(demoBuildConfig)
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/build-config/env-vars", () =>
    HttpResponse.json({ build_env_vars: "" })
  ),

  http.put("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/build-config/env-vars", () =>
    HttpResponse.json({ build_env_vars: "" })
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/build-config/deploy-token", () =>
    HttpResponse.json({ ...demoBuildConfig, deploy_token: "dtkn-newtoken" })
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/deployments", () =>
    HttpResponse.json([demoDeployment])
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/deployments", () =>
    HttpResponse.json({ ...demoDeployment, id: crypto.randomUUID(), status: "pending" })
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/deployments/:deploymentId", () =>
    HttpResponse.json(demoDeployment)
  ),

  http.delete("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/deployments/:deploymentId", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.delete("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/deployments/:deploymentId/record", () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/deployments/:deploymentId/rollback", () =>
    HttpResponse.json({ ...demoDeployment, id: crypto.randomUUID() })
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/deployments/:deploymentId/retry", () =>
    HttpResponse.json({ ...demoDeployment, id: crypto.randomUUID(), status: "pending" })
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/pods", () =>
    HttpResponse.json(demoPods)
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/pods/metrics", () =>
    HttpResponse.json([
      { pod_name: demoPods[0].name, cpu_millis: 45, memory_mib: 128 },
      { pod_name: demoPods[1].name, cpu_millis: 38, memory_mib: 112 },
    ])
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/database-config", () =>
    HttpResponse.json({
      id: "00000000-0000-0000-0000-000000000031",
      service_id: DEMO_SVC_DB,
      engine: "postgres",
      version: "16",
      storage_gb: 20,
      slug: "demo_db",
      db_name: "demo",
      db_user: "demo",
      db_password: "demo-password",
      node_port: 30003,
    })
  ),

  http.post("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/reset", ({ params }) =>
    HttpResponse.json({ ...demoDeployment, service_id: params.serviceId as string })
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/mounts", () =>
    HttpResponse.json([])
  ),

  http.get("/api/v1/orgs/:orgId/projects/:projectId/services/:serviceId/db/schema", () =>
    HttpResponse.json([
      {
        name: "users",
        columns: [
          { name: "id", data_type: "uuid", nullable: false },
          { name: "email", data_type: "varchar", nullable: false },
          { name: "created_at", data_type: "timestamptz", nullable: false },
        ],
      },
    ])
  ),
]
