import type { StorageIntegration, NotificationChannel } from "@/types"

export const mockStorage: StorageIntegration[] = [
  {
    id: "sto_01",
    name: "Backups — R2",
    provider: "r2",
    endpoint: "https://abc123.r2.cloudflarestorage.com",
    bucket: "meshploy-backups",
    organizationId: "org_01jf3m2n4k5p6q7r8s9t0u1v2w",
    createdAt: new Date("2024-11-01"),
  },
]

export const mockNotifications: NotificationChannel[] = [
  {
    id: "ntf_01",
    name: "Deployments — Slack",
    type: "slack",
    events: ["deployment.success", "deployment.failed"],
    organizationId: "org_01jf3m2n4k5p6q7r8s9t0u1v2w",
    createdAt: new Date("2024-11-10"),
  },
  {
    id: "ntf_02",
    name: "Node alerts",
    type: "webhook",
    events: ["node.offline", "node.online"],
    organizationId: "org_01jf3m2n4k5p6q7r8s9t0u1v2w",
    createdAt: new Date("2024-12-01"),
  },
]
