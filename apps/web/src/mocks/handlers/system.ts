import { http, HttpResponse } from "msw"

export const systemHandlers = [
  http.get("/api/v1/system/version", () =>
    HttpResponse.json({
      current: "0.5.0",
      latest: "0.5.0",
      update_available: false,
      release_url: "https://github.com/meshploy/meshploy/releases/tag/v0.5.0",
    })
  ),

  // The demo has no server to upgrade, so the dialog shows its read-only state.
  http.get("/api/v1/system/upgrade", () =>
    HttpResponse.json({
      enabled: true,
      can_upgrade: false,
      pending: false,
      id: "",
      state: "",
      step: "",
      channel: "stable",
      cli_from: "",
      cli_to: "",
      started_at: "",
      finished_at: "",
      error: "",
      log_tail: [],
    })
  ),
  http.post("/api/v1/system/upgrade", () =>
    HttpResponse.json({ detail: "The demo has no server to upgrade." }, { status: 409 })
  ),

  http.get("/api/v1/health", () => HttpResponse.json({ status: "ok" })),

  // Serve install/uninstall scripts as text
  http.get("/api/v1/system/install-script", () =>
    new HttpResponse("#!/bin/bash\necho demo", { headers: { "Content-Type": "text/plain" } })
  ),
]
