import { http, HttpResponse } from "msw"
import { db, buildConfigs } from "../state"
import { demoProject } from "../data"

// The Domains page offline. Registered ahead of the generic CRUD so the rules
// the real API enforces - a primary cannot be removed, a domain holding routes
// cannot be removed, a new domain arrives unverified with a TXT to publish -
// behave the same here, and the page can be judged on what it would really do.

const O = "/api/v1/orgs/:orgId"
const now = () => new Date().toISOString()
const error = (detail: string, status = 422) => HttpResponse.json({ detail }, { status })

// Nodes whose control connection goes through a domain's headscale name. One
// that recorded none joined through the install's domain, as on a real gateway.
function nodesOn(d: any) {
  if (!d.is_primary && !d.former_primary) return []
  const want = `https://headscale.${d.base_domain}`
  return db.nodes
    .filter((n: any) => n.k3s_role !== "server" && (n.control_url || "https://headscale.demo.example.com") === want)
    .map((n: any) => ({ id: n.id, name: n.name, tailscale_ip: n.tailscale_ip, status: n.status, control_url: want }))
}

function deployHooksOn(d: any) {
  if (!d.is_primary && !d.former_primary) return []
  const hosts = [`api.${d.base_domain}`, `console.${d.base_domain}`]
  return Object.entries(buildConfigs)
    .filter(([, c]: [string, any]) => hosts.includes(c.deploy_hook_host))
    .map(([serviceId, c]: [string, any]) => {
      const svc: any = db.services.find((x: any) => x.id === serviceId)
      return {
        service_id: serviceId,
        service_name: svc?.name ?? "service",
        project_id: svc?.project_id ?? demoProject.id,
        project_name: demoProject.name,
        host: c.deploy_hook_host,
        called_at: c.deploy_hook_called_at,
      }
    })
}

function primaryBase(sub: string): string {
  const p: any = db.domains.find((x: any) => x.is_primary)
  return `https://${sub}.${p.base_domain}`
}

function autoDeployRepos(integrationId: string): string[] {
  return [...new Set(Object.values(buildConfigs)
    .filter((c: any) => c.git_integration_id === integrationId && c.auto_deploy && c.git_repo)
    .map((c: any) => c.git_repo as string))].sort()
}

function registrationsOn(d: any) {
  if (!d.is_primary && !d.former_primary) return []
  const api = `api.${d.base_domain}`
  const out: any[] = []
  for (const g of db["git-integrations"] as any[]) {
    const registered = g.registered_api_base || "https://api.demo.example.com"
    const onIt = new URL(registered).hostname === api
    const ref = { integration_id: g.id, name: g.name, provider: g.provider }
    if (g.provider === "github" && g.auth_method === "app" && onIt) {
      out.push({
        ...ref, kind: "github_app", automatic: false,
        urls: [
          ["Webhook URL", `/api/v1/webhooks/github/${g.id}`],
          ["Callback URL", "/api/v1/github/callback"],
          ["Setup URL", "/api/v1/github/callback"],
        ].map(([label, path]) => ({ label, current: registered + path, new: primaryBase("api") + path })),
      })
    }
    if (g.provider !== "github" && onIt) {
      const repos = autoDeployRepos(g.id)
      if (repos.length > 0) {
        const path = `/api/v1/webhooks/git/${g.provider}/${g.id}`
        out.push({
          ...ref, kind: "push_hooks", automatic: true, repos,
          urls: [{ label: "Push webhook", current: registered + path, new: primaryBase("api") + path }],
        })
      }
    }
  }
  return out
}

function redirectsTo(r: any): string | undefined {
  const targets = r.targets ?? []
  if (targets.length !== 1 || !targets[0].redirect_route_id) return undefined
  return db.routes.find((x: any) => x.id === targets[0].redirect_route_id)?.hostname
}

