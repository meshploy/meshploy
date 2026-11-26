import { http, HttpResponse } from "msw"
import { demoAt } from "../data"

// The demo server is linked to Meshploy Cloud, and the demo person's account
// too: agent apps they connected there have been working here as them.
const MIN = 60_000
const agentCalls = [
  { app: "Claude Code", method: "POST", path: "/api/v1/orgs/demo/projects/demo/services/api/deploy", status: 202, ago: 12 * MIN },
  { app: "Claude Code", method: "GET", path: "/api/v1/orgs/demo/projects/demo/services/api/logs", status: 200, ago: 14 * MIN },
  { app: "Cursor", method: "GET", path: "/api/v1/orgs/demo/projects/demo/routes", status: 200, ago: 3 * 60 * MIN },
  { app: "Cursor", method: "DELETE", path: "/api/v1/orgs/demo/nodes/worker-1", status: 403, refused: "not allowed to remove nodes", ago: 3 * 60 * MIN + 2 * MIN },
  { app: "Claude Code", method: "GET", path: "/api/v1/orgs/demo/projects", status: 200, ago: 2 * 24 * 60 * MIN },
].map((c, i) => ({ id: `call-${i}`, at: demoAt(-c.ago), app: c.app, method: c.method, path: c.path, status: c.status, refused: c.refused }))

export const systemHandlers = [
  http.get("/api/v1/me/meshploy-account", () =>
    HttpResponse.json({
      server_linked: true,
      available: true,
      linked: true,
      email: "demo@meshploy.com",
      linked_at: demoAt(-6 * 24 * 60 * MIN),
      app_url: "https://app.meshploy.com",
    })
  ),
  http.get("/api/v1/me/meshploy-account/calls", () => HttpResponse.json({ calls: agentCalls })),
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
