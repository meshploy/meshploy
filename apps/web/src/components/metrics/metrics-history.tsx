import { useId, useMemo, useState } from "react"
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
}: {
  queryKey: unknown[]
  load: (range: MetricRange) => Promise<ApiMetricHistory<P>>
  series: HistorySeries<P>[]
  empty: string
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

  const rows = useMemo<Row[]>(
    () =>
      (q.data?.points ?? []).map((p) => {
        const row: Row = { t: new Date(p.at).getTime() }
        series.forEach((s, i) => (row[`s${i}`] = s.value(p)))
        return row
      }),
    [q.data, series],
  )
  const long = range === "7d" || range === "30d"
  const tick = (t: number) =>
    new Date(t).toLocaleString([], long ? { month: "short", day: "numeric" } : { hour: "2-digit", minute: "2-digit", hour12: false })
  const full = (t: number) =>
    new Date(t).toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", hour12: false })

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          Stored every minute, kept {keep} {keep === 1 ? "day" : "days"}
          {q.data?.resolution === "5m" ? ". Each point is five minutes, averaged" : ""}.
        </p>
        <SegmentedControl value={range} onValueChange={setRange} options={offered} />
      </div>
      {q.isPending ? (
        <div className="flex h-40 items-center justify-center">
          <Loader2 className="size-4 animate-spin text-muted-foreground" />
        </div>
      ) : q.isError ? (
        <p className="rounded-lg border border-border/60 bg-card px-4 py-8 text-center text-sm text-muted-foreground">
          {(q.error as Error).message}
        </p>
      ) : rows.length < 2 ? (
        <p className="rounded-lg border border-border/60 bg-card px-4 py-8 text-center text-sm text-muted-foreground">{empty}</p>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {series.map((s, i) => (
            <HistoryChart key={s.label} series={s} dataKey={`s${i}`} rows={rows} tick={tick} full={full} />
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
}: {
  series: HistorySeries<P>
  dataKey: string
  rows: Row[]
  tick: (t: number) => string
  full: (t: number) => string
}) {
  const gradient = `mh-${useId().replace(/:/g, "")}`
  const last = rows[rows.length - 1]?.[dataKey]
  return (
    <div className="space-y-3 rounded-lg border border-border/60 bg-card p-4">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-muted-foreground">{series.label}</span>
        <span className="text-sm font-semibold tabular-nums">{last != null ? series.format(last) : "-"}</span>
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
            tickFormatter={series.format}
            width={52}
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
