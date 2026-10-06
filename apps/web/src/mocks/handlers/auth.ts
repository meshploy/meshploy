import { http, HttpResponse } from "msw"
import { DEMO_TOKEN, demoUser } from "../data"

const demoConnections: { id: string; client_name: string; approved_by: string; approved_by_name: string; created_at: string; last_used_at?: string; revoked_at?: string }[] = [
  { id: "00000000-0000-0000-0000-0000000000c1", client_name: "Claude", approved_by: demoUser.id, approved_by_name: demoUser.username,
    created_at: new Date(Date.now() - 3 * 86400_000).toISOString(), last_used_at: new Date(Date.now() - 2 * 3600_000).toISOString() },
]

const demoBrowsers = [
  { id: "00000000-0000-0000-0000-0000000000b1", user_agent: "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0", ip: "203.0.113.24",
    created_at: new Date(Date.now() - 2 * 3600_000).toISOString(), last_seen_at: new Date().toISOString() },
  { id: "00000000-0000-0000-0000-0000000000b2", user_agent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1", ip: "198.51.100.9",
    created_at: new Date(Date.now() - 20 * 3600_000).toISOString(), last_seen_at: new Date(Date.now() - 5 * 3600_000).toISOString() },
]

export const authHandlers = [
  http.get("/api/v1/auth/status", () =>
    HttpResponse.json({ registration_open: false })
  ),

  http.post("/api/v1/auth/login", () =>
    HttpResponse.json({ token: DEMO_TOKEN, totp_required: false })
  ),

  http.get("/api/v1/me", () => HttpResponse.json(demoUser)),

  // This browser and a phone, signed in to the console as the demo user.
  http.post("/api/v1/auth/logout", () => new HttpResponse(null, { status: 204 })),
  http.get("/api/v1/me/sessions", () => HttpResponse.json(demoBrowsers.map((b, i) => ({ ...b, current: i === 0 })))),
  http.delete("/api/v1/me/sessions/:id", () => new HttpResponse(null, { status: 204 })),
  http.delete("/api/v1/me/sessions", () => HttpResponse.json({ ended: demoBrowsers.length - 1 })),
  http.get("/api/v1/orgs/:orgId/console-sessions", () =>
    HttpResponse.json(demoBrowsers.map((b) => ({ ...b, user_id: demoUser.id, user_name: demoUser.username, elsewhere: false })))
  ),
  http.delete("/api/v1/orgs/:orgId/console-sessions/:id", () => new HttpResponse(null, { status: 204 })),

  http.patch("/api/v1/me", () => HttpResponse.json(demoUser)),

  http.patch("/api/v1/me/password", () => new HttpResponse(null, { status: 204 })),

  http.post("/api/v1/me/totp/setup", () =>
    HttpResponse.json({ otp_url: "otpauth://totp/demo?secret=DEMO", secret: "DEMO" })
  ),

  http.post("/api/v1/me/totp/enable", () =>
    HttpResponse.json({ recovery_codes: ["aaaaa-bbbbb", "ccccc-ddddd"] })
  ),

  // A CLI login waiting on any code, so the approval page can be seen.
  http.get("/api/v1/cli/logins/:code", ({ params }) =>
    HttpResponse.json({
      user_code: String(params.code).toUpperCase(),
      host: "asha-laptop",
      expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
    })
  ),
  http.post("/api/v1/cli/logins/:code/approve", () => new HttpResponse(null, { status: 204 })),
  http.post("/api/v1/cli/logins/:code/deny", () => new HttpResponse(null, { status: 204 })),
  http.get("/api/v1/me/cli-sessions", () =>
    HttpResponse.json([
      { id: "c0000000-0000-0000-0000-000000000001", host: "asha-laptop", token_prefix: "mcli-3f9a1c2", created_at: new Date(Date.now() - 3 * 86_400_000).toISOString(), last_used_at: new Date(Date.now() - 20 * 60_000).toISOString(), current: false },
      { id: "c0000000-0000-0000-0000-000000000002", host: "gw-1", token_prefix: "mcli-81be0d4", created_at: new Date(Date.now() - 12 * 86_400_000).toISOString(), last_used_at: new Date(Date.now() - 2 * 86_400_000).toISOString(), current: false },
    ])
  ),
  http.delete("/api/v1/me/cli-sessions/:id", () => new HttpResponse(null, { status: 204 })),

  // AI assistants connected through OAuth: one Claude connection as the demo
  // user, which can be disconnected for the session.
  http.get("/api/v1/orgs/:orgId/oauth/connections", ({ request }) => {
    const q = new URL(request.url).searchParams
    if (q.get("agent_id")) return HttpResponse.json([])
    return HttpResponse.json(demoConnections)
  }),
  http.get("/api/v1/orgs/:orgId/session-counts", () =>
    HttpResponse.json({ [demoUser.id]: demoConnections.filter((c) => !c.revoked_at).length + 1 + demoBrowsers.length })
  ),
  // The demo user's one CLI, as an admin sees members' CLIs.
  http.get("/api/v1/orgs/:orgId/cli-sessions", () =>
    HttpResponse.json([{ id: "00000000-0000-0000-0000-0000000000c2", user_id: demoUser.id, user_name: demoUser.username,
      host: "demo-laptop", elsewhere: false, created_at: new Date(Date.now() - 5 * 86400_000).toISOString(), last_used_at: new Date(Date.now() - 3600_000).toISOString() }])
  ),
  http.delete("/api/v1/orgs/:orgId/oauth/connections/:id", ({ params }) => {
    const c = demoConnections.find((x) => x.id === params.id)
    if (c) c.revoked_at = new Date().toISOString()
    return new HttpResponse(null, { status: 204 })
  }),
  http.delete("/api/v1/orgs/:orgId/cli-sessions/:id", () => new HttpResponse(null, { status: 204 })),
  http.get("/api/v1/oauth/authorize", () => HttpResponse.json({ client_name: "Claude", redirect_host: "claude.ai" })),
  http.post("/api/v1/oauth/authorize", () => HttpResponse.json({ redirect: "/" })),
]
