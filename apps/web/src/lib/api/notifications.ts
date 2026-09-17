import { apiFetch } from "./core"

export type NotificationChannelType = "email" | "webhook" | "slack" | "discord"

export interface ApiNotificationChannel {
  id: string
  name: string
  type: NotificationChannelType
  config: Record<string, string>
  events: string[]
  enabled: boolean
  created_at: string
  /** The latest attempt, test or not. Null when nothing has been sent yet. */
  last_delivery: ApiNotificationDelivery | null
  /** Attempts in a row that failed since the last one that worked. */
  failing_streak: number
}

/** One attempt to send an event to a channel. */
export interface ApiNotificationDelivery {
  id: string
  channel_id: string
  event: string
  /** What the event was about: service, project, stack, node, detail. */
  data: Record<string, string>
  success: boolean
  error: string
  test: boolean
  /** The attempt this one sent again. */
  retry_of?: string
  created_at: string
}

export interface CreateNotificationBody {
  name: string
  type: NotificationChannelType
  config: Record<string, string>
  events: string[]
}

/** One event a channel can subscribe to, as the API describes it. */
export interface ApiNotificationEvent {
  event: string
  group: string
  title: string
  /** When this fires, in plain words. */
  description: string
  tone: "good" | "bad" | "warning"
  /** Part of the set a new channel starts with. */
  recommended: boolean
}

export const notifications = {
  /** The catalogue, served so the console cannot drift from what is dispatched. */
  events: (token: string) =>
    apiFetch<ApiNotificationEvent[]>("/api/v1/notification-events", {}, token),

  list: (orgId: string, token: string) =>
    apiFetch<ApiNotificationChannel[]>(
      `/api/v1/orgs/${orgId}/notification-channels`,
      {},
      token
    ),

  create: (orgId: string, body: CreateNotificationBody, token: string) =>
    apiFetch<ApiNotificationChannel>(
      `/api/v1/orgs/${orgId}/notification-channels`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  update: (orgId: string, id: string, body: Partial<CreateNotificationBody> & { enabled?: boolean }, token: string) =>
    apiFetch<ApiNotificationChannel>(
      `/api/v1/orgs/${orgId}/notification-channels/${id}`,
      { method: "PUT", body: JSON.stringify(body) },
      token
    ),

  delete: (orgId: string, id: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/notification-channels/${id}`,
      { method: "DELETE" },
      token
    ),

  /** Sends at once. A failed send resolves too, with `success: false`. */
  test: (orgId: string, id: string, token: string) =>
    apiFetch<ApiNotificationDelivery>(
      `/api/v1/orgs/${orgId}/notification-channels/${id}/test`,
      { method: "POST" },
      token
    ),

  deliveries: (orgId: string, id: string, status: "all" | "failed", token: string) =>
    apiFetch<ApiNotificationDelivery[]>(
      `/api/v1/orgs/${orgId}/notification-channels/${id}/deliveries?status=${status}`,
      {},
      token
    ),

  retry: (orgId: string, deliveryId: string, token: string) =>
    apiFetch<ApiNotificationDelivery>(
      `/api/v1/orgs/${orgId}/notification-deliveries/${deliveryId}/retry`,
      { method: "POST" },
      token
    ),
}
