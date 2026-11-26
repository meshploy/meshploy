import { http, HttpResponse } from "msw"
import { demoRoute, demoStack } from "../data"
import { db, record } from "../state"
import { applyStack } from "./workspace"

// Demo catalog - mirrors the shape the real API returns (manifest + compose).
// Kept in-file since it's demo-only mock data specific to templates.
const demoTemplates = [
  {
    manifest: {
      id: "pgadmin",
      name: "pgAdmin 4",
      description: "Web-based administration and management tool for PostgreSQL.",
      category: "database",
      version: "1.0.0",
      icon: "logo.svg",
      links: { website: "https://www.pgadmin.org", source: "https://github.com/pgadmin-org/pgadmin4" },
      maintainers: ["pritthishnath"],
      variables: [
        { key: "PGADMIN_EMAIL", prompt: "Login email", required: true },
        { key: "PGADMIN_PASSWORD", generate: "password" },
        { key: "PRIMARY_DOMAIN", generate: "subdomain", expose: { service: "pgadmin", port: 5050 } },
      ],
    },
    compose: `services:
  pgadmin:
    image: dpage/pgadmin4:8.14
    environment:
      PGADMIN_DEFAULT_EMAIL: \${PGADMIN_EMAIL}
      PGADMIN_DEFAULT_PASSWORD: \${PGADMIN_PASSWORD}
      PGADMIN_LISTEN_PORT: "5050"
    volumes:
      - pgadmin-data:/var/lib/pgadmin
    x-meshploy:
      type: application
      deploy:
        port: 5050
        replicas: 1

volumes:
  pgadmin-data:
`,
  },
  {
    manifest: {
      id: "plausible",
      name: "Plausible Analytics",
      description: "Lightweight, privacy-friendly web analytics.",
      category: "analytics",
      version: "1.0.0",
      icon: "logo.svg",
      links: { website: "https://plausible.io", source: "https://github.com/plausible/analytics" },
      maintainers: ["pritthishnath"],
      variables: [
        { key: "SECRET_KEY_BASE", generate: "secret64" },
        { key: "ADMIN_EMAIL", prompt: "Admin email", required: true },
        { key: "ADMIN_PASSWORD", generate: "password" },
        { key: "PRIMARY_DOMAIN", generate: "subdomain", expose: { service: "plausible", port: 8000 } },
      ],
    },
    compose: `services:
  plausible:
    image: plausible/analytics:v2.1.0
    environment:
      SECRET_KEY_BASE: \${SECRET_KEY_BASE}
      ADMIN_USER_EMAIL: \${ADMIN_EMAIL}
      ADMIN_USER_PWD: \${ADMIN_PASSWORD}
      BASE_URL: https://\${PRIMARY_DOMAIN}
    x-meshploy:
      type: application
      deploy:
        port: 8000
        replicas: 1
  db:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: \${DB_PASSWORD}
    x-meshploy:
      type: database
      database: { engine: postgres, version: "16", storage_gb: 5 }
`,
  },
  {
    manifest: {
      id: "n8n",
      name: "n8n",
      description: "Workflow automation tool with a fair-code license.",
      category: "application",
      version: "1.0.0",
      icon: "logo.svg",
      links: { website: "https://n8n.io", source: "https://github.com/n8n-io/n8n" },
      maintainers: ["pritthishnath"],
      variables: [
        { key: "N8N_ENCRYPTION_KEY", generate: "hex32" },
        { key: "PRIMARY_DOMAIN", generate: "subdomain", expose: { service: "n8n", port: 5678 } },
      ],
    },
    compose: `services:
  n8n:
    image: n8nio/n8n:1.68.0
    environment:
      N8N_ENCRYPTION_KEY: \${N8N_ENCRYPTION_KEY}
      N8N_HOST: \${PRIMARY_DOMAIN}
      WEBHOOK_URL: https://\${PRIMARY_DOMAIN}/
    volumes:
      - n8n-data:/home/node/.n8n
    x-meshploy:
      type: application
      deploy:
        port: 5678
        replicas: 1

volumes:
  n8n-data:
`,
  },
]

