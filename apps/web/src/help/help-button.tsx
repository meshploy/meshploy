import { CircleHelp } from "lucide-react"
import { cn } from "@/lib/utils"
import { useHelp } from "./store"

/** A page's "?": opens its explanation in the help drawer. */
export function HelpButton({ topic, section, label = "How this works", className }: { topic: string; section?: string; label?: string; className?: string }) {
  const show = useHelp((s) => s.show)
  return (
    <button
      type="button"
      onClick={() => show(topic, section)}
      aria-label={label}
      title={label}
      className={cn("inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground", className)}
    >
      <CircleHelp className="size-4" />
    </button>
  )
}
