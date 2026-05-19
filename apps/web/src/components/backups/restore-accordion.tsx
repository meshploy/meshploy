import { useState } from "react"
import { ChevronDown, ChevronRight, HardDrive, Loader2, RotateCcw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn, formatBytes } from "@/lib/utils"
import type { BackupObject } from "@/lib/api"

interface RestoreAccordionProps {
  objects: BackupObject[] | undefined
  isLoading: boolean
  onRestore: (key: string) => void
  isRestorePending: boolean
  restoringKey: string | null
  onOpenChange?: (open: boolean) => void
}

export function RestoreAccordion({
  objects,
  isLoading,
  onRestore,
  isRestorePending,
  restoringKey,
  onOpenChange,
}: RestoreAccordionProps) {
  const [open, setOpen] = useState(false)

  function toggle() {
    const next = !open
    setOpen(next)
    onOpenChange?.(next)
  }
  const count = objects?.length ?? 0

  return (
    <div className="border-t border-border/40 -mx-4 px-4 mt-2">
      <button
        onClick={toggle}
        className="flex items-center gap-1.5 w-full text-xs text-muted-foreground/50 hover:text-muted-foreground py-2 transition-colors"
      >
        {open ? <ChevronDown className="h-3 w-3 shrink-0" /> : <ChevronRight className="h-3 w-3 shrink-0" />}
        <span>{isLoading && open ? "Loading restore points…" : `Restore points${count > 0 ? ` (${count})` : ""}`}</span>
      </button>

      {open && (
        <div className="pb-2 space-y-0.5">
          {isLoading ? (
            <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
              <Loader2 className="h-3 w-3 animate-spin" />
              <span>Loading…</span>
            </div>
          ) : !objects || objects.length === 0 ? (
            <div className="flex items-center gap-2 text-xs text-muted-foreground/40 py-2">
              <HardDrive className="h-3 w-3 shrink-0" />
              <span>No restore points found</span>
            </div>
          ) : (
            objects.map((obj) => {
              const isThisOne = restoringKey === obj.key && isRestorePending
              const filename = obj.key.split("/").pop() ?? obj.key
              return (
                <div
                  key={obj.key}
                  className={cn(
                    "flex items-center justify-between rounded-md px-2 py-1.5 gap-3",
                    "hover:bg-muted/20 transition-colors"
                  )}
                >
                  <div className="flex-1 min-w-0">
                    <code className="text-xs font-mono text-foreground/70 truncate block">{filename}</code>
                    <span className="text-xs text-muted-foreground/40">
                      {formatBytes(obj.size)} · {new Date(obj.last_modified).toLocaleString()}
                    </span>
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    className="h-6 text-xs px-2 gap-1 shrink-0"
                    onClick={() => onRestore(obj.key)}
                    disabled={isRestorePending}
                    title={`Restore from ${filename}`}
                  >
                    {isThisOne
                      ? <Loader2 className="h-3 w-3 animate-spin" />
                      : <RotateCcw className="h-3 w-3" />
                    }
                  </Button>
                </div>
              )
            })
          )}
        </div>
      )}
    </div>
  )
}
