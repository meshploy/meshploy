import { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CheckCircle2, ExternalLink, Loader2, XCircle } from "lucide-react"
import { ApiError } from "@/lib/api/core"
import { system as systemApi, type UpgradeStatus, type VersionInfo } from "@/lib/api/system"
import { useAuthStore } from "@/store/auth-store"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

type Phase = "idle" | "queued" | "running" | "restarting" | "done" | "failed"

// How long a queued request may sit unclaimed before the dialog suggests
// looking at the server. The path unit normally starts the runner within a
// second.
const UNCLAIMED_AFTER_MS = 60_000

function phaseOf(s: UpgradeStatus | undefined, tracked: string | null, unreachable: boolean): Phase {
  if (!tracked) return "idle"
  const ours = s?.id === tracked
  if (ours && s?.state === "succeeded") return "done"
  if (ours && (s?.state === "failed" || s?.state === "interrupted")) return "failed"
  // The upgrade recreates the API partway through, so an unanswered poll is
  // expected rather than an error.
  if (unreachable) return "restarting"
  if (ours && s?.state === "running") return "running"
  return "queued"
}

/**
 * Upgrades this server from the console.
 *
 * The console never upgrades anything itself. It asks the API to queue a
 * request; a systemd unit on the gateway runs the upgrade as root and reports
 * progress through a file the API reads. The upgrade recreates the API and this
 * console's own container, so following it has to survive both going away.
 */
