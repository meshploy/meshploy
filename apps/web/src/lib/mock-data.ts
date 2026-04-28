import type { NotificationChannel } from "@/types"

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
