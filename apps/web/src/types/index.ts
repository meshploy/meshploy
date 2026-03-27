export type NodeStatus = "online" | "offline"
export type K3sRole = "server" | "agent"
export type ServiceStatus = "running" | "stopped" | "deploying" | "failed"
export type ServiceType = "application" | "database"
export type OrgRole = "owner" | "admin" | "member"
export type DeploymentStatus = "pending" | "running" | "success" | "failed"

export interface Org {
  id: string
  name: string
  slug: string
}

export interface OrgMember {
  id: string
  userId: string
  name: string
  email: string
  role: OrgRole
  joinedAt: Date
}

export interface Node {
  id: string
  name: string
  tailscaleIP: string
  status: NodeStatus
  k3sRole: K3sRole
  k3sVersion: string
  os: string
  cpuCores: number
  memoryGB: number
  diskGB: number
  lastSeenAt: Date
  organizationId: string
}

export interface Project {
  id: string
  name: string
  slug: string
  organizationId: string
  servicesCount: number
  routesCount: number
  createdAt: Date
}

export interface Service {
  id: string
  name: string
  projectId: string
  type: ServiceType
  status: ServiceStatus
  image: string
  replicas: number
  nodeId?: string
  createdAt: Date
}


export interface Deployment {
  id: string
  serviceId: string
  status: DeploymentStatus
  image: string
  deployedAt: Date
}

export interface RegistryIntegration {
  id: string
  name: string
  provider: "dockerhub" | "ghcr" | "ecr" | "generic"
  endpoint?: string
  username: string
  organizationId: string
  createdAt: Date
}

export interface StorageIntegration {
  id: string
  name: string
  provider: "s3" | "r2" | "minio"
  endpoint?: string
  bucket: string
  region?: string
  organizationId: string
  createdAt: Date
}

export interface NotificationChannel {
  id: string
  name: string
  type: "slack" | "webhook" | "email"
  events: string[]
  organizationId: string
  createdAt: Date
}

export type RouteZone = "public" | "internal"

export interface RouteTarget {
  path: string
  nodeId: string
  port: number
  override?: string
}

export interface AppRoute {
  id: string
  subdomain: string
  zone: RouteZone
  targets: RouteTarget[]
  projectId: string
  organizationId: string
}

export type TemplateCategory = "web" | "database" | "monitoring" | "messaging" | "storage" | "other"

export interface Template {
  id: string
  name: string
  description: string
  category: TemplateCategory
  icon: string
  compose: string
}
