import { useEffect, useMemo, useRef, useState } from "react"
import { useRouterState } from "@tanstack/react-router"
import { BookOpen, ExternalLink, Pin, PinOff, Search, X } from "lucide-react"
import { cn } from "@/lib/utils"
import { HelpMarkdown } from "./render"
import { topic as topicById, topicForPath, topics } from "./topics"
import { useHelp } from "./store"

/** Where the full docs live; the drawer's topics are published there as Concepts. */
const DOCS_URL = "https://docs.meshploy.com/concepts/"

/**
 * The help drawer: the page's explanation beside the page, so a reader can
 * look at their board while reading how Promote decides. Opened by a page's
 * "?", the ? key, an ⓘ's "Learn more" or an empty state; unpinned, it follows
 * the page as the reader moves.
 */
export function HelpDrawer() {
  const { open, topic: chosen, section, pinned, close, setPinned, show } = useHelp()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const [query, setQuery] = useState("")
  const body = useRef<HTMLDivElement>(null)

  // Unpinned, the topic follows the page; pinned, it stays.
  const pageTopic = topicForPath(pathname)
  const current = (pinned || !pageTopic ? topicById(chosen ?? "") : pageTopic) ?? topicById(chosen ?? "") ?? pageTopic

  // The ? key opens the page's topic, or closes the drawer.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement
      if (e.key !== "?" || el.closest("input, textarea, [contenteditable=true], .cm-editor")) return
      e.preventDefault()
      if (useHelp.getState().open) close()
      else show(topicForPath(window.location.pathname)?.id)
    }
    const onEscape = (e: KeyboardEvent) => e.key === "Escape" && useHelp.getState().open && close()
    window.addEventListener("keydown", onKey)
    window.addEventListener("keydown", onEscape)
    return () => {
      window.removeEventListener("keydown", onKey)
      window.removeEventListener("keydown", onEscape)
    }
  }, [close, show])

  // Opened at a section: scroll to it.
  useEffect(() => {
    if (!open || !section) return
    const el = body.current?.querySelector(`[data-section="${section}"]`)
    el?.scrollIntoView({ block: "start" })
  }, [open, section, current?.id])

  const results = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return []
    return topics.flatMap((t) =>
      t.sections
        .filter((s) => s.id !== "terms" && (s.title + " " + s.body).toLowerCase().includes(q))
        .map((s) => ({ topic: t, section: s }))
    )
  }, [query])

  if (!open) return null
  return (
    <aside
      role="complementary"
      aria-label="Help"
      className="fixed inset-y-0 right-0 z-50 flex w-full flex-col border-l border-border bg-card shadow-2xl sm:w-[440px]"
      data-testid="help-drawer"
    >
      <div className="flex items-center gap-2 border-b border-border px-4 py-3">
        <BookOpen className="size-4 text-primary" />
        <p className="flex-1 truncate text-sm font-semibold">{current?.title ?? "Help"}</p>
        <button
          type="button"
          onClick={() => setPinned(!pinned)}
          aria-label={pinned ? "Unpin: follow the page" : "Pin this topic"}
          title={pinned ? "Pinned: stays on this topic" : "Follows the page you are on"}
          className="rounded p-1 text-muted-foreground hover:bg-muted/40 hover:text-foreground"
        >
          {pinned ? <PinOff className="size-4" /> : <Pin className="size-4" />}
        </button>
        <button type="button" onClick={close} aria-label="Close help" className="rounded p-1 text-muted-foreground hover:bg-muted/40 hover:text-foreground">
          <X className="size-4" />
        </button>
      </div>
      <div className="border-b border-border/60 px-4 py-2">
        <label className="flex items-center gap-2 rounded-md border border-border/60 bg-background/40 px-2.5 py-1.5">
          <Search className="size-3.5 text-muted-foreground" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search help"
            aria-label="Search help"
            className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground/60"
          />
        </label>
      </div>
      <div ref={body} className="flex-1 overflow-y-auto px-5 py-4">
        {query.trim() ? (
          results.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nothing in help matches that.</p>
          ) : (
            <ul className="space-y-1">
              {results.map(({ topic: t, section: s }) => (
                <li key={t.id + s.id}>
                  <button
                    type="button"
                    onClick={() => {
                      setQuery("")
                      setPinned(true)
                      show(t.id, s.id)
                    }}
                    className="w-full rounded-md px-2 py-1.5 text-left hover:bg-muted/40"
                  >
                    <p className="text-sm font-medium">{s.title}</p>
                    <p className="text-xs text-muted-foreground">{t.title}</p>
                  </button>
                </li>
              ))}
            </ul>
          )
        ) : !current ? (
          <div className="space-y-3 text-sm text-muted-foreground">
            <p>There is no explanation written for this page yet. Search above, or open a topic:</p>
            <ul className="space-y-1">
              {topics.map((t) => (
                <li key={t.id}>
                  <button type="button" onClick={() => show(t.id)} className="text-primary hover:underline">
                    {t.title}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        ) : (
          <div className="space-y-6">
            <p className="text-sm text-foreground/90">{current.summary}</p>
            <nav aria-label="On this topic" className="flex flex-wrap gap-1.5">
              {current.sections
                .filter((s) => s.id !== "terms")
                .map((s) => (
                  <button
                    key={s.id}
                    type="button"
                    onClick={() => body.current?.querySelector(`[data-section="${s.id}"]`)?.scrollIntoView({ block: "start", behavior: "smooth" })}
                    className="rounded-full border border-border/60 px-2 py-0.5 text-[11px] text-muted-foreground hover:border-primary/40 hover:text-foreground"
                  >
                    {s.title}
                  </button>
                ))}
            </nav>
            {current.sections
              .filter((s) => s.id !== "terms")
              .map((s) => (
                <section
                  key={s.id}
                  data-section={s.id}
                  className={cn("scroll-mt-4 space-y-2 rounded-lg", section === s.id && "ring-1 ring-primary/30 -mx-2 px-2 py-1")}
                >
                  <h3 className="text-sm font-semibold text-foreground">{s.title}</h3>
                  <HelpMarkdown source={s.body} />
                </section>
              ))}
          </div>
        )}
      </div>
      {current && (
        <a
          href={DOCS_URL + current.id}
          target="_blank"
          rel="noopener noreferrer"
          className="flex items-center justify-between border-t border-border px-5 py-3 text-xs text-muted-foreground hover:text-foreground"
        >
          Full docs
          <ExternalLink className="size-3.5" />
        </a>
      )}
    </aside>
  )
}
