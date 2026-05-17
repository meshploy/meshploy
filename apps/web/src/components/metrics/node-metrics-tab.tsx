import { useMemo, useState, useId } from "react"
import { Loader2 } from "lucide-react"
import { Cpu, HardDrive, MemoryStick, Network } from "lucide-react"
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts"
import { useMetricsStore, type RawSample } from "@/store/metrics-store"
import { type MetricsPayload } from "@/store/tab-store"
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import { cn } from "@/lib/utils"

const EMPTY_HISTORY: RawSample[] = []

const RANGES = [
  { label: "1m",  seconds: 60 },
  { label: "5m",  seconds: 300 },
  { label: "15m", seconds: 900 },
  { label: "All", seconds: Infinity },
] as const

interface ChartPoint {
  time: string
  cpu:  number | null
  mem:  number
  disk: number
  net:  number | null
}

function buildChartData(history: RawSample[], seconds: number): ChartPoint[] {
  if (history.length < 2) return []
  const cutoff = seconds === Infinity ? 0 : Date.now() - seconds * 1000
  const filtered = history.filter((s) => s.ts >= cutoff)
  if (filtered.length < 2) return []

  return filtered.slice(1).map((c, i) => {
    const p = filtered[i]
    const dTotal = c.cpuTotal - p.cpuTotal
    const dIdle  = c.cpuIdle  - p.cpuIdle
    const cpu = dTotal > 0 ? (1 - dIdle / dTotal) * 100 : null

    const GB  = 1_073_741_824
    const mem  = c.memTotal  > 0 ? (1 - c.memAvail  / c.memTotal)  * 100 : 0
    const disk = c.diskTotal > 0 ? (1 - c.diskAvail / c.diskTotal) * 100 : 0

    const dtS = (c.ts - p.ts) / 1000
    const net = dtS > 0
      ? ((c.netRx - p.netRx + c.netTx - p.netTx) / dtS / 1_000_000) * 8
      : null

    const time = new Date(c.ts).toLocaleTimeString([], {
      hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
    })
    return { time, cpu, mem, disk, net }
  })
}

function MetricTooltip({
  active,
  payload,
  label,
  formatter,
}: {
  active?: boolean
  payload?: { value?: number }[]
  label?: string
  formatter: (v: number) => string
}) {
  if (!active || !payload?.length) return null
  const v = payload[0]?.value
  if (v == null) return null
  return (
    <div className="rounded-lg border border-border/50 bg-background px-2.5 py-1.5 text-xs shadow-xl space-y-0.5">
      <p className="text-muted-foreground">{label}</p>
      <p className="font-mono font-semibold text-foreground">{formatter(Number(v))}</p>
    </div>
  )
}

function FullChart({
  title,
  icon,
  data,
  dataKey,
  color,
  currentLabel,
  yFormatter,
  tooltipFormatter,
  yDomain,
}: {
  title: string
  icon: React.ReactNode
  data: ChartPoint[]
  dataKey: keyof ChartPoint
  color: string
  currentLabel: string
  yFormatter: (v: number) => string
  tooltipFormatter: (v: number) => string
  yDomain?: [number, number]
}) {
  const uid = useId().replace(/:/g, "")
  const gradientId = `mg-${uid}`

  return (
    <div className="rounded-lg border border-border/60 bg-card p-4 space-y-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5 text-muted-foreground">
          {icon}
          <span className="text-xs font-medium">{title}</span>
        </div>
        <span className="text-xl font-semibold tabular-nums">{currentLabel}</span>
      </div>
      <ChartContainer
        config={{ [dataKey]: { label: title, color } }}
        className="aspect-auto h-[160px] w-full"
      >
        <AreaChart data={data} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%"  stopColor={color} stopOpacity={0.3} />
              <stop offset="95%" stopColor={color} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="oklch(0.28 0 0)" />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 10 }}
            tickLine={false}
            axisLine={false}
            interval="preserveStartEnd"
          />
          <YAxis
            tick={{ fontSize: 10 }}
            tickLine={false}
            axisLine={false}
            tickFormatter={yFormatter}
            width={38}
            domain={yDomain ?? ["auto", "auto"]}
          />
          <ChartTooltip
            content={(props) => (
              <MetricTooltip
                active={props.active}
                payload={props.payload as unknown as { value?: number }[]}
                label={props.label as string}
                formatter={tooltipFormatter}
              />
            )}
          />
          <Area
            type="monotone"
            dataKey={dataKey as string}
            stroke={color}
            strokeWidth={1.5}
            fill={`url(#${gradientId})`}
            dot={false}
            isAnimationActive={false}
            connectNulls={false}
          />
        </AreaChart>
      </ChartContainer>
    </div>
  )
}

