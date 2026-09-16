import { cn } from "@/lib/utils"
import { Field, inputCls } from "@/components/services/form-primitives"
import { SegmentedControl } from "@/components/ui/segmented-control"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"

export const CRON_PRESETS = [
  { label: "Every 5 min", value: "*/5 * * * *" },
  { label: "Hourly",      value: "0 * * * *"   },
  { label: "Daily",       value: "0 0 * * *"   },
  { label: "Weekly",      value: "0 0 * * 0"   },
  { label: "Monthly",     value: "0 0 1 * *"   },
]

export const CONCURRENCY_OPTIONS = [
  { value: "allow",   label: "Allow",   hint: "Multiple runs can overlap." },
  { value: "forbid",  label: "Forbid",  hint: "Skip if already running." },
  { value: "replace", label: "Replace", hint: "Cancel running, start new." },
]

interface CronScheduleBlockProps {
  enabled: boolean
  onToggle: () => void
  schedule: string
  onScheduleChange: (v: string) => void
  concurrency: string
  onConcurrencyChange: (v: string) => void
  historyLimit: string
  onHistoryLimitChange: (v: string) => void
}

export function CronScheduleBlock({
  enabled,
  onToggle,
  schedule,
  onScheduleChange,
  concurrency,
  onConcurrencyChange,
  historyLimit,
  onHistoryLimitChange,
}: CronScheduleBlockProps) {
  return (
    // Sits among Sections, so it carries their box: same radius, border and
    // padding, rather than a tighter one of its own.
    <div className="rounded-[12px] border border-border bg-card overflow-hidden">
      <div className="flex items-center justify-between gap-4 px-[22px] py-4">
        <div>
          <p className="text-sm font-medium text-foreground">Run on a schedule</p>
          <p className="text-xs text-muted-foreground mt-0.5">Repeat this job on a cron expression</p>
        </div>
        <Switch checked={enabled} onCheckedChange={onToggle} className="shrink-0" />
      </div>

      {enabled && (
        <div className="border-t border-border px-[22px] pb-[22px] pt-5 space-y-4">
          <Field label="Cron expression" required>
            <div className="flex flex-wrap gap-1.5 mb-2">
              {CRON_PRESETS.map((p) => (
                <Button
                  key={p.value}
                  variant="ghost"
                  onClick={() => onScheduleChange(p.value)}
                  className={cn(
                    "px-2.5 py-1 text-xs rounded-md border transition-colors",
                    schedule === p.value
                      ? "border-foreground/40 bg-foreground/10 text-foreground"
                      : "border-border/40 text-muted-foreground hover:text-foreground hover:border-foreground/30"
                  )}
                >
                  {p.label}
                </Button>
              ))}
            </div>
            <input
              value={schedule}
              onChange={(e) => onScheduleChange(e.target.value)}
              placeholder="*/5 * * * *"
              className={cn(inputCls, "font-mono text-xs")}
            />
            <p className="text-xs text-muted-foreground/40 mt-1">5-field cron: minute hour day month weekday</p>
          </Field>

          <div className="grid grid-cols-2 gap-4">
            <Field label="Concurrency policy">
              <SegmentedControl
                value={concurrency}
                onValueChange={onConcurrencyChange}
                options={CONCURRENCY_OPTIONS}
                className="w-fit"
              />
              <p className="text-xs text-muted-foreground/50 mt-1.5">
                {CONCURRENCY_OPTIONS.find((o) => o.value === concurrency)?.hint}
              </p>
            </Field>
            <Field label="History limit">
              <input
                type="number"
                min={1}
                max={50}
                value={historyLimit}
                onChange={(e) => onHistoryLimitChange(e.target.value)}
                className={cn(inputCls, "text-xs")}
              />
              <p className="text-xs text-muted-foreground/50 mt-1.5">Completed runs to keep.</p>
            </Field>
          </div>
        </div>
      )}
    </div>
  )
}
