import { http, HttpResponse } from "msw"
import { DEMO_TOKEN, demoUser } from "../data"

export const authHandlers = [
  http.get("/api/v1/auth/status", () =>
    HttpResponse.json({ registration_open: false })
  ),

  http.post("/api/v1/auth/login", () =>
    HttpResponse.json({ token: DEMO_TOKEN, totp_required: false })
  ),

  http.get("/api/v1/me", () => HttpResponse.json(demoUser)),

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
]
