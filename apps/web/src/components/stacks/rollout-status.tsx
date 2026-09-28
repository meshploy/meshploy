import type { RolloutStepStatus, StackRunStatus } from "@/lib/api"

// How a stack run and each service in its rollout read, in the same dots and
// colours as a service's deployments.

const RUN: Record<StackRunStatus, { label: string; dot: string; text: string }> = {
  running:     { label: "Rolling out", dot: "bg-amber-400 animate-pulse", text: "text-amber-400" },
  succeeded:   { label: "Done",        dot: "bg-emerald-400",            text: "text-emerald-400" },
  failed:      { label: "Failed",      dot: "bg-destructive",            text: "text-destructive" },
  stopped:     { label: "Stopped",     dot: "bg-destructive",            text: "text-destructive" },
  interrupted: { label: "Interrupted", dot: "bg-muted-foreground/50",    text: "text-muted-foreground" },
}

const STEP: Record<RolloutStepStatus, { label: string; dot: string; text: string }> = {
  waiting:     { label: "Waiting",     dot: "bg-muted-foreground/40",    text: "text-muted-foreground" },
  started:     { label: "Building or deploying", dot: "bg-amber-400 animate-pulse", text: "text-amber-400" },
  succeeded:   { label: "Done",        dot: "bg-emerald-400",            text: "text-emerald-400" },
  failed:      { label: "Failed",      dot: "bg-destructive",            text: "text-destructive" },
  not_started: { label: "Not started", dot: "bg-muted-foreground/40",    text: "text-muted-foreground" },
}

export function RunStatus({ status }: { status: StackRunStatus }) {
  const s = RUN[status] ?? RUN.interrupted
  return (
    <span className={`inline-flex items-center gap-1.5 text-xs font-medium ${s.text}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${s.dot}`} />{s.label}
    </span>
  )
}

export function StepStatus({ status }: { status: RolloutStepStatus }) {
  const s = STEP[status] ?? STEP.waiting
  return (
    <span className={`inline-flex items-center gap-1.5 text-xs ${s.text}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${s.dot}`} />{s.label}
    </span>
  )
}
