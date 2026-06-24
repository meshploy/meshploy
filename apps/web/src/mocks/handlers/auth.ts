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
]
