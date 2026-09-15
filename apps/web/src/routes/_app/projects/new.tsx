import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { ChevronLeft, Loader2 } from "lucide-react"
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
    <div className="console-page new-project-workspace space-y-7">
      <Button variant="ghost" onClick={() => navigate({ to: "/projects" })} className="text-muted-foreground -ml-3"><ChevronLeft className="size-4"/>Projects</Button>
      <div><h1>New project</h1><p className="mt-2 text-sm text-muted-foreground">Give your applications and resources a shared home.</p></div>
      <div className="creation-workspace">
          {/* Form */}
          <form onSubmit={handleSubmit} className="quiet-surface creation-form"><div className="creation-form-fields space-y-6">
            {/* Name */}
            <div className="flex flex-col gap-3">
              <label htmlFor="project-name" className="text-sm font-medium">Project name</label>
              <input
                id="project-name"
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
                <label htmlFor="project-slug" className="text-sm font-medium">Namespace slug</label>
                <span className="text-xs text-muted-foreground/50 font-mono">k8s namespace</span>
              </div>
              <div className="flex items-stretch rounded-md border border-border/60 bg-input/30 overflow-hidden focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/20 transition-all">
                <span className="flex items-center pl-3 text-sm text-muted-foreground/50 select-none shrink-0">
                  ns/
                </span>
                <input
                  id="project-slug"
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

            </div><footer className="creation-form-footer"><Button type="button" variant="outline" onClick={() => navigate({ to: "/projects" })} disabled={mutation.isPending}>Cancel</Button>
            <Button
              type="submit"
              disabled={!isValid || mutation.isPending}
              className="gap-2"
            >
              {mutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {mutation.isPending ? "Creating…" : "Create project"}
            </Button></footer>
          </form>
          <aside className="creation-guidance"><h2 className="text-base font-semibold">Start with a project.</h2><p className="mt-4 text-sm text-muted-foreground leading-7">Then add a service, deploy a stack, or connect a database. Keep shared configuration and persistent storage alongside your workloads.</p><div className="mt-7 pt-6 border-t border-border"><h3 className="text-sm font-medium">Choose a name your team recognizes</h3><p className="mt-2 text-xs text-muted-foreground leading-6">The namespace slug identifies this project in the cluster. It is generated from your name, and you can adjust it before creating the project.</p></div></aside>
      </div>
    </div>
  )
}
