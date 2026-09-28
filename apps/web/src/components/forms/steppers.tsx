import { ChevronDown, Minus, Plus } from "lucide-react"
import { Select as SelectPrimitive } from "@base-ui/react/select"
import { Select, SelectContent, SelectItem, SelectValue } from "@/components/ui/select"
import { cn } from "@/lib/utils"

// Resources are picked, not typed: − and + step through sizes that make sense,
// and the value between them opens the whole list. A typed "2G" or "1000"
// read as something else by Kubernetes (2 GB, not 2 GiB; a thousand cores) is
// not possible this way. A value set elsewhere that is not on the list, from a
// compose file or the API, stays where it sorts, so nothing is lost by
// opening a form.

const NONE = "__none"

type QuantityProps = {
  value: string
  onChange: (value: string) => void
  /** What an empty value means here ("No cap", "Default"); offered first. Without it an empty value can only be left. */
  emptyLabel?: string
  disabled?: boolean
  className?: string
  "aria-label"?: string
  /** The most the node it runs on has, in the stepper's own unit (millicores, bytes): larger sizes are not offered, since nothing could run them. */
  max?: number
}

function QuantityStepper({ value, onChange, emptyLabel, ladder, parse, format, max, disabled, className, ...rest }: QuantityProps & {
  ladder: string[]
  parse: (v: string) => number | null
  format: (v: string) => string
}) {
  const current = value.trim()
  const currentSize = current ? parse(current) : null
  const fits = (v: string) => max === undefined || (parse(v) ?? 0) <= max
  const items = ladder.filter(fits)
  // One already set that the list lacks joins it, in its place.
  if (current && currentSize !== null && !items.some((v) => parse(v) === currentSize)) items.push(current)
  items.sort((a, b) => (parse(a) ?? 0) - (parse(b) ?? 0))
  const options = [...(emptyLabel || !current ? [""] : []), ...items]
  const index = current
    ? Math.max(0, options.findIndex((v) => v !== "" && parse(v) === currentSize))
    : 0
  const label = (v: string) => (v === "" ? emptyLabel ?? "Not set" : format(v))
  const shown = current && currentSize === null ? current : label(current ? options[index] : "")
  // Set before, or elsewhere, above what the node has: kept, and said.
  const over = current !== "" && !fits(current)

  return (
    <div className={cn("flex h-9 w-full items-stretch overflow-hidden rounded-lg border border-border/60 bg-muted/20", className)}>
      <StepButton icon={Minus} label="Smaller" disabled={disabled || index <= 0 || (!emptyLabel && options[index - 1] === "")}
        onClick={() => onChange(options[index - 1])} />
      <Select value={current ? options[index] : NONE} disabled={disabled}
        onValueChange={(v) => onChange(v === NONE || v == null ? "" : String(v))}>
        {/* Its own trigger, not the shared one: that one draws a rounded
            border and background of its own, which showed as a box inside
            the stepper. Only the dividers between the three parts here. */}
        <SelectPrimitive.Trigger aria-label={rest["aria-label"]}
          title={over ? "More than the node it runs on has: it could not be scheduled" : undefined}
          className={cn("flex min-w-0 flex-1 cursor-pointer items-center justify-center gap-1.5 border-x border-border/60 px-2 text-sm font-medium tabular-nums outline-none transition-colors hover:bg-muted/40 focus-visible:bg-muted/40 disabled:cursor-not-allowed disabled:opacity-50", over && "text-amber-400")}>
          <SelectValue className="flex-none">{shown}</SelectValue>
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        </SelectPrimitive.Trigger>
        <SelectContent>
          {options.map((v) => (
            <SelectItem key={v || NONE} value={v || NONE}>{label(v)}</SelectItem>
          ))}
        </SelectContent>
      </Select>
      <StepButton icon={Plus} label="Larger" disabled={disabled || index >= options.length - 1}
        onClick={() => onChange(options[index + 1])} />
    </div>
  )
}

function StepButton({ icon: Icon, label, disabled, onClick }: {
  icon: typeof Plus; label: string; disabled?: boolean; onClick: () => void
}) {
  return (
    <button type="button" aria-label={label} disabled={disabled} onClick={onClick}
      className="flex w-9 shrink-0 items-center justify-center text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground disabled:pointer-events-none disabled:opacity-30">
      <Icon className="h-3.5 w-3.5" />
    </button>
  )
}

// ── CPU ──────────────────────────────────────────────────────────────────────

const CPU_LADDER = ["50m", "100m", "250m", "500m", "750m", "1", "1500m", "2", "3", "4", "6", "8", "12", "16"]

