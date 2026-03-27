import { cn } from "@/lib/utils"
import type { NodeStatus } from "@/types"

interface NodeStatusDotProps {
  status: NodeStatus
  className?: string
}

export function NodeStatusDot({ status, className }: NodeStatusDotProps) {
  const isOnline = status === "online"

  return (
    <span className={cn("relative inline-flex h-2 w-2", className)}>
      {isOnline && (
        <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-50" />
      )}
      <span
        className={cn(
          "relative inline-flex h-2 w-2 rounded-full",
          isOnline ? "bg-emerald-400" : "bg-zinc-600"
        )}
      />
    </span>
  )
}
