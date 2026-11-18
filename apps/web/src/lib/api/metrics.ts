import { apiFetch } from "./core"

export type MetricRange = "1h" | "6h" | "24h" | "7d" | "30d"

/** A node's stored sample: one minute, or the average of five with the highest beside it. */
export interface ApiNodeMetricSample {
  at: string
  cpu_pct: number
  cpu_pct_max: number
  mem_used: number
  mem_used_max: number
  mem_total: number
  disk_used: number
  disk_total: number
  net_rx_bps: number
  net_tx_bps: number
}

/** A service's stored sample, summed over its running pods. Restarts count up. */
export interface ApiServiceMetricSample {
  at: string
  cpu_millis: number
  cpu_millis_max: number
  mem_bytes: number
  mem_bytes_max: number
  pods: number
  restarts: number
}

export interface ApiMetricHistory<T> {
  range: MetricRange
  resolution: "1m" | "5m"
  retention_days: number
  points: T[]
}

export interface ApiMetricSettings {
  retention_days: number
  choices: number[]
}

export const metrics = {
  nodeHistory: (orgId: string, nodeId: string, range: MetricRange, token: string) =>
    apiFetch<ApiMetricHistory<ApiNodeMetricSample>>(`/api/v1/orgs/${orgId}/nodes/${nodeId}/metrics/history?range=${range}`, {}, token),

  serviceHistory: (orgId: string, projectId: string, serviceId: string, range: MetricRange, token: string) =>
    apiFetch<ApiMetricHistory<ApiServiceMetricSample>>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/metrics/history?range=${range}`,
      {},
      token,
    ),

  settings: (orgId: string, token: string) =>
    apiFetch<ApiMetricSettings>(`/api/v1/orgs/${orgId}/metrics/settings`, {}, token),

  setRetention: (orgId: string, retentionDays: number, token: string) =>
    apiFetch<ApiMetricSettings>(
      `/api/v1/orgs/${orgId}/metrics/settings`,
      { method: "PUT", body: JSON.stringify({ retention_days: retentionDays }) },
      token,
    ),
}
