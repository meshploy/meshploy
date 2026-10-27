import { http, HttpResponse } from "msw"
import { demoOrg, demoOrgMember, demoOrgMemberRavi, DEMO_ORG_ID } from "../data"
import { db } from "../state"

export const orgsHandlers = [
  http.get("/api/v1/orgs", () => HttpResponse.json([demoOrg])),

  http.get(`/api/v1/orgs/:orgId`, () => HttpResponse.json(demoOrg)),

  http.patch(`/api/v1/orgs/:orgId`, () => HttpResponse.json(demoOrg)),

  http.get(`/api/v1/orgs/:orgId/sandbox`, () => {
    const projects = db.projects
    const byId = new Map(projects.map((p) => [p.id, p]))
    return HttpResponse.json({
      nodes: db.nodes
        .filter((n) => n.k3s_labels?.["meshploy.com/sandbox"] === "true")
        .map((n) => n.name),
      projects: projects.filter((p) => p.sandboxed).map((p) => {
        const parent = p.parent_project_id ? byId.get(p.parent_project_id) : undefined
        return {
          id: p.id,
          project_id: parent?.id ?? p.id,
          name: parent?.name ?? p.name,
          level: parent ? p.env_name : undefined,
          required: false,
        }
      }),
    })
  }),

  http.get(`/api/v1/orgs/:orgId/projects/:projectId/sandbox`, ({ params }) => {
    const project = db.projects.find((p) => p.id === params.projectId)
    return HttpResponse.json({ on: !!project?.sandboxed, required: false })
  }),

  http.put(`/api/v1/orgs/:orgId/projects/:projectId/sandbox`, async ({ params, request }) => {
    const { on } = await request.json() as { on: boolean }
    const project = db.projects.find((p) => p.id === params.projectId)
    if (project) project.sandboxed = on
    return HttpResponse.json({ on, required: false })
  }),

  http.get(`/api/v1/orgs/:orgId/members`, () =>
    HttpResponse.json([demoOrgMember, demoOrgMemberRavi])
  ),

  http.post(`/api/v1/orgs/:orgId/members`, () =>
    HttpResponse.json(demoOrgMember)
  ),

  http.patch(`/api/v1/orgs/:orgId/members/:userId`, () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.delete(`/api/v1/orgs/:orgId/members/:userId`, () =>
    new HttpResponse(null, { status: 204 })
  ),

  http.get(`/api/v1/orgs/:orgId/invitations`, () => HttpResponse.json([])),

  http.post(`/api/v1/orgs/:orgId/invitations`, () =>
    HttpResponse.json({
      id: "00000000-0000-0000-0000-000000000030",
      org_id: DEMO_ORG_ID,
      email: "invited@example.com",
      role: "member",
      expires_at: "2026-07-24T10:00:00Z",
    })
  ),
]
