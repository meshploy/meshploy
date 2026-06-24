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

  http.get("/api/v1/health", () => HttpResponse.json({ status: "ok" })),

  // Serve install/uninstall scripts as text
  http.get("/api/v1/system/install-script", () =>
    new HttpResponse("#!/bin/bash\necho demo", { headers: { "Content-Type": "text/plain" } })
  ),
]
