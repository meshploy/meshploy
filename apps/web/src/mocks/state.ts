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

function tcpRoute(gatewayPort: number, over: Record<string, unknown>) {
  return record({
    organization_id: seed.DEMO_ORG_ID,
    project_id: projectId,
    gateway_port: gatewayPort,
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
    seed.demoProject,
    {
      ...seed.demoProject,
      id: secondProjectId,
      name: "Experiments",
      slug: "experiments",
    },
  ],
  services: [seed.demoServiceApi, seed.demoServiceWeb, seed.demoServiceDb, ...demoDatabases.map((d) => d.service)],
  jobs: [seed.demoJob],
  volumes: [{ ...seed.demoVolume, stack_id: null }],
  stacks: [seed.demoStack],
  routes: [
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
  ],
  // The demo database, published on the gateway, so a TCP route has a page.
  // One published port per managed database engine, so each engine's connect
  // command can be seen, and one to a plain port on a node, which has none.
  "tcp-routes": [
    tcpRoute(15432, { service_id: seed.DEMO_SVC_DB, service_port: 5432, target_port: 31432 }),
    ...demoDatabases.map((d) =>
      tcpRoute(d.gatewayPort, { service_id: d.service.id, service_port: d.port, target_port: d.nodePort })
    ),
    tcpRoute(19000, {
      node_id: seed.DEMO_NODE_W1,
      target_ip: "100.64.0.2",
      target_port: 9000,
      allowed_cidrs: ["203.0.113.0/24", "10.0.0.0/8"],
    }),
  ],
  "variable-groups": [
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
      name: "Team GitHub",
      provider: "github",
      auth_method: "pat",
      base_url: "https://github.com",
      connected: true,
      organization_id: seed.DEMO_ORG_ID,
    }),
    record({
      name: "Demo GitLab",
      provider: "gitlab",
      auth_method: "pat",
      base_url: "https://gitlab.com",
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
  domains: [
    record({
      organization_id: seed.DEMO_ORG_ID,
      base_domain: "demo.example.com",
      internal_subdomain: "internal",
      preview_subdomain: "preview",
      verified: true,
    }),
  ],
  deployments: [seed.demoDeployment],
  runs: [seed.demoJobRun],
  backups: [],
  permissions: [],
}
export const org = { ...seed.demoOrg }
export const user = { ...seed.demoUser }
export const settings: Record<string, any> = {}
export const buildConfigs: Record<string, any> = {
  [seed.DEMO_SVC_API]: { ...seed.demoBuildConfig },
}
export const attachments: Record<string, string[]> = {
  [seed.DEMO_SVC_API]: [groupId],
}
export function projectCounts(p: DemoRecord) {
  const count = (kind: string, filter = (_r: DemoRecord) => true) =>
    db[kind].filter((r) => r.project_id === p.id && filter(r)).length
  return {
    ...p,
    services_count: count("services", (r) => r.type !== "database"),
    databases_count: count("services", (r) => r.type === "database"),
    routes_count: count("routes"),
    variables_count: count("variable-groups"),
    jobs_count: count("jobs"),
    stacks_count: count("stacks"),
    volumes_count: count("volumes"),
    config_files_count: count("config-files"),
  }
}
