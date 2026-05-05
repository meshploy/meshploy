import { createFileRoute, Link, useParams } from "@tanstack/react-router"
import { useEffect, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { ArrowLeft, Check, Loader2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { deployments as deploymentsApi, type ApiDeployment } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/deployments/$deploymentId"
)({
  component: DeploymentLogsPage,
})

const STATUS_STYLES: Record<ApiDeployment["status"], string> = {
  pending:   "bg-muted text-muted-foreground border-border",
  building:  "bg-amber-500/10 text-amber-400 border-amber-500/20",
  deploying: "bg-blue-500/10 text-blue-400 border-blue-500/20",
  running:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  success:   "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  failed:    "bg-destructive/10 text-destructive border-destructive/20",
}

const ACTIVE_STATUSES = new Set(["pending", "building", "deploying"])

function DeploymentLogsPage() {
  const { id: projectId, serviceId, deploymentId } = useParams({
    from: "/_app/projects/$id/services/$serviceId/deployments/$deploymentId",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const [logLines, setLogLines] = useState<string[]>([])
  const [streaming, setStreaming] = useState(false)
  const [streamDone, setStreamDone] = useState(false)
  const [, setTick] = useState(0)

  // Re-render every second so the relative timestamp stays current
  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 1000)
    return () => clearInterval(id)
  }, [])
  const bottomRef = useRef<HTMLDivElement>(null)
  const abortRef = useRef<AbortController | null>(null)

  const { data: deployment, isLoading } = useQuery({
    queryKey: ["deployment", orgId, projectId, serviceId, deploymentId],
    queryFn: () => deploymentsApi.get(orgId!, projectId, serviceId, deploymentId, token),
    enabled: !!orgId,
    refetchInterval: (query) => {
      const d = query.state.data as ApiDeployment | undefined
      return d && ACTIVE_STATUSES.has(d.status) ? 3000 : false
    },
  })

  // Auto-scroll to bottom as new lines arrive
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [logLines])

  // Stream logs via SSE
  useEffect(() => {
    if (!orgId || !token) return

    const controller = new AbortController()
    abortRef.current = controller
    setLogLines([])
    setStreamDone(false)
    setStreaming(true)

    const url = `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/deployments/${deploymentId}/logs/stream`

    ;(async () => {
      // Reconnect on network drops — long builds can outlive proxy timeouts.
      // The server replays all existing log from the start on reconnect so we
      // clear and re-render rather than appending duplicates.
      const MAX_RETRIES = 50
      let attempt = 0

      while (attempt <= MAX_RETRIES) {
        if (controller.signal.aborted) break
        try {
          const res = await fetch(url, {
            headers: { Authorization: `Bearer ${token}` },
            signal: controller.signal,
          })
          if (!res.ok || !res.body) {
            setLogLines(["Error: failed to connect to log stream"])
            setStreaming(false)
            return
          }

          // Reset log on each reconnect — server replays full history
          setLogLines([])
          const reader = res.body.getReader()
          const decoder = new TextDecoder()
          let buf = ""
          let completed = false

          while (true) {
            const { value, done } = await reader.read()
            if (done) break
            buf += decoder.decode(value, { stream: true })

            const events = buf.split("\n\n")
            buf = events.pop() ?? ""

            for (const event of events) {
              const lines = event.split("\n")
              const eventLine = lines.find((l) => l.startsWith("event:"))
              const dataLine = lines.find((l) => l.startsWith("data:"))

              if (eventLine?.includes("done")) {
                setStreamDone(true)
                setStreaming(false)
                completed = true
                return
              }
              if (dataLine) {
                const text = dataLine.slice("data:".length).trimStart()
                setLogLines((prev) => [...prev, text])
              }
            }
          }

          // Stream ended cleanly without a done event — treat as complete
          if (!completed) {
            setStreamDone(true)
            setStreaming(false)
          }
          return
        } catch (err: unknown) {
          if (controller.signal.aborted || (err instanceof Error && err.name === "AbortError")) return
          attempt++
          if (attempt > MAX_RETRIES) {
            setLogLines((prev) => [...prev, `Stream error: ${(err as Error).message} (gave up after ${MAX_RETRIES} retries)`])
            break
          }
          const delay = Math.min(1000 * attempt, 8000)
          setLogLines((prev) => [...prev, `--- stream disconnected, reconnecting in ${delay / 1000}s (attempt ${attempt}/${MAX_RETRIES}) ---`])
          await new Promise((r) => setTimeout(r, delay))
        }
      }
      setStreaming(false)
    })()

    return () => {
      controller.abort()
    }
  }, [orgId, projectId, serviceId, deploymentId, token])

  return (
    <div className="flex flex-col h-full p-6 space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link
            to="/projects/$id/services/$serviceId/deployments"
            params={{ id: projectId, serviceId }}
            className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Back to deployments
          </Link>

          {!isLoading && deployment && (
            <>
              <span className="text-border/60">·</span>
              <div className="flex items-center gap-2">
                <code className="text-xs font-mono text-foreground/80">
                  {deployment.id.slice(0, 8)}
                </code>
                <Badge
                  className={`text-[10px] px-1.5 py-0 h-4 border ${STATUS_STYLES[deployment.status]}`}
                >
                  {deployment.status}
                </Badge>
                <span className="text-xs text-muted-foreground">
                  {formatRelativeTime(new Date(deployment.created_at))}
                </span>
              </div>
            </>
          )}
        </div>

        {streaming && (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Loader2 className="h-3 w-3 animate-spin" />
            Streaming…
          </div>
        )}
      </div>

      {/* Progress stepper */}
      {deployment && <DeploymentStepper status={deployment.status} />}

      {/* Log terminal */}
      <div className="flex-1 rounded-lg border border-border/60 bg-[oklch(0.12_0_0)] overflow-hidden flex flex-col min-h-0">
        {/* Terminal bar */}
        <div className="flex items-center gap-2 px-4 py-2 border-b border-border/40 shrink-0">
          <div className="flex gap-1.5">
            <span className="h-2.5 w-2.5 rounded-full bg-destructive/60" />
            <span className="h-2.5 w-2.5 rounded-full bg-amber-500/60" />
            <span className="h-2.5 w-2.5 rounded-full bg-emerald-500/60" />
          </div>
          <code className="text-[11px] font-mono text-muted-foreground/50 ml-1">
            deployment/{deploymentId.slice(0, 8)} · build log
          </code>
          {streamDone && (
            <span className="ml-auto text-[10px] text-emerald-400/70 font-mono">
              stream complete
            </span>
          )}
        </div>

        {/* Log content */}
        <div className="flex-1 overflow-y-auto px-4 py-3">
          {isLoading && logLines.length === 0 ? (
            <div className="flex items-center justify-center h-32">
              <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
            </div>
          ) : logLines.length === 0 && !streaming ? (
            <p className="text-[12px] font-mono text-muted-foreground/40">
              Waiting for log output…
            </p>
          ) : (
            <pre className="text-[12px] font-mono text-muted-foreground leading-relaxed whitespace-pre-wrap break-all">
              {logLines.map((line, i) => (
                <span key={i} className="block">
                  {line}
                </span>
              ))}
              {streaming && (
                <span className="inline-block w-2 h-3.5 bg-muted-foreground/60 animate-pulse align-middle ml-0.5" />
              )}
            </pre>
          )}
          <div ref={bottomRef} />
        </div>
      </div>
    </div>
  )
}

