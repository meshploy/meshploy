import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, Loader2 } from "lucide-react"
import { system as systemApi, type FleetLink } from "@/lib/api/system"
import { ApiError } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Section } from "@/components/services/form-primitives"
import { TermInfo } from "@/help/term"
import { cn, formatRelativeTime } from "@/lib/utils"

/**
 * The server's link to Meshploy Cloud. The server's owner alone sees it: the
 * link decides what leaves the server. Everything here is the owner's choice
 * and can be undone from this card; the server works the same unlinked.
 */
export function CloudLinkSection() {
  const token = useAuthStore((s) => s.token)!
  const upgrade = useQuery({ queryKey: ["system-upgrade"], queryFn: () => systemApi.upgradeStatus(token) })
  const owner = upgrade.data?.owner === true
  const link = useQuery({
    queryKey: ["system-fleet"],
    queryFn: () => systemApi.fleet(token),
    enabled: owner,
    // The connection comes up a moment after linking, and drops and returns
    // on its own; the card follows it.
    refetchInterval: (q) => (q.state.data?.linked && !q.state.data.connected ? 3_000 : 30_000),
  })

  if (!owner) return null
  if (link.isLoading || !link.data) {
    return (
      <Section title="Meshploy Cloud">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span>Loading…</span>
        </div>
      </Section>
    )
  }
  return link.data.linked ? <Linked link={link.data} /> : <NotLinked />
}

function errorText(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.detail : fallback
}

function NotLinked() {
  const token = useAuthStore((s) => s.token)!
  const qc = useQueryClient()
  const [value, setValue] = useState("")
  const [error, setError] = useState<string | null>(null)
  const link = useMutation({
    mutationFn: () => systemApi.linkFleet(value.trim(), token),
    onSuccess: (data) => {
      qc.setQueryData(["system-fleet"], data)
      setValue("")
      setError(null)
    },
    onError: (e) => setError(errorText(e, "Could not link this server.")),
  })

  return (
    <Section title="Meshploy Cloud" subtitle="Not linked">
      <div className="space-y-3">
        <p className="text-xs text-muted-foreground leading-relaxed">
          Link this server to your Meshploy account to see it beside your other workspaces at app.meshploy.com.
          It reports counts and states only: nodes online, services running, backups. No names, addresses or secrets
          leave the server. The server connects out, so no port needs opening.
        </p>
        <form
          className="flex flex-col sm:flex-row gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (value.trim()) link.mutate()
          }}
        >
          <label htmlFor="fleet-link-token" className="sr-only">Link token</label>
          <Input
            id="fleet-link-token"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder="mflt-…"
            autoComplete="off"
            spellCheck={false}
            className="font-mono text-xs h-8"
          />
          <Button type="submit" size="sm" className="h-8 shrink-0" disabled={!value.trim() || link.isPending}>
            {link.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            Link server
          </Button>
        </form>
        <p className="text-xs text-muted-foreground">
          Get a link token from <span className="font-medium text-foreground">Link self-hosted</span> under Spaces in
          your Meshploy account. A token works once. <TermInfo id="cloud-link.token" className="align-middle" />
        </p>
        {error && <ErrorLine text={error} />}
      </div>
    </Section>
  )
}

function Linked({ link }: { link: FleetLink }) {
  const token = useAuthStore((s) => s.token)!
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const [confirming, setConfirming] = useState(false)

  const report = useMutation({
    mutationFn: (on: boolean) => systemApi.setFleetReport(on, token),
    onSuccess: (data) => {
      qc.setQueryData(["system-fleet"], data)
      setError(null)
    },
    onError: (e) => setError(errorText(e, "Could not change reporting.")),
  })
  const unlink = useMutation({
    mutationFn: () => systemApi.unlinkFleet(token),
    onSuccess: () => {
      setConfirming(false)
      setError(null)
      qc.invalidateQueries({ queryKey: ["system-fleet"] })
    },
    onError: (e) => setError(errorText(e, "Could not unlink this server.")),
  })

  const host = link.connect_url ? new URL(link.connect_url).host : ""
  const state = link.connected
    ? { dot: "bg-primary", label: "Connected" }
    : link.last_error
      ? { dot: "bg-amber-400", label: "Not connected, retrying" }
      : { dot: "bg-muted-foreground/50 animate-pulse", label: "Connecting" }

  return (
    <Section title="Meshploy Cloud" subtitle={link.account ? `Linked to ${link.account}` : "Linked"}>
      {/* One line, like the host agent's on the Server section. */}
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md border border-border/60 px-3 py-2 text-xs">
        <span className={cn("size-1.5 rounded-full shrink-0", state.dot)} />
        <span className="font-medium">{state.label}</span>
        <span className="text-muted-foreground min-w-0 break-words">
          {[
            host && `to ${host}`,
            link.connected && link.connected_at && `connected ${formatRelativeTime(new Date(link.connected_at))}`,
            link.linked_at && `linked ${formatRelativeTime(new Date(link.linked_at))}`,
            !link.connected && link.last_error && `last attempt: ${link.last_error}`,
          ]
            .filter(Boolean)
            .join(" · ")}
        </span>
      </div>

      <div className="mt-4 flex items-start justify-between gap-4 border-t border-border/60 pt-4">
        <div className="space-y-1 min-w-0">
          <div className="flex items-center gap-1.5">
            <label htmlFor="fleet-report" className="text-sm font-medium">Report to Meshploy Cloud</label>
            <TermInfo id="cloud-link.reporting" />
          </div>
          <p className="text-xs text-muted-foreground leading-relaxed">
            {link.report
              ? "Every minute: version, nodes online, services running, backups scheduled and failing. No names, addresses or secrets."
              : "Off. The link stands, and nothing about this server is sent."}
          </p>
        </div>
        <div className="shrink-0 pt-0.5">
          {report.isPending ? (
            <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
          ) : (
            <Switch id="fleet-report" checked={link.report} onCheckedChange={(on) => report.mutate(on)} />
          )}
        </div>
      </div>

      <div className="mt-4 flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t border-border/60 pt-4">
        <div className="space-y-1 min-w-0">
          <span className="text-sm font-medium">Unlink</span>
          <p className="text-xs text-muted-foreground leading-relaxed">
            {confirming
              ? "Meshploy Cloud stops seeing this server. You can link it again with a new token."
              : "Ends the link here and in your Meshploy account. Nothing else on the server changes."}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {confirming ? (
            <>
              <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setConfirming(false)}>
                Keep linked
              </Button>
              <Button
                size="sm"
                variant="destructive"
                className="h-7 text-xs"
                disabled={unlink.isPending}
                onClick={() => unlink.mutate()}
              >
                {unlink.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                Unlink
              </Button>
            </>
          ) : (
            <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => setConfirming(true)}>
              Unlink server
            </Button>
          )}
        </div>
      </div>
      {error && <div className="mt-4"><ErrorLine text={error} /></div>}
    </Section>
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