export function UpgradeDialog({
  open,
  onOpenChange,
  version,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  version: VersionInfo
}) {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  // The run this dialog follows: one it queued, or one already under way when
  // it opened. The status on disk belongs to an earlier upgrade until the
  // runner reports this id.
  const [tracked, setTracked] = useState<string | null>(null)
  const [queuedAt, setQueuedAt] = useState<number | null>(null)
  const [now, setNow] = useState(() => Date.now())

  const status = useQuery({
    queryKey: ["system-upgrade"],
    queryFn: () => systemApi.upgradeStatus(token!),
    enabled: open && !!token,
    retry: false,
    refetchInterval: (query) => {
      const p = phaseOf(query.state.data, tracked, query.state.status === "error")
      return tracked && p !== "done" && p !== "failed" ? 2000 : false
    },
  })
  const s = status.data
  const phase = phaseOf(s, tracked, status.isError)

  // Opened while an upgrade is queued or running: follow that one.
  useEffect(() => {
    if (tracked || !s) return
    if (s.pending && s.pending_id) setTracked(s.pending_id)
    else if (s.state === "running") setTracked(s.id)
  }, [s, tracked])

  useEffect(() => {
    if (phase !== "queued") return
    const t = setInterval(() => setNow(Date.now()), 5000)
    return () => clearInterval(t)
  }, [phase])

  useEffect(() => {
    if (phase === "done") qc.invalidateQueries({ queryKey: ["system-version"] })
  }, [phase, qc])

  const start = useMutation({
    mutationFn: () => systemApi.requestUpgrade(token!),
    onSuccess: (st) => {
      qc.setQueryData(["system-upgrade"], st)
      setTracked(st.pending_id ?? null)
      setQueuedAt(Date.now())
    },
  })

  const reset = () => {
    setTracked(null)
    setQueuedAt(null)
    start.reset()
  }

  const flag = version.channel === "edge" ? " --edge" : ""
  const inProgress = phase === "queued" || phase === "running" || phase === "restarting"

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Upgrade this server</DialogTitle>
          <DialogDescription>
            {version.channel === "edge" ? (
              <>
                Edge build <code className="font-mono text-xs">{version.current}</code> to{" "}
                <code className="font-mono text-xs">{version.latest}</code>, from main
              </>
            ) : (
              <>
                From v{version.current} to v{version.latest}
              </>
            )}
          </DialogDescription>
        </DialogHeader>

        {phase === "idle" && (
          <IdleBody status={s} loading={status.isLoading} flag={flag} startError={start.error} />
        )}

        {inProgress && (
          <div className="space-y-3">
            <div className="flex items-center gap-2 text-xs text-foreground">
              <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-primary" />
              <span>
                {phase === "queued"
                  ? "Waiting for the server to start the upgrade…"
                  : phase === "restarting"
                    ? "Restarting services. The console is unavailable for a moment; this keeps checking."
                    : s?.step || "Upgrading…"}
              </span>
            </div>
            {phase === "queued" && queuedAt !== null && now - queuedAt > UNCLAIMED_AFTER_MS && (
              <p className="text-[11px] text-amber-400">
                Nothing has picked the request up yet. On the gateway, check{" "}
                <code className="font-mono">meshploy updater status</code>.
              </p>
            )}
            {phase !== "queued" && <LogTail lines={s?.log_tail} />}
            <p className="text-[11px] text-muted-foreground/70">
              You can close this; the upgrade carries on without it.
            </p>
          </div>
        )}

        {phase === "done" && (
          <div className="space-y-2">
            <div className="flex items-center gap-2 text-xs text-emerald-400">
              <CheckCircle2 className="h-3.5 w-3.5 shrink-0" />
              Upgrade finished
            </div>
            {s?.cli_to && s.cli_to !== s.cli_from && (
              <p className="text-[11px] text-muted-foreground">
                CLI {s.cli_from} → {s.cli_to}
              </p>
            )}
            <p className="text-[11px] text-muted-foreground">
              Reload the console to load the new version.
            </p>
          </div>
        )}

        {phase === "failed" && (
          <div className="space-y-2">
            <div className="flex items-center gap-2 text-xs text-destructive">
              <XCircle className="h-3.5 w-3.5 shrink-0" />
              {s?.state === "interrupted" ? "The upgrade was interrupted" : "The upgrade failed"}
            </div>
            <p className="text-[11px] text-muted-foreground break-words">
              {s?.state === "interrupted"
                ? "The server stopped reporting progress, most likely because it restarted."
                : s?.error}
            </p>
            <p className="text-[11px] text-muted-foreground">
              If the services did not come back, the previous version was put back. The full log is
              on the gateway: <code className="font-mono">meshploy updater status</code>.
            </p>
            <LogTail lines={s?.log_tail} />
          </div>
        )}

        <DialogFooter>
          {version.release_url && (
            <a
              href={version.release_url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 self-center text-xs text-muted-foreground hover:text-foreground sm:mr-auto"
            >
              What changed
              <ExternalLink className="h-3 w-3" />
            </a>
          )}
          {phase === "done" ? (
            <Button onClick={() => window.location.reload()}>Reload the console</Button>
          ) : phase === "failed" ? (
            <>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                Close
              </Button>
              <Button onClick={reset}>Back</Button>
            </>
          ) : inProgress ? (
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              Close
            </Button>
          ) : (
            <>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button
                onClick={() => start.mutate()}
                disabled={!s?.can_upgrade || s.pending || start.isPending}
              >
                {start.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                Upgrade now
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function IdleBody({
  status: s,
  loading,
  flag,
  startError,
}: {
  status: UpgradeStatus | undefined
  loading: boolean
  flag: string
  startError: Error | null
}) {
  if (loading || !s) {
    return <p className="text-xs text-muted-foreground">Checking this server…</p>
  }

  if (!s.enabled) {
    return (
      <div className="space-y-2 text-xs text-muted-foreground">
        <p>Upgrades from the console are not turned on for this server yet. On the gateway, run:</p>
        <pre className="overflow-x-auto rounded-md bg-muted/50 p-2.5 font-mono text-[11px] text-foreground">
          {`sudo meshploy update${flag}\nsudo meshploy server-upgrade${flag}\nsudo meshploy updater start`}
        </pre>
        <p>After that, this button upgrades the server for you. New installs are set up this way.</p>
      </div>
    )
  }

  const last = s.state && s.state !== "running" ? s : null
  return (
    <div className="space-y-2 text-xs text-muted-foreground">
      <p>
        The console, API, proxy and Caddy restart while the server upgrades. Workloads on your nodes
        keep running, but traffic to them pauses for about a minute.
      </p>
      <p>
        If the services do not come back, the previous version is put back. Database migrations the
        new version already ran are not undone.
      </p>
      {!s.can_upgrade && (
        <p className="text-amber-400">Only the owner of this server can start an upgrade.</p>
      )}
      {last && (
        <p className="text-[11px] text-muted-foreground/70">
          Last upgrade: {last.state}
          {last.finished_at && `, ${new Date(last.finished_at).toLocaleString()}`}
          {last.error && `. ${last.error}`}
        </p>
      )}
      {startError && (
        <p className="text-destructive">
          {startError instanceof ApiError ? startError.detail : "Could not queue the upgrade."}
        </p>
      )}
    </div>
  )
}

function LogTail({ lines }: { lines?: string[] }) {
  if (!lines || lines.length === 0) return null
  return (
    <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted/50 p-2.5 font-mono text-[10.5px] leading-relaxed text-muted-foreground">
      {lines.join("\n")}
    </pre>
  )
}
