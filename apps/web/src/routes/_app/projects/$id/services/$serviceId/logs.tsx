import { createFileRoute, useParams } from "@tanstack/react-router"
import { useEffect, useRef, useState } from "react"
import { Download, Loader2, ScrollText, Search, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/logs"
)({
  component: LogsTab,
})

const TAIL_OPTIONS = [
  { value: "100", label: "Last 100 lines" },
  { value: "200", label: "Last 200 lines" },
  { value: "500", label: "Last 500 lines" },
  { value: "1000", label: "Last 1000 lines" },
  { value: "0", label: "All lines" },
]

const SINCE_OPTIONS = [
  { value: "", label: "From start" },
  { value: "1h", label: "Last 1 hour" },
  { value: "6h", label: "Last 6 hours" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
]

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
  const [downloading, setDownloading] = useState(false)

  const [tail, setTail] = useState("200")
  const [since, setSince] = useState("")
  const [follow, setFollow] = useState(true)
  const [search, setSearch] = useState("")

  const bottomRef = useRef<HTMLDivElement>(null)
  const abortRef = useRef<AbortController | null>(null)
  const autoScrollRef = useRef(true)

  const buildStreamURL = () => {
    const params = new URLSearchParams()
    const tailNum = parseInt(tail, 10)
    if (tailNum > 0) params.set("tail", tail)
    if (since) params.set("since", since)
    if (!follow) params.set("follow", "false")
    const qs = params.toString()
    return `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/logs/stream${qs ? `?${qs}` : ""}`
  }

  const startStream = () => {
    if (!orgId || !token) return

    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setLogLines([])
    setStreamDone(false)
    setError(null)
    setStreaming(true)
    autoScrollRef.current = true

    ;(async () => {
      try {
        const res = await fetch(buildStreamURL(), {
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

  const handleDownload = async () => {
    if (!orgId || !token) return
    setDownloading(true)
    try {
      const params = new URLSearchParams()
      const tailNum = parseInt(tail, 10)
      if (tailNum > 0) params.set("tail", tail)
      if (since) params.set("since", since)
      const qs = params.toString()
      const url = `/api/v1/orgs/${orgId}/projects/${projectId}/services/${serviceId}/logs${qs ? `?${qs}` : ""}`
      const res = await fetch(url, { headers: { Authorization: `Bearer ${token}` } })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const text = await res.text()
      const blob = new Blob([text], { type: "text/plain" })
      const a = document.createElement("a")
      a.href = URL.createObjectURL(blob)
      a.download = `service-${serviceId}.log`
      a.click()
      URL.revokeObjectURL(a.href)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Download failed")
    } finally {
      setDownloading(false)
    }
  }

  useEffect(() => {
    startStream()
    return () => abortRef.current?.abort()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [orgId, projectId, serviceId, token, tail, since, follow])

  // Auto-scroll only when user hasn't scrolled up
  useEffect(() => {
    if (autoScrollRef.current) {
      bottomRef.current?.scrollIntoView({ behavior: "smooth" })
    }
  }, [logLines])

  const filteredLines =
    search.trim()
      ? logLines.filter((l) => l.toLowerCase().includes(search.toLowerCase()))
      : logLines

  const tailLabel =
    TAIL_OPTIONS.find((o) => o.value === tail)?.label ?? `Last ${tail} lines`

  const sinceLabel =
    SINCE_OPTIONS.find((o) => o.value === since)?.label ?? since

  return (
    <div className="flex flex-col h-full p-6 space-y-3">
      {/* Toolbar */}
      <div className="flex items-center gap-2 flex-wrap">
        {/* Tail */}
        <Select value={tail} onValueChange={(v) => v != null && setTail(v)}>
          <SelectTrigger className="h-7 text-xs w-36">
            <SelectValue placeholder="Lines" />
          </SelectTrigger>
          <SelectContent>
            {TAIL_OPTIONS.map((o) => (
              <SelectItem key={o.value} value={o.value} className="text-xs">
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        {/* Since */}
        <Select value={since} onValueChange={(v) => v != null && setSince(v)}>
          <SelectTrigger className="h-7 text-xs w-36">
            <SelectValue placeholder="Time range" />
          </SelectTrigger>
          <SelectContent>
            {SINCE_OPTIONS.map((o) => (
              <SelectItem key={o.value} value={o.value} className="text-xs">
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        {/* Search */}
        <div className="relative flex-1 min-w-40">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Filter lines…"
            className="h-7 text-xs pl-6 pr-6"
          />
          {search && (
            <button
              onClick={() => setSearch("")}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>

        <div className="flex items-center gap-2 ml-auto shrink-0">
          {/* Live toggle */}
          <Button
            size="sm"
            variant={follow ? "default" : "outline"}
            className="h-7 text-xs gap-1.5"
            onClick={() => setFollow((f) => !f)}
          >
            {follow ? (
              <>
                <span className="h-1.5 w-1.5 rounded-full bg-current animate-pulse" />
                Live
              </>
            ) : (
              "Snapshot"
            )}
          </Button>

          {/* Download */}
          <Button
            size="sm"
            variant="outline"
            className="h-7 text-xs gap-1.5"
            onClick={handleDownload}
            disabled={downloading}
          >
            {downloading ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Download className="h-3 w-3" />
            )}
            Download
          </Button>
        </div>
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
            {tailLabel.toLowerCase()} · {sinceLabel.toLowerCase()} ·{" "}
            {follow ? "follow" : "snapshot"}
            {search ? ` · filter: ${search}` : ""}
          </code>
          {search && filteredLines.length !== logLines.length && (
            <span className="ml-auto text-[10px] text-amber-400/70 font-mono">
              {filteredLines.length} / {logLines.length} lines
            </span>
          )}
          {!search && streamDone && (
            <span className="ml-auto text-[10px] text-muted-foreground/50 font-mono">
              stream ended
            </span>
          )}
          {!search && streaming && (
            <span className="ml-auto flex items-center gap-1 text-[10px] text-emerald-400/70 font-mono">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" />
              live
            </span>
          )}
        </div>

        {/* Log output */}
        <div
          className="flex-1 overflow-y-auto px-4 py-3"
          onScroll={(e) => {
            const el = e.currentTarget
            autoScrollRef.current =
              el.scrollHeight - el.scrollTop - el.clientHeight < 40
          }}
        >
          {!streaming && filteredLines.length === 0 && !error ? (
            <div className="flex flex-col items-center justify-center h-32 gap-3 text-muted-foreground/40">
              <ScrollText className="h-7 w-7" />
              <p className="text-xs font-mono">
                {search ? "No lines match the filter." : "No log output. Is the service running?"}
              </p>
            </div>
          ) : (
            <div className="text-[12px] font-mono leading-relaxed">
              {filteredLines.map((raw, i) => {
                const { time, text } = parseLogLine(raw)
                return (
                  <div key={i} className="flex gap-3 hover:bg-white/[0.03] px-1 -mx-1 rounded">
                    {time && (
                      <span className="shrink-0 text-muted-foreground/40 w-40 text-[11px] pt-px select-none">
                        {time}
                      </span>
                    )}
                    <span className="flex-1 text-muted-foreground whitespace-pre-wrap break-all">
                      {search ? <Highlighted text={text} query={search} /> : text}
                    </span>
                  </div>
                )
              })}
              {streaming && (
                <span className="inline-block w-2 h-3.5 bg-muted-foreground/60 animate-pulse align-middle ml-0.5" />
              )}
            </div>
          )}
          <div ref={bottomRef} />
        </div>
      </div>
    </div>
  )
}

// K8s timestamp prefix: 2026-05-13T10:23:45.123456789Z
const TS_RE = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z) ([\s\S]*)$/

function parseLogLine(raw: string): { time: string; text: string } {
  const m = raw.match(TS_RE)
  if (!m) return { time: "", text: raw }
  const d = new Date(m[1])
  const time = isNaN(d.getTime())
    ? m[1]
    : d.toLocaleString("en-US", {
        month: "2-digit",
        day: "2-digit",
        year: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: true,
      })
  return { time, text: m[2] }
}

function Highlighted({ text, query }: { text: string; query: string }) {
  if (!query) return <>{text}</>
  const lower = text.toLowerCase()
  const q = query.toLowerCase()
  const parts: React.ReactNode[] = []
  let last = 0
  let idx = lower.indexOf(q, last)
  while (idx !== -1) {
    if (idx > last) parts.push(text.slice(last, idx))
    parts.push(
      <mark key={idx} className="bg-amber-400/30 text-amber-200 rounded-sm">
        {text.slice(idx, idx + query.length)}
      </mark>
    )
    last = idx + query.length
    idx = lower.indexOf(q, last)
  }
  if (last < text.length) parts.push(text.slice(last))
  return <>{parts}</>
}
