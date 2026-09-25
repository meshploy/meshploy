import * as seed from "./data"

// Demo-only, heterogeneous API records. Never imported by the production API client.
// Values live in memory: reloading deliberately restores the sample workspace.
export type DemoRecord = { id: string; [key: string]: any }
export const now = () => new Date().toISOString()
export const record = (values: Record<string, unknown> = {}): DemoRecord => ({
  id: crypto.randomUUID(),
  created_at: now(),
  updated_at: now(),
  ...values,
})
const projectId = seed.DEMO_PROJECT_ID
export const secondProjectId = "00000000-0000-0000-0000-000000000040"
export const stagingLevelId = "00000000-0000-0000-0000-0000000000b1"
export const stagingWebId = "00000000-0000-0000-0000-0000000000b2"
export const groupId = "00000000-0000-0000-0000-000000000041"
export const fileId = "00000000-0000-0000-0000-000000000042"
// The managed databases beyond the seeded Postgres, one per engine.
const demoDatabases = (
  [
    ["mysql", "8.4", "mysql:8.4", 3306, 13306, "app", "shop"],
    ["redis", "7", "redis:7", 6379, 16379, "default", "0"],
    ["mongodb", "7", "mongo:7", 27017, 17017, "app", "catalog"],
    ["dragonfly", "1.21", "docker.dragonflydb.io/dragonflydb/dragonfly:v1.21", 6379, 16380, "default", "0"],
    ["clickhouse", "24.8", "clickhouse/clickhouse-server:24.8", 9000, 19001, "default", "events"],
  ] as const
).map(([engine, version, image, port, gatewayPort, dbUser, dbName], i) => {
  const id = `00000000-0000-0000-0000-0000000000d${i}`
  return {
    port,
    gatewayPort,
    nodePort: 31000 + i,
    service: {
      ...seed.demoServiceDb,
      id,
      name: engine,
      image,
      engine,
      version,
      db_user: dbUser,
      db_name: dbName,
      ports: [{ id: `pd${i}`, service_id: id, name: engine, port, is_http: false, is_primary: true, is_public: false, node_port: 31000 + i }],
    },
  }
})

export function firewall(state: string, over: Record<string, unknown> = {}) {
  return { state, tool: "ufw", checked_at: now(), ...over }
}

function tcpRoute(gatewayPort: number, over: Record<string, unknown>) {
  return record({
    organization_id: seed.DEMO_ORG_ID,
    project_id: projectId,
    gateway_port: gatewayPort,
    zone: "public",
    service_id: null,
    service_port: 0,
    node_id: null,
    target_ip: "100.64.0.1",
    target_port: 0,
    allowed_cidrs: [],
    status: "open",
    last_error: "",
    published: true,
    published_changed_at: null,
    published_changed_by: null,
    ...over,
  })
}

