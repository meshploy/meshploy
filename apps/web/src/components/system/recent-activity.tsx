import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Box, CircleCheck, CircleX, Clock, Database, Loader2, Timer } from "lucide-react"
import { activity as activityApi, type ApiActivityEntry } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"
import { originTitle } from "@/components/services/deployment-origin"

/**
 * What has happened in this workspace lately.
 *
 * The overview used to spend its largest panel on the mesh diagram, which says
 * the same thing every day on a mesh that does not change, and says it again
 * three panels lower where the nodes are listed with their status. It still
 * exists, on the cluster page, where someone goes to look at the mesh on
 * purpose.
 *
 * This answers the question a dashboard is actually opened for: what ran, and
 * did it work. Two things in Meshploy run and then either worked or did not - a
 * deployment and a job run - and both are here, interleaved by time. A failure
 * is the entry most worth seeing, so nothing filters by status: a cron that
 * failed at three in the morning is exactly what this panel is for.
 *
 * Scoped server-side to the projects the caller can see, the same way the
 * project list is: a feed is a good way to leak the names of projects somebody
 * was not given.
 */
export function RecentActivity() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const { data = [], isLoading } = useQuery({
    queryKey: ["activity", orgId],
    queryFn: () => activityApi.recent(orgId!, token, 12),
    enabled: !!orgId,
    // A deployment or a run in flight changes state on its own, so this is
    // worth refetching while someone is watching it.
    refetchInterval: 15_000,
  })

  return (
    <div className="quiet-surface flex h-full min-h-0 flex-col rounded-xl border border-border/60 bg-card overflow-hidden">
      <div className="px-4 py-3 border-b border-border/40 flex items-center justify-between shrink-0">
        <p className="text-sm font-semibold text-foreground">Recent activity</p>
        <span className="text-xs text-muted-foreground">Deployments and job runs</span>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto">
        {isLoading ? (
          <div className="flex items-center gap-2 px-4 py-8 text-sm text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" />
            Loading…
          </div>
        ) : data.length === 0 ? (
          <div className="flex h-full items-center justify-center px-4 text-sm text-muted-foreground">
            Nothing has run yet.
          </div>
        ) : (
          <div className="divide-y divide-border/30">
            {data.map((e) => (
              <ActivityRow key={`${e.kind}-${e.id}`} entry={e} />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function ActivityRow({ entry: e }: { entry: ApiActivityEntry }) {
  const Kind = e.resource_type === "job" ? Timer : e.resource_type === "database" ? Database : Box
  const body = (
    <>
      <StatusMark entry={e} />
      <Kind className="size-3.5 shrink-0 text-muted-foreground/60" />
      <div className="flex-1 min-w-40">
        <p className="text-sm font-medium leading-tight truncate">
          {e.resource_name}
          <span className="text-muted-foreground font-normal">
            {" "}in {e.project_name}
            {e.level ? ` (${e.level})` : ""}
          </span>
        </p>
        <p className="text-xs text-muted-foreground truncate">{describe(e)}</p>
      </div>
      <span className="text-xs text-muted-foreground shrink-0">
        {formatRelativeTime(new Date(e.finished_at ?? e.created_at))}
      </span>
    </>
  )
  const className =
    "flex flex-wrap items-center gap-3 px-4 py-2.5 hover:bg-secondary/40 transition-colors"

  // Each kind has its own page behind it, which is the reason the feed keeps
  // them as two kinds rather than flattening them into one row shape.
  if (e.kind === "job_run") {
    return (
      <Link
        to="/projects/$id/jobs/$jobId/runs"
        params={{ id: e.project_id, jobId: e.resource_id }}
        className={className}
      >
        {body}
      </Link>
    )
  }
  return (
    <Link
      to="/projects/$id/services/$serviceId/deployments/$deploymentId"
      params={{ id: e.project_id, serviceId: e.resource_id, deploymentId: e.id }}
      className={className}
    >
      {body}
    </Link>
  )
}

/** Done, broken, or still going. Three states is all a row of this size can
 *  carry; the record's own page has the detail. */
function StatusMark({ entry }: { entry: ApiActivityEntry }) {
  if (entry.status === "failed") return <CircleX className="size-4 shrink-0 text-destructive" />
  if (entry.status === "success" || entry.status === "running")
    return <CircleCheck className="size-4 shrink-0 text-primary" />
  // A cron waiting for its next fire is not in progress, so it does not spin.
  if (entry.status === "idle") return <Clock className="size-4 shrink-0 text-muted-foreground" />
  return <Loader2 className="size-4 shrink-0 animate-spin text-muted-foreground" />
}

/**
 * The two kinds share the words success and failed and disagree on the rest, so
 * each is read in its own vocabulary rather than in a common one that would be
 * wrong for both.
 */
function statusText(e: ApiActivityEntry): string {
  if (e.kind === "job_run") {
    switch (e.status) {
      case "failed":
        return "Run failed"
      case "success":
        return "Ran"
      case "running":
        return "Running"
      case "idle":
        return "Waiting for its schedule"
      default:
        return "Queued"
    }
  }
  switch (e.status) {
    case "failed":
      return "Deploy failed"
    case "success":
    case "running":
      return "Deployed"
    case "building":
      return "Building"
    case "deploying":
      return "Rolling out"
    default:
      return "Queued"
  }
}

/**
 * The line under the name: for a deployment that knows where its image came
 * from, that ("Promoted from staging · develop@4a1b9c2 Add the checkout
 * page"); otherwise its status and image, or a job's schedule.
 */
function describe(e: ApiActivityEntry) {
  if (e.kind === "deployment" && e.source && e.status !== "failed") {
    // "Built from develop" names the branch already; a promotion does not.
    const named = e.source === "build" || e.source === "redeploy"
    const commit = [named ? undefined : e.source_branch, e.source_commit?.slice(0, 7)].filter(Boolean).join("@")
    return [originTitle(e), commit && commit + (e.source_commit_message ? ` ${e.source_commit_message}` : "")]
      .filter(Boolean)
      .join(" · ")
  }
  return statusText(e) + (e.detail ? ` · ${e.detail}` : "")
}
