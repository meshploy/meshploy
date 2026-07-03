import { http, HttpResponse } from "msw"
import { demoStack } from "../data"

// Demo catalog — mirrors the shape the real API returns (manifest + compose).
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

export const templatesHandlers = [
  http.get("/api/v1/templates", () =>
    HttpResponse.json(demoTemplates.map((t) => t.manifest))
  ),

  http.get("/api/v1/templates/:templateId/icon", () =>
    new HttpResponse(demoIcon, { headers: { "Content-Type": "image/svg+xml" } })
  ),

  http.get("/api/v1/templates/:templateId", ({ params }) => {
    const tpl = demoTemplates.find((t) => t.manifest.id === params.templateId)
    if (!tpl) return new HttpResponse("template not found", { status: 404 })
    return HttpResponse.json({ manifest: tpl.manifest, compose: tpl.compose })
  }),

  http.post(
    "/api/v1/orgs/:orgId/projects/:projectId/templates/:templateId/deploy",
    async ({ params, request }) => {
      const body = (await request.json()) as { spec?: string; prompt_values?: Record<string, string> }
      const tpl = demoTemplates.find((t) => t.manifest.id === params.templateId)
      if (!tpl) return new HttpResponse("template not found", { status: 404 })
      return HttpResponse.json(
        {
          ...demoStack,
          id: crypto.randomUUID(),
          name: tpl.manifest.id,
          spec: body.spec || tpl.compose,
          template_id: tpl.manifest.id,
          template_version: tpl.manifest.version,
        },
        { status: 201 }
      )
    }
  ),
]
