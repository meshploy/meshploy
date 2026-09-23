import { apiFetch } from "./core"

/** How a base domain's DNS is arranged. Per domain, not per server. */
export type DnsMode = "delegation" | "ondemand"

export interface ApiDomain {
  id: string
  organization_id: string
  base_domain: string
  internal_subdomain: string
  preview_subdomain: string
  verified: boolean
  /** The TXT value that proves ownership, published at
   *  `_meshploy-verify.<base_domain>`. Absent on the seeded domain, whose
   *  ownership is implicit. */
  verify_token?: string
  /** The one domain whose platform subdomains serve and that new routes
   *  default to. Every base domain routes; primary is a pointer, not a tier. */
  is_primary: boolean
  /** The primary moved away from this domain. Its console, api and headscale
   *  names keep serving until it is removed. */
  former_primary?: boolean
  dns_mode: DnsMode
  /** Set while the domain is being retired: nothing new can attach to it, and
   *  everything already on it keeps serving. Null means no. */
  retiring_at?: string | null
  created_at: string
  updated_at: string
}

/**
 * Whether a new route may use this domain: verified, and not being retired.
 *
 * One rule for every picker. A retiring domain keeps serving what it has, but
 * it leaves the pickers so nothing new attaches - that is what lets the list of
 * what still holds it only ever shrink. The API refuses the same thing, so a
 * picker that forgot would fail on submit rather than silently succeed.
 */
export function usableForNewRoutes(d: ApiDomain): boolean {
  return d.verified && !d.retiring_at
}

/** One hostname a domain carries, as the Domains page lists it. */
export interface ApiDomainRoute {
  id: string
  hostname: string
  subdomain: string
  zone: "public" | "internal" | "preview"
  published: boolean
  project_id: string
  project_name: string
  created_at: string
  /** Only meaningful for a custom domain, where ownership is proved per
   *  hostname rather than once for the zone. */
  verified: boolean
  /** Set when all this route does is send requests to another hostname - the
   *  redirect a move leaves behind on a retiring domain. */
  redirects_to?: string
}

/** A machine whose Tailscale client reaches Headscale through a domain's
 *  headscale name. */
export interface ApiControlNode {
  id: string
  name: string
  tailscale_ip: string
  status: string
  control_url: string
}

/** A registration at a git provider - a GitHub App's URLs, an OAuth app's
 *  redirect, repository push hooks - that still points at a domain. */
export interface ApiIntegrationRegistration {
  integration_id: string
  name: string
  provider: string
  kind: "github_app" | "oauth_redirect" | "push_hooks"
  /** Meshploy can change it through the provider's API. Otherwise it is
   *  changed at the provider by hand, then marked. */
  automatic: boolean
  urls: { label: string; current: string; new: string }[]
  repos?: string[]
}

export interface ApiHookMoveResult {
  moved: string[]
  failed: { repo: string; error: string }[]
}

/** A service whose CI deploy webhook was last called through a domain. */
export interface ApiDeployHook {
  service_id: string
  service_name: string
  project_id: string
  project_name: string
  host: string
  called_at: string | null
}

export const domains = {
  list: (orgId: string, token: string) =>
    apiFetch<ApiDomain[]>(`/api/v1/orgs/${orgId}/domains`, {}, token),

  get: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiDomain>(`/api/v1/orgs/${orgId}/domains/${domainId}`, {}, token),

  create: (orgId: string, body: { base_domain: string; dns_mode?: DnsMode }, token: string) =>
    apiFetch<ApiDomain>(
      `/api/v1/orgs/${orgId}/domains`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  verify: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiDomain>(
      `/api/v1/orgs/${orgId}/domains/${domainId}/verify`,
      { method: "POST" },
      token
    ),

  setDnsMode: (orgId: string, domainId: string, dnsMode: DnsMode, token: string) =>
    apiFetch<ApiDomain>(
      `/api/v1/orgs/${orgId}/domains/${domainId}/dns-mode`,
      { method: "PATCH", body: JSON.stringify({ dns_mode: dnsMode }) },
      token
    ),

  routes: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiDomainRoute[]>(`/api/v1/orgs/${orgId}/domains/${domainId}/routes`, {}, token),

  custom: (orgId: string, token: string) =>
    apiFetch<ApiDomainRoute[]>(`/api/v1/orgs/${orgId}/custom-domains`, {}, token),

  makePrimary: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiDomain>(`/api/v1/orgs/${orgId}/domains/${domainId}/make-primary`, { method: "POST" }, token),

  /** Nodes whose control connection goes through this domain's headscale. */
  nodes: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiControlNode[]>(`/api/v1/orgs/${orgId}/domains/${domainId}/nodes`, {}, token),

  /** Records that a node now reaches Headscale through the primary. */
  markNodeMoved: (orgId: string, nodeId: string, token: string) =>
    apiFetch<unknown>(`/api/v1/orgs/${orgId}/nodes/${nodeId}/control-moved`, { method: "POST" }, token),

  /** Git provider registrations that still point at this domain. */
  integrations: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiIntegrationRegistration[]>(`/api/v1/orgs/${orgId}/domains/${domainId}/integrations`, {}, token),

  moveHooks: (orgId: string, integrationId: string, token: string) =>
    apiFetch<ApiHookMoveResult>(`/api/v1/orgs/${orgId}/git-integrations/${integrationId}/move-hooks`, { method: "POST" }, token),

  markRegistrationUpdated: (
    orgId: string,
    integrationId: string,
    body: { kind: ApiIntegrationRegistration["kind"]; domain_id: string },
    token: string
  ) =>
    apiFetch<unknown>(
      `/api/v1/orgs/${orgId}/git-integrations/${integrationId}/registration-updated`,
      { method: "POST", body: JSON.stringify(body) },
      token
    ),

  /** Services whose CI deploy webhook was last called through this domain. */
  deployHooks: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiDeployHook[]>(`/api/v1/orgs/${orgId}/domains/${domainId}/deploy-hooks`, {}, token),

  /** Forget a deploy webhook's last caller, for a CI job that no longer exists. */
  forgetDeployHookCall: (orgId: string, serviceId: string, token: string) =>
    apiFetch<void>(`/api/v1/orgs/${orgId}/services/${serviceId}/deploy-hook-call`, { method: "DELETE" }, token),

  retire: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiDomain>(`/api/v1/orgs/${orgId}/domains/${domainId}/retire`, { method: "POST" }, token),

  stopRetiring: (orgId: string, domainId: string, token: string) =>
    apiFetch<ApiDomain>(`/api/v1/orgs/${orgId}/domains/${domainId}/retire`, { method: "DELETE" }, token),

  remove: (orgId: string, domainId: string, token: string) =>
    apiFetch<void>(
      `/api/v1/orgs/${orgId}/domains/${domainId}`,
      { method: "DELETE" },
      token
    ),
}
