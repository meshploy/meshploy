import { http, HttpResponse } from "msw"
import { parse } from "yaml"
import {
  db,
  org,
  user,
  settings,
  record,
  now,
  projectCounts,
  buildConfigs,
  attachments,
  type DemoRecord,
} from "../state"
import {
  demoServiceApi,
  demoJob,
  demoVolume,
  demoStack,
  demoRoute,
  demoBuildConfig,
  DEMO_ORG_ID,
  DEMO_TOKEN,
  DEMO_SVC_DB,
} from "../data"

const O = "/api/v1/orgs/:orgId"
const P = `${O}/projects/:projectId`
const S = `${P}/services/:serviceId`
const json = (value: any, status = 200) => HttpResponse.json(value, { status })
const error = (message: string, status = 400) =>
  json({ detail: message }, status)
const ok = () => json({ message: "Saved in demo workspace" })
const missing = () => error("Resource not found in this demo workspace", 404)
const body = async (request: Request): Promise<Record<string, any>> => {
  try {
    return await request.json()
  } catch {
    return {}
  }
}
const find = (kind: string, id: unknown) => db[kind].find((r) => r.id === id)

// The one migration step the demo has running, if any, and until when.
let demoMigrationRun: { kind: string; until: number } | null = null
// Which groups each job has attached, for the demo workspace.
const jobGroups: Record<string, string[]> = {}
const defaults: Record<string, any> = {
  services: { ...demoServiceApi, status: "stopped", ports: [] },
  jobs: demoJob,
  volumes: { ...demoVolume, status: "idle" },
  stacks: demoStack,
  routes: { ...demoRoute, targets: [] },
  "tcp-routes": {
    service_id: null,
    service_port: 0,
    node_id: null,
    target_ip: "100.64.0.1",
    target_port: 31432,
    allowed_cidrs: [],
    status: "open",
    last_error: "",
    published: true,
    published_changed_at: null,
    published_changed_by: null,
    // A new port on a gateway whose ufw denies by default.
    host_firewall: { state: "blocked", tool: "ufw" },
  },
  "variable-groups": { description: "", system_managed: false, items: [] },
  "config-files": {
    stack_id: null,
    size: 0,
    services: [],
    attached_services: [],
  },
}
function sanitize(kind: string, row: DemoRecord) {
  if (kind === "projects") return projectCounts(row)
  const result = { ...row }
  for (const key of [
    "password",
    "token",
    "access_key_id",
    "secret_access_key",
    "content",
    "client_secret",
  ])
    if (key !== "token" || kind !== "invitations") delete result[key]
  if (kind === "variable-groups")
    result.items = row.items.map((i: DemoRecord) =>
      i.is_secret ? { ...i, value: undefined } : i
    )
  return result
}
function crud(kind: string, base: string, scoped = false) {
  return [
    http.get(base, ({ params }) => {
      const list = db[kind]
        .filter((r) => !scoped || r.project_id === params.projectId)
        .map((r) => sanitize(kind, r))
      return json(kind === "config-files" ? { files: list } : list)
    }),
    http.post(base, async ({ request, params }) => {
      const input = await body(request)
      if (input.name !== undefined && !input.name.trim())
        return error("A name is required")
      if (
        db[kind].some(
          (r) =>
            r.name === input.name &&
            input.name &&
            (!scoped || r.project_id === params.projectId)
        )
      )
        return error("That name is already in use", 409)
      const row = record({
        ...structuredClone(defaults[kind] ?? {}),
        ...input,
        id: crypto.randomUUID(),
        created_at: now(),
        updated_at: now(),
        organization_id: params.orgId ?? DEMO_ORG_ID,
        ...(scoped ? { project_id: params.projectId } : {}),
      })
      if (kind === "services") {
        row.type = input.type ?? "application"
        row.image =
          input.image ||
          (row.type === "database"
            ? `${input.engine || "postgres"}:${input.version || "16"}`
            : "demo/app:latest")
        row.ports = (input.ports ?? []).map((p: any) =>
          record({
            ...p,
            service_id: row.id,
            node_port: p.is_public ? 30010 + db.services.length : 0,
          })
        )
        if (input.git_repo)
          buildConfigs[row.id] = {
            ...demoBuildConfig,
            ...input,
            service_id: row.id,
          }
      }
      if (kind === "config-files") {
        row.size = new TextEncoder().encode(input.content || "").length
        delete row.content
      }
      if (kind === "invitations") {
        row.token =
          "demo-invite." +
          btoa(
            encodeURIComponent(
              JSON.stringify({
                email: input.email,
                role: input.role,
                org_name: org.name,
              })
            )
          )
            .replace(/=/g, "")
            .replace(/\+/g, "-")
            .replace(/\//g, "_")
        row.expires_at = new Date(Date.now() + 604800000).toISOString()
        row.org_id = params.orgId
      }
      if (kind === "members") {
        row.user_id = row.id
        row.user_email = input.email
        row.user_name = input.email.split("@")[0]
      }
      if (kind === "routes") {
        row.hostname =
          input.hostname ||
          `${input.subdomain}.${db.domains[0]?.base_domain || "demo.example.com"}`
        // A custom hostname arrives unproved, with a token to publish - as the
        // real API does - so the ownership step can be walked through offline.
        if (!input.domain_id) {
          row.domain_id = null
          row.custom_domain_verified = false
          row.custom_domain_verify_token = Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) =>
            b.toString(16).padStart(2, "0")
          ).join("")
        }
        row.targets = (input.targets || []).map((t: any) =>
          routeTarget(t, row.id)
        )
      }
      if (kind === "git-integrations") {
        row.connected = true
        row.auth_method = "pat"
      }
      if (kind === "notification-channels") row.enabled = true
      if (kind === "agents") {
        row.tokens = [
          record({
            name: input.token_name || "Default",
            token_prefix: "mpa_demo",
            expires_at: input.expires_at,
          }),
        ]
        db[kind].push(row)
        return json(
          { agent: row, token: `mpa_demo_${crypto.randomUUID()}` },
          201
        )
      }
      db[kind].push(row)
      if (kind === "services") deploy(row.id)
      return json(sanitize(kind, row), 201)
    }),
    http.get(`${base}/:resourceId`, ({ params }) => {
      const row = find(kind, params.resourceId)
      return row && (!scoped || row.project_id === params.projectId)
        ? json(sanitize(kind, row))
        : missing()
    }),
    ...[http.patch, http.put].map((method) =>
      method(`${base}/:resourceId`, async ({ params, request }) => {
        const row =
          find(kind, params.resourceId) ??
          (kind === "members"
            ? db.members.find((r) => r.user_id === params.resourceId)
            : undefined)
        if (!row) return missing()
        const input = await body(request)
        Object.assign(row, input, { updated_at: now() })
        if (kind === "config-files") {
          row.size = new TextEncoder().encode(input.content || "").length
          delete row.content
        }
        return json(sanitize(kind, row))
      })
    ),
    http.delete(`${base}/:resourceId`, ({ params }) => {
      const row =
        find(kind, params.resourceId) ??
        (kind === "members"
          ? db.members.find((r) => r.user_id === params.resourceId)
          : undefined)
      if (!row) return missing()
      if (kind === "config-files" && row.attached_services.length)
        return error(
          "Detach this file from its services before deleting it",
          409
        )
      if (kind === "volumes" && row.mounts.length)
        return error("Detach this volume before deleting it", 409)
      db[kind] = db[kind].filter((r) => r !== row)
      if (kind === "projects")
        for (const key of Object.keys(defaults))
          db[key] = db[key].filter((r) => r.project_id !== row.id)
      return ok()
    }),
  ]
}
function routeTarget(input: Record<string, any>, routeId: string) {
  const svc = find("services", input.service_id)
  const port = svc?.ports.find((p: any) => p.id === input.service_port_id)
  return record({
    path: "/",
    strip_path: false,
    service_id: null,
    node_id: null,
    target_ip: "100.64.0.2",
    redirect_route_id: null,
    redirect_code: 301,
    ...input,
    route_id: routeId,
    target_port: port?.port || input.port || input.target_port || 80,
  })
}
function deploy(serviceId: string) {
  const svc = find("services", serviceId)
  if (!svc) return null
  const deployment = record({
    service_id: serviceId,
    status: "pending",
    image: svc.image,
    log: "[demo] Deployment queued",
    deployed_at: null,
  })
  svc.status = "deploying"
  db.deployments.unshift(deployment)
  setTimeout(() => {
    deployment.status = "building"
    deployment.log += "\n[demo] Preparing image"
  }, 800)
  setTimeout(() => {
    deployment.status = "success"
    deployment.deployed_at = now()
    deployment.log += "\n[demo] Health checks passed. Deployment complete."
    svc.status = "running"
  }, 2200)
  return deployment
}
export function applyStack(stack: DemoRecord) {
  const spec = parse(stack.spec) || {}
  if (!spec.services || typeof spec.services !== "object")
    throw new Error("Add at least one service to the stack")
  const names: string[] = []
  for (const [name, definition] of Object.entries(spec.services) as [
    string,
    any,
  ][]) {
    let svc = db.services.find(
      (s) => s.stack_id === stack.id && s.name === name
    )
    if (!svc) {
      svc = record({
        ...demoServiceApi,
        id: crypto.randomUUID(),
        project_id: stack.project_id,
        stack_id: stack.id,
        name,
        ports: [],
        replicas: 1,
      })
      db.services.push(svc)
    }
    Object.assign(svc, {
      image: definition.image || "demo/app:latest",
      type: definition["x-meshploy"]?.type || "application",
      status: "running",
    })
    names.push(name)
  }
  stack.status = "running"
  stack.last_applied_at = now()
  return {
    stack,
    created: names,
    updated: [],
    deleted: [],
    errors: [],
    warnings: [],
    suggested_mode: stack.git_mode,
    warning: "",
  }
}

