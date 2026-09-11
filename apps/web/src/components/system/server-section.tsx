import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { AlertCircle, ExternalLink, Loader2 } from "lucide-react"
import { system as systemApi, type ChannelCommit, type Channels } from "@/lib/api/system"
import { useAuthStore } from "@/store/auth-store"
import { Button } from "@/components/ui/button"
import { Section } from "@/components/services/form-primitives"
import { UpgradeDialog, type ChannelSwitchTarget } from "@/components/system/upgrade-dialog"
import { cn, formatRelativeTime } from "@/lib/utils"

const REPO = "https://github.com/meshploy/meshploy"

// How many of main's commits the edge lane lists before linking to the rest.
const EDGE_SHOWN = 6

/**
 * The server's version and release channel, both channels side by side, and
 * the switch between them.
 *
 * Readable by every member. Switching is the server owner's, and runs through
 * the same upgrade dialog, so it survives the restart the same way.
 */
export function ServerSection() {
  const token = useAuthStore((s) => s.token)!
  const [switching, setSwitching] = useState(false)

  const channels = useQuery({
    queryKey: ["system-channels"],
    queryFn: () => systemApi.channels(token),
    // Built from GitHub, which rations requests; the API caches too.
    staleTime: 5 * 60_000,
  })
  const version = useQuery({
    queryKey: ["system-version"],
    queryFn: () => systemApi.versionInfo(token),
    staleTime: 60_000,
  })

  const ch = channels.data
  if (channels.isLoading || !ch) {
    return (
      <Section title="Server">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          {channels.isError ? (
            <span>Could not load the release channels.</span>
          ) : (
            <>
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              <span>Loading…</span>
            </>
          )}
        </div>
      </Section>
    )
  }

  const cur = ch.current
  const target = switchTarget(ch)

  return (
    <Section
      title="Server"
      subtitle={
        cur.channel
          ? `Running ${cur.channel === "stable" ? `v${cur.version}` : cur.version} on the ${cur.channel} channel`
          : `Running a development build (${cur.version}), which has no release channel`
      }
    >
      {ch.unavailable && (
        <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/5 p-3">
          <AlertCircle className="h-3.5 w-3.5 text-amber-400 shrink-0 mt-0.5" />
          <p className="text-xs text-amber-400">{ch.unavailable}. Try again in a few minutes.</p>
        </div>
      )}

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        <StableLane ch={ch} />
        <EdgeLane ch={ch} />
      </div>

      {ch.switch && (
        <SwitchPanel
          ch={ch}
          onSwitch={() => setSwitching(true)}
          canOpen={!!target && !!version.data}
        />
      )}

      {version.data && target && (
        <UpgradeDialog
          open={switching}
          onOpenChange={setSwitching}
          version={version.data}
          switchTo={target}
        />
      )}
    </Section>
  )
}

/** What the switch lands on, for the dialog. */
function switchTarget(ch: Channels): ChannelSwitchTarget | undefined {
  const sw = ch.switch
  if (!sw?.allowed) return undefined
  const from = ch.current.channel === "stable" ? `v${ch.current.version}` : ch.current.commit
  if (sw.to === "edge") {
    const head = ch.edge.head?.sha
    if (!head) return undefined
    return { channel: "edge", target: `main @ ${head}`, url: `${REPO}/compare/${from}...${head}` }
  }
  const latest = ch.stable.releases[0]?.tag
  if (!latest) return undefined
  return { channel: "stable", target: latest, url: `${REPO}/compare/${from}...${latest}` }
}

function StableLane({ ch }: { ch: Channels }) {
  const cur = ch.current
  const here = cur.channel === "stable" ? `v${cur.version}` : ""
  const listed = ch.stable.releases.some((r) => r.tag === here)

  return (
    <Lane
      title="Stable"
      blurb="Releases, each one a cut of main. Upgrades wait for the next release."
      active={cur.channel === "stable"}
    >
      {ch.stable.releases.length === 0 && <Empty>No releases to show.</Empty>}
      {ch.stable.releases.map((r, i) => (
        <Point
          key={r.tag}
          label={r.tag}
          detail={[i === 0 ? "latest" : "", when(r.published_at)].filter(Boolean).join(" · ")}
          href={r.url}
          here={r.tag === here}
        />
      ))}
      {here && !listed && <Point label={here} detail="an older release" here />}
    </Lane>
  )
}

