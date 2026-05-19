import { createFileRoute, Link, Outlet, useNavigate, useParams } from "@tanstack/react-router"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Clock, Loader2, Play, ServerCrash, Trash2, Zap } from "lucide-react"
import { jobs as jobsApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { DetailPageHeader, tabLinkCls } from "@/components/layout/detail-page-header"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/jobs/$jobId")({
  component: JobLayout,
})

const STATUS_STYLES: Record<string, string> = {
  idle:    "bg-muted/30 text-muted-foreground border-border/40",
  pending: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  running: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  success: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  failed:  "bg-destructive/10 text-destructive border-destructive/20",
}

const STATUS_DOT: Record<string, string> = {
  idle:    "bg-muted-foreground/50",
  pending: "bg-amber-400 animate-pulse",
  running: "bg-amber-400 animate-pulse",
  success: "bg-emerald-400",
  failed:  "bg-destructive",
}

function JobLayout() {
  const { id: projectId, jobId } = useParams({ from: "/_app/projects/$id/jobs/$jobId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const qc = useQueryClient()

  const { data: job, isLoading, isError } = useQuery({
    queryKey: ["job", orgId, projectId, jobId],
    queryFn: () => jobsApi.get(orgId, projectId, jobId, token),
    enabled: !!orgId,
    refetchInterval: (query) => {
      const d = query.state.data
      return d?.status === "running" || d?.status === "pending" ? 3000 : false
    },
  })

  const triggerMut = useMutation({
    mutationFn: () => jobsApi.trigger(orgId, projectId, jobId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["job", orgId, projectId, jobId] })
      qc.invalidateQueries({ queryKey: ["job-runs", orgId, projectId, jobId] })
      qc.invalidateQueries({ queryKey: ["jobs", orgId, projectId] })
      navigate({ to: "/projects/$id/jobs/$jobId/runs", params: { id: projectId, jobId } })
    },
  })

  const deleteMut = useMutation({
    mutationFn: () => jobsApi.delete(orgId, projectId, jobId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["jobs", orgId, projectId] })
      navigate({ to: "/projects/$id/jobs", params: { id: projectId } })
    },
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-24 gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Loading…</span>
      </div>
    )
  }

  if (isError || !job) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-3 text-muted-foreground">
        <ServerCrash className="h-8 w-8 text-destructive/60" />
        <p className="text-sm">Job not found</p>
      </div>
    )
  }

  const tabs = [
    { label: "Runs",          to: "/projects/$id/jobs/$jobId/runs"   as const },
    { label: "Configuration", to: "/projects/$id/jobs/$jobId/config" as const },
  ]

  return (
    <div className="flex flex-col min-h-full">
      <DetailPageHeader
        backTo="/projects/$id/jobs"
        backLabel="Back to jobs"
        backParams={{ id: projectId }}
        icon={job.is_cron
          ? <Clock className="h-4 w-4 text-muted-foreground" />
          : <Zap className="h-4 w-4 text-muted-foreground" />
        }
        name={job.name}
        badge={
          <Badge className={cn("text-[10px] px-1.5 py-0 h-4 border gap-1", STATUS_STYLES[job.status] ?? STATUS_STYLES.idle)}>
            <span className={cn("h-1.5 w-1.5 rounded-full", STATUS_DOT[job.status] ?? STATUS_DOT.idle)} />
            {job.status}
          </Badge>
        }
        actions={
          <>
            <Button size="sm" onClick={() => triggerMut.mutate()} disabled={triggerMut.isPending} className="gap-1.5 h-7 text-xs">
              {triggerMut.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Play className="h-3 w-3" />}
              Run now
            </Button>
            <Button size="sm" variant="outline" onClick={() => deleteMut.mutate()} disabled={deleteMut.isPending}
              className="text-muted-foreground hover:text-destructive gap-1.5 h-7 text-xs">
              {deleteMut.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
              Delete
            </Button>
          </>
        }
      >
        {tabs.map(({ label, to }) => (
          <Link key={label} to={to} params={{ id: projectId, jobId }} className={tabLinkCls}>
            {label}
          </Link>
        ))}
      </DetailPageHeader>

      <div className="flex-1">
        <Outlet />
      </div>
    </div>
  )
}
