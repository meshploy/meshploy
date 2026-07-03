import { apiFetch, API_BASE } from "./core"
import type { ApiStack } from "./stacks"

export interface TemplateVariable {
  key: string
  prompt?: string
  required?: boolean
  generate?: string
  expose?: { service: string; port: number }
}

export interface TemplateManifest {
  id: string
  name: string
  description: string
  category: string
  version: string
  icon: string
  links: { website?: string; source?: string }
  maintainers?: string[]
  variables: TemplateVariable[]
}

export interface TemplateDetail {
  manifest: TemplateManifest
  compose: string
}

export interface DeployTemplateBody {
  /** Optional user-edited compose from the stack editor; empty = template default. */
  spec?: string
  prompt_values?: Record<string, string>
}

export const templates = {
  list: (token: string) =>
    apiFetch<TemplateManifest[]>(`/api/v1/templates`, {}, token),

  /** Public icon URL for an <img src> (unauthenticated route). */
  iconUrl: (id: string) => `${API_BASE}/api/v1/templates/${id}/icon`,

  get: (id: string, token: string) =>
    apiFetch<TemplateDetail>(`/api/v1/templates/${id}`, {}, token),

  deploy: (
    orgId: string,
    projectId: string,
    templateId: string,
    body: DeployTemplateBody,
    token: string
  ) =>
    apiFetch<ApiStack>(
      `/api/v1/orgs/${orgId}/projects/${projectId}/templates/${templateId}/deploy`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),
}