// Notification deliveries for the demo workspace. The seeded channel has been
// failing, as a gateway with a misconfigured provider would.
const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString()
const deliveries: DemoRecord[] = (() => {
  const channel = db["notification-channels"][0]
  if (!channel) return []
  const tls = "dial smtp.demo.meshploy.app:587: tls: first record does not look like a TLS handshake"
  const attempt = (m: number, event: string, success: boolean, data: Record<string, string>) =>
    record({ channel_id: channel.id, event, success, error: success ? "" : tls, test: false, data, created_at: minutesAgo(m) })
  return [
    attempt(4, "deploy.failed", false, { service: "api", project: "Storefront" }),
    attempt(38, "node.offline", false, { node: "worker-2" }),
    attempt(60 * 26, "deploy.success", true, { service: "worker", project: "Storefront", stack: "shop" }),
    // Enough history to need a second page.
    ...Array.from({ length: 55 }, (_, i) =>
      attempt(60 * 30 + i * 180, "deploy.success", true, { service: i % 2 ? "api" : "worker", project: "Storefront" })
    ),
  ]
})()
const sendDemo = (channel: DemoRecord, event: string, data: Record<string, string>, extra: Record<string, unknown> = {}) => {
  const provider = settings[`/api/v1/orgs/${channel.organization_id}/email-config`]
  const failure = channel.type === "email" && !provider?.host ? "no SMTP provider configured for this org" : ""
  const row = record({ channel_id: channel.id, event, data, success: !failure, error: failure, test: false, ...extra })
  deliveries.unshift(row)
  return row
}
const withStatus = (channel: DemoRecord) => {
  const mine = deliveries.filter((d) => d.channel_id === channel.id)
  const streak = mine.findIndex((d) => d.success)
  return { ...channel, last_delivery: mine[0] ?? null, failing_streak: streak === -1 ? mine.length : streak }
}

