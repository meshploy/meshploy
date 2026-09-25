import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts"
import { ChartContainer, ChartTooltip } from "@/components/ui/chart"
import type { ApiDelivery } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TermInfo } from "@/help/term"

/**
 * How the workspace ships: what ran each day for two weeks, and the four
 * numbers delivery is judged by (how often production gets a deploy, how many
 * of those fail, how long a failure takes to fix, and how long an image takes
 * from its build in staging to production).
 *
 * Each number is measured from the deployments the platform records, so a
 * workspace with nothing to measure says "no data" rather than a zero that
 * reads as a result.
 */
export function DeliveryPanel({ delivery }: { delivery: ApiDelivery }) {
  const data = delivery.days.map((d) => ({
    ...d,
    label: new Date(d.date + "T00:00:00Z").toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" }),
  }))
  const empty = data.every((d) => d.deployed + d.deploy_failed + d.jobs_succeeded + d.jobs_failed === 0)
  const rate = delivery.change_failure_rate
  return (
    <section aria-labelledby="delivery-heading" className="quiet-surface overflow-hidden rounded-xl border border-border bg-card">
      <div className="flex flex-wrap items-baseline justify-between gap-2 border-b border-border/40 px-4 py-3">
        <h2 id="delivery-heading" className="text-sm font-semibold">Delivery</h2>
        <span className="text-xs text-muted-foreground">Last 14 days · numbers are production</span>
      </div>
      <div className="grid grid-cols-2 gap-px bg-border/40 md:grid-cols-4">
        <Metric term="overview.deploys-a-week" label="Deploys a week" value={delivery.deploys_per_week ? round(delivery.deploys_per_week) : "0"} note="successful, to production" />
        <Metric
          term="overview.change-failure-rate"
          label="Change failure rate"
          value={rate === undefined ? undefined : `${Math.round(rate * 100)}%`}
          note="deploys that failed"
          tone={rate === undefined ? undefined : rate > 0.3 ? "bad" : rate > 0.15 ? "warn" : "good"}
        />
        <Metric term="overview.time-to-recover" label="Time to recover" value={duration(delivery.recovery_seconds)} note="failed deploy to the next good one" />
        <Metric term="overview.staging-to-production" label="Staging to production" value={duration(delivery.promotion_seconds)} note="build to promotion, median" />
      </div>
      <div className="px-4 pb-3 pt-4">
        {empty ? (
          <p className="py-10 text-center text-sm text-muted-foreground">Nothing has deployed or run in the last 14 days.</p>
        ) : (
          <ChartContainer
            config={{
              deployed: { label: "Deployed", color: COLORS.deployed },
              deploy_failed: { label: "Deploy failed", color: COLORS.deploy_failed },
              jobs_succeeded: { label: "Job ran", color: COLORS.jobs_succeeded },
              jobs_failed: { label: "Job failed", color: COLORS.jobs_failed },
            }}
            className="aspect-auto h-[180px] w-full"
          >
            <BarChart data={data} margin={{ top: 4, right: 4, bottom: 0, left: 0 }} barGap={3} barCategoryGap="28%">
              <defs>
                {Object.entries(COLORS).map(([key, color]) => (
                  <linearGradient key={key} id={`delivery-${key}`} x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor={color} stopOpacity={0.95} />
                    <stop offset="100%" stopColor={color} stopOpacity={0.45} />
                  </linearGradient>
                ))}
              </defs>
              <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="oklch(0.28 0 0)" />
              <XAxis dataKey="label" tick={{ fontSize: 10 }} tickLine={false} axisLine={false} interval="preserveStartEnd" />
              <YAxis tick={{ fontSize: 10 }} tickLine={false} axisLine={false} width={28} allowDecimals={false} />
              <ChartTooltip cursor={{ fill: "oklch(1 0 0 / 4%)" }} content={DayTooltip} />
              {/* The top segment of each stack gets the rounded corners,
                  whichever one it is on that day. */}
              <Bar dataKey="deployed" stackId="deploys" fill="url(#delivery-deployed)" maxBarSize={14} shape={segment("deploy_failed")} isAnimationActive={false} />
              <Bar dataKey="deploy_failed" stackId="deploys" fill="url(#delivery-deploy_failed)" maxBarSize={14} shape={segment()} isAnimationActive={false} />
              <Bar dataKey="jobs_succeeded" stackId="jobs" fill="url(#delivery-jobs_succeeded)" maxBarSize={14} shape={segment("jobs_failed")} isAnimationActive={false} />
              <Bar dataKey="jobs_failed" stackId="jobs" fill="url(#delivery-jobs_failed)" maxBarSize={14} shape={segment()} isAnimationActive={false} />
            </BarChart>
          </ChartContainer>
        )}
        <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
          <Legend color={COLORS.deployed} label="Deployed" />
          <Legend color={COLORS.deploy_failed} label="Deploy failed" />
          <Legend color={COLORS.jobs_succeeded} label="Job ran" />
          <Legend color={COLORS.jobs_failed} label="Job failed" />
        </div>
      </div>
    </section>
  )
}

