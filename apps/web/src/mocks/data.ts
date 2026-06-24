// Fixed IDs for stable Playwright tests
export const DEMO_USER_ID = "00000000-0000-0000-0000-000000000002"
export const DEMO_ORG_ID = "00000000-0000-0000-0000-000000000001"
export const DEMO_PROJECT_ID = "00000000-0000-0000-0000-000000000003"
export const DEMO_NODE_GW = "00000000-0000-0000-0000-000000000004"
export const DEMO_NODE_W1 = "00000000-0000-0000-0000-000000000005"
export const DEMO_SVC_API = "00000000-0000-0000-0000-000000000006"
export const DEMO_SVC_WEB = "00000000-0000-0000-0000-000000000007"
export const DEMO_SVC_DB = "00000000-0000-0000-0000-000000000008"
export const DEMO_DEPLOYMENT_ID = "00000000-0000-0000-0000-000000000009"
export const DEMO_JOB_ID = "00000000-0000-0000-0000-00000000000a"
export const DEMO_VOLUME_ID = "00000000-0000-0000-0000-00000000000b"
export const DEMO_STACK_ID = "00000000-0000-0000-0000-00000000000c"
export const DEMO_ROUTE_ID = "00000000-0000-0000-0000-00000000000d"

// Built at runtime so btoa is available (browser context)
export const DEMO_TOKEN = (() => {
  const payload = btoa(JSON.stringify({ uid: DEMO_USER_ID, exp: 9999999999 }))
  return `eyJhbGciOiJub25lIn0.${payload}.`
})()

export const DEMO_NOW = "2026-06-24T10:00:00Z"

export const demoOrg = {
  id: DEMO_ORG_ID,
  name: "Demo Org",
  slug: "demo-org",
  created_at: DEMO_NOW,
  updated_at: DEMO_NOW,
}

export const demoOrgMember = {
  id: "00000000-0000-0000-0000-000000000010",
  user_id: DEMO_USER_ID,
  role: "owner" as const,
  user_name: "Demo User",
  user_email: "demo@meshploy.com",
}

export const demoUser = {
  id: DEMO_USER_ID,
  username: "demo",
  email: "demo@meshploy.com",
  totp_enabled: false,
}

export const demoProject = {
  id: DEMO_PROJECT_ID,
  name: "Demo Project",
  slug: "demo-project",
  organization_id: DEMO_ORG_ID,
  services_count: 3,
  databases_count: 1,
  routes_count: 2,
  secrets_count: 0,
  jobs_count: 1,
  stacks_count: 0,
  volumes_count: 1,
  created_at: DEMO_NOW,
  updated_at: DEMO_NOW,
}

export const demoNodeGateway = {
  id: DEMO_NODE_GW,
  name: "gateway",
  organization_id: DEMO_ORG_ID,
  tailscale_ip: "100.64.0.1",
  status: "online",
  k3s_role: "server",
  mesh_role: "workload_builder",
  k3s_version: "v1.31.5+k3s1",
  os: "Ubuntu 24.04.2 LTS",
  cpu_cores: 4,
  memory_gb: 8,
  disk_gb: 80,
  last_seen_at: DEMO_NOW,
  headscale_id: "1",
  headscale_online: true,
  headscale_last_seen: DEMO_NOW,
  headscale_expiry: "2027-01-01T00:00:00Z",
  headscale_tags: ["tag:server"],
  headscale_user: "meshploy",
  headscale_fqdn: "gateway.meshploy.ts.net",
  k8s_member: true,
  k8s_ready: true,
  k8s_node_name: "gateway",
  active_projects: [DEMO_PROJECT_ID],
}

