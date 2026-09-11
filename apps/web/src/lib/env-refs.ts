import { autocompletion, type Completion, type CompletionContext, type CompletionResult } from "@codemirror/autocomplete"
import { EditorView } from "@codemirror/view"
import type { Extension } from "@codemirror/state"

export interface EnvRefName {
  name: string
  source: string // shown beside the name: the group it comes from
  secret?: boolean
}

// envRefAutocomplete suggests names for ${…} references in the env editor, in a
// menu styled like the console's own dropdowns.
export function envRefAutocomplete(groupNames: EnvRefName[]): Extension {
  return [
    autocompletion({
      override: [envRefCompletions(groupNames)],
      icons: false,
      addToOptions: [{ render: secretMark, position: 90 }],
    }),
    envRefTheme,
  ]
}

const keyLine = /^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=/

// envRefCompletions offers names once ${ is typed in a value: the service's own
// variables from the editor, its attached groups' variables, and PORT, which
// every service is given. Picking one closes the brace. The line's own key is
// left out, since a variable referring to itself is a loop, and $${ (a literal
// "${") suggests nothing.
function envRefCompletions(groupNames: EnvRefName[]) {
  return (ctx: CompletionContext): CompletionResult | null => {
    const typed = ctx.matchBefore(/\$\{[A-Za-z0-9_]*/)
    if (!typed) return null
    if (typed.from > 0 && ctx.state.sliceDoc(typed.from - 1, typed.from) === "$") return null
    const line = ctx.state.doc.lineAt(ctx.pos)
    const eq = line.text.indexOf("=")
    if (eq < 0 || typed.from <= line.from + eq) return null

    const seen = new Set([line.text.slice(0, eq).trim()])
    const options: Completion[] = []
    const add = (name: string, detail: string, secret = false) => {
      if (seen.has(name)) return
      seen.add(name)
      options.push({ label: name, detail, type: secret ? "secret" : "variable", apply: closeRef(name) })
    }
    // The service's own variables win over a group's on a clash, as at deploy.
    for (let i = 1; i <= ctx.state.doc.lines; i++) {
      const key = keyLine.exec(ctx.state.doc.line(i).text)?.[1]
      if (key) add(key, "this service")
    }
    for (const g of groupNames) add(g.name, g.source, g.secret)
    add("PORT", "every service")
    return { from: typed.from + 2, options, validFor: /^[A-Za-z0-9_]*$/ }
  }
}

// closeRef inserts the name and its "}", or steps over a "}" already there.
function closeRef(name: string) {
  return (view: EditorView, _completion: Completion, from: number, to: number) => {
    const closed = view.state.sliceDoc(to, to + 1) === "}"
    view.dispatch({
      changes: { from, to, insert: closed ? name : name + "}" },
      selection: { anchor: from + name.length + 1 },
    })
  }
}

// secretMark puts lucide's lock after a secret variable's group name.
function secretMark(completion: Completion): Node | null {
  if (completion.type !== "secret") return null
  const mark = document.createElement("span")
  mark.className = "cm-envRefSecret"
  mark.title = "Secret"
  mark.innerHTML =
    '<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>'
  return mark
}

// The editor runs the One Dark theme, which comes earlier in the extension
// order and so wins at equal specificity; these selectors are more specific.
const menu = "&.cm-editor .cm-tooltip.cm-tooltip-autocomplete"

const envRefTheme = EditorView.theme({
  [menu]: {
    backgroundColor: "var(--popover)",
    color: "var(--popover-foreground)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius)",
    boxShadow: "0 10px 30px -10px oklch(0 0 0 / 0.6)",
    padding: "4px",
    overflow: "hidden",
  },
  [`${menu} > ul`]: {
    fontFamily: 'var(--font-mono, "Geist Mono", ui-monospace, monospace)',
    fontSize: "12px",
    minWidth: "16rem",
    maxWidth: "min(28rem, 90vw)",
    maxHeight: "15rem",
  },
  [`${menu} > ul > li`]: {
    display: "flex",
    alignItems: "center",
    gap: "12px",
    padding: "5px 8px",
    borderRadius: "calc(var(--radius) * 0.6)",
    lineHeight: "1.35",
    color: "var(--popover-foreground)",
  },
  [`${menu} > ul > li[aria-selected]`]: {
    backgroundColor: "var(--accent)",
    color: "var(--accent-foreground)",
  },
  [`${menu} .cm-completionLabel`]: {
    flex: "1",
    minWidth: "0",
    overflow: "hidden",
    textOverflow: "ellipsis",
  },
  // The part typed so far, in the editor's key colour.
  [`${menu} .cm-completionMatchedText`]: {
    textDecoration: "none",
    color: "oklch(0.78 0.12 200)",
  },
  [`${menu} .cm-completionDetail`]: {
    marginLeft: "auto",
    fontFamily: 'var(--font-sans, "Onest", ui-sans-serif, system-ui, sans-serif)',
    fontStyle: "normal",
    fontSize: "11px",
    color: "var(--muted-foreground)",
    maxWidth: "12rem",
    overflow: "hidden",
    textOverflow: "ellipsis",
  },
  [`${menu} .cm-envRefSecret`]: {
    display: "flex",
    color: "var(--muted-foreground)",
  },
})