const COLORS = {
  deployed: "oklch(0.74 0.16 155)",
  deploy_failed: "oklch(0.66 0.19 25)",
  jobs_succeeded: "oklch(0.62 0.06 250)",
  jobs_failed: "oklch(0.58 0.14 25)",
} as const

/**
 * A bar segment, its top corners rounded when it is the top of its stack: the
 * upper segment always, the lower one on a day the upper one is empty.
 */
function segment(above?: string) {
  return function Segment(props: unknown) {
    const { x, y, width, height, fill, payload } = props as {
      x: number; y: number; width: number; height: number; fill: string; payload: Record<string, number>
    }
    if (!height || height <= 0) return <g />
    const top = !above || !payload[above]
    const r = top ? Math.min(4, width / 2, height) : 0
    return (
      <path
        fill={fill}
        d={`M${x},${y + height} V${y + r} Q${x},${y} ${x + r},${y} H${x + width - r} Q${x + width},${y} ${x + width},${y + r} V${y + height} Z`}
      />
    )
  }
}

function Metric({ label, value, note, tone, term }: { label: string; value?: string; note: string; tone?: "good" | "warn" | "bad"; term?: string }) {
  return (
    <div className="bg-card px-4 py-3" data-testid={`metric-${label}`}>
      <p className="flex items-center gap-1 text-xs text-muted-foreground">
        {label}
        {term && <TermInfo id={term} />}
      </p>
      <p
        className={cn(
          "mt-1 text-2xl font-semibold tabular-nums",
          !value && "text-base font-normal text-muted-foreground/60",
          tone === "good" && "text-emerald-400",
          tone === "warn" && "text-amber-400",
          tone === "bad" && "text-red-400"
        )}
      >
        {value ?? "no data"}
      </p>
      <p className="mt-0.5 text-[11px] text-muted-foreground">{note}</p>
    </div>
  )
}

function Legend({ color, label }: { color: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="size-2 rounded-sm" style={{ background: color }} />
      {label}
    </span>
  )
}

function DayTooltip(props: { active?: boolean; payload?: readonly { payload?: Record<string, number | string> }[] }) {
  const d = props.payload?.[0]?.payload
  if (!props.active || !d) return null
  return (
    <div className="space-y-0.5 rounded-lg border border-border/50 bg-background px-2.5 py-1.5 text-xs shadow-xl">
      <p className="text-muted-foreground">{d.label}</p>
      <p>{d.deployed} deployed{Number(d.deploy_failed) > 0 && <span className="text-red-400">, {d.deploy_failed} failed</span>}</p>
      <p className="text-muted-foreground">
        {d.jobs_succeeded} job {Number(d.jobs_succeeded) === 1 ? "run" : "runs"}
        {Number(d.jobs_failed) > 0 && <span className="text-red-400">, {d.jobs_failed} failed</span>}
      </p>
    </div>
  )
}

function round(n: number) {
  return n >= 10 ? String(Math.round(n)) : n.toFixed(1).replace(/\.0$/, "")
}

/** "45m", "3h", "2.5d": a duration at the scale it matters. */
export function duration(seconds?: number) {
  if (seconds === undefined) return undefined
  if (seconds < 3600) return `${Math.max(1, Math.round(seconds / 60))}m`
  if (seconds < 86_400) return `${round(seconds / 3600)}h`
  return `${round(seconds / 86_400)}d`
}

/** Fourteen days as tiny bars: a project's deploys, every level together. */
export function Sparkline({ counts, className }: { counts?: number[]; className?: string }) {
  if (!counts || counts.every((c) => c === 0)) return null
  const max = Math.max(...counts)
  const total = counts.reduce((a, b) => a + b, 0)
  const busiest = counts.indexOf(max)
  const busiestDay = new Date(Date.now() - (counts.length - 1 - busiest) * 86_400_000)
    .toLocaleDateString(undefined, { month: "short", day: "numeric" })
  return (
    <Tooltip>
      <TooltipTrigger
        render={<span className={cn("flex h-5 items-end gap-px", className)} data-testid="sparkline" />}
      >
        {counts.map((c, i) => (
          <span key={i} className="w-[3px] rounded-[1px] bg-primary/70" style={{ height: `${Math.max(c ? 20 : 6, (c / max) * 100)}%`, opacity: c ? 1 : 0.25 }} />
        ))}
      </TooltipTrigger>
      <TooltipContent>
        <div className="space-y-0.5">
          <p className="font-medium">
            {total} {total === 1 ? "deploy" : "deploys"} in the last 14 days
          </p>
          <p className="opacity-70">
            Every level together · busiest {busiestDay} ({max})
          </p>
        </div>
      </TooltipContent>
    </Tooltip>
  )
}