export const demoNodeWorker = {
  id: DEMO_NODE_W1,
  name: "worker-1",
  organization_id: DEMO_ORG_ID,
  tailscale_ip: "100.64.0.2",
  status: "online",
  k3s_role: "agent",
  mesh_role: "workload",
  k3s_version: "v1.31.5+k3s1",
  os: "Ubuntu 24.04.2 LTS",
  cpu_cores: 8,
  memory_gb: 16,
  disk_gb: 160,
  last_seen_at: DEMO_NOW,
  headscale_id: "2",
  headscale_online: true,
  headscale_last_seen: DEMO_NOW,
  headscale_expiry: "2027-01-01T00:00:00Z",
  headscale_tags: ["tag:worker"],
  headscale_user: "meshploy",
  headscale_fqdn: "worker-1.meshploy.ts.net",
  k8s_member: true,
  k8s_ready: true,
  k8s_node_name: "worker-1",
  active_projects: [DEMO_PROJECT_ID],
}

export const demoServiceApi = {
  id: DEMO_SVC_API,
  name: "api",
  project_id: DEMO_PROJECT_ID,
  node_id: DEMO_NODE_W1,
  stack_id: null,
  type: "application" as const,
  status: "running" as const,
  image: "ghcr.io/demo/api:latest",
  pull_registry_integration_id: null,
  ports: [
    { id: "p1", service_id: DEMO_SVC_API, name: "http", port: 4000, is_http: true, is_primary: true, is_public: false, node_port: 30001 },
  ],
  replicas: 2,
  cpu_request: "250m",
  cpu_limit: "500m",
  memory_request: "256Mi",
  memory_limit: "512Mi",
  created_at: DEMO_NOW,
  updated_at: DEMO_NOW,
}

export const demoServiceWeb = {
  id: DEMO_SVC_WEB,
  name: "web",
  project_id: DEMO_PROJECT_ID,
  node_id: DEMO_NODE_W1,
  stack_id: null,
  type: "application" as const,
  status: "running" as const,
  image: "ghcr.io/demo/web:latest",
  pull_registry_integration_id: null,
  ports: [
    { id: "p2", service_id: DEMO_SVC_WEB, name: "http", port: 3000, is_http: true, is_primary: true, is_public: true, node_port: 30002 },
  ],
  replicas: 1,
  cpu_request: "100m",
  cpu_limit: "250m",
  memory_request: "128Mi",
  memory_limit: "256Mi",
  created_at: DEMO_NOW,
  updated_at: DEMO_NOW,
}

export const demoServiceDb = {
  id: DEMO_SVC_DB,
  name: "postgres",
  project_id: DEMO_PROJECT_ID,
  node_id: DEMO_NODE_W1,
  stack_id: null,
  type: "database" as const,
  status: "running" as const,
  image: "postgres:16",
  pull_registry_integration_id: null,
  ports: [
    { id: "p3", service_id: DEMO_SVC_DB, name: "postgres", port: 5432, is_http: false, is_primary: true, is_public: false, node_port: 30003 },
  ],
  replicas: 1,
  cpu_request: "500m",
  cpu_limit: "1000m",
  memory_request: "512Mi",
  memory_limit: "1Gi",
  created_at: DEMO_NOW,
  updated_at: DEMO_NOW,
}

export const demoDeployment = {
  id: DEMO_DEPLOYMENT_ID,
  service_id: DEMO_SVC_API,
  status: "success" as const,
  image: "ghcr.io/demo/api:abc1234",
  build_job_name: "build-api-abc1234",
  log: "Build successful\nPushed image\nDeployment complete",
  deployed_at: DEMO_NOW,
  created_at: DEMO_NOW,
  updated_at: DEMO_NOW,
}

export const demoBuildConfig = {
  id: "00000000-0000-0000-0000-000000000020",
  service_id: DEMO_SVC_API,
  builder: "railpack" as const,
  git_repo: "https://github.com/demo/api",
  branch: "main",
  dockerfile_path: "Dockerfile",
  registry_integration_id: null,
  git_integration_id: null,
  builder_node: "",
  builder_cpu_request: "1000m",
  builder_memory_request: "1Gi",
  last_built_image: "ghcr.io/demo/api:abc1234",
  last_built_at: DEMO_NOW,
  rollback_enabled: true,
  image_retention: 5,
  auto_deploy: true,
  deploy_token: "dtkn-demo",
  created_at: DEMO_NOW,
  updated_at: DEMO_NOW,
}