function hostnameOn(r: any, d: any): string {
  if (r.zone === "internal") return `${r.subdomain}.${d.internal_subdomain}.${d.base_domain}`
  if (r.zone === "preview") return `${r.subdomain}.${d.preview_subdomain}.${d.base_domain}`
  return `${r.subdomain}.${d.base_domain}`
}

function domainRoutes(filter: (r: any) => boolean) {
  return db.routes
    .filter(filter)
    .map((r: any) => ({
      id: r.id,
      hostname: r.hostname,
      subdomain: r.subdomain,
      redirects_to: redirectsTo(r),
      zone: r.zone,
      published: r.published !== false,
      project_id: r.project_id,
      project_name: demoProject.name,
      created_at: r.created_at,
      verified: !!r.custom_domain_verified,
    }))
    .sort((a, b) => a.hostname.localeCompare(b.hostname))
}

export const domainsHandlers = [
  http.get(`${O}/domains`, () =>
    HttpResponse.json(
      [...db.domains].sort((a: any, b: any) => Number(!!b.is_primary) - Number(!!a.is_primary))
    )
  ),

  http.post(`${O}/domains`, async ({ request, params }) => {
    const input = (await request.json()) as { base_domain?: string; dns_mode?: string }
    const name = (input.base_domain ?? "").trim().toLowerCase().replace(/\.$/, "")
    if (!/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$/.test(name))
      return error(`"${input.base_domain}" is not a domain name - give a name with a dot in it, no scheme, port or wildcard`)
    if (db.domains.some((d: any) => d.base_domain === name)) return error(`${name} is already registered`, 409)
    const row = {
      id: crypto.randomUUID(),
      organization_id: params.orgId,
      base_domain: name,
      internal_subdomain: "internal",
      preview_subdomain: "preview",
      verified: false,
      is_primary: false,
      dns_mode: input.dns_mode || "delegation",
      verify_token: Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) => b.toString(16).padStart(2, "0")).join(""),
      created_at: now(),
      updated_at: now(),
    }
    db.domains.push(row as any)
    return HttpResponse.json(row)
  }),

  // Verification cannot look anything up offline, so it succeeds on the second
  // try: the first shows what "not propagated yet" looks like.
  http.post(`${O}/domains/:domainId/verify`, ({ params }) => {
    const d: any = db.domains.find((x: any) => x.id === params.domainId)
    if (!d) return error("domain not found", 404)
    if (!d.verified) {
      if (!d._checked) {
        d._checked = true
        return error("TXT record not found or not yet propagated")
      }
      d.verified = true
    }
    return HttpResponse.json(d)
  }),

  http.patch(`${O}/domains/:domainId/dns-mode`, async ({ request, params }) => {
    const d: any = db.domains.find((x: any) => x.id === params.domainId)
    if (!d) return error("domain not found", 404)
    const input = (await request.json()) as { dns_mode: string }
    d.dns_mode = input.dns_mode
    d.updated_at = now()
    return HttpResponse.json(d)
  }),

  // Same shape as the domain check: the first attempt fails the way a real one
  // does before DNS propagates, the second succeeds.
  http.post(`${O}/projects/:projectId/routes/:routeId/verify-hostname`, ({ params }) => {
    const r: any = db.routes.find((x: any) => x.id === params.routeId)
    if (!r) return error("route not found", 404)
    if (r.domain_id) return error("route uses a managed domain — no custom-domain verification needed", 400)
    if (!r.custom_domain_verified) {
      if (!r._checked) {
        r._checked = true
        return error("TXT record not found or not yet propagated")
      }
      r.custom_domain_verified = true
    }
    return HttpResponse.json(r)
  }),

  // Moving the primary: the old one keeps serving the platform's names.
  http.post(`${O}/domains/:domainId/make-primary`, ({ params }) => {
    const d: any = db.domains.find((x: any) => x.id === params.domainId)
    if (!d) return error("domain not found", 404)
    if (d.is_primary) return HttpResponse.json(d)
    if (!d.verified)
      return error("verify this domain first - nothing is served on it until it is, so the console would not be reachable there")
    if (d.retiring_at) return error("this domain is being retired - stop retiring it before making it primary")
    for (const x of db.domains as any[]) {
      if (x.is_primary) {
        x.is_primary = false
        x.former_primary = true
      }
    }
    d.is_primary = true
    d.former_primary = false
    return HttpResponse.json(d)
  }),

  http.get(`${O}/domains/:domainId/nodes`, ({ params }) => {
    const d: any = db.domains.find((x: any) => x.id === params.domainId)
    if (!d) return error("domain not found", 404)
    return HttpResponse.json(nodesOn(d))
  }),

  http.post(`${O}/nodes/:nodeId/control-moved`, ({ params }) => {
    const n: any = db.nodes.find((x: any) => x.id === params.nodeId)
    if (!n) return error("node not found", 404)
    const p: any = db.domains.find((x: any) => x.is_primary)
    n.control_url = `https://headscale.${p.base_domain}`
    return HttpResponse.json(n)
  }),

  // Git provider registrations on a domain, with the API's rules: an
  // integration made before the address was recorded registered the install's.
  http.get(`${O}/domains/:domainId/integrations`, ({ params }) => {
    const d: any = db.domains.find((x: any) => x.id === params.domainId)
    if (!d) return error("domain not found", 404)
    return HttpResponse.json(registrationsOn(d))
  }),

  http.post(`${O}/git-integrations/:id/move-hooks`, ({ params }) => {
    const g: any = (db["git-integrations"] as any[]).find((x) => x.id === params.id)
    if (!g) return error("git integration not found", 404)
    g.registered_api_base = primaryBase("api")
    return HttpResponse.json({ moved: autoDeployRepos(g.id), failed: [] })
  }),

  http.post(`${O}/git-integrations/:id/registration-updated`, async ({ request, params }) => {
    const g: any = (db["git-integrations"] as any[]).find((x) => x.id === params.id)
    if (!g) return error("git integration not found", 404)
    const input = (await request.json()) as { kind: string }
    if (input.kind === "oauth_redirect") {
      const u = new URL(g.oauth_redirect_uri)
      g.oauth_redirect_uri = primaryBase(u.hostname.startsWith("console.") ? "console" : "api") + u.pathname
    } else {
      g.registered_api_base = primaryBase("api")
    }
    return new HttpResponse(null, { status: 204 })
  }),

  http.get(`${O}/domains/:domainId/deploy-hooks`, ({ params }) => {
    const d: any = db.domains.find((x: any) => x.id === params.domainId)
    if (!d) return error("domain not found", 404)
    return HttpResponse.json(deployHooksOn(d))
  }),

  http.delete(`${O}/services/:serviceId/deploy-hook-call`, ({ params }) => {
    const c: any = buildConfigs[String(params.serviceId)]
    if (!c) return error("service not found", 404)
    c.deploy_hook_host = ""
    c.deploy_hook_called_at = null
    return new HttpResponse(null, { status: 204 })
  }),

  // Retiring: the same rules as the API, so the page behaves as it would.
  http.post(`${O}/domains/:domainId/retire`, ({ params }) => {
    const d: any = db.domains.find((x: any) => x.id === params.domainId)
    if (!d) return error("domain not found", 404)
    if (d.is_primary)
      return error("the primary domain cannot be retired - the console, the API and Headscale are served on it. Make another domain primary first")
    if (!d.verified) return error("this domain was never verified, so nothing was ever served on it - remove it instead")
    d.retiring_at ??= now()
    return HttpResponse.json(d)
  }),

  http.delete(`${O}/domains/:domainId/retire`, ({ params }) => {
    const d: any = db.domains.find((x: any) => x.id === params.domainId)
    if (!d) return error("domain not found", 404)
    d.retiring_at = null
    return HttpResponse.json(d)
  }),

  // Ahead of the generic route create, which would otherwise accept a route on
  // a retiring domain. Returning nothing passes the request on to it.
  http.post(`${O}/projects/:projectId/routes`, async ({ request }) => {
    const input = (await request.clone().json()) as { domain_id?: string }
    const d: any = input.domain_id && db.domains.find((x: any) => x.id === input.domain_id)
    if (d?.retiring_at) return error(`${d.base_domain} is being retired, so no new routes can use it`)
    return undefined
  }),

  http.post(`${O}/projects/:projectId/routes/:routeId/move`, async ({ request, params }) => {
    const r: any = db.routes.find((x: any) => x.id === params.routeId)
    if (!r) return error("route not found", 404)
    const input = (await request.json()) as { domain_id: string; keep_redirect?: boolean }
    if (!r.domain_id)
      return error("a custom hostname is a whole name, not a subdomain - it cannot move to another base domain")
    if (r.domain_id === input.domain_id) return error("the route is already on that domain")
    const to: any = db.domains.find((x: any) => x.id === input.domain_id)
    if (!to) return error("domain not found", 404)
    if (!to.verified) return error(`${to.base_domain} is not verified yet`)
    if (to.retiring_at) return error(`${to.base_domain} is being retired itself, so no routes can move onto it`)
    if (input.keep_redirect && r.zone !== "public") return error("only a public route can leave a redirect behind")
    const newHost = hostnameOn(r, to)
    if (db.routes.some((x: any) => x.hostname === newHost)) return error(`${newHost} is already routed`, 409)

    const oldHost = r.hostname
    const oldDomain = r.domain_id
    r.domain_id = to.id
    r.hostname = newHost
    let redirect: any
    if (input.keep_redirect) {
      redirect = {
        ...r,
        id: crypto.randomUUID(),
        domain_id: oldDomain,
        hostname: oldHost,
        published: true,
        created_at: now(),
        updated_at: now(),
      }
      redirect.targets = [{ id: crypto.randomUUID(), route_id: redirect.id, path: "/", redirect_route_id: r.id, redirect_code: 301 }]
      db.routes.push(redirect)
    }
    return HttpResponse.json({ route: r, redirect })
  }),

  http.get(`${O}/domains/:domainId/routes`, ({ params }) =>
    HttpResponse.json(domainRoutes((r) => r.domain_id === params.domainId))
  ),

  http.get(`${O}/custom-domains`, () =>
    HttpResponse.json(domainRoutes((r) => !r.domain_id && !!r.hostname))
  ),

  http.delete(`${O}/domains/:domainId`, ({ params }) => {
    const i = db.domains.findIndex((x: any) => x.id === params.domainId)
    if (i < 0) return error("domain not found", 404)
    const d: any = db.domains[i]
    if (d.is_primary)
      return error("this is the primary domain - the console, the API and Headscale are served on it. Make another domain primary first")
    if (d.verified && !d.retiring_at)
      return error("start retiring this domain first - that stops new routes attaching, and shows what still holds it")
    const held = db.routes.filter((r: any) => r.domain_id === d.id).length
    if (held > 0) return error(`${held} route(s) still use this domain - move or remove them first`)
    const ci = deployHooksOn(d).length
    if (ci > 0)
      return error(`${ci} CI deploy webhook(s) last called through ${d.base_domain} - update the URL in the CI job, or its deploys fail when this domain goes`)
    const regs = registrationsOn(d).length
    if (regs > 0)
      return error(`${regs} git integration registration(s) still point at ${d.base_domain} - pushes and sign-ins through them would stop when it goes`)
    const nodes = nodesOn(d).length
    if (nodes > 0)
      return error(`${nodes} node(s) still reach the mesh through headscale.${d.base_domain} - move them to the primary's headscale first, or they drop off the mesh when this domain goes`)
    db.domains.splice(i, 1)
    return new HttpResponse(null, { status: 204 })
  }),
]
