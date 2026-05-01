import { useEffect, useRef, useState } from "react"
import { Terminal } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import { Loader2 } from "lucide-react"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import type { TerminalPayload } from "@/store/tab-store"
import "@xterm/xterm/css/xterm.css"

type ConnState = "connecting" | "connected" | "error" | "closed"

function terminalWsUrl(orgId: string, nodeId: string, token: string): string {
  const apiBase =
    (window as { __MESHPLOY_CONFIG__?: { apiUrl?: string } }).__MESHPLOY_CONFIG__?.apiUrl ??
    import.meta.env.VITE_API_URL ??
    ""
  let base = apiBase
  if (!base) {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:"
    base = `${proto}//${window.location.host}`
  } else {
    base = base.replace(/^http/, "ws")
  }
  return `${base}/api/v1/orgs/${orgId}/nodes/${nodeId}/terminal?token=${encodeURIComponent(token)}`
}

export function NodeTerminal({ payload }: { payload: TerminalPayload }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const containerRef = useRef<HTMLDivElement>(null)
  const [connState, setConnState] = useState<ConnState>("connecting")

  useEffect(() => {
    if (!containerRef.current) return

    const term = new Terminal({
      theme: {
        background:    "#0a0a0a",
        foreground:    "#e4e4e7",
        cursor:        "#a1a1aa",
        black:         "#18181b",
        red:           "#f87171",
        green:         "#4ade80",
        yellow:        "#fbbf24",
        blue:          "#60a5fa",
        magenta:       "#c084fc",
        cyan:          "#22d3ee",
        white:         "#e4e4e7",
        brightBlack:   "#3f3f46",
        brightRed:     "#f87171",
        brightGreen:   "#4ade80",
        brightYellow:  "#fbbf24",
        brightBlue:    "#60a5fa",
        brightMagenta: "#c084fc",
        brightCyan:    "#22d3ee",
        brightWhite:   "#fafafa",
      },
      fontFamily: '"Geist Mono", "JetBrains Mono", ui-monospace, monospace',
      fontSize: 13,
      lineHeight: 1.5,
      cursorBlink: true,
      allowTransparency: true,
    })

    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(containerRef.current)
    fit.fit()

    const ws = new WebSocket(terminalWsUrl(orgId, payload.nodeId, token))
    ws.binaryType = "arraybuffer"

    ws.onopen = () => {
      setConnState("connected")
      ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }))
    }

    ws.onmessage = (e) => {
      if (e.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(e.data))
      } else {
        term.write(e.data)
      }
    }

    ws.onerror = () => {
      setConnState("error")
      term.write("\r\n\x1b[31mWebSocket error — check the API logs.\x1b[0m\r\n")
    }
    ws.onclose = () => {
      setConnState((s) => s === "connected" ? "closed" : s)
      term.write("\r\n\x1b[33mConnection closed.\x1b[0m\r\n")
    }

    const onData = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data))
      }
    })

    const observer = new ResizeObserver(() => {
      fit.fit()
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }))
      }
    })
    observer.observe(containerRef.current!)

    return () => {
      onData.dispose()
      observer.disconnect()
      ws.close()
      term.dispose()
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [payload.nodeId])

  const dotColor =
    connState === "connected" ? "bg-emerald-400" :
    connState === "error"     ? "bg-red-400" :
    connState === "closed"    ? "bg-zinc-500" :
    "bg-amber-400"

  return (
    <div className="h-full flex flex-col bg-[#0a0a0a]">
      {/* Header */}
      <div className="flex items-center gap-2 px-4 py-2 border-b border-border/40 shrink-0">
        <span className={`h-2 w-2 rounded-full shrink-0 ${dotColor} ${connState === "connecting" ? "animate-pulse" : ""}`} />
        <span className="text-xs font-medium text-foreground/80">{payload.nodeLabel}</span>
        <span className="text-xs text-muted-foreground/50">root shell</span>
        {connState === "connecting" && (
          <span className="ml-auto text-xs text-amber-400/70">provisioning pod…</span>
        )}
        {connState === "error" && (
          <span className="ml-auto text-xs text-red-400/70">connection failed</span>
        )}
        {connState === "closed" && (
          <span className="ml-auto text-xs text-muted-foreground/50">disconnected</span>
        )}
      </div>

      {/* Connecting overlay */}
      {connState === "connecting" && (
        <div className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-3 bg-[#0a0a0a]/90 pointer-events-none">
          <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          <div className="text-center space-y-1">
            <p className="text-sm text-foreground/80">Provisioning shell pod</p>
            <p className="text-xs text-muted-foreground/60">This may take up to 30 s on first run</p>
          </div>
        </div>
      )}

      <div ref={containerRef} className="flex-1 min-h-0 p-2" />
    </div>
  )
}