export const demoJob = {
  id: DEMO_JOB_ID,
  project_id: DEMO_PROJECT_ID,
  node_id: null,
  name: "db-migrate",
  is_cron: false,
  image: "ghcr.io/demo/api:latest",
  command: "npm run db:migrate",
  schedule: "",
  concurrency_policy: "Forbid",
  history_limit: 10,
  cpu_request: "250m",
  cpu_limit: "500m",
  memory_request: "256Mi",
  memory_limit: "512Mi",
  env_vars: "",
  status: "idle",
  last_run_at: DEMO_NOW,
  k8s_name: "demo-project-db-migrate",
  created_at: DEMO_NOW,
  updated_at: DEMO_NOW,
}

export const demoJobRun = {
  id: "00000000-0000-0000-0000-000000000021",
  job_id: DEMO_JOB_ID,
  status: "success",
  started_at: DEMO_NOW,
  finished_at: DEMO_NOW,
  log: "Running migrations...\nDone.",
  k8s_job_name: "demo-project-db-migrate-run-1",
  created_at: DEMO_NOW,
}

export const demoVolume = {
  id: DEMO_VOLUME_ID,
  project_id: DEMO_PROJECT_ID,
  name: "uploads",
  slug: "uploads",
  storage_gb: 10,
  status: "ready" as const,
  mounts: [],
  created_at: DEMO_NOW,
  updated_at: DEMO_NOW,
}

export const demoStack = {
  id: DEMO_STACK_ID,
  project_id: DEMO_PROJECT_ID,
  name: "app-stack",
  spec: "version: '3'\nservices:\n  api:\n    image: ghcr.io/demo/api:latest\n",
  variables: {},
  status: "idle" as const,
  last_applied_at: DEMO_NOW,
  git_mode: "" as const,
  git_repo: "",
  git_branch: "",
  git_path: "",
  git_integration_id: null,
  git_last_synced_at: null,
  git_last_sync_sha: "",
  created_at: DEMO_NOW,
  updated_at: DEMO_NOW,
}

export const demoRoute = {
  id: DEMO_ROUTE_ID,
  hostname: "api.demo.meshploy.app",
  subdomain: "api",
  zone: "public" as const,
  domain_id: null,
  project_id: DEMO_PROJECT_ID,
  organization_id: DEMO_ORG_ID,
  created_at: DEMO_NOW,
  updated_at: DEMO_NOW,
  targets: [
    {
      id: "00000000-0000-0000-0000-000000000022",
      route_id: DEMO_ROUTE_ID,
      service_id: DEMO_SVC_API,
      target_port: 4000,
      weight: 100,
    },
  ],
}

export const demoPods = [
  {
    name: "api-6d8f4b9c7-xk4np",
    phase: "Running",
    ready: true,
    restarts: 0,
    node_name: "worker-1",
    started_at: DEMO_NOW,
  },
  {
    name: "api-6d8f4b9c7-m2wrt",
    phase: "Running",
    ready: true,
    restarts: 1,
    node_name: "worker-1",
    started_at: DEMO_NOW,
  },
]

export const demoNodeMetrics = {
  cpu_percent: 24.5,
  memory_percent: 61.2,
  memory_used_mb: 4998,
  memory_total_mb: 8192,
  disk_used_gb: 22.4,
  disk_total_gb: 80,
  load_average: [0.85, 0.72, 0.68],
  network_rx_bytes: 1024 * 1024 * 120,
  network_tx_bytes: 1024 * 1024 * 45,
  uptime_seconds: 1209600,
}
