import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { ChevronLeft, FolderKanban, Loader2 } from "lucide-react"
import { projects as projectsApi, ApiError } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"

export const Route = createFileRoute("/_app/projects/new")({
  component: NewProjectPage,
})

function toSlug(name: string) {
  return name
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9\s-]/g, "")
    .replace(/\s+/g, "-")
    .replace(/-+/g, "-")
    .slice(0, 50)
}

function NewProjectPage() {
  const navigate  = useNavigate()
  const qc        = useQueryClient()
  const token     = useAuthStore((s) => s.token)!
  const orgId     = useOrgStore((s) => s.currentOrg?.id)!

  const [name,         setName]         = useState("")
  const [slug,         setSlug]         = useState("")
  const [slugTouched,  setSlugTouched]  = useState(false)
  const [error,        setError]        = useState<string | null>(null)

  const derivedSlug = slugTouched ? slug : toSlug(name)

  const slugError =
    derivedSlug.length > 0 && !/^[a-z0-9-]+$/.test(derivedSlug)
      ? "Only lowercase letters, numbers, and hyphens"
      : null

  const mutation = useMutation({
    mutationFn: () => projectsApi.create(orgId, name.trim(), derivedSlug, token),
    onSuccess: (project) => {
      qc.invalidateQueries({ queryKey: ["projects", orgId] })
      navigate({ to: "/projects/$id", params: { id: project.id } })
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.detail : "Something went wrong")
    },
  })

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim() || !derivedSlug || slugError || mutation.isPending) return
    setError(null)
    mutation.mutate()
  }

  const isValid = name.trim() && derivedSlug && !slugError

  return (
    <div className="min-h-screen bg-background flex flex-col">
      {/* Top bar */}
      <div className="sticky top-0 z-10 border-b border-border/40 bg-background/90 backdrop-blur-sm">
        <div className="h-14 flex items-center gap-3 px-6">
          <button
            onClick={() => navigate({ to: "/projects" })}
            className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <ChevronLeft className="h-4 w-4" />
            Projects
          </button>
          <span className="text-muted-foreground/40">/</span>
          <span className="text-sm font-medium">New project</span>
        </div>
      </div>

      <div className="flex-1 flex items-start justify-center py-12 px-6">
        <div className="w-full max-w-md space-y-8">
          {/* Header */}
          <div className="flex items-center gap-3">
            <div className="flex items-center justify-center w-10 h-10 rounded-lg bg-primary/10 shrink-0">
              <FolderKanban className="h-5 w-5 text-primary" />
            </div>
            <div>
              <h1 className="text-base font-semibold">New project</h1>
              <p className="text-sm text-muted-foreground mt-0.5">
                Each project maps to a Kubernetes namespace
              </p>
            </div>
          </div>

          {/* Form */}
          <form onSubmit={handleSubmit} className="space-y-5">
            {/* Name */}
            <div className="space-y-1.5">
              <label className="text-sm font-medium">Project name</label>
              <input
                type="text"
                autoFocus
                autoComplete="off"
                placeholder="My API"
                value={name}
                onChange={(e) => {
                  setName(e.target.value)
                  if (!slugTouched) setSlug(toSlug(e.target.value))
                }}
                className="w-full h-9 rounded-md border border-border/60 bg-input/30 px-3 text-sm text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-ring focus:ring-2 focus:ring-ring/20 transition-all"
              />
            </div>

            {/* Slug */}
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium">Namespace slug</label>
                <span className="text-xs text-muted-foreground/50 font-mono">k8s namespace</span>
              </div>
              <div className="flex items-stretch rounded-md border border-border/60 bg-input/30 overflow-hidden focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/20 transition-all">
                <span className="flex items-center pl-3 text-sm text-muted-foreground/50 select-none shrink-0">
                  ns/
                </span>
                <input
                  type="text"
                  autoComplete="off"
                  placeholder="my-api"
                  value={derivedSlug}
                  onChange={(e) => {
                    setSlugTouched(true)
                    setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))
                  }}
                  className="flex-1 min-w-0 bg-transparent px-2 py-2 text-sm font-mono text-foreground placeholder:text-muted-foreground/40 outline-none"
                />
              </div>
              {slugError ? (
                <p className="text-xs text-destructive">{slugError}</p>
              ) : derivedSlug ? (
                <p className="text-xs text-muted-foreground/50 font-mono">
                  {derivedSlug}.svc.cluster.local
                </p>
              ) : null}
            </div>

            {/* API error */}
            {error && (
              <p className="text-xs text-destructive bg-destructive/10 border border-destructive/20 rounded-md px-3 py-2">
                {error}
              </p>
            )}

            <Button
              type="submit"
              disabled={!isValid || mutation.isPending}
              className="w-full gap-2"
            >
              {mutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {mutation.isPending ? "Creating…" : "Create project"}
            </Button>
          </form>
        </div>
      </div>
    </div>
  )
}
