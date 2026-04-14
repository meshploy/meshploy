import { createFileRoute, useParams } from "@tanstack/react-router"
import { useEffect, useRef, useState } from "react"
import { Loader2, RefreshCw, ScrollText } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/logs"
)({
  component: LogsTab,
})

function LogsTab() {
  const { id: projectId, serviceId } = useParams({
    from: "/_app/projects/$id/services/$serviceId/logs",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const [logLines, setLogLines] = useState<string[]>([])
  const [streaming, setStreaming] = useState(false)
  const [streamDone, setStreamDone] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const abortRef = useRef<AbortController | null>(null)

  const startStream = () => {
    if (!orgId || !token) return

    // Abort any existing stream
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setLogLines([])
    setStreamDone(false)
    setError(null)
    setStreaming(true)

    const url = `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/logs/stream`

    ;(async () => {
      try {
        const res = await fetch(url, {
          headers: { Authorization: `Bearer ${token}` },
          signal: controller.signal,
        })
        if (!res.ok || !res.body) {
          setError("Failed to connect to log stream")
          setStreaming(false)
          return
        }

        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buf = ""

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
              return
            }
            if (dataLine) {
              const text = dataLine.slice("data:".length).trimStart()
              setLogLines((prev) => [...prev, text])
            }
          }
        }
      } catch (err: unknown) {
        if (err instanceof Error && err.name !== "AbortError") {
          setError(err.message)
        }
      } finally {
        setStreaming(false)
      }
    })()
  }

  // Start stream on mount
  useEffect(() => {
    startStream()
    return () => abortRef.current?.abort()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [orgId, projectId, serviceId, token])

  // Auto-scroll
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [logLines])

  return (
    <div className="flex flex-col h-full p-6 space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-medium">Runtime Logs</h2>
          {streaming && (
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Loader2 className="h-3 w-3 animate-spin" />
              Live
            </div>
          )}
        </div>

        <Button
          size="sm"
          variant="outline"
          className="gap-1.5 h-7 text-xs"
          onClick={startStream}
          disabled={streaming}
        >
          <RefreshCw className="h-3 w-3" />
          Reconnect
        </Button>
      </div>

      {error && (
        <div className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2">
          <p className="text-xs text-destructive">{error}</p>
        </div>
      )}

      {/* Terminal */}
      <div className="flex-1 rounded-lg border border-border/60 bg-[oklch(0.12_0_0)] overflow-hidden flex flex-col min-h-0">
        {/* Terminal bar */}
        <div className="flex items-center gap-2 px-4 py-2 border-b border-border/40 shrink-0">
          <div className="flex gap-1.5">
            <span className="h-2.5 w-2.5 rounded-full bg-destructive/60" />
            <span className="h-2.5 w-2.5 rounded-full bg-amber-500/60" />
            <span className="h-2.5 w-2.5 rounded-full bg-emerald-500/60" />
          </div>
          <code className="text-[11px] font-mono text-muted-foreground/50 ml-1">
            container stdout · tail 200 + follow
          </code>
          {streamDone && (
            <span className="ml-auto text-[10px] text-muted-foreground/50 font-mono">
              stream ended
            </span>
          )}
          {streaming && (
            <span className="ml-auto flex items-center gap-1 text-[10px] text-emerald-400/70 font-mono">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" />
              live
            </span>
          )}
        </div>

        {/* Log output */}
        <div className="flex-1 overflow-y-auto px-4 py-3">
          {!streaming && logLines.length === 0 && !error ? (
            <div className="flex flex-col items-center justify-center h-32 gap-3 text-muted-foreground/40">
              <ScrollText className="h-7 w-7" />
              <p className="text-xs font-mono">No log output. Is the service running?</p>
            </div>
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
