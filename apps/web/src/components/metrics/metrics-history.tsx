import { useId, useMemo, useState, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts"
import { Loader2 } from "lucide-react"
import { ChartContainer, ChartTooltip } from "@/components/ui/chart"
import { SegmentedControl } from "@/components/ui/segmented-control"
import type { ApiMetricHistory, MetricRange } from "@/lib/api"

const RANGES: { value: MetricRange; label: string; days: number }[] = [
  { value: "1h", label: "1h", days: 0 },
  { value: "6h", label: "6h", days: 0 },
  { value: "24h", label: "24h", days: 1 },
  { value: "7d", label: "7d", days: 7 },
  { value: "30d", label: "30d", days: 30 },
]

/** One chart: a value from each point, and how to show it. */
export interface HistorySeries<P> {
  label: string
  color: string
  value: (p: P) => number | null
  format: (v: number) => string
  /** The fixed axis, for a percentage. */
  domain?: [number, number]
  /** The axis's labels, when the value's own words are too long for it. */
  axis?: (v: number) => string
  /** A count: the axis has whole numbers only. */
  whole?: boolean
  icon?: ReactNode
}

/**
 * What the page reads from the source right now, newer than anything stored:
 * appended to each chart so its line ends where its number says.
 */
export interface LiveTail {
  points: { t: number; values: (number | null)[] }[]
  /** Per series, a line under the number, such as "5 vCPU · 1.15 in use". */
  details?: (string | undefined)[]
}

type Row = { t: number } & Record<string, number | null>

/**
 * A node's or service's stored metrics over a range the person picks, one
 * chart per series. Ranges past what the organization keeps are not offered.
 */
export function MetricsHistory<P extends { at: string }>({
  queryKey,
  load,
  series,
  empty,
  live,
}: {
  queryKey: unknown[]
  load: (range: MetricRange) => Promise<ApiMetricHistory<P>>
  series: HistorySeries<P>[]
  empty: string
  live?: LiveTail
}) {
  const [range, setRange] = useState<MetricRange>("24h")
  const q = useQuery({
    queryKey: [...queryKey, "metrics-history", range],
    queryFn: () => load(range),
    refetchInterval: 60_000,
    retry: false,
  })
  const keep = q.data?.retention_days ?? 30
  const offered = RANGES.filter((r) => r.days <= keep)

  const rows = useMemo<Row[]>(() => {
    const stored = (q.data?.points ?? []).map((p) => {
      const row: Row = { t: new Date(p.at).getTime() }
      series.forEach((s, i) => (row[`s${i}`] = s.value(p)))
      return row
    })
    // The live readings carry on from the last stored minute, so the chart
    // runs up to now and its last point is the number above it.
    const after = stored.length ? stored[stored.length - 1].t : 0
    const tail = (live?.points ?? []).filter((p) => p.t > after).map((p) => {
      const row: Row = { t: p.t }
      p.values.forEach((v, i) => (row[`s${i}`] = v))
      return row
    })
    return [...stored, ...tail]
  }, [q.data, series, live])
  const long = range === "7d" || range === "30d"
  const tick = (t: number) =>
    new Date(t).toLocaleString([], long ? { month: "short", day: "numeric" } : { hour: "2-digit", minute: "2-digit", hour12: false })
  const full = (t: number) =>
    new Date(t).toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", hour12: false })

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {live ? "Updates every few seconds; kept" : "Stored every minute, kept"} {keep} {keep === 1 ? "day" : "days"}
          {q.data?.resolution === "5m" ? ". Each point is five minutes, averaged" : ""}.
        </p>
        <SegmentedControl value={range} onValueChange={setRange} options={offered} />
      </div>
      {q.isPending ? (
        <div className="flex h-40 items-center justify-center">
          <Loader2 className="size-4 animate-spin text-muted-foreground" />
        </div>
      ) : q.isError && rows.length < 2 ? (
        <p className="rounded-lg border border-border/60 bg-card px-4 py-8 text-center text-sm text-muted-foreground">
          {(q.error as Error).message}
        </p>
      ) : rows.length < 2 ? (
        <p className="rounded-lg border border-border/60 bg-card px-4 py-8 text-center text-sm text-muted-foreground">{empty}</p>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {series.map((s, i) => (
            <HistoryChart key={s.label} series={s} dataKey={`s${i}`} rows={rows} tick={tick} full={full} detail={live?.details?.[i]} />
          ))}
        </div>
      )}
    </div>
  )
}

function HistoryChart<P>({
  series,
  dataKey,
  rows,
  tick,
  full,
  detail,
}: {
  series: HistorySeries<P>
  dataKey: string
  rows: Row[]
  tick: (t: number) => string
  full: (t: number) => string
  detail?: string
}) {
  const gradient = `mh-${useId().replace(/:/g, "")}`
  // The newest reading this series has: a live reading may lack one, such as
  // the pods while their list is still loading.
  const last = [...rows].reverse().find((r) => r[dataKey] != null)?.[dataKey]
  const percent = series.domain?.[0] === 0 && series.domain?.[1] === 100
  return (
    <div className="space-y-3 rounded-lg border border-border/60 bg-card p-4">
      <div className="space-y-2">
        <div className="flex items-center gap-1.5 text-muted-foreground">
          {series.icon}
          <span className="text-xs font-medium">{series.label}</span>
        </div>
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <span className="text-2xl font-semibold leading-none tabular-nums">{last != null ? series.format(last) : "-"}</span>
          {detail && <span className="text-xs text-muted-foreground tabular-nums">{detail}</span>}
        </div>
        {percent && (
          <div className="h-1.5 overflow-hidden rounded-full bg-muted/50">
            <div className="h-full rounded-full transition-[width] duration-500" style={{ width: `${Math.min(100, Math.max(0, last ?? 0))}%`, background: series.color }} />
          </div>
        )}
      </div>
      <ChartContainer config={{ [dataKey]: { label: series.label, color: series.color } }} className="aspect-auto h-[140px] w-full">
        <AreaChart data={rows} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id={gradient} x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor={series.color} stopOpacity={0.3} />
              <stop offset="95%" stopColor={series.color} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="var(--border)" />
          <XAxis
            dataKey="t"
            type="number"
            scale="time"
            domain={["dataMin", "dataMax"]}
            tickFormatter={tick}
            tick={{ fontSize: 10 }}
            tickLine={false}
            axisLine={false}
            minTickGap={32}
          />
          <YAxis
            tick={{ fontSize: 10 }}
            tickLine={false}
            axisLine={false}
            tickFormatter={series.axis ?? series.format}
            allowDecimals={!series.whole}
            width={percent ? 40 : 68}
            domain={series.domain ?? [0, "auto"]}
          />
          <ChartTooltip
            content={({ active, payload, label }) => {
              const v = payload?.[0]?.value
              if (!active || v == null) return null
              return (
                <div className="space-y-0.5 rounded-lg border border-border/50 bg-background px-2.5 py-1.5 text-xs shadow-xl">
                  <p className="text-muted-foreground">{full(Number(label))}</p>
                  <p className="font-mono font-semibold text-foreground">{series.format(Number(v))}</p>
                </div>
              )
            }}
          />
          <Area
            type="monotone"
            dataKey={dataKey}
            stroke={series.color}
            strokeWidth={1.5}
            fill={`url(#${gradient})`}
            dot={false}
            isAnimationActive={false}
          />
        </AreaChart>
      </ChartContainer>
    </div>
  )
}

/** Bytes in the largest unit that keeps the number readable. */
export function formatBytes(v: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"]
  let i = 0
  while (Math.abs(v) >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}
