import raw from "virtual:help-topics"

/**
 * The help topics, read the way packages/help reads them in Go: a header, then
 * level-two sections with stable ids, and a Terms section behind the console's
 * tooltips. Both parsers read one format; packages/help's test is what keeps
 * the files to it.
 */
export interface HelpSection {
  id: string
  title: string
  body: string
}
export interface HelpTerm {
  id: string
  label: string
  text: string
  section: string
}
export interface HelpTopic {
  id: string
  title: string
  summary: string
  pages: string[]
  sections: HelpSection[]
  terms: HelpTerm[]
}

const headingRe = /^## (.+?) \{#([a-z0-9-]+)\}\s*$/
const termRe = /^- \*\*(.+?)\*\* \{#([a-z0-9-]+) -> ([a-z0-9-]+)\}: (.+)$/

function parse(source: string): HelpTopic | null {
  if (!source.startsWith("---\n")) return null
  const end = source.indexOf("\n---\n", 4)
  if (end < 0) return null
  const topic: HelpTopic = { id: "", title: "", summary: "", pages: [], sections: [], terms: [] }
  for (const line of source.slice(4, end).split("\n")) {
    const at = line.indexOf(":")
    if (at < 0) continue
    const key = line.slice(0, at).trim()
    const value = line.slice(at + 1).trim()
    if (key === "id") topic.id = value
    if (key === "title") topic.title = value
    if (key === "summary") topic.summary = value
    if (key === "pages") topic.pages = value.replace(/^\[|\]$/g, "").split(",").map((p) => p.trim()).filter(Boolean)
  }
  let current: HelpSection | null = null
  for (const line of source.slice(end + 5).split("\n")) {
    const heading = headingRe.exec(line)
    if (heading) {
      current = { id: heading[2], title: heading[1], body: "" }
      topic.sections.push(current)
      continue
    }
    if (!current) continue
    if (current.id === "terms") {
      const term = termRe.exec(line)
      if (term) topic.terms.push({ id: term[2], label: term[1], section: term[3], text: term[4] })
      continue
    }
    current.body += line + "\n"
  }
  for (const s of topic.sections) s.body = s.body.trim()
  return topic.id ? topic : null
}

export const topics: HelpTopic[] = Object.values(raw)
  .map(parse)
  .filter((t): t is HelpTopic => t !== null)

export function topic(id: string) {
  return topics.find((t) => t.id === id)
}

/** A term, named "topic.term", as the console's tooltips refer to it. */
export function term(ref: string) {
  const [topicId, termId] = ref.split(".")
  const t = topic(topicId)
  const found = t?.terms.find((x) => x.id === termId)
  return found && t ? { topic: t, term: found } : undefined
}

/** The console page a path is, in the names topics use in their header. */
export function pageOf(pathname: string) {
  const project = /^\/projects\/[^/]+(\/([a-z-]+))?/.exec(pathname)
  if (project) {
    const tab = project[2]
    if (!tab) return "project-overview"
    if (tab === "config-files") return "config-files"
    return tab // services, databases, routes, variables, volumes, jobs, stacks, settings, new
  }
  if (pathname === "/" || pathname === "") return "workspace-overview"
  const top = /^\/([a-z-]+)/.exec(pathname)?.[1]
  if (top === "cluster") return "nodes"
  return top // nodes, domains, agents, ...
}

/** The topic for the page at pathname, if one is written for it. */
export function topicForPath(pathname: string) {
  const page = pageOf(pathname)
  return page ? topics.find((t) => t.pages.includes(page)) : undefined
}