export const workspaceHandlers = [
  http.get(`${O}/notification-channels`, () =>
    json(db["notification-channels"].map(withStatus))
  ),
  http.post(`${O}/notification-channels/:channelId/test`, ({ params }) => {
    const channel = find("notification-channels", params.channelId)
    if (!channel) return missing()
    return json(sendDemo(channel, "notification.test", { detail: "Sent from the Meshploy console to check this channel works." }, { test: true }))
  }),
  http.get(`${O}/notification-channels/:channelId/deliveries`, ({ params, request }) => {
    const query = new URL(request.url).searchParams
    const failed = query.get("status") === "failed"
    const before = query.get("before")
    const limit = Number(query.get("limit") ?? 50)
    return json(
      deliveries
        .filter((d) => d.channel_id === params.channelId && (!failed || !d.success) && (!before || d.created_at < before))
        .slice(0, limit)
    )
  }),
  http.post(`${O}/notification-deliveries/:deliveryId/retry`, ({ params }) => {
    const original = deliveries.find((d) => d.id === params.deliveryId)
    const channel = original && find("notification-channels", original.channel_id)
    if (!original || !channel) return missing()
    return json(sendDemo(channel, original.event, original.data, { test: original.test, retry_of: original.id }))
  }),
  http.post(`${O}/email-config/test`, async ({ params, request }) => {
    const { to } = await body(request)
    const provider = settings[`/api/v1/orgs/${params.orgId}/email-config`]
    if (!provider?.host) return json({ success: false, error: "no email provider configured for this org" })
    return json({ success: true, error: "", to })
  }),
  http.get("/api/v1/invitations/:inviteToken", ({ params }) => {
    const token = String(params.inviteToken)
    if (!token.startsWith("demo-invite.")) return missing()
    try {
      return json(
        JSON.parse(
          decodeURIComponent(
            atob(token.slice(12).replace(/-/g, "+").replace(/_/g, "/"))
          )
        )
      )
    } catch {
      return missing()
    }
  }),
  http.post("/api/v1/invitations/:inviteToken/accept", ({ params }) =>
    String(params.inviteToken).startsWith("demo-invite.") ? ok() : missing()
  ),
  http.get("/api/v1/auth/status", () =>
    json({ registration_open: false, setup_required: false })
  ),
  http.get("/api/v1/me", () => json(user)),
  http.post("/api/v1/auth/register", async ({ request }) => {
    const b = await body(request)
    return json({ id: user.id, username: b.username, email: b.email }, 201)
  }),
  http.post("/api/v1/auth/recovery", () => json({ token: DEMO_TOKEN })),
  http.post("/api/v1/me/totp/enable", () => {
    user.totp_enabled = true
    return json({
      recovery_codes: ["demo-1111", "demo-2222", "demo-3333", "demo-4444"],
    })
  }),
  http.delete("/api/v1/me/totp", () => {
    user.totp_enabled = false
    return ok()
  }),
  http.post("/api/v1/me/recovery-codes/regenerate", () =>
    json({
      recovery_codes: ["demo-5555", "demo-6666", "demo-7777", "demo-8888"],
    })
  ),
  http.get("/api/v1/orgs", () => json([org])),
  http.get(O, () => json(org)),
  http.patch(O, async ({ request }) => {
    Object.assign(org, await body(request))
    return json(org)
  }),
  http.get(`${O}/nodes/:nodeId/metrics`, () => {
    const t = Date.now() / 1000
    return json({
      cpu_total_seconds: t * 4,
      cpu_idle_seconds: t * 3,
      cpu_cores: 4,
      memory_total_bytes: 8589934592,
      memory_available_bytes: 5368709120,
      disk_total_bytes: 85899345920,
      disk_avail_bytes: 60129542144,
      net_rx_bytes: t * 1200,
      net_tx_bytes: t * 800,
    })
  }),
  http.get(`${O}/node-registration-token`, () =>
    json({ token: "mreg-demo-not-valid-on-a-real-server" })
  ),
  http.post(`${O}/node-registration-token`, () =>
    json({ token: `mreg-demo-${crypto.randomUUID()}` })
  ),
  http.post(`${O}/node-provisioning-tokens`, async ({ request }) =>
    json(
      record({
        ...(await body(request)),
        token: `mprov-demo-${crypto.randomUUID()}`,
        organization_id: DEMO_ORG_ID,
        used_at: null,
      })
    )
  ),
  http.get("/api/v1/notification-events", () =>
    json([
      { event: "deploy.success", group: "Deploy", title: "Deployment succeeded", description: "A service finished deploying and is running. Covers builds, image deploys, rollbacks and databases.", tone: "good", recommended: false },
      { event: "deploy.failed", group: "Deploy", title: "Deployment failed", description: "A deploy did not finish: the build failed, or the workload never became ready.", tone: "bad", recommended: true },
      { event: "service.crashed", group: "Service", title: "Service crashed", description: "A service that was running stopped working on its own, with no deploy involved.", tone: "bad", recommended: true },
      { event: "service.recovered", group: "Service", title: "Service recovered", description: "A failing service started working again without a deploy.", tone: "good", recommended: false },
      { event: "job.success", group: "Job", title: "Job succeeded", description: "A job run finished with exit code 0.", tone: "good", recommended: false },
      { event: "job.failed", group: "Job", title: "Job failed", description: "A job run exited non-zero, or could not start.", tone: "bad", recommended: true },
      { event: "backup.success", group: "Backup", title: "Backup succeeded", description: "A scheduled or manual backup was written to storage.", tone: "good", recommended: false },
      { event: "backup.failed", group: "Backup", title: "Backup failed", description: "A backup did not reach storage. The last good copy is older than you think.", tone: "bad", recommended: true },
      { event: "backup.skipped", group: "Backup", title: "Backups not running", description: "A schedule came round for a database that is stopped, so there was nothing to back up. Sent once, not every night.", tone: "warning", recommended: true },
      { event: "restore.success", group: "Backup", title: "Restore succeeded", description: "A restore finished and the data is back.", tone: "good", recommended: false },
      { event: "restore.failed", group: "Backup", title: "Restore failed", description: "A restore did not finish. The service may be holding partial data.", tone: "bad", recommended: true },
      { event: "member.joined", group: "Organization", title: "Member joined", description: "Someone accepted an invitation and can now sign in to this organization.", tone: "warning", recommended: true },
      { event: "agent.token_created", group: "Organization", title: "Agent token created", description: "A token was minted for an agent. It can act on this organization until revoked.", tone: "warning", recommended: true },
      { event: "node.offline", group: "Node", title: "Node went offline", description: "A node stopped answering on the mesh. Its workloads reschedule only if another node can take them.", tone: "warning", recommended: true },
      { event: "node.online", group: "Node", title: "Node came back", description: "A node that was reported offline is answering again.", tone: "good", recommended: false },
    ])
  ),
  http.get(`${O}/routes`, () => json(db["routes"])),
  http.get(`${O}/tcp-routes`, () =>
    json({
      routes: db["tcp-routes"],
      reserved: [22, 53, 80, 443, 2019, 4000, 5000, 6443, 8081, 8085, 9090, 9100, 10250],
    })
  ),
  ...crud("projects", `${O}/projects`),
  ...Object.keys(defaults).flatMap((kind) => crud(kind, `${P}/${kind}`, true)),
  ...[
    "nodes",
    "members",
    "invitations",
    "agents",
    "git-integrations",
    "registry-integrations",
    "storage-integrations",
    "notification-channels",
    "domains",
  ].flatMap((kind) => crud(kind, `${O}/${kind}`)),
  http.delete(`${P}/build-cache`, ok),
  ...["start", "stop"].map((action) =>
    http.post(`${S}/${action}`, ({ params }) => {
      const svc = find("services", params.serviceId)
      if (!svc) return missing()
      svc.status = action === "start" ? "running" : "stopped"
      return json(svc)
    })
  ),
  http.get(`${S}/env-vars`, ({ params }) =>
    json({
      env_vars:
        find("services", params.serviceId)?.env_vars ||
        "NODE_ENV=production\nPORT=4000",
    })
  ),
  http.get(`${S}/build-config`, ({ params }) =>
    json(
      buildConfigs[String(params.serviceId)] ?? {
        ...demoBuildConfig,
        service_id: params.serviceId,
        git_repo: "",
        builder: "",
        last_built_image: "",
      }
    )
  ),
  http.patch(`${S}/build-config`, async ({ params, request }) => {
    const id = String(params.serviceId)
    buildConfigs[id] = {
      ...demoBuildConfig,
      ...buildConfigs[id],
      ...(await body(request)),
      service_id: id,
    }
    return json(buildConfigs[id])
  }),
  http.get(`${S}/build-config/env-vars`, ({ params }) =>
    json({
      build_env_vars:
        buildConfigs[String(params.serviceId)]?.build_env_vars || "",
    })
  ),
  http.put(`${S}/build-config/env-vars`, async ({ params, request }) => {
    const id = String(params.serviceId)
    buildConfigs[id] = {
      ...demoBuildConfig,
      ...buildConfigs[id],
      ...(await body(request)),
    }
    return json({ build_env_vars: buildConfigs[id].build_env_vars })
  }),
  http.post(`${S}/build-config/deploy-token`, ({ params }) => {
    const id = String(params.serviceId)
    buildConfigs[id] = {
      ...demoBuildConfig,
      ...buildConfigs[id],
      deploy_token: `dtkn-demo-${crypto.randomUUID()}`,
    }
    return json(buildConfigs[id])
  }),
  http.get(`${S}/deployments`, ({ params }) =>
    json(db.deployments.filter((d) => d.service_id === params.serviceId))
  ),
  // The overview's activity feed: deployments and job runs across projects,
  // joined to the names it shows and interleaved by time. The real one is
  // scoped to what the caller can see; in the demo there is one member and they
  // can see everything.
  http.get(`${O}/activity`, ({ request }) => {
    const limit = Number(new URL(request.url).searchParams.get("limit") ?? 20)

    const deployments = db.deployments.map((d) => {
      const svc = find("services", d.service_id)
      const project = svc ? find("projects", svc.project_id) : null
      return {
        kind: "deployment",
        id: d.id,
        status: d.status,
        detail: d.image,
        created_at: d.created_at,
        finished_at: d.deployed_at ?? null,
        resource_id: d.service_id,
        resource_name: svc?.name ?? "unknown",
        resource_type: svc?.type ?? "application",
        project_id: project?.id ?? "",
        project_name: project?.name ?? "unknown",
      }
    })

    const runs = db.runs.map((r: DemoRecord) => {
      const job = find("jobs", r.job_id)
      const project = job ? find("projects", job.project_id) : null
      return {
        kind: "job_run",
        id: r.id,
        status: r.status,
        detail: job?.schedule ?? "",
        created_at: r.created_at,
        finished_at: r.finished_at ?? null,
        resource_id: r.job_id,
        resource_name: job?.name ?? "unknown",
        resource_type: "job",
        project_id: project?.id ?? "",
        project_name: project?.name ?? "unknown",
      }
    })

    const rows = [...deployments, ...runs]
      .filter((e) => e.project_id !== "")
      .sort((a, b) => String(b.created_at).localeCompare(String(a.created_at)))
      .slice(0, limit)
    return json(rows)
  }),
  http.post(`${S}/deployments`, ({ params }) => {
    const d = deploy(String(params.serviceId))
    return d ? json(d, 201) : missing()
  }),
  http.get(`${S}/deployments/:deploymentId`, ({ params }) => {
    const d = find("deployments", params.deploymentId)
    return d ? json(d) : missing()
  }),
  ...["rollback", "retry"].map((action) =>
    http.post(`${S}/deployments/:deploymentId/${action}`, ({ params }) =>
      json(deploy(String(params.serviceId)))
    )
  ),
  ...["", "/record"].map((suffix) =>
    http.delete(`${S}/deployments/:deploymentId${suffix}`, ({ params }) => {
      db.deployments = db.deployments.filter(
        (d) => d.id !== params.deploymentId
      )
      return ok()
    })
  ),
  http.post(`${S}/reset`, ({ params }) =>
    json(deploy(String(params.serviceId)))
  ),
  http.get(`${S}/pods`, ({ params }) => {
    const s = find("services", params.serviceId)
    return json(
      s?.status === "running"
        ? Array.from({ length: s.replicas || 1 }, (_, i) => ({
            name: `${s.name}-6d8f4b9c7-demo${i}`,
            phase: "Running",
            ready: true,
            restarts: 0,
            node_name: "worker-1",
            started_at: now(),
          }))
        : []
    )
  }),
  http.get(`${S}/pods/metrics`, ({ params }) => {
    const s = find("services", params.serviceId)
    return json(
      s?.status === "running"
        ? Array.from({ length: s.replicas || 1 }, (_, i) => ({
            pod_name: `${s.name}-6d8f4b9c7-demo${i}`,
            cpu_millis: 40 + i * 6,
            memory_mib: 120 + i * 4,
          }))
        : []
    )
  }),
  http.get(`${S}/database-config`, ({ params }) => {
    const s = find("services", params.serviceId)
    return json({
      id: params.serviceId,
      service_id: params.serviceId,
      engine: s?.engine || "postgres",
      version: s?.version || "16",
      storage_gb: s?.storage_gb || 20,
      slug: "demo-db-4f21ac",
      db_name: s?.db_name || "demo",
      db_user: s?.db_user || "demo",
      db_password: "demo-password",
      mesh_exposed: true,
      node_port: 30003,
      nodeport_mesh_only: true,
    })
  }),
  http.patch(`${S}/database-config`, async ({ params, request }) => {
    const b = await body(request)
    const exposed = b.mesh_exposed ?? true
    const s = find("services", params.serviceId)
    return json({
      id: params.serviceId,
      service_id: params.serviceId,
      engine: s?.engine || "postgres",
      version: s?.version || "16",
      storage_gb: s?.storage_gb || 20,
      slug: "demo-db-4f21ac",
      db_name: s?.db_name || "demo",
      db_user: s?.db_user || "demo",
      db_password: "demo-password",
      mesh_exposed: exposed,
      node_port: exposed ? b.node_port || 30003 : 0,
      nodeport_mesh_only: true,
    })
  }),
  http.post(`${S}/db/query`, async ({ request }) => {
    const b = await body(request)
    if (!/^\s*(select|explain|show)/i.test(b.query || ""))
      return error(
        "The demo database explorer supports SELECT, SHOW, and EXPLAIN queries only"
      )
    return json({
      columns: ["id", "email", "created_at"],
      rows: [
        ["demo-1", "alex@example.com", now()],
        ["demo-2", "sam@example.com", now()],
      ],
      count: 2,
    })
  }),
  http.get(`${P}/jobs/:jobId/runs`, ({ params }) =>
    json(db.runs.filter((r) => r.job_id === params.jobId))
  ),
  http.post(`${P}/jobs/:jobId/trigger`, ({ params }) => {
    const run = record({
      job_id: params.jobId,
      status: "running",
      started_at: now(),
      finished_at: null,
      log: "[demo] Starting job…",
      k8s_job_name: "demo-job",
    })
    db.runs.unshift(run)
    const job = find("jobs", params.jobId)
    if (job) {
      job.status = "running"
      job.last_run_at = now()
    }
    setTimeout(() => {
      if (job) job.status = "idle"
      run.status = "success"
      run.finished_at = now()
      run.log += "\n[demo] Job completed successfully."
    }, 1800)
    return json(run)
  }),
  http.delete(`${P}/jobs/:jobId/runs/:runId`, ({ params }) => {
    db.runs = db.runs.filter((r) => r.id !== params.runId)
    return ok()
  }),
  http.get(`${P}/stacks/:stackId/services`, ({ params }) =>
    json(db.services.filter((s) => s.stack_id === params.stackId))
  ),
  ...["apply", "sync"].map((action) =>
    http.post(`${P}/stacks/:stackId/${action}`, ({ params }) => {
      const stack = find("stacks", params.stackId)
      if (!stack) return missing()
      try {
        return json(applyStack(stack))
      } catch (e) {
        return error((e as Error).message)
      }
    })
  ),
  http.post(`${P}/stacks/:stackId/destroy`, ({ params }) => {
    const stack = find("stacks", params.stackId)
    if (!stack) return missing()
    const removed = db.services
      .filter((s) => s.stack_id === stack.id)
      .map((s) => s.name)
    db.services = db.services.filter((s) => s.stack_id !== stack.id)
    stack.status = "destroyed"
    return json({
      stack,
      destroyed: removed,
      volumes: [],
      routes: [],
      errors: [],
    })
  }),
  http.put(
    `${P}/variable-groups/:groupId/items`,
    async ({ params, request }) => {
      const group = find("variable-groups", params.groupId)
      if (!group) return missing()
      const b = await body(request)
      const item =
        group.items.find((i: any) => i.key === b.key) ??
        record({ group_id: group.id })
      if (!group.items.includes(item)) group.items.push(item)
      Object.assign(item, b)
      return json(b.is_secret ? { ...item, value: undefined } : item)
    }
  ),
  // A secret's value is asked for one at a time, never carried in a list.
  http.get(`${P}/variable-groups/:groupId/items/:itemId/value`, ({ params }) => {
    const g = find("variable-groups", params.groupId)
    const item = g?.items.find((i: any) => i.id === params.itemId)
    if (!item) return missing()
    return json({ key: item.key, value: item.value ?? "demo-secret-value" })
  }),
  http.delete(`${P}/variable-groups/:groupId/items/:itemId`, ({ params }) => {
    const g = find("variable-groups", params.groupId)
    if (!g) return missing()
    g.items = g.items.filter((i: any) => i.id !== params.itemId)
    return ok()
  }),
  http.get(`${S}/variable-groups`, ({ params }) =>
    json(
      db["variable-groups"]
        .filter((g) =>
          (attachments[String(params.serviceId)] || []).includes(g.id)
        )
        .map((g) => sanitize("variable-groups", g))
    )
  ),
  http.post(`${S}/variable-groups`, async ({ params, request }) => {
    const b = await body(request)
    const id = String(params.serviceId)
    attachments[id] = [...new Set([...(attachments[id] || []), b.group_id])]
    return ok()
  }),
  http.delete(`${S}/variable-groups/:groupId`, ({ params }) => {
    const id = String(params.serviceId)
    attachments[id] = (attachments[id] || []).filter(
      (g) => g !== params.groupId
    )
    return ok()
  }),
  ...[http.post, http.delete].map((method) =>
    method(
      `${P}/config-files/:fileId/attach/:serviceId`,
      ({ params, request }) => {
        const f = find("config-files", params.fileId)
        const s = find("services", params.serviceId)
        if (!f || !s) return missing()
        if (request.method === "POST") {
          if (!f.attached_services.some((a: any) => a.id === s.id))
            f.attached_services.push({ id: s.id, name: s.name })
        } else
          f.attached_services = f.attached_services.filter(
            (a: any) => a.id !== s.id
          )
        f.services = f.attached_services.map((a: any) => a.name)
        return ok()
      }
    )
  ),
  http.get(`${S}/mounts`, ({ params }) =>
    json(
      db.volumes.flatMap((v) =>
        v.mounts
          .filter((m: any) => m.service_id === params.serviceId)
          .map((m: any) => ({ ...m, volume: { ...v, mounts: undefined } }))
      )
    )
  ),
  http.get(`${P}/volumes/:volumeId/placement`, ({ params }) => {
    const v = find("volumes", params.volumeId)
    return json({
      exists: !!v?.mounts.length,
      phase: v?.mounts.length ? "Bound" : "Pending",
      bound: !!v?.mounts.length,
      node: v?.mounts.length ? "worker-1" : "",
    })
  }),
  http.put(`${P}/volumes/:volumeId/node`, async ({ params, request }) => {
    const v = find("volumes", params.volumeId)
    if (!v) return missing()
    Object.assign(v, await body(request))
    return json(v)
  }),
  http.post(`${P}/volumes/:volumeId/mounts`, async ({ params, request }) => {
    const v = find("volumes", params.volumeId)
    if (!v) return missing()
    const m = record({ ...(await body(request)), volume_id: v.id })
    v.mounts.push(m)
    v.status = "ready"
    return json(m)
  }),
  http.delete(`${P}/volumes/:volumeId/mounts/:mountId`, ({ params }) => {
    const v = find("volumes", params.volumeId)
    if (!v) return missing()
    v.mounts = v.mounts.filter((m: any) => m.id !== params.mountId)
    return ok()
  }),
  http.get(`${P}/jobs/:jobId/variable-groups`, ({ params }) =>
    json(
      (jobGroups[params.jobId as string] ?? [])
        .map((id) => find("variable-groups", id))
        .filter(Boolean)
    )
  ),
  http.post(`${P}/jobs/:jobId/variable-groups`, async ({ params, request }) => {
    const b = await body(request)
    const jobId = params.jobId as string
    const ids = jobGroups[jobId] ?? (jobGroups[jobId] = [])
    if (b.group_id && !ids.includes(b.group_id)) ids.push(b.group_id)
    return ok()
  }),
  http.delete(`${P}/jobs/:jobId/variable-groups/:groupId`, ({ params }) => {
    const jobId = params.jobId as string
    jobGroups[jobId] = (jobGroups[jobId] ?? []).filter((id) => id !== params.groupId)
    return ok()
  }),
  ...(["routes", "tcp-routes"] as const).flatMap((kind) =>
    (["publish", "pause"] as const).map((action) =>
      http.post(`${P}/${kind}/:routeId/${action}`, ({ params }) => {
        const r = find(kind, params.routeId)
        if (!r) return missing()
        r.published = action === "publish"
        r.published_changed_at = now()
        if (kind === "tcp-routes") r.status = r.published ? "open" : "paused"
        return json(r)
      })
    )
  ),
  http.get(`${P}/routes/:routeId/targets`, ({ params }) =>
    json(find("routes", params.routeId)?.targets || [])
  ),
  http.post(`${P}/routes/:routeId/targets`, async ({ params, request }) => {
    const r = find("routes", params.routeId)
    if (!r) return missing()
    const t = routeTarget(await body(request), r.id)
    r.targets.push(t)
    return json(t)
  }),
  http.patch(
    `${P}/routes/:routeId/targets/:targetId`,
    async ({ params, request }) => {
      const r = find("routes", params.routeId)
      const t = r?.targets.find((t: any) => t.id === params.targetId)
      if (!t) return missing()
      Object.assign(t, routeTarget(await body(request), r!.id), { id: t.id })
      return json(t)
    }
  ),
  http.delete(`${P}/routes/:routeId/targets/:targetId`, ({ params }) => {
    const r = find("routes", params.routeId)
    if (!r) return missing()
    r.targets = r.targets.filter((t: any) => t.id !== params.targetId)
    return ok()
  }),
  http.post(`${O}/agents/:agentId/tokens`, async ({ params, request }) => {
    const a = find("agents", params.agentId)
    if (!a) return missing()
    const metadata = record({
      ...(await body(request)),
      token_prefix: "mpa_demo",
    })
    a.tokens.push(metadata)
    return json({ metadata, token: `mpa_demo_${crypto.randomUUID()}` })
  }),
  http.delete(`${O}/agents/:agentId/tokens/:tokenId`, ({ params }) => {
    const a = find("agents", params.agentId)
    const t = a?.tokens.find((t: any) => t.id === params.tokenId)
    if (!t) return missing()
    t.revoked_at = now()
    return ok()
  }),
  http.get(`${O}/git-integrations/:integrationId/repos`, () =>
    json([
      { full_name: "demo/api", default_branch: "main", private: true },
      { full_name: "demo/web", default_branch: "main", private: false },
    ])
  ),
  // What a provider needs to deliver pushes, and whether the hook is on the
  // repository. The demo starts with it missing, so the "Add it" button has
  // something to do.
  ...(() => {
    const installed = new Set<string>()
    const details = (integration: any, repo: string | null) => {
      const field: Record<string, [string, string]> = {
        gitlab: ["Secret token", "Push events"],
        gitea: ["Secret", "Push"],
        bitbucket: ["Secret", "Repository push"],
      }
      const [secret_field, event] = field[integration.provider as string] ?? ["Secret", "Push"]
      return {
        provider: integration.provider,
        url: `${window.location.origin}/api/v1/webhooks/git/${integration.provider}/${integration.id}`,
        secret: "demo-secret-0123456789abcdef0123456789abcdef",
        secret_field,
        event,
        state: !repo ? "unknown" : installed.has(`${integration.id}:${repo}`) ? "installed" : "missing",
      }
    }
    return [
      http.get(`${O}/git-integrations/:integrationId/push-hook`, ({ params, request }) => {
        const integration = find("git-integrations", params.integrationId)
        if (!integration) return missing()
        const repo = new URL(request.url).searchParams.get("repo")
        if (integration.provider === "github") {
          // The App holds the URL and secret; what can be wrong is which
          // repositories it was installed on.
          return json({
            provider: "github", url: "", secret: "", secret_field: "", event: "",
            state: repo ? "installed" : "unknown",
          })
        }
        return json(details(integration, repo))
      }),
      http.post(`${O}/git-integrations/:integrationId/push-hook`, async ({ params, request }) => {
        const integration = find("git-integrations", params.integrationId)
        if (!integration) return missing()
        const body = (await request.json()) as { repo?: string }
        if (!body.repo) return error("name the repository to add the webhook to", 400)
        installed.add(`${integration.id}:${body.repo}`)
        return json(details(integration, body.repo), 201)
      }),
    ]
  })(),
  http.get(`${O}/git-integrations/:integrationId/branches`, () =>
    json(["main", "develop", "staging"])
  ),
  ...["github", "oauth"].map((action) =>
    http.post(`${O}/git-integrations/${action}`, () =>
      error(
        "External authorization requires a connected server. Use the preconfigured Demo GitLab integration to explore deployment.",
        409
      )
    )
  ),
  ...["install-url", "oauth-reconnect"].map((action) =>
    http.get(`${O}/git-integrations/:integrationId/${action}`, () =>
      error("External authorization is unavailable in the browser demo", 409)
    )
  ),
  http.get(`${O}/cluster/mesh-health`, () =>
    json({
      configured: true,
      checked: true,
      healthy: true,
      unauthorized: false,
      last_success_at: now(),
    })
  ),
  http.get(`${O}/cluster/orphans`, () => json({ orphans: [] })),
  http.get("/api/v1/entitlements", () =>
    json({
      licensed: false,
      expired: false,
      features: [],
      node_count: db.nodes.length,
      over_limit: false,
      can_activate: false,
      edition: "community",
    })
  ),
  http.post("/api/v1/entitlements/license", () =>
    error("License activation requires a connected server", 409)
  ),
  http.get("/api/v1/system/version", () =>
    json({
      current: "v0.11.0-demo",
      latest: "v0.11.0-demo",
      channel: "dev",
      update_available: false,
      release_url: "",
    })
  ),
  http.get("/api/v1/system/host-agent", () =>
    json({
      reporting: true,
      version: "0.16.0-demo",
      started_at: now(),
      heartbeat_at: now(),
      tasks: { firewall: { ok: true, at: now() } },
      firewall: "ufw",
    })
  ),
  // A server part-way through migrating: prepared, one group moved, one still
  // to go. Enough to see every state the page has.
  http.get("/api/v1/system/migrate/dokploy", () =>
    json({
      agent_reporting: true,
      detect: { dokploy: true, version: "v0.30.7" },
      plan: { generated_at: now() },
      prepare: { created: 9, skipped: 0 },
      prepare_at: now(),
      move: { group: "g-shop", moved: true, downtime: "13s" },
      move_at: now(),
      cutover: null,
      rollback: null,
      finish: null,
      status: {
        updated_at: now(),
        prepared: true,
        cut_over: false,
        finished: false,
        groups: [
          {
            id: "g-shop", name: "shop / shopdb with shop-admin",
            members: ["application shop-admin", "database shopdb"],
            domains: ["shop.example.com"], data: ["shopdb (dump and restore)"],
            moved: true, moved_at: now(), can_move: true,
            downtime: "a restart plus under a minute to copy data",
          },
          {
            id: "g-docs", name: "shop / docs",
            members: ["application docs"], domains: ["docs.example.com"],
            moved: false, can_move: true,
            downtime: "a restart, usually under a minute",
          },
          {
            id: "g-legacy", name: "shop / legacy",
            members: ["application legacy"], domains: ["legacy.example.com", "old.example.com"],
            moved: false, can_move: false,
            blockers: ["legacy: its certificate was uploaded by hand - upload it here, or let Meshploy issue one"],
            downtime: "a restart, usually under a minute",
          },
        ],
      },
      status_at: now(),
      requests: {
        "migrate.prepare": { id: "req-1", state: "succeeded", requested_at: now() },
        // A step started from the page runs for a few seconds, the way one on
        // a real server takes a while, so a running step can be seen.
        ...(demoMigrationRun && Date.now() < demoMigrationRun.until
          ? { [`migrate.${demoMigrationRun.kind}`]: { id: "req-demo", state: "running", requested_at: now() } }
          : {}),
      },
    })
  ),
  http.post("/api/v1/system/migrate/dokploy/:kind", ({ params }) => {
    demoMigrationRun = { kind: String(params.kind), until: Date.now() + 6_000 }
    return json({ id: "req-demo", state: "queued", requested_at: now() }, 202)
  }),
  http.post("/api/v1/system/check-updates", () =>
    json({
      current: "v0.11.0-demo",
      latest: "v0.11.0-demo",
      channel: "dev",
      update_available: false,
      release_url: "",
      checked_at: now(),
    })
  ),
  http.get("/api/v1/system/exposure", () =>
    json({
      firewall_state: "ufw",
      checked_at: "2026-09-10T08:00:00Z",
      ports: [],
      dismissed: true,
      // The demo gateway manages its own DNS, so the internal-route notice is
      // reachable here instead of only on a real ondemand server.
      dns_mode: "ondemand",
    })
  ),
  // Nothing dismissed, so the demo can be pointed at /?start=1 to see the
  // getting-started panel the way a fresh install gets it.
  http.get("/api/v1/system/notices", () => json({ dismissed: [] })),
  http.post("/api/v1/system/notices/:key/dismiss", ok),
  http.get("/api/v1/system/channels", () =>
    json({
      current: { version: "v0.11.0-demo", channel: "", commit: "" },
      stable: { releases: [] },
      edge: { ahead_by: 0, commits: [] },
      unavailable: "Release information is unavailable in the browser demo.",
    })
  ),
  http.post("/api/v1/terminal/ticket", () =>
    json({
      ticket: "demo-only",
      expires_at: new Date(Date.now() + 60000).toISOString(),
    })
  ),
]

