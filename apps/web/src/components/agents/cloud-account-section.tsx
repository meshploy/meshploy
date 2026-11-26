import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, ArrowUpRight, Loader2 } from "lucide-react"
import { system as systemApi } from "@/lib/api/system"
import { ApiError } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { Button } from "@/components/ui/button"
import { Section } from "@/components/services/form-primitives"
import { TermInfo } from "@/help/term"
import { cn, formatRelativeTime } from "@/lib/utils"

/**
 * Agent apps reaching this server through the person's Meshploy account: the
 * person links their own account, approves it there, and sees here what each
 * app did as them. Shown only while the server's owner allows agent apps,
 * which they do under Settings, Meshploy Cloud.
 */
export function CloudAccountSection() {
  const token = useAuthStore((s) => s.token)!
  const me = useQuery({
    queryKey: ["my-meshploy-account"],
    queryFn: () => systemApi.myMeshployAccount(token),
    // The owner is linked a moment after turning agent apps on.
    refetchInterval: (q) => (q.state.data?.available && !q.state.data.linked ? 5_000 : false),
  })
  // On a server Meshploy Cloud runs, people reach it through their accounts
  // already, so there is nothing to link.
  if (!me.data?.server_linked || !me.data.available || me.data.managed) return null
  const d = me.data
  return (
    <Section
      title="Through Meshploy Cloud"
      subtitle="Agent apps you approved in your Meshploy account act as you here, with exactly your permissions."
    >
      {d.linked ? <Linked email={d.email} linkedAt={d.linked_at} appURL={d.app_url} /> : <NotLinked />}
    </Section>
  )
}

function errorText(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.detail : fallback
}

function NotLinked() {
  const token = useAuthStore((s) => s.token)!
  const [error, setError] = useState<string | null>(null)
  const start = useMutation({
    mutationFn: () => systemApi.linkMyMeshployAccount(token),
    // The approval is on the account plane: the person signs in there and
    // says yes; this page learns of it when they come back.
    onSuccess: ({ url }) => window.location.assign(url),
    onError: (e) => setError(errorText(e, "Could not start linking your account.")),
  })
  return (
    <div className="space-y-3">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1 min-w-0">
          <div className="flex items-center gap-1.5">
            <span className="text-sm font-medium">Your Meshploy account</span>
            <TermInfo id="cloud-link.my-account" />
          </div>
          <p className="text-xs text-muted-foreground leading-relaxed">
            Not linked. Link it to let Claude, Cursor and the other agent apps you connect to your Meshploy account work
            on this server as you. You approve it on app.meshploy.com, and can end it here or there.
          </p>
        </div>
        <Button size="sm" className="h-7 shrink-0 text-xs" disabled={start.isPending} onClick={() => start.mutate()}>
          {start.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          Link my account
        </Button>
      </div>
      {error && <ErrorLine text={error} />}
    </div>
  )
}

function Linked({ email, linkedAt, appURL }: { email?: string; linkedAt?: string; appURL?: string }) {
  const token = useAuthStore((s) => s.token)!
  const qc = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const calls = useQuery({
    queryKey: ["my-agent-calls"],
    queryFn: () => systemApi.myAgentCalls(token),
    refetchInterval: 30_000,
  })
  const unlink = useMutation({
    mutationFn: () => systemApi.unlinkMyMeshployAccount(token),
    onSuccess: () => {
      setConfirming(false)
      qc.invalidateQueries({ queryKey: ["my-meshploy-account"] })
      qc.invalidateQueries({ queryKey: ["system-fleet"] })
    },
    onError: (e) => setError(errorText(e, "Could not unlink your account.")),
  })
  const recent = (calls.data?.calls ?? []).slice(0, 8)

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md border border-border/60 px-3 py-2 text-xs">
        <span className="size-1.5 rounded-full bg-primary shrink-0" />
        <span className="font-medium">Linked</span>
        <span className="text-muted-foreground min-w-0 break-words">
          {[email && `to ${email}`, linkedAt && `since ${formatRelativeTime(new Date(linkedAt))}`].filter(Boolean).join(" · ")}
        </span>
        {appURL && (
          <a
            href={`${appURL}/agent-apps`}
            target="_blank"
            rel="noreferrer"
            className="ml-auto inline-flex items-center gap-1 text-primary hover:underline"
          >
            Manage agent apps
            <ArrowUpRight className="h-3 w-3" />
          </a>
        )}
      </div>

      <div className="space-y-2">
        <span className="text-sm font-medium">What agent apps did here as you</span>
        {recent.length === 0 ? (
          <p className="text-xs text-muted-foreground">Nothing yet.</p>
        ) : (
          <ul className="divide-y divide-border/50 rounded-md border border-border/60 text-xs">
            {recent.map((c) => (
              <li key={c.id} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-3 py-2">
                <span className="font-medium">{c.app || "An agent app"}</span>
                <span className="font-mono text-muted-foreground min-w-0 break-all">
                  {c.method} {c.path}
                </span>
                <span className={cn("ml-auto shrink-0", c.status >= 400 ? "text-destructive" : "text-muted-foreground")}>
                  {c.refused ? `refused: ${c.refused}` : c.status} · {formatRelativeTime(new Date(c.at))}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t border-border/60 pt-4">
        <p className="text-xs text-muted-foreground leading-relaxed min-w-0">
          {confirming
            ? "Agent apps stop acting as you here at once. You can link again any time."
            : "Unlinking stops every agent app acting as you on this server."}
        </p>
        <div className="flex shrink-0 items-center gap-2">
          {confirming ? (
            <>
              <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setConfirming(false)}>
                Keep linked
              </Button>
              <Button size="sm" variant="destructive" className="h-7 text-xs" disabled={unlink.isPending} onClick={() => unlink.mutate()}>
                {unlink.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                Unlink
              </Button>
            </>
          ) : (
            <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => setConfirming(true)}>
              Unlink my account
            </Button>
          )}
        </div>
      </div>
      {error && <ErrorLine text={error} />}
    </div>
  )
}

function ErrorLine({ text }: { text: string }) {
  return (
    <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 p-3">
      <AlertCircle className="h-3.5 w-3.5 text-destructive shrink-0 mt-0.5" />
      <p className="text-xs text-destructive">{text}</p>
    </div>
  )
}