const STEPS: { key: ApiDeployment["status"][]; label: string; sub: string }[] = [
  { key: ["pending"],            label: "Queued",         sub: "Waiting for a builder node" },
  { key: ["building"],           label: "Building",       sub: "Cloning repo & building image" },
  { key: ["deploying", "running"], label: "Deploying",    sub: "Rolling update to cluster" },
  { key: ["success"],            label: "Live",           sub: "All replicas healthy" },
]

function stepIndex(status: ApiDeployment["status"]) {
  if (status === "failed") return -1
  return STEPS.findIndex((s) => s.key.includes(status))
}

function DeploymentStepper({ status }: { status: ApiDeployment["status"] }) {
  const current = stepIndex(status)
  const failed = status === "failed"

  return (
    <div className="flex items-center">
      {STEPS.map((step, i) => {
        const done    = !failed && current > i
        const active  = !failed && current === i
        const isFailed = failed && i === Math.max(current, 0)

        const boxCls = done
          ? "border-emerald-500/30 bg-emerald-500/5"
          : active
          ? "border-primary/40 bg-primary/5"
          : isFailed
          ? "border-destructive/30 bg-destructive/5"
          : "border-border/50 bg-card"

        const labelCls = done
          ? "text-emerald-400"
          : active
          ? "text-foreground"
          : isFailed
          ? "text-destructive"
          : "text-muted-foreground/40"

        const lineCls = done ? "bg-emerald-500/30" : "bg-border/40"

        return (
          <div key={step.label} className="flex items-center flex-1 min-w-0">
            {/* Step box */}
            <div className={`flex items-center gap-2.5 px-3 py-2.5 rounded-lg border shrink-0 transition-colors ${boxCls}`}>
              <div className={`flex items-center justify-center w-5 h-5 rounded-full shrink-0 text-[11px] font-semibold
                ${done    ? "bg-emerald-500/20 text-emerald-400 border border-emerald-500/30" : ""}
                ${active  ? "bg-primary/20 text-primary border border-primary/40" : ""}
                ${isFailed ? "bg-destructive/10 text-destructive border border-destructive/20" : ""}
                ${!done && !active && !isFailed ? "bg-muted text-muted-foreground/30 border border-border/50" : ""}
              `}>
                {done ? <Check className="h-3 w-3" /> : active ? <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse" /> : i + 1}
              </div>
              <div className="hidden sm:block">
                <p className={`text-xs font-medium leading-tight ${labelCls}`}>{step.label}</p>
                <p className="text-[10px] text-muted-foreground/40 mt-0.5">{step.sub}</p>
              </div>
            </div>
            {/* Connector — touches box borders */}
            {i < STEPS.length - 1 && (
              <div className={`flex-1 h-px ${lineCls}`} />
            )}
          </div>
        )
      })}
    </div>
  )
}
