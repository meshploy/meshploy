import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  ArrowRight,
  BookOpen,
  Check,
  Globe,
  Hammer,
  LayoutGrid,
  Radar,
  Server,
  Terminal,
  TriangleAlert,
  X,
} from "lucide-react"
import { NOTICE_GETTING_STARTED, domains as domainsApi, system as systemApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import type { Node, Project } from "@/types"
import { Button } from "@/components/ui/button"

/**
 * What to do first, on a workspace that has not done it yet.
 *
 * This is the whole of onboarding: no tour, no wizard, no separate page. The
 * overview is the getting-started screen while the workspace is empty and stops
 * being one when it isn't, which is the same rule the sidebar uses for
 * Migration and the exposure notice uses for itself.
 *
 * Every line of it is derived from what the workspace actually is - nodes,
 * projects, the domain record - and nothing is remembered. So it cannot
 * congratulate anyone for a service they have since deleted, it needs no
 * bookkeeping, and if someone empties the workspace it comes back, which is
 * right: they are at the beginning again.
 *
 * The readiness block is the part worth having. The first deployment fails in
 * three ways and two of them are invisible from here: a domain that was never
 * pointed at this gateway, and no node that can build from source. Saying so
 * before ten minutes are spent is worth more than the rest of this panel.
 */

const GUIDE_URL = "https://docs.meshploy.com/guides/deploy-first-application/"

export function StartHere({
  nodes,
  projects,
  forced,
}: {
  nodes: Node[]
  projects: Project[]
  /** Render even on a workspace that is past this, and past a dismissal. See
   *  the overview's `start` search param: it exists so this can be looked at
   *  without emptying a real workspace, and so a support answer can point at
   *  it. */
  forced?: boolean
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const qc = useQueryClient()

  const services = projects.reduce((n, p) => n + p.servicesCount, 0)
  const routes = projects.reduce((n, p) => n + p.routesCount, 0)
  const done = services > 0 && routes > 0
  const wanted = !done || !!forced

  const { data: notices } = useQuery({
    queryKey: ["dismissed-notices"],
    queryFn: () => systemApi.dismissedNotices(token),
    enabled: !!token && wanted,
    staleTime: Infinity,
  })

  const { data: domainList = [] } = useQuery({
    queryKey: ["domains", orgId],
    queryFn: () => domainsApi.list(orgId!, token),
    enabled: !!orgId && wanted,
  })

  const dismiss = useMutation({
    mutationFn: () => systemApi.dismissNotice(NOTICE_GETTING_STARTED, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["dismissed-notices"] }),
  })

  if (!wanted) return null
  // Wait for the answer rather than flashing the panel at someone who put it
  // away. `forced` ignores the dismissal, which is the point of it.
  if (!forced && (!notices || notices.dismissed.includes(NOTICE_GETTING_STARTED))) return null

  const online = nodes.filter((n) => n.status === "online")
  const canRun = online.filter((n) => n.meshRole === "workload_builder" || n.meshRole === "workload")
  const canBuild = online.filter((n) => n.meshRole === "workload_builder" || n.meshRole === "builder")
  const verified = domainList.filter((d) => d.verified)

  const next = nextStep(projects)

  return (
    <section className="quiet-surface rounded-xl border border-border bg-card overflow-hidden">
      <div className="flex items-start gap-3 border-b border-border/60 px-5 py-4">
        <div className="flex-1 min-w-0">
          <h2 className="text-sm font-semibold">Start here</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Get one application running and served over HTTPS. This panel goes away on its own once
            a service has a route.
          </p>
        </div>
        <button
          type="button"
          onClick={() => dismiss.mutate()}
          disabled={dismiss.isPending}
          aria-label="Hide the getting started panel"
          className="shrink-0 rounded p-1 text-muted-foreground/60 hover:text-foreground hover:bg-secondary/60 disabled:opacity-50"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* Readiness: the three things that make a first deployment fail. */}
      <div className="divide-y divide-border/40">
        <Readiness
          ok={canRun.length > 0}
          icon={<Server className="h-3.5 w-3.5" />}
          okText="Somewhere to run it"
          okDetail={`${canRun.length} node${canRun.length === 1 ? "" : "s"} accepting workloads`}
          warnText="No node is accepting workloads"
          warnDetail="A deployment has nowhere to be scheduled until one is online and set to run workloads."
          to="/nodes"
          action="Review nodes"
        />
        <Readiness
          ok={canBuild.length > 0}
          icon={<Hammer className="h-3.5 w-3.5" />}
          okText="Somewhere to build it"
          okDetail={`${canBuild.length} node${canBuild.length === 1 ? "" : "s"} can build from source`}
          warnText="No node can build from source"
          warnDetail="Deploying from Git will fail until one can. Deploying an existing image still works."
          to="/nodes"
          action="Review nodes"
        />
        <Readiness
          ok={verified.length > 0}
          icon={<Globe className="h-3.5 w-3.5" />}
          okText="Your domain points here"
          okDetail={verified[0]?.base_domain ?? ""}
          warnText={
            domainList.length > 0
              ? `${domainList[0].base_domain} is not verified yet`
              : "No domain is configured"
          }
          warnDetail="A route will not serve over HTTPS until DNS reaches this gateway and the domain is verified."
          to="/settings"
          hash="domains"
          action="Set up the domain"
        />
      </div>

      {/* The next real step, wherever this workspace has got to. */}
      <div className="flex flex-wrap items-center gap-4 border-t border-border/60 bg-secondary/20 px-5 py-4">
        <div className="flex-1 min-w-48">
          <p className="text-sm font-medium">{next.title}</p>
          <p className="mt-0.5 text-xs text-muted-foreground">{next.detail}</p>
        </div>
        <Button
          size="sm"
          render={
            next.params ? (
              <Link to={next.to} params={next.params} search={next.search} />
            ) : (
              <Link to={next.to} />
            )
          }
        >
          {next.action}
          <ArrowRight className="ml-1 h-3.5 w-3.5" />
        </Button>
      </div>

      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 px-5 py-3 text-xs">
        <span className="text-muted-foreground/70">Or start from</span>
        <Link to="/templates" className="flex items-center gap-1.5 text-muted-foreground hover:text-foreground">
          <LayoutGrid className="h-3.5 w-3.5" />
          A template
          <span className="text-muted-foreground/60">- one click, proves the whole path works</span>
        </Link>
        <Link to="/discovery" className="flex items-center gap-1.5 text-muted-foreground hover:text-foreground">
          <Radar className="h-3.5 w-3.5" />
          What is already running on this machine
        </Link>
      </div>

      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 border-t border-border/40 px-5 py-3 text-xs">
        <a
          href={GUIDE_URL}
          target="_blank"
          rel="noreferrer"
          className="flex items-center gap-1.5 text-muted-foreground hover:text-foreground"
        >
          <BookOpen className="h-3.5 w-3.5" />
          Read the first-deployment guide
        </a>
        <span className="flex items-center gap-1.5 text-muted-foreground/70">
          <Terminal className="h-3.5 w-3.5" />
          Or do all of this from a terminal:
          <code className="font-mono text-foreground/80">meshploy login</code>
        </span>
      </div>
    </section>
  )
}

