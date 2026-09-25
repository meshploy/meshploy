import { Fragment, type ReactNode } from "react"
import { ArrowRight } from "lucide-react"
import { cn } from "@/lib/utils"
import { useHelp } from "./store"

/**
 * Renders a help section's Markdown: paragraphs, "- " lists, **bold**,
 * `code`, [links](url), and named diagrams (`::diagram name | fallback`).
 * The topics are Meshploy's own text, kept to this much on purpose: enough to
 * explain, little enough to style in the console's own way.
 */
export function HelpMarkdown({ source, className }: { source: string; className?: string }) {
  const blocks: ReactNode[] = []
  let list: string[] = []
  let para: string[] = []
  const flush = () => {
    if (para.length) blocks.push(<p key={blocks.length}>{inline(para.join(" "))}</p>)
    if (list.length)
      blocks.push(
        <ul key={blocks.length} className="list-disc space-y-1 pl-5">
          {list.map((item, i) => (
            <li key={i}>{inline(item)}</li>
          ))}
        </ul>
      )
    para = []
    list = []
  }
  for (const line of source.split("\n")) {
    const diagram = /^::diagram ([a-z-]+)(?: \| (.+))?$/.exec(line)
    if (diagram) {
      flush()
      blocks.push(<Diagram key={blocks.length} name={diagram[1]} fallback={diagram[2] ?? ""} />)
    } else if (line.startsWith("- ")) {
      if (para.length) flush()
      list.push(line.slice(2))
    } else if (line.trim() === "") {
      flush()
    } else if (list.length && line.startsWith("  ")) {
      list[list.length - 1] += " " + line.trim()
    } else {
      if (list.length) flush()
      para.push(line.trim())
    }
  }
  flush()
  return <div className={cn("space-y-3 text-sm leading-relaxed text-muted-foreground [&_strong]:font-medium [&_strong]:text-foreground", className)}>{blocks}</div>
}

function inline(text: string): ReactNode[] {
  const out: ReactNode[] = []
  const re = /\*\*(.+?)\*\*|`(.+?)`|\[(.+?)\]\((.+?)\)/g
  let last = 0
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index > last) out.push(text.slice(last, m.index))
    if (m[1]) out.push(<strong key={out.length}>{m[1]}</strong>)
    else if (m[2]) out.push(<code key={out.length} className="rounded bg-muted/50 px-1 py-0.5 font-mono text-[0.85em] text-foreground">{m[2]}</code>)
    else if (m[4].startsWith("/concepts/")) out.push(<TopicLink key={out.length} href={m[4]}>{m[3]}</TopicLink>)
    else out.push(<a key={out.length} href={m[4]} target="_blank" rel="noopener noreferrer" className="text-primary hover:underline">{m[3]}</a>)
    last = m.index + m[0].length
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

/**
 * A link to another topic (/concepts/<topic>#<section>, the docs site's path)
 * opens that topic in the drawer, at the section, rather than leaving the
 * console for a page it does not have.
 */
function TopicLink({ href, children }: { href: string; children: ReactNode }) {
  const show = useHelp((s) => s.show)
  const setPinned = useHelp((s) => s.setPinned)
  const [, topic, section] = /^\/concepts\/([a-z-]+)(?:#([a-z0-9-]+))?/.exec(href) ?? []
  return (
    <button
      type="button"
      className="text-primary hover:underline"
      onClick={() => {
        setPinned(true) // the reader chose this topic: it stays while they move
        show(topic, section)
      }}
    >
      {children}
    </button>
  )
}

/** The diagrams topics can name, drawn in the console's style. */
function Diagram({ name, fallback }: { name: string; fallback: string }) {
  if (name === "level-chain") return <LevelChain levels={fallback.split("→").map((s) => s.trim())} />
  return <p className="font-mono text-xs">{fallback}</p>
}

/** Levels left to right, lowest first, the way changes travel; production last. */
export function LevelChain({ levels, className }: { levels: string[]; className?: string }) {
  return (
    <div className={cn("flex flex-wrap items-center gap-1.5 rounded-lg border border-border/60 bg-muted/10 p-3", className)} data-testid="level-chain-diagram">
      {levels.map((l, i) => {
        const production = i === levels.length - 1
        return (
          <Fragment key={l}>
            {i > 0 && <ArrowRight className="size-3.5 text-muted-foreground/60" />}
            <span
              className={cn(
                "inline-flex items-center gap-1.5 rounded-md border px-2 py-1 font-mono text-xs",
                production ? "border-emerald-500/40 text-emerald-300" : "border-amber-500/30 text-amber-300/90"
              )}
            >
              <span className={cn("size-1.5 rounded-full", production ? "bg-emerald-400" : "bg-amber-400")} />
              {l}
            </span>
          </Fragment>
        )
      })}
    </div>
  )
}
