import { useEffect, useState, type ReactElement } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { ArrowRight, FolderKanban } from "lucide-react"
import { projects as projectsApi, toProject } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

/**
 * UseTemplateDialog is the one deploy hand-off for a template. Because the
 * gallery is project-agnostic, "Use" first asks which project to deploy into,
 * then deep-links into the stack-new flow with the Template source preselected
 * (?type=stack&template=<id>). Projects are only fetched once the dialog opens.
 */
export function UseTemplateDialog({
  templateId,
  templateName,
  trigger,
}: {
  templateId: string
  templateName: string
  trigger: ReactElement
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const navigate = useNavigate()

  const [open, setOpen] = useState(false)
  const [projectId, setProjectId] = useState("")

  const { data: projects = [], isLoading } = useQuery({
    queryKey: ["projects", orgId],
    queryFn: () => projectsApi.list(orgId!, token).then((r) => r.map(toProject)),
    enabled: !!orgId && open,
  })

  // One project → preselect it so it's a single click.
  useEffect(() => {
    if (open && !projectId && projects.length === 1) setProjectId(projects[0].id)
  }, [open, projects, projectId])

  function go() {
    if (!projectId) return
    navigate({
      to: "/projects/$id/new",
      params: { id: projectId },
      search: { type: "stack", template: templateId },
    })
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={trigger} />
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Use {templateName}</DialogTitle>
          <DialogDescription>
            Pick a project. You'll review the compose and fill in any required values before it deploys.
          </DialogDescription>
        </DialogHeader>

        {isLoading ? (
          <p className="text-sm text-muted-foreground py-2">Loading projects…</p>
        ) : projects.length === 0 ? (
          <div className="flex items-start gap-2.5 rounded-lg border border-border/60 bg-muted/20 px-3 py-3">
            <FolderKanban className="h-4 w-4 text-muted-foreground shrink-0 mt-0.5" />
            <p className="text-sm text-muted-foreground">
              You need a project first.{" "}
              <Link to="/projects/new" className="text-primary hover:underline">
                Create one
              </Link>
              .
            </p>
          </div>
        ) : (
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">Project</label>
            <Select value={projectId} onValueChange={(v) => setProjectId(v ?? "")}>
              <SelectTrigger className="w-full h-9 text-sm bg-muted/20 border-border/60">
                <SelectValue placeholder="Select a project…">
                  {projects.find((p) => p.id === projectId)?.name}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {projects.map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}

        <DialogFooter>
          <Button className="gap-2" disabled={!projectId} onClick={go}>
            Continue
            <ArrowRight className="h-4 w-4" />
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
