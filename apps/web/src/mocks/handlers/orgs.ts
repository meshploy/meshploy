import { http, HttpResponse } from "msw"
import { demoOrg, demoOrgMember, DEMO_ORG_ID } from "../data"

export const orgsHandlers = [
  http.get("/api/v1/orgs", () => HttpResponse.json([demoOrg])),

  http.get(`/api/v1/orgs/:orgId`, () => HttpResponse.json(demoOrg)),

  http.patch(`/api/v1/orgs/:orgId`, () => HttpResponse.json(demoOrg)),

  http.get(`/api/v1/orgs/:orgId/members`, () =>
    HttpResponse.json([demoOrgMember])
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