// A tiny generic icon so <img src> (gallery, later) doesn't 404 in the demo.
const demoIcon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="#326690"/></svg>`

// The demo shows the real catalog, read from its public repository as the API
// reads it, and falls back to the three above when that cannot be reached.
const catalogBase = "https://raw.githubusercontent.com/meshploy/meshploy-templates/main/"
type Manifest = (typeof demoTemplates)[number]["manifest"]
let catalog: Promise<Manifest[] | null> | undefined
const remoteCatalog = () =>
  (catalog ??= fetch(catalogBase + "index.json")
    .then((r) => (r.ok ? r.json() : null))
    .then((j: { templates?: Manifest[] } | null) => (j?.templates?.length ? j.templates : null))
    .catch(() => null))

async function findTemplate(id: string): Promise<{ manifest: Manifest; compose: string } | undefined> {
  const remote = await remoteCatalog()
  const manifest = remote?.find((t) => t.id === id)
  if (manifest) {
    const r = await fetch(`${catalogBase}templates/${id}/docker-compose.yml`).catch(() => undefined)
    if (r?.ok) return { manifest, compose: await r.text() }
  }
  return demoTemplates.find((t) => t.manifest.id === id)
}

export const templatesHandlers = [
  http.get("/api/v1/templates", async () =>
    HttpResponse.json((await remoteCatalog()) ?? demoTemplates.map((t) => t.manifest))
  ),

  http.get("/api/v1/templates/:templateId/icon", async ({ params }) => {
    const remote = await remoteCatalog()
    const m = remote?.find((t) => t.id === params.templateId)
    if (m) {
      const file = m.icon || "logo.svg"
      const r = await fetch(`${catalogBase}templates/${m.id}/${file}`).catch(() => undefined)
      // raw.githubusercontent answers text/plain, so the type follows the name.
      const type = file.endsWith(".png") ? "image/png" : file.endsWith(".webp") ? "image/webp" : "image/svg+xml"
      if (r?.ok) return new HttpResponse(await r.arrayBuffer(), { headers: { "Content-Type": type } })
    }
    return new HttpResponse(demoIcon, { headers: { "Content-Type": "image/svg+xml" } })
  }),

  http.get("/api/v1/templates/:templateId", async ({ params }) => {
    const tpl = await findTemplate(String(params.templateId))
    if (!tpl) return new HttpResponse("template not found", { status: 404 })
    return HttpResponse.json({ manifest: tpl.manifest, compose: tpl.compose })
  }),

  http.post(
    "/api/v1/orgs/:orgId/projects/:projectId/templates/:templateId/deploy",
    async ({ params, request }) => {
      const body = (await request.json()) as { spec?: string; prompt_values?: Record<string, string> }
      const tpl = await findTemplate(String(params.templateId))
      if (!tpl) return new HttpResponse("template not found", { status: 404 })
      const stack = {
          ...demoStack,
          project_id: String(params.projectId),
          id: crypto.randomUUID(),
          name: tpl.manifest.id,
          spec: body.spec || tpl.compose,
          template_id: tpl.manifest.id,
          template_version: tpl.manifest.version,
        }
      db.stacks.push(stack)
      try { applyStack(stack) } catch (e) { return HttpResponse.json({ detail: (e as Error).message }, { status: 400 }) }
      // A variable that exposes a service becomes the stack's route, on a
      // subdomain of the primary domain, as the API makes it.
      const primary = db.domains.find((d) => d.is_primary)?.base_domain ?? "demo.example.com"
      for (const v of (tpl.manifest.variables ?? []) as { key: string; expose?: { service: string; port: number } }[]) {
        if (!v.expose) continue
        const svc = db.services.find((x) => x.stack_id === stack.id && x.name === v.expose!.service)
        if (!svc) continue
        const sub = body.prompt_values?.[v.key]?.split(".")[0] || `${tpl.manifest.id}-${Math.random().toString(36).slice(2, 6)}`
        const routeId = crypto.randomUUID()
        db.routes.push(record({
          ...demoRoute, id: routeId, project_id: String(params.projectId), stack_id: stack.id,
          subdomain: sub, hostname: `${sub}.${primary}`, published: true,
          targets: [{ id: crypto.randomUUID(), route_id: routeId, service_id: svc.id, target_port: v.expose.port, weight: 100,
            path: "/", strip_path: false, node_id: null, target_ip: "", redirect_route_id: null, redirect_code: 301 }],
        }))
      }
      return HttpResponse.json(stack, { status: 201 })
    }
  ),
]