// Singleton settings, permission grants, and backup configurations retain edits too.
for (const endpoint of [
  `${O}/email-config`,
  `${O}/system-backup`,
  `${P}/volumes/:volumeId/backup`,
]) {
  workspaceHandlers.push(
    http.get(endpoint, ({ request }) =>
      json(settings[new URL(request.url).pathname] ?? null)
    ),
    http.put(endpoint, async ({ request }) => {
      const key = new URL(request.url).pathname
      settings[key] = record({ ...settings[key], ...(await body(request)) })
      delete settings[key].password
      return json(settings[key])
    }),
    http.delete(endpoint, ({ request }) => {
      delete settings[new URL(request.url).pathname]
      return ok()
    })
  )
}
workspaceHandlers.push(
  http.get(`${S}/backups`, ({ params }) =>
    json(db.backups.filter((b) => b.service_id === params.serviceId))
  ),
  http.post(`${S}/backups`, async ({ params, request }) => {
    const b = record({
      ...(await body(request)),
      service_id: params.serviceId,
      enabled: true,
      last_backup_at: null,
      last_backup_status: null,
    })
    db.backups.push(b)
    return json(b)
  }),
  http.patch(`${S}/backups/:backupId`, async ({ params, request }) => {
    const b = find("backups", params.backupId)
    if (!b) return missing()
    Object.assign(b, await body(request))
    return json(b)
  }),
  http.delete(`${S}/backups/:backupId`, ({ params }) => {
    db.backups = db.backups.filter((b) => b.id !== params.backupId)
    return ok()
  }),
  http.post(`${S}/backups/:backupId/trigger`, ({ params }) => {
    const b = find("backups", params.backupId)
    if (!b) return missing()
    b.last_backup_status = "success"
    b.last_backup_at = now()
    return json(b)
  }),
  http.get(`${S}/backups/:backupId/objects`, ({ params }) =>
    json(
      find("backups", params.backupId)?.last_backup_at
        ? [{ key: "demo/backup.sql.gz", size: 24832, last_modified: now() }]
        : []
    )
  ),
  http.post(`${S}/backups/:backupId/restore`, ok),
  http.get(`${O}/system-backup/objects`, () => json([])),
  http.post(`${O}/system-backup/trigger`, ({ request }) => {
    const key = new URL(request.url).pathname.replace(/\/trigger$/, "")
    if (!settings[key]) return error("Configure a backup first")
    settings[key].last_backup_at = now()
    settings[key].last_backup_status = "success"
    return json(settings[key])
  }),
  http.post(`${O}/system-backup/restore`, ok),
  http.get(`${O}/members/:userId/permissions`, ({ params }) =>
    json(db.permissions.filter((p) => p.user_id === params.userId))
  ),
  http.post(`${O}/members/:userId/permissions`, async ({ params, request }) => {
    const p = record({ ...(await body(request)), user_id: params.userId })
    if (
      !db.permissions.some(
        (e) =>
          e.user_id === p.user_id &&
          e.resource_id === p.resource_id &&
          e.action === p.action
      )
    )
      db.permissions.push(p)
    return ok()
  }),
  http.delete(
    `${O}/members/:userId/permissions`,
    async ({ params, request }) => {
      const b = await body(request)
      db.permissions = db.permissions.filter(
        (p) =>
          !(
            p.user_id === params.userId &&
            p.resource_id === b.resource_id &&
            p.action === b.action
          )
      )
      return ok()
    }
  ),
  http.get(`${O}/:resourceType/:resourceId/permissions`, ({ params }) =>
    json(
      db.permissions
        .filter((p) => p.resource_id === params.resourceId)
        .map((p) => ({
          ...p,
          ...db.members.find((m) => m.user_id === p.user_id),
        }))
    )
  )
)
// Seed a database backup without implying any external storage is connected.
db.backups.push(
  record({
    service_id: DEMO_SVC_DB,
    storage_integration_id: db["storage-integrations"][0].id,
    schedule: "0 2 * * *",
    retention_days: 7,
    enabled: true,
    last_backup_at: now(),
    last_backup_status: "success",
  })
)