/** One readiness line: green when it holds, amber with a way to fix it when it
 *  does not. Never a blocker - a workspace is allowed to be half-ready. */
function Readiness({
  ok,
  icon,
  okText,
  okDetail,
  warnText,
  warnDetail,
  to,
  hash,
  action,
}: {
  ok: boolean
  icon: React.ReactNode
  okText: string
  okDetail: string
  warnText: string
  warnDetail: string
  to: "/nodes" | "/settings"
  hash?: string
  action: string
}) {
  return (
    <div className="flex flex-wrap items-center gap-3 px-5 py-3">
      <span
        className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-full ${
          ok ? "bg-primary/10 text-primary" : "bg-amber-500/10 text-amber-400"
        }`}
      >
        {ok ? <Check className="h-3 w-3" /> : <TriangleAlert className="h-3 w-3" />}
      </span>
      <span className="text-muted-foreground/60">{icon}</span>
      <div className="flex-1 min-w-48">
        <p className={`text-sm ${ok ? "text-foreground/90" : "text-amber-400"}`}>
          {ok ? okText : warnText}
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">{ok ? okDetail : warnDetail}</p>
      </div>
      {!ok && (
        <Link
          to={to}
          hash={hash}
          className="text-xs text-primary hover:underline shrink-0"
        >
          {action} →
        </Link>
      )}
    </div>
  )
}

type Step = {
  title: string
  detail: string
  action: string
  to: "/projects/new" | "/projects/$id/new" | "/projects/$id/routes"
  params?: { id: string }
  search?: { type: "service" }
}

/** Where this workspace actually is on the path, which is the only honest way
 *  to say what to do next. */
function nextStep(projects: Project[]): Step {
  const withServices = projects.find((p) => p.servicesCount > 0)
  if (withServices && withServices.routesCount === 0) {
    return {
      title: "Give it a hostname",
      detail: "A service runs but nothing reaches it yet. Publish a route and Meshploy gets the certificate.",
      action: "Publish a route",
      to: "/projects/$id/routes",
      params: { id: withServices.id },
    }
  }
  const empty = projects.find((p) => p.servicesCount === 0)
  if (empty) {
    return {
      title: `Add the first service to ${empty.name}`,
      detail: "Build from a Git repository, or run an image you already have.",
      action: "New service",
      to: "/projects/$id/new",
      params: { id: empty.id },
      search: { type: "service" },
    }
  }
  return {
    title: "Create a project",
    detail: "A project holds the services, routes and volumes that belong together.",
    action: "New project",
    to: "/projects/new",
  }
}
