import { useState, useId } from "react"
import { useQueryClient, useMutation } from "@tanstack/react-query"
import { FolderKanban, Loader2 } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog"
import { projects as projectsApi, ApiError } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

interface NewProjectModalProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function toSlug(name: string) {
  return name
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9\s-]/g, "")
    .replace(/\s+/g, "-")
    .replace(/-+/g, "-")
    .slice(0, 50)
}

export function NewProjectModal({ open, onOpenChange }: NewProjectModalProps) {
  const id = useId()
  const queryClient = useQueryClient()
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const [name, setName] = useState("")
  const [slug, setSlug] = useState("")
  const [slugTouched, setSlugTouched] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const derivedSlug = slugTouched ? slug : toSlug(name)

  const slugError =
    derivedSlug.length > 0 && !/^[a-z0-9-]+$/.test(derivedSlug)
      ? "Only lowercase letters, numbers, and hyphens"
      : null

  const createMutation = useMutation({
    mutationFn: () =>
      projectsApi.create(orgId!, name.trim(), derivedSlug, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects", orgId] })
      handleClose()
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.detail : "Something went wrong")
    },
  })

  function handleClose() {
    onOpenChange(false)
    // Reset after close animation
    setTimeout(() => {
      setName("")
      setSlug("")
      setSlugTouched(false)
      setError(null)
    }, 150)
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim() || !derivedSlug || slugError) return
    setError(null)
    createMutation.mutate()
  }

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) handleClose() }}>
      <DialogContent className="sm:max-w-md" showCloseButton>
        <DialogHeader>
          <div className="flex items-center gap-3">
            <div className="flex items-center justify-center w-9 h-9 rounded-lg bg-primary/10 shrink-0">
              <FolderKanban className="h-4.5 w-4.5 text-primary" />
            </div>
            <div>
              <DialogTitle>New Project</DialogTitle>
              <p className="text-sm text-muted-foreground mt-0.5">
                Each project maps to a Kubernetes namespace
              </p>
            </div>
          </div>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4 py-1">
          {/* Name */}
          <div className="space-y-1.5">
            <label htmlFor={`${id}-name`} className="text-sm font-medium">
              Project name
            </label>
            <input
              id={`${id}-name`}
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
              <label htmlFor={`${id}-slug`} className="text-sm font-medium">
                Namespace slug
              </label>
              <span className="text-xs text-muted-foreground/50 font-mono">
                k8s namespace
              </span>
            </div>
            <div className="flex items-stretch rounded-md border border-border/60 bg-input/30 overflow-hidden focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/20 transition-all">
              <span className="flex items-center pl-3 text-sm text-muted-foreground/50 select-none shrink-0">
                ns/
              </span>
              <input
                id={`${id}-slug`}
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
            {slugError && (
              <p className="text-xs text-destructive">{slugError}</p>
            )}
            {!slugError && derivedSlug && (
              <p className="text-xs text-muted-foreground/50 font-mono">
                {derivedSlug}.svc.cluster.local
              </p>
            )}
          </div>

          {/* API error */}
          {error && (
            <p className="text-xs text-destructive bg-destructive/10 border border-destructive/20 rounded-md px-3 py-2">
              {error}
            </p>
          )}
        </form>

        <DialogFooter showCloseButton>
          <button
            type="submit"
            form={`${id}-form`}
            disabled={
              !name.trim() ||
              !derivedSlug ||
              !!slugError ||
              createMutation.isPending
            }
            onClick={handleSubmit}
            className="inline-flex items-center gap-2 h-8 px-3.5 rounded-md bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
          >
            {createMutation.isPending ? (
              <>
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                Creating…
              </>
            ) : (
              "Create project"
            )}
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
