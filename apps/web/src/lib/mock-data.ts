import type {
  Org,
  OrgMember,
  Project,
  AppRoute,
  Template,
  GitIntegration,
  RegistryIntegration,
  StorageIntegration,
  NotificationChannel,
} from "@/types"

export const mockOrgs: Org[] = [
  { id: "org_01jf3m2n4k5p6q7r8s9t0u1v2w", name: "Acme Corp", slug: "acme-corp" },
  { id: "org_02jf3m2n4k5p6q7r8s9t0u1v2x", name: "Personal", slug: "personal" },
]

export const mockMembers: OrgMember[] = [
  {
    id: "mem_01",
    userId: "usr_01",
    name: "Alice Chen",
    email: "alice@acme.com",
    role: "owner",
    joinedAt: new Date("2024-10-01"),
  },
  {
    id: "mem_02",
    userId: "usr_02",
    name: "Bob Martins",
    email: "bob@acme.com",
    role: "admin",
    joinedAt: new Date("2024-10-15"),
  },
  {
    id: "mem_03",
    userId: "usr_03",
    name: "Carol Singh",
    email: "carol@acme.com",
    role: "member",
    joinedAt: new Date("2024-11-03"),
  },
  {
    id: "mem_04",
    userId: "usr_04",
    name: "Dave Kim",
    email: "dave@acme.com",
    role: "member",
    joinedAt: new Date("2025-01-20"),
  },
]


export const mockProjects: Project[] = [
  {
    id: "proj_01jf3m2n4k5p6q7r8s9t0u1v2w",
    name: "production-api",
    slug: "production-api",
    organizationId: mockOrgs[0].id,
    servicesCount: 3,
    routesCount: 2,
    createdAt: new Date("2024-11-15"),
  },
  {
    id: "proj_02jf3m2n4k5p6q7r8s9t0u1v2x",
    name: "frontend-web",
    slug: "frontend-web",
    organizationId: mockOrgs[0].id,
    servicesCount: 1,
    routesCount: 1,
    createdAt: new Date("2024-12-02"),
  },
  {
    id: "proj_03jf3m2n4k5p6q7r8s9t0u1v2y",
    name: "data-pipeline",
    slug: "data-pipeline",
    organizationId: mockOrgs[0].id,
    servicesCount: 5,
    routesCount: 0,
    createdAt: new Date("2025-01-08"),
  },
]


export const mockRoutes: AppRoute[] = [
  {
    id: "route_01",
    hostname: "api.cs.example.com",
    subdomain: "api",
    zone: "public",
    domainId: null,
    targetIp: "100.64.0.1",
    targetPort: 3000,
    projectId: mockProjects[0].id,
    organizationId: mockOrgs[0].id,
  },
  {
    id: "route_02",
    hostname: "api.internal.cs.example.com",
    subdomain: "api",
    zone: "internal",
    domainId: null,
    targetIp: "100.64.0.1",
    targetPort: 3001,
    projectId: mockProjects[0].id,
    organizationId: mockOrgs[0].id,
  },
  {
    id: "route_03",
    hostname: "shop.cs.example.com",
    subdomain: "shop",
    zone: "public",
    domainId: null,
    targetIp: "100.64.0.2",
    targetPort: 3000,
    projectId: mockProjects[1].id,
    organizationId: mockOrgs[0].id,
  },
]