export function NodeMetricsTab({ payload }: { payload: MetricsPayload }) {
  const [rangeIdx, setRangeIdx] = useState(1)
  const history  = useMetricsStore((s) => s.history[payload.nodeId] ?? EMPTY_HISTORY)
  const chartData = useMemo(
    () => buildChartData(history, RANGES[rangeIdx].seconds),
    [history, rangeIdx],
  )

  const latest = chartData[chartData.length - 1]

  return (
    <div className="p-6 space-y-5 overflow-y-auto h-full">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-base font-semibold">{payload.nodeLabel}</h2>
          <p className="text-xs text-muted-foreground">Live metrics · updates every 5s</p>
        </div>
        <div className="flex items-center gap-0.5 rounded-md border border-border/60 p-0.5">
          {RANGES.map((r, i) => (
            <button
              key={r.label}
              onClick={() => setRangeIdx(i)}
              className={cn(
                "px-2.5 py-1 text-xs rounded font-medium transition-colors",
                i === rangeIdx
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>

      {chartData.length < 2 ? (
        <div className="flex flex-col items-center justify-center h-64 gap-3">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground/60" />
          <div className="text-center space-y-1">
            <p className="text-sm text-foreground/80">Collecting metrics</p>
            <p className="text-xs text-muted-foreground/60">Waiting for the first two samples to compute deltas</p>
          </div>
        </div>
      ) : (
        <div className="grid gap-4 grid-cols-1 lg:grid-cols-2">
          <FullChart
            title="CPU"
            icon={<Cpu className="h-3.5 w-3.5" />}
            data={chartData}
            dataKey="cpu"
            color="oklch(0.65 0.18 250)"
            currentLabel={latest?.cpu != null ? `${latest.cpu.toFixed(1)}%` : "—"}
            yFormatter={(v) => `${v.toFixed(0)}%`}
            tooltipFormatter={(v) => `${v.toFixed(1)}%`}
            yDomain={[0, 100]}
          />
          <FullChart
            title="Memory"
            icon={<MemoryStick className="h-3.5 w-3.5" />}
            data={chartData}
            dataKey="mem"
            color="oklch(0.65 0.18 300)"
            currentLabel={latest?.mem != null ? `${latest.mem.toFixed(1)}%` : "—"}
            yFormatter={(v) => `${v.toFixed(0)}%`}
            tooltipFormatter={(v) => `${v.toFixed(1)}%`}
            yDomain={[0, 100]}
          />
          <FullChart
            title="Disk"
            icon={<HardDrive className="h-3.5 w-3.5" />}
            data={chartData}
            dataKey="disk"
            color="oklch(0.72 0.18 70)"
            currentLabel={latest?.disk != null ? `${latest.disk.toFixed(1)}%` : "—"}
            yFormatter={(v) => `${v.toFixed(0)}%`}
            tooltipFormatter={(v) => `${v.toFixed(1)}%`}
            yDomain={[0, 100]}
          />
          <FullChart
            title="Network"
            icon={<Network className="h-3.5 w-3.5" />}
            data={chartData}
            dataKey="net"
            color="oklch(0.65 0.18 200)"
            currentLabel={latest?.net != null ? `${latest.net.toFixed(2)} Mbps` : "—"}
            yFormatter={(v) => `${v.toFixed(1)}`}
            tooltipFormatter={(v) => `${v.toFixed(2)} Mbps`}
          />
        </div>
      )}
    </div>
  )
}