export const db: Record<string, DemoRecord[]> = {
  projects: [
    { ...seed.demoProject, env_name: "production", env_level: 0 },
    {
      ...seed.demoProject,
      id: secondProjectId,
      name: "Experiments",
      slug: "experiments",
      env_name: "production",
      env_level: 0,
    },
    // A staging level of the demo project, so the level switcher has
    // something to switch to. Levels start empty: services arrive in one by
    // being promoted into it.
    {
      ...seed.demoProject,
      id: stagingLevelId,
      slug: `${seed.demoProject.slug}-staging`,
      parent_project_id: seed.demoProject.id,
      env_name: "staging",
      env_level: 1,
    },
  ],
  services: [
    seed.demoServiceApi,
    seed.demoServiceWeb,
    // Backed up, so a level's own copy of it can be cloned in the demo.
    { ...seed.demoServiceDb, has_backup: true },
    ...demoDatabases.map((d) => d.service),
    // web, built in staging from a newer commit than production runs, so the
    // board has a promotion waiting.
    {
      ...seed.demoServiceWeb,
      id: stagingWebId,
      project_id: stagingLevelId,
      lineage_id: seed.demoServiceWeb.id,
      image: "ghcr.io/demo/web:sha-4a1b9c2",
      deployed_at: new Date(Date.now() - 40 * 60_000).toISOString(),
    },
  ],
  // The app group: web, built in staging and promoted to production.
  "promotion-groups": [
    {
      id: "00000000-0000-0000-0000-000000000051",
      project_id: seed.demoProject.id,
      name: "app",
      path: [stagingLevelId, seed.demoProject.id],
      lineages: [seed.demoServiceWeb.id],
    },
  ],
  jobs: [seed.demoJob],
  volumes: [{ ...seed.demoVolume, stack_id: null }],
  stacks: [seed.demoStack],
  routes: [
    // web's address in production and its derived one in staging, so the
    // board's cards have something to open.
    ...[
      { id: "00000000-0000-0000-0000-0000000000f1", host: "app", project: seed.demoProject.id, service: seed.demoServiceWeb.id },
      { id: "00000000-0000-0000-0000-0000000000f2", host: "app-staging", project: stagingLevelId, service: stagingWebId },
    ].map((r) => ({
      ...seed.demoRoute,
      id: r.id,
      project_id: r.project,
      subdomain: r.host,
      hostname: `${r.host}.demo.example.com`,
      domain_id: "00000000-0000-0000-0000-0000000000d1",
      stack_id: null,
      custom_domain_verified: false,
      targets: [{ id: `${r.id.slice(0, -2)}a${r.id.slice(-1)}`, route_id: r.id, service_id: r.service, target_port: 3000, weight: 100,
        path: "/", strip_path: false, node_id: null, target_ip: "100.64.0.2", redirect_route_id: null, redirect_code: 301 }],
    })),
    {
      ...seed.demoRoute,
      stack_id: null,
      custom_domain_verified: true,
      targets: seed.demoRoute.targets.map((t) => ({
        ...t,
        path: "/",
        strip_path: false,
        node_id: null,
        target_ip: "100.64.0.2",
        redirect_route_id: null,
        redirect_code: 301,
      })),
    },
    // Routes on the base domains, so a domain's page has something to list and
    // the on-demand one cannot be removed while a route holds it.
    ...[
      { host: "shop", domain: "00000000-0000-0000-0000-0000000000d1", zone: "public", published: true },
      { host: "grafana", domain: "00000000-0000-0000-0000-0000000000d1", zone: "internal", published: true },
      { host: "blog", domain: "00000000-0000-0000-0000-0000000000d2", zone: "public", published: false },
    ].map((r, i) => ({
      ...seed.demoRoute,
      id: `00000000-0000-0000-0000-0000000000e${i + 1}`,
      subdomain: r.host,
      zone: r.zone,
      domain_id: r.domain,
      published: r.published,
      hostname:
        r.domain.endsWith("d1")
          ? `${r.host}.${r.zone === "internal" ? "internal." : ""}demo.example.com`
          : `${r.host}.apps.example.org`,
      stack_id: null,
      custom_domain_verified: false,
      targets: [],
    })),
    {
      ...seed.demoRoute,
      id: "00000000-0000-0000-0000-0000000000e9",
      subdomain: "",
      hostname: "store.customer.example",
      domain_id: null,
      stack_id: null,
      custom_domain_verified: false,
      custom_domain_verify_token: "7d2e9a0c4b1f8e3a6c5d0b9f2e7a4c1d",
      targets: [],
    },
  ],
  // The demo database, published on the gateway, so a TCP route has a page.
  // One published port per managed database engine, so each engine's connect
  // command can be seen, and one to a plain port on a node, which has none.
  "tcp-routes": [
    // Each firewall verdict the host agent can give, spread across the routes.
    tcpRoute(15432, { service_id: seed.DEMO_SVC_DB, service_port: 5432, target_port: 31432, host_firewall: firewall("blocked") }),
    ...demoDatabases.map((d) =>
      tcpRoute(d.gatewayPort, {
        service_id: d.service.id,
        service_port: d.port,
        target_port: d.nodePort,
        host_firewall:
          d.service.engine === "mysql" ? firewall("restricted", { sources: ["203.0.113.0/24", "10.0.0.0/8"] })
          : d.service.engine === "mongodb" ? { state: "unknown", reason: "the host agent's last report is out of date" }
          : firewall("open"),
      })
    ),
    tcpRoute(19000, {
      node_id: seed.DEMO_NODE_W1,
      target_ip: "100.64.0.2",
      target_port: 9000,
      allowed_cidrs: ["203.0.113.0/24", "10.0.0.0/8"],
      host_firewall: firewall("blocked"),
    }),
    // A container running outside Meshploy on the gateway, published to the
    // mesh: the case an address target and a zone exist for.
    tcpRoute(3580, {
      zone: "mesh",
      target_ip: "127.0.0.1",
      target_port: 3580,
      host_firewall: {
        state: "open",
        reason: "this port is bound to the mesh address, so the host firewall does not apply to it",
      },
    }),
  ],
  "variable-groups": [
    // Postgres's connection, published for the services that read it: api
    // does, so deleting Postgres has someone to warn about.
    record({
      id: "00000000-0000-0000-0000-0000000000b5",
      project_id: projectId,
      service_id: seed.DEMO_SVC_DB,
      name: "postgres",
      description: "Connection details for postgres",
      system_managed: true,
      items: [],
    }),
    record({
      id: groupId,
      project_id: projectId,
      name: "Production environment",
      description: "Shared application configuration",
      system_managed: false,
      items: [
        record({
          group_id: groupId,
          key: "NODE_ENV",
          value: "production",
          is_secret: false,
        }),
        record({ group_id: groupId, key: "API_KEY", is_secret: true }),
      ],
    }),
  ],
  "config-files": [
    record({
      id: fileId,
      project_id: projectId,
      name: "application-config",
      path: "/etc/app/config.json",
      stack_id: null,
      size: 64,
      services: [],
      attached_services: [],
    }),
  ],
  nodes: [seed.demoNodeGateway, seed.demoNodeWorker].map((n) => ({
    ...n,
    k3s_labels: {},
    public_ip: "203.0.113.10",
    created_at: seed.DEMO_NOW,
    updated_at: seed.DEMO_NOW,
  })),
  members: [
    seed.demoOrgMember,
    record({
      user_id: "00000000-0000-0000-0000-000000000043",
      user_name: "Alex Morgan",
      user_email: "alex@example.com",
      role: "member",
    }),
  ],
  agents: [
    record({
      id: "00000000-0000-0000-0000-000000000044",
      name: "Deployment assistant",
      role: "member",
      tokens: [
        record({
          name: "CI pipeline",
          token_prefix: "mpa_demo",
          last_used_at: now(),
        }),
      ],
    }),
  ],
  invitations: [],
  "git-integrations": [
    record({
      id: seed.DEMO_GIT_GITHUB_ID,
      name: "Team GitHub",
      provider: "github",
      auth_method: "app",
      base_url: "https://github.com",
      connected: true,
      organization_id: seed.DEMO_ORG_ID,
    }),
    record({
      id: seed.DEMO_GIT_GITLAB_ID,
      name: "Demo GitLab",
      provider: "gitlab",
      auth_method: "pat",
      base_url: "https://gitlab.com",
      connected: true,
      organization_id: seed.DEMO_ORG_ID,
    }),
    record({
      id: seed.DEMO_GIT_BITBUCKET_ID,
      name: "Demo Bitbucket",
      provider: "bitbucket",
      auth_method: "pat",
      base_url: "",
      groups: "demo-workspace",
      connected: true,
      organization_id: seed.DEMO_ORG_ID,
    }),
  ],
  "registry-integrations": [
    record({
      name: "Meshploy registry",
      provider: "builtin",
      endpoint: "registry.mesh.internal:5000",
      namespace: "demo",
      organization_id: seed.DEMO_ORG_ID,
    }),
  ],
  "storage-integrations": [
    record({
      id: "00000000-0000-0000-0000-0000000000c1",
      name: "Demo backups",
      provider: "s3",
      endpoint: "",
      region: "eu-west-1",
      bucket: "meshploy-demo",
      organization_id: seed.DEMO_ORG_ID,
    }),
  ],
  "notification-channels": [
    record({
      organization_id: seed.DEMO_ORG_ID,
      name: "Ops alerts",
      type: "email",
      config: { address: "ops@demo.meshploy.app" },
      events: ["deploy.failed", "node.offline"],
      enabled: true,
    }),
  ],
  // Three base domains, so every state the Domains page has can be seen
  // offline: the primary on NS delegation, a second served on-demand, and one
  // just added that is waiting for its TXT record.
  domains: [
    record({
      id: "00000000-0000-0000-0000-0000000000d1",
      organization_id: seed.DEMO_ORG_ID,
      base_domain: "demo.example.com",
      internal_subdomain: "internal",
      preview_subdomain: "preview",
      verified: true,
      is_primary: true,
      dns_mode: "delegation",
    }),
    record({
      id: "00000000-0000-0000-0000-0000000000d2",
      organization_id: seed.DEMO_ORG_ID,
      base_domain: "apps.example.org",
      internal_subdomain: "internal",
      preview_subdomain: "preview",
      verified: true,
      is_primary: false,
      dns_mode: "ondemand",
      verify_token: "3f9c2a7e1b8d4c6f0a5e9b2d7c1f8a4e",
    }),
    record({
      id: "00000000-0000-0000-0000-0000000000d3",
      organization_id: seed.DEMO_ORG_ID,
      base_domain: "shop.example.net",
      internal_subdomain: "internal",
      preview_subdomain: "preview",
      verified: false,
      is_primary: false,
      dns_mode: "delegation",
      verify_token: "a41e0c9d7b3f2e8a6c5d1b0f9e7a3c2d",
    }),
  ],
  deployments: [
    ...demoDeployHistory(),
    seed.demoDeployment,
    // web in staging was built from develop; production runs an older commit
    // of it, promoted up from staging.
    {
      ...seed.demoDeployment,
      id: "00000000-0000-0000-0000-0000000000b3",
      service_id: stagingWebId,
      image: "ghcr.io/demo/web:sha-4a1b9c2",
      build_job_name: "build-web-4a1b9c2",
      source: "build",
      source_branch: "develop",
      source_commit: "4a1b9c2",
      source_commit_message: "Add the dark checkout page",
      deployed_at: new Date(Date.now() - 40 * 60_000).toISOString(),
      created_at: new Date(Date.now() - 42 * 60_000).toISOString(),
    },
    {
      ...seed.demoDeployment,
      id: "00000000-0000-0000-0000-0000000000b4",
      service_id: seed.demoServiceWeb.id,
      image: seed.demoServiceWeb.image,
      build_job_name: "",
      source: "promotion",
      from_level: "staging",
      source_branch: "develop",
      source_commit: "9e3f210",
      source_commit_message: "Cache product images",
    },
  ],
  runs: [seed.demoJobRun, ...demoRunHistory()],
  // Every demo database is backed up nightly, as a real workspace should be.
  // Postgres has a good backup to clone from; the rest are set up but have not
  // run yet, which is what the board's "no backup yet" says for them.
  backups: [seed.demoServiceDb, ...demoDatabases.map((d) => d.service)].map((svc, i) =>
    record({
      id: `00000000-0000-0000-0000-000000000${300 + i}`,
      service_id: svc.id,
      storage_integration_id: "00000000-0000-0000-0000-0000000000c1",
      schedule: "0 2 * * *",
      retention_days: 14,
      enabled: true,
      last_backup_at: svc.id === seed.demoServiceDb.id ? new Date(Date.now() - 6 * 3_600_000).toISOString() : null,
      last_backup_status: svc.id === seed.demoServiceDb.id ? "success" : null,
    })
  ),
  permissions: [],
}
export const org = { ...seed.demoOrg }
export const user = { ...seed.demoUser }
export const settings: Record<string, any> = {}
export const buildConfigs: Record<string, any> = {
  // A CI job deploys this one through its webhook, last via the install's API
  // name - so moving the primary and retiring the old domain has one to show.
  [seed.DEMO_SVC_API]: {
    ...seed.demoBuildConfig,
    deploy_hook_host: "api.demo.example.com",
    deploy_hook_called_at: "2026-09-20T08:15:00Z",
  },
  // A second one on Bitbucket, so the auto-deploy screens can be seen on more
  // than one provider without connecting anything.
  [seed.DEMO_SVC_WEB]: {
    ...seed.demoBuildConfig,
    id: "00000000-0000-0000-0000-000000000021",
    service_id: seed.DEMO_SVC_WEB,
    git_integration_id: seed.DEMO_GIT_BITBUCKET_ID,
    git_repo: "demo-workspace/web",
    branch: "main",
    builder: "dockerfile",
  },
}
export const attachments: Record<string, string[]> = {
  [seed.DEMO_SVC_API]: [groupId, "00000000-0000-0000-0000-0000000000b5"],
}
export function projectCounts(p: DemoRecord) {
  const count = (kind: string, filter = (_r: DemoRecord) => true) =>
    db[kind].filter((r) => r.project_id === p.id && filter(r)).length
  return {
    ...p,
    services_count: count("services", (r) => r.type !== "database"),
    databases_count: count("services", (r) => r.type === "database"),
    routes_count: count("routes") + count("tcp-routes"),
    variables_count: count("variable-groups"),
    jobs_count: count("jobs"),
    stacks_count: count("stacks"),
    volumes_count: count("volumes"),
    config_files_count: count("config-files"),
    stats: projectStats(p),
    levels: db.projects
      .filter((l) => l.parent_project_id === p.id)
      .sort((a, b) => a.env_level - b.env_level)
      .map((l) => ({ project_id: l.id, name: l.env_name, level: l.env_level })),
  }
}
/** The overview cards' breakdowns, as the API computes them. */
export function projectStats(p: DemoRecord) {
  const out: Record<string, Record<string, number>> = {}
  const add = (kind: string, key: string) => {
    out[kind] ??= {}
    out[kind][key] = (out[kind][key] ?? 0) + 1
  }
  const mine = (kind: string) => (db[kind] ?? []).filter((r) => r.project_id === p.id)
  for (const s of mine("services")) add(s.type === "database" ? "databases" : "services", s.status ?? "stopped")
  for (const s of mine("stacks")) add("stacks", s.status ?? "idle")
  for (const v of mine("volumes")) add("volumes", v.status ?? "idle")
  for (const r of mine("routes")) add("routes", r.published === false ? "paused" : r.zone === "internal" ? "internal" : "https")
  for (const _ of mine("tcp-routes")) add("routes", "tcp")
  for (const j of mine("jobs")) {
    add("jobs", j.schedule ? "scheduled" : "manual")
    if (j.status === "failed") add("jobs", "failed")
  }
  for (const g of mine("variable-groups")) add("variables", g.service_id ? "published" : "shared")
  for (const f of mine("config-files")) add("config_files", f.attached_services?.length ? "attached" : "unused")
  return out
}