/** Millicores of a Kubernetes CPU quantity: "250m", "1", "1.5". */
export function cpuMillis(v: string): number | null {
  const s = v.trim()
  const m = /^(\d+(?:\.\d+)?)(m?)$/.exec(s)
  if (!m) return null
  const n = parseFloat(m[1])
  return m[2] ? n : n * 1000
}

export function formatCpu(v: string): string {
  const m = cpuMillis(v)
  if (m === null) return v
  return `${+(m / 1000).toFixed(3)} CPU`
}

export function CpuStepper(props: QuantityProps) {
  return <QuantityStepper {...props} ladder={CPU_LADDER} parse={cpuMillis} format={formatCpu} />
}

// ── Memory ───────────────────────────────────────────────────────────────────

const MEMORY_LADDER = ["64Mi", "128Mi", "256Mi", "384Mi", "512Mi", "768Mi", "1Gi", "1536Mi", "2Gi", "3Gi", "4Gi",
  "6Gi", "8Gi", "12Gi", "16Gi", "24Gi", "32Gi", "48Gi", "64Gi"]

const MEMORY_UNITS: Record<string, number> = {
  "": 1, k: 1e3, K: 1e3, M: 1e6, G: 1e9, T: 1e12, Ki: 1024, Mi: 1024 ** 2, Gi: 1024 ** 3, Ti: 1024 ** 4,
}

/** Bytes of a Kubernetes memory quantity: "512Mi", "2Gi", "1G". */
export function memoryBytes(v: string): number | null {
  const m = /^(\d+(?:\.\d+)?)(Ki|Mi|Gi|Ti|k|K|M|G|T)?$/.exec(v.trim())
  if (!m) return null
  return parseFloat(m[1]) * MEMORY_UNITS[m[2] ?? ""]
}

export function formatMemory(v: string): string {
  const b = memoryBytes(v)
  if (b === null) return v
  const gib = b / 1024 ** 3
  return gib >= 1 ? `${+gib.toFixed(2)} GiB` : `${+(b / 1024 ** 2).toFixed(0)} MiB`
}

export function MemoryStepper(props: QuantityProps) {
  return <QuantityStepper {...props} ladder={MEMORY_LADDER} parse={memoryBytes} format={formatMemory} />
}

// ── Storage ──────────────────────────────────────────────────────────────────

const STORAGE_LADDER = [1, 2, 5, 10, 20, 30, 50, 75, 100, 150, 200, 250, 500, 750, 1000]

/** A size in GiB, as volumes and databases are created (storage_gb becomes "<n>Gi"). */
export function StorageStepper({ value, onChange, min = 1, max = 1000, emptyLabel, disabled, className, ...rest }: {
  value: number | ""; onChange: (gb: number | "") => void; min?: number; max?: number
  /** What an empty value means here ("Default"); offered first. */
  emptyLabel?: string
  disabled?: boolean; className?: string; "aria-label"?: string
}) {
  const ladder = STORAGE_LADDER.filter((n) => n >= min && n <= max).map(String)
  return (
    <QuantityStepper value={value === "" ? "" : String(value)} emptyLabel={emptyLabel} disabled={disabled}
      onChange={(v) => onChange(v === "" ? "" : Number(v) || min)}
      className={className} aria-label={rest["aria-label"]} ladder={ladder}
      parse={(v) => (/^\d+$/.test(v) ? Number(v) : null)} format={(v) => `${v} GiB`} />
  )
}

// ── Counts ───────────────────────────────────────────────────────────────────

/** A whole number: replicas, how many to keep. */
export function CountStepper({ value, onChange, min = 1, max = 50, unit, disabled, className, ...rest }: {
  value: number; onChange: (n: number) => void; min?: number; max?: number
  /** Said after the number: "replica", pluralised. */
  unit?: string
  disabled?: boolean; className?: string; "aria-label"?: string
}) {
  const n = Number.isFinite(value) ? value : min
  return (
    <div className={cn("flex h-9 w-full items-stretch overflow-hidden rounded-lg border border-border/60 bg-muted/20", className)}>
      <StepButton icon={Minus} label="Fewer" disabled={disabled || n <= min} onClick={() => onChange(Math.max(min, n - 1))} />
      <span aria-label={rest["aria-label"]} aria-live="polite"
        className={cn("flex flex-1 items-center justify-center border-x border-border/60 text-sm font-medium tabular-nums",
          disabled && "opacity-50")}>
        {n}{unit ? ` ${unit}${n === 1 ? "" : "s"}` : ""}
      </span>
      <StepButton icon={Plus} label="More" disabled={disabled || n >= max} onClick={() => onChange(Math.min(max, n + 1))} />
    </div>
  )
}