function EdgeLane({ ch }: { ch: Channels }) {
  const cur = ch.current
  const here = cur.channel === "edge" ? cur.commit : ""
  const latest = ch.stable.releases[0]?.tag
  const commits: ChannelCommit[] =
    ch.edge.commits.length > 0 ? ch.edge.commits : ch.edge.head ? [ch.edge.head] : []
  const shown = commits.slice(0, EDGE_SHOWN)
  const hereIndex = here ? commits.findIndex((c) => c.sha === here) : -1
  const more = Math.max(ch.edge.ahead_by, commits.length) - shown.length
  // An edge build not among main's commits since the release is either in the
  // release, which the switch check has confirmed, or somewhere the lane does
  // not show.
  const inRelease = !!here && hereIndex < 0 && ch.switch?.to === "stable" && ch.switch.allowed
  const unplaced = !!here && hereIndex < 0 && !inRelease

  return (
    <Lane
      title="Edge"
      blurb="Every push to main, as soon as its images are built. Unreleased."
      active={cur.channel === "edge"}
    >
      {shown.length === 0 && <Empty>Nothing on main since the latest release.</Empty>}
      {shown.map((c) => (
        <Point
          key={c.sha}
          label={c.sha}
          detail={c.subject || when(c.date)}
          href={`${REPO}/commit/${c.sha}`}
          here={c.sha === here}
        />
      ))}
      {/* The server's commit is further down than the lane shows, or could
          not be placed. */}
      {(hereIndex >= EDGE_SHOWN || unplaced) && <Point label={here} detail="this server's build" here />}
      {more > 0 && latest && (
        <li className="pl-0.5">
          <a
            href={`${REPO}/compare/${latest}...main`}
            target="_blank"
            rel="noopener noreferrer"
            className="text-[11px] text-muted-foreground hover:text-foreground"
          >
            and {more} more since {latest}
          </a>
        </li>
      )}
      {/* Where main left the release channel. An edge build older than the
          latest release is part of it, so it is marked there. */}
      {latest && (
        <Point
          label={latest}
          detail="the latest release"
          here={inRelease}
          hereLabel={inRelease ? `${here} is in it` : undefined}
          dim
        />
      )}
    </Lane>
  )
}

function SwitchPanel({
  ch,
  onSwitch,
  canOpen,
}: {
  ch: Channels
  onSwitch: () => void
  canOpen: boolean
}) {
  const sw = ch.switch!
  const [open, setOpen] = useState(false)
  const url = switchTarget(ch)?.url

  return (
    <div className="rounded-md border border-border/60 bg-muted/20 p-3 space-y-2">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <p className="text-xs font-medium">Switch to {sw.to}</p>
          <p className="text-[11px] text-muted-foreground mt-0.5">
            {!sw.allowed
              ? sw.reason
              : sw.total === 0
                ? "No code changes: the server lands on the build it runs now, on the other channel."
                : `Brings ${sw.total} commit${sw.total === 1 ? "" : "s"}${
                    sw.to === "edge" ? " not released yet" : " from the release"
                  }.`}
          </p>
        </div>
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-xs shrink-0"
          disabled={!sw.allowed || !canOpen}
          onClick={onSwitch}
        >
          Switch to {sw.to}
        </Button>
      </div>

      {sw.allowed && sw.changes.length > 0 && (
        <div>
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            className="text-[11px] text-muted-foreground hover:text-foreground"
          >
            {open ? "Hide what changes" : "Show what changes"}
          </button>
          {open && (
            <ul className="mt-2 space-y-1">
              {sw.changes.map((c) => (
                <li key={c.sha} className="flex items-baseline gap-2 min-w-0 text-[11px]">
                  <a
                    href={`${REPO}/commit/${c.sha}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-mono text-muted-foreground hover:text-foreground shrink-0"
                  >
                    {c.sha}
                  </a>
                  <span className="truncate">{c.subject}</span>
                </li>
              ))}
              {sw.total > sw.changes.length && url && (
                <li>
                  <a
                    href={url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground"
                  >
                    and {sw.total - sw.changes.length} more
                    <ExternalLink className="h-3 w-3" />
                  </a>
                </li>
              )}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}

function Lane({
  title,
  blurb,
  active,
  children,
}: {
  title: string
  blurb: string
  active: boolean
  children: React.ReactNode
}) {
  return (
    <div
      className={cn(
        "rounded-md border p-3 space-y-2 min-w-0",
        active ? "border-primary/50 bg-primary/5" : "border-border/60 bg-muted/20"
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <p className="text-xs font-medium">{title}</p>
        {active && (
          <span className="text-[10px] px-1.5 py-0.5 rounded bg-primary/15 text-primary">
            This server
          </span>
        )}
      </div>
      <p className="text-[11px] text-muted-foreground">{blurb}</p>
      <ol className="space-y-1.5 border-l border-border/60 pl-3 ml-1">{children}</ol>
    </div>
  )
}

function Point({
  label,
  detail,
  href,
  here,
  hereLabel,
  dim,
}: {
  label: string
  detail?: string
  href?: string
  here?: boolean
  hereLabel?: string
  dim?: boolean
}) {
  return (
    <li className="relative">
      <span
        className={cn(
          "absolute -left-[16.5px] top-1.5 h-2 w-2 rounded-full border",
          here ? "bg-primary border-primary" : "bg-background border-border"
        )}
      />
      <div className={cn("flex items-baseline gap-2 min-w-0", dim && !here && "opacity-60")}>
        {href ? (
          <a
            href={href}
            target="_blank"
            rel="noopener noreferrer"
            className="font-mono text-[11px] shrink-0 hover:text-primary"
          >
            {label}
          </a>
        ) : (
          <span className="font-mono text-[11px] shrink-0">{label}</span>
        )}
        {detail && <span className="text-[11px] text-muted-foreground truncate">{detail}</span>}
        {here && (
          <span className="ml-auto text-[10px] text-primary shrink-0">{hereLabel ?? "you are here"}</span>
        )}
      </div>
    </li>
  )
}

function Empty({ children }: { children: React.ReactNode }) {
  return <li className="text-[11px] text-muted-foreground">{children}</li>
}

function when(iso: string): string {
  if (!iso) return ""
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? "" : formatRelativeTime(d)
}