/**
 * Two weeks of the demo's shipping, so the overview's delivery chart and
 * numbers have something to show: api built and deployed from main, with one
 * failed deploy fixed two hours later; web built in staging from develop and
 * promoted to production. Each service's newest one here is what its card
 * shows, so they match the rest of the demo.
 */
function demoDeployHistory() {
  const ago = (days: number, hours = 0) => new Date(Date.now() - days * 86_400_000 + hours * 3_600_000).toISOString()
  const id = (n: number) => `00000000-0000-0000-0000-000000000${String(100 + n).padStart(3, "0")}`
  const dep = (n: number, service: string, at: string, status: string, fields: Record<string, unknown>) => ({
    ...seed.demoDeployment, id: id(n), service_id: service, status, created_at: at, updated_at: at,
    deployed_at: status === "success" ? at : null, build_job_name: "", log: "[demo] history", ...fields,
  })
  const api = (n: number, days: number, hours: number, status: string, commit: string, message: string) =>
    dep(n, seed.DEMO_SVC_API, ago(days, hours), status, { image: `ghcr.io/demo/api:${commit}`, source: "build", source_branch: "main", source_commit: commit, source_commit_message: message })
  const staging = (n: number, days: number, commit: string, message: string) =>
    dep(n, stagingWebId, ago(days), "success", { image: `ghcr.io/demo/web:sha-${commit}`, source: "build", source_branch: "develop", source_commit: commit, source_commit_message: message })
  const promoted = (n: number, days: number, builtDays: number, commit: string, message: string) =>
    dep(n, seed.demoServiceWeb.id, ago(days), "success", {
      image: `ghcr.io/demo/web:sha-${commit}`, source: "promotion", from_level: "staging", arrival: "promotion",
      source_branch: "develop", source_commit: commit, source_commit_message: message, image_built_at: ago(builtDays),
    })
  return [
    api(1, 13, 0, "success", "5d1e0a7", "Add request logging"),
    api(2, 11, 0, "failed", "8c24f19", "Upgrade the router"),
    api(3, 11, 2, "success", "b7e3d40", "Fix the router upgrade"),
    api(4, 8, 0, "success", "e91a6c3", "Cache sessions"),
    api(5, 4, 0, "success", "31f0b2d", "Tune the connection pool"),
    staging(6, 12, "0a4c7e1", "Add product search"),
    staging(7, 9, "6b2f913", "Speed up the product grid"),
    staging(8, 6, "d83a5f0", "Show stock on product pages"),
    staging(9, 3, "9e3f210", "Cache product images"),
    promoted(10, 11, 12, "0a4c7e1", "Add product search"),
    promoted(11, 7, 9, "6b2f913", "Speed up the product grid"),
    promoted(12, 2, 3, "9e3f210", "Cache product images"),
  ]
}

/** The demo's migration job, run most days, failing once. */
function demoRunHistory() {
  return [1, 2, 4, 5, 6, 8, 9, 10, 12, 13].map((days, i) => {
    const at = new Date(Date.now() - days * 86_400_000).toISOString()
    return {
      ...seed.demoJobRun, id: `00000000-0000-0000-0000-000000000${200 + i}`,
      status: days === 6 ? "failed" : "success", created_at: at, started_at: at, finished_at: at,
    }
  })
}
