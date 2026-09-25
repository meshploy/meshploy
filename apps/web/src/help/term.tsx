import { Popover } from "@base-ui/react/popover"
import { Info } from "lucide-react"
import { cn } from "@/lib/utils"
import { term as findTerm } from "./topics"
import { useHelp } from "./store"

/**
 * An ⓘ beside a word the console uses in its own sense: the short meaning,
 * and "Learn more", which opens the help drawer at the section behind it.
 * Named "topic.term", from the topic's Terms section, so the text lives in
 * packages/help with everything else.
 */
export function TermInfo({ id, className }: { id: string; className?: string }) {
  const found = findTerm(id)
  const show = useHelp((s) => s.show)
  if (!found) return null
  const { topic, term } = found
  return (
    <Popover.Root>
      <Popover.Trigger
        aria-label={`What is ${term.label.toLowerCase()}?`}
        className={cn("inline-flex shrink-0 items-center text-muted-foreground/70 outline-none transition-colors hover:text-foreground focus-visible:text-foreground", className)}
        onClick={(e) => e.stopPropagation()}
      >
        <Info className="size-3.5" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner side="bottom" align="start" sideOffset={6} className="z-50">
          <Popover.Popup className="w-72 rounded-lg border border-border bg-popover p-3 text-xs shadow-xl outline-none" data-testid="term-popover">
            <p className="font-semibold text-foreground">{term.label}</p>
            <p className="mt-1 leading-relaxed text-muted-foreground">{term.text}</p>
            <Popover.Close
              className="mt-2 text-primary hover:underline"
              onClick={() => show(topic.id, term.section)}
            >
              Learn more →
            </Popover.Close>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}