export const mockTemplates: Template[] = [
  {
    id: "tpl_wordpress",
    name: "WordPress",
    description: "Self-hosted CMS with MySQL",
    category: "web",
    icon: "🌐",
    compose: `services:
  wordpress:
    image: wordpress:latest
    ports: ["80:80"]
    environment:
      WORDPRESS_DB_HOST: db
      WORDPRESS_DB_USER: wordpress
      WORDPRESS_DB_PASSWORD: \${DB_PASSWORD}
      WORDPRESS_DB_NAME: wordpress
    depends_on: [db]
  db:
    image: mysql:8
    environment:
      MYSQL_DATABASE: wordpress
      MYSQL_USER: wordpress
      MYSQL_PASSWORD: \${DB_PASSWORD}
      MYSQL_RANDOM_ROOT_PASSWORD: "1"
    volumes:
      - db_data:/var/lib/mysql
volumes:
  db_data:`,
  },
  {
    id: "tpl_ghost",
    name: "Ghost",
    description: "Modern publishing platform",
    category: "web",
    icon: "👻",
    compose: `services:
  ghost:
    image: ghost:5
    ports: ["2368:2368"]
    environment:
      url: https://\${SUBDOMAIN}.acme.com
      database__client: mysql
      database__connection__host: db
      database__connection__user: ghost
      database__connection__password: \${DB_PASSWORD}
      database__connection__database: ghost
    depends_on: [db]
  db:
    image: mysql:8
    environment:
      MYSQL_USER: ghost
      MYSQL_PASSWORD: \${DB_PASSWORD}
      MYSQL_DATABASE: ghost
      MYSQL_RANDOM_ROOT_PASSWORD: "1"
    volumes:
      - db_data:/var/lib/mysql
volumes:
  db_data:`,
  },
  {
    id: "tpl_plausible",
    name: "Plausible Analytics",
    description: "Privacy-friendly web analytics",
    category: "monitoring",
    icon: "📊",
    compose: `services:
  plausible:
    image: ghcr.io/plausible/community-edition:v2
    ports: ["8000:8000"]
    environment:
      BASE_URL: https://\${SUBDOMAIN}.acme.com
      SECRET_KEY_BASE: \${SECRET_KEY}
    depends_on:
      - db
      - clickhouse
  db:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: \${DB_PASSWORD}
    volumes:
      - db_data:/var/lib/postgresql/data
  clickhouse:
    image: clickhouse/clickhouse-server:23
    volumes:
      - events_data:/var/lib/clickhouse
volumes:
  db_data:
  events_data:`,
  },
  {
    id: "tpl_uptime_kuma",
    name: "Uptime Kuma",
    description: "Self-hosted uptime monitoring",
    category: "monitoring",
    icon: "🟢",
    compose: `services:
  uptime-kuma:
    image: louislam/uptime-kuma:1
    ports: ["3001:3001"]
    volumes:
      - uptime_data:/app/data
volumes:
  uptime_data:`,
  },
  {
    id: "tpl_minio",
    name: "MinIO",
    description: "S3-compatible object storage",
    category: "storage",
    icon: "🪣",
    compose: `services:
  minio:
    image: minio/minio:latest
    ports:
      - "9000:9000"
      - "9001:9001"
    environment:
      MINIO_ROOT_USER: \${MINIO_USER}
      MINIO_ROOT_PASSWORD: \${MINIO_PASSWORD}
    command: server /data --console-address ":9001"
    volumes:
      - minio_data:/data
volumes:
  minio_data:`,
  },
  {
    id: "tpl_n8n",
    name: "n8n",
    description: "Workflow automation platform",
    category: "other",
    icon: "⚙️",
    compose: `services:
  n8n:
    image: n8nio/n8n:latest
    ports: ["5678:5678"]
    environment:
      N8N_HOST: \${SUBDOMAIN}.acme.com
      N8N_PROTOCOL: https
      WEBHOOK_URL: https://\${SUBDOMAIN}.acme.com/
    volumes:
      - n8n_data:/home/node/.n8n
volumes:
  n8n_data:`,
  },
  {
    id: "tpl_rabbitmq",
    name: "RabbitMQ",
    description: "Message broker with management UI",
    category: "messaging",
    icon: "🐇",
    compose: `services:
  rabbitmq:
    image: rabbitmq:3-management
    ports:
      - "5672:5672"
      - "15672:15672"
    environment:
      RABBITMQ_DEFAULT_USER: \${RABBITMQ_USER}
      RABBITMQ_DEFAULT_PASS: \${RABBITMQ_PASSWORD}
    volumes:
      - rabbitmq_data:/var/lib/rabbitmq
volumes:
  rabbitmq_data:`,
  },
  {
    id: "tpl_gitea",
    name: "Gitea",
    description: "Lightweight self-hosted Git service",
    category: "other",
    icon: "🍵",
    compose: `services:
  gitea:
    image: gitea/gitea:latest
    ports:
      - "3000:3000"
      - "222:22"
    environment:
      USER_UID: "1000"
      USER_GID: "1000"
      GITEA__database__DB_TYPE: postgres
      GITEA__database__HOST: db:5432
      GITEA__database__NAME: gitea
      GITEA__database__USER: gitea
      GITEA__database__PASSWD: \${DB_PASSWORD}
    volumes:
      - gitea_data:/data
    depends_on: [db]
  db:
    image: postgres:16
    environment:
      POSTGRES_USER: gitea
      POSTGRES_PASSWORD: \${DB_PASSWORD}
      POSTGRES_DB: gitea
    volumes:
      - db_data:/var/lib/postgresql/data
volumes:
  gitea_data:
  db_data:`,
  },
]

