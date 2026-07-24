import { createFileRoute, Link } from "@tanstack/react-router"
import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Globe, LayoutTemplate, Loader2, Search, ServerCrash } from "lucide-react"
import { SiGithub } from "@icons-pack/react-simple-icons"
import { templates as templatesApi, type TemplateManifest } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { TemplateLogo } from "@/components/templates/template-logo"
import { UseTemplateDialog } from "@/components/templates/use-template-dialog"

export const Route = createFileRoute("/_app/templates/")({
  component: TemplatesPage,
})

function TemplatesPage() {
  const token = useAuthStore((s) => s.token)!
  const [category, setCategory] = useState<string>("all")
  const [q, setQ] = useState("")

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["templates"],
    queryFn: () => templatesApi.list(token),
  })
  const list = data ?? []

  const categories = useMemo(() => {
    const set = new Set<string>()
    list.forEach((t) => t.category && set.add(t.category))
    return ["all", ...Array.from(set).sort()]
  }, [list])

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return list.filter((t) => {
      if (category !== "all" && t.category !== category) return false
      if (!needle) return true
      return (
        t.name.toLowerCase().includes(needle) ||
        t.description.toLowerCase().includes(needle) ||
        t.id.toLowerCase().includes(needle)
      )
    })
  }, [list, category, q])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64 gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Loading templates…</span>
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center h-64 gap-3 text-muted-foreground">
        <ServerCrash className="h-8 w-8 text-destructive/60" />
        <p className="text-sm">Failed to load templates</p>
        <p className="text-xs text-muted-foreground/60">{(error as Error).message}</p>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-lg font-semibold tracking-tight">Templates</h1>
        <p className="text-sm text-muted-foreground mt-0.5">
          One-click apps. Deploy a template as a stack into any project.
        </p>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex flex-wrap gap-1.5">
          {categories.map((c) => (
            <button
              key={c}
              onClick={() => setCategory(c)}
              className={cn(
                "px-2.5 h-7 rounded-md text-xs font-medium capitalize transition-colors",
                category === c
                  ? "bg-primary/10 text-primary"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted/40"
              )}
            >
              {c}
            </button>
          ))}
        </div>
        <div className="relative ml-auto">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground/60" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search templates…"
            className="h-8 w-56 pl-8 pr-3 rounded-md bg-muted/20 border border-border/60 text-sm outline-none focus:border-border"
          />
        </div>
      </div>

      {/* Grid */}
      {filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center h-48 gap-2 text-muted-foreground">
          <LayoutTemplate className="h-7 w-7 text-muted-foreground/40" />
          <p className="text-sm">No templates match.</p>
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {filtered.map((t) => (
            <TemplateCard key={t.id} t={t} />
          ))}
        </div>
      )}
    </div>
  )
}

function TemplateCard({ t }: { t: TemplateManifest }) {
  return (
    <div className="group flex flex-col rounded-xl border border-border/60 bg-card transition-colors hover:border-border">
      <Link to="/templates/$id" params={{ id: t.id }} className="flex flex-col gap-3 p-4 flex-1">
        <div className="flex items-start gap-3">
          <TemplateLogo id={t.id} name={t.name} className="w-9 h-9" />
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium truncate">{t.name}</p>
            <span className="text-[11px] text-muted-foreground capitalize">{t.category}</span>
          </div>
          <span className="text-[10px] font-mono text-muted-foreground/50 shrink-0">v{t.version}</span>
        </div>
        <p className="text-xs text-muted-foreground line-clamp-2 leading-relaxed">{t.description}</p>
      </Link>
      <div className="flex items-center justify-between border-t border-border/40 px-4 py-2.5">
        <div className="flex items-center gap-2.5 text-muted-foreground/50">
          {t.links?.source && (
            <a href={t.links.source} target="_blank" rel="noopener" title="Source" className="hover:text-foreground transition-colors">
              <SiGithub className="h-3.5 w-3.5" />
            </a>
          )}
          {t.links?.website && (
            <a href={t.links.website} target="_blank" rel="noopener" title="Website" className="hover:text-foreground transition-colors">
              <Globe className="h-3.5 w-3.5" />
            </a>
          )}
        </div>
        <UseTemplateDialog
          templateId={t.id}
          templateName={t.name}
          trigger={<Button size="sm" variant="outline" className="h-7 text-xs px-3">Use</Button>}
        />
      </div>
    </div>
  )
}
