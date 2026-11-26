import { http, HttpResponse } from "msw"
import { demoAt } from "../data"

export const clusterHandlers = [
  http.get("/api/v1/orgs/:orgId/cluster/join-token", () =>
    HttpResponse.json({
      token: "K10demo::server:demo0000000000000000000000000000000000000000000000",
      server_url: "https://100.64.0.1:6443",
    })
  ),

  http.get("/api/v1/orgs/:orgId/cluster/headscale-preauth-key", () =>
    HttpResponse.json({
      has_active_key: true,
      key: "demo-headscale-preauth-key-0000000000000",
      headscale_url: "https://hs.demo.meshploy.app",
    })
  ),

  http.post("/api/v1/orgs/:orgId/cluster/headscale-preauth-key", () =>
    HttpResponse.json({
      key: "demo-headscale-preauth-key-new-0000000000",
      reusable: true,
      expiration: demoAt(30 * 86_400_000),
      headscale_url: "https://hs.demo.meshploy.app",
    })
  ),
]