export const mockDeployments: Deployment[] = [
  {
    id: "dep_01",
    serviceId: "svc_01",
    status: "success",
    image: "ghcr.io/acme/api-server:v2.3.1",
    deployedAt: new Date(Date.now() - 2 * 60 * 60 * 1000),
  },
  {
    id: "dep_02",
    serviceId: "svc_01",
    status: "success",
    image: "ghcr.io/acme/api-server:v2.3.0",
    deployedAt: new Date(Date.now() - 2 * 24 * 60 * 60 * 1000),
  },
  {
    id: "dep_03",
    serviceId: "svc_04",
    status: "running",
    image: "ghcr.io/acme/frontend:latest",
    deployedAt: new Date(Date.now() - 10 * 60 * 1000),
  },
  {
    id: "dep_04",
    serviceId: "svc_07",
    status: "failed",
    image: "bitnami/kafka:3.7",
    deployedAt: new Date(Date.now() - 6 * 60 * 60 * 1000),
  },
]

export const mockGitIntegrations: GitIntegration[] = [
  {
    id: "git_01",
    name: "GitHub — acme-org",
    provider: "github",
    organizationId: mockOrgs[0].id,
    createdAt: new Date("2024-10-01"),
  },
  {
    id: "git_02",
    name: "GitLab — self-hosted",
    provider: "gitlab",
    baseUrl: "https://gitlab.acme.internal",
    organizationId: mockOrgs[0].id,
    createdAt: new Date("2024-11-15"),
  },
]

export const mockRegistries: RegistryIntegration[] = [
  {
    id: "reg_01",
    name: "GitHub Container Registry",
    provider: "ghcr",
    endpoint: "ghcr.io",
    username: "acme-bot",
    organizationId: mockOrgs[0].id,
    createdAt: new Date("2024-10-05"),
  },
  {
    id: "reg_02",
    name: "Docker Hub",
    provider: "dockerhub",
    username: "acmecorp",
    organizationId: mockOrgs[0].id,
    createdAt: new Date("2024-10-10"),
  },
]

export const mockStorage: StorageIntegration[] = [
  {
    id: "sto_01",
    name: "Backups — R2",
    provider: "r2",
    endpoint: "https://abc123.r2.cloudflarestorage.com",
    bucket: "meshploy-backups",
    organizationId: mockOrgs[0].id,
    createdAt: new Date("2024-11-01"),
  },
]

export const mockNotifications: NotificationChannel[] = [
  {
    id: "ntf_01",
    name: "Deployments — Slack",
    type: "slack",
    events: ["deployment.success", "deployment.failed"],
    organizationId: mockOrgs[0].id,
    createdAt: new Date("2024-11-10"),
  },
  {
    id: "ntf_02",
    name: "Node alerts",
    type: "webhook",
    events: ["node.offline", "node.online"],
    organizationId: mockOrgs[0].id,
    createdAt: new Date("2024-12-01"),
  },
]
