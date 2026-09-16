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
}
