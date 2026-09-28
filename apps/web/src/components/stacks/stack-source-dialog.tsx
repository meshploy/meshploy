import { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { gitIntegrations, stacks as stacksApi, type ApiStack, type StackGitMode } from "@/lib/api"

// Where a git stack's file comes from, and what reads it. A stack moved from
// another platform arrives with its repository and no git access - that
// platform's connection stays behind - so choosing the integration here is how
// its builds and syncs start working.

const NONE = "none"

export function StackSourceDialog({ open, onOpenChange, stack, orgId, projectId, token }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  stack: ApiStack
  orgId: string
  projectId: string
  token: string
}) {
  const qc = useQueryClient()
  const [integration, setIntegration] = useState(stack.git_integration_id ?? NONE)
  const [repo, setRepo] = useState(stack.git_repo)
  const [branch, setBranch] = useState(stack.git_branch)
  const [path, setPath] = useState(stack.git_path)
  const [mode, setMode] = useState<StackGitMode>(stack.git_mode || "file")

  useEffect(() => {
    if (!open) return
    setIntegration(stack.git_integration_id ?? NONE)
    setRepo(stack.git_repo)
    setBranch(stack.git_branch)
    setPath(stack.git_path)
    setMode(stack.git_mode || "file")
  }, [open, stack])

  const { data: integrations = [], isLoading } = useQuery({
    queryKey: ["git-integrations", orgId],
    queryFn: () => gitIntegrations.list(orgId, token),
    enabled: open,
  })

  const save = useMutation({
    mutationFn: () => stacksApi.update(orgId, projectId, stack.id, {
      git_integration_id: integration === NONE ? "" : integration,
      git_repo: repo.trim(),
      git_branch: branch.trim(),
      git_path: path.trim(),
      git_mode: mode,
    }, token),
    onSuccess: (updated) => {
      qc.setQueryData(["stack", orgId, projectId, stack.id], updated)
      onOpenChange(false)
    },
  })

  // A repository named owner/repo has no host of its own: only an
  // integration says which server it is on and how to read it.
  const needsAccess = integration === NONE && !/^[a-z]+:\/\//.test(repo.trim())
  const chosen = integrations.find((i) => i.id === integration)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Edit source</DialogTitle>
          <DialogDescription>
            Where Sync reads the compose file, and where services with a build: section build from.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <Field label="Git access" hint={integrations.length === 0 && !isLoading
            ? <>No git integration yet. <Link to="/integrations/new" search={{ category: "git" }} className="text-primary hover:underline">Connect one</Link>, then choose it here.</>
            : "The integration that can read the repository."}>
            <Select value={integration} onValueChange={(v) => setIntegration(v ?? NONE)}>
              <SelectTrigger className="w-full h-9">
                <SelectValue>
                  {chosen ? `${chosen.name} (${chosen.provider})` : "None: a public repository"}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>None: a public repository</SelectItem>
                {integrations.map((i) => (
                  <SelectItem key={i.id} value={i.id}>{i.name} ({i.provider}){i.connected ? "" : ", not authorized yet"}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label="Repository" hint={needsAccess
            ? "owner/repo names no server: choose the integration it is on."
            : "owner/repo on the integration's server, or a full URL."}>
            <Input value={repo} onChange={(e) => setRepo(e.target.value)} className="font-mono h-9" />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Branch">
              <Input value={branch} onChange={(e) => setBranch(e.target.value)} className="font-mono h-9" />
            </Field>
            <Field label="Compose file">
              <Input value={path} onChange={(e) => setPath(e.target.value)} className="font-mono h-9" placeholder="docker-compose.yml" />
            </Field>
          </div>
          <Field label="Reads" hint="The whole repository when the file mounts its own folders, such as ./scripts.">
            <Select value={mode} onValueChange={(v) => setMode((v ?? "file") as StackGitMode)}>
              <SelectTrigger className="w-full h-9">
                <SelectValue>{mode === "repo" ? "The whole repository" : "The compose file alone"}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="repo">The whole repository</SelectItem>
                <SelectItem value="file">The compose file alone</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          {save.isError && <p className="text-xs text-destructive">{(save.error as Error).message}</p>}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button onClick={() => save.mutate()} disabled={save.isPending || !repo.trim() || needsAccess}>
            {save.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Field({ label, hint, children }: { label: string; hint?: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <p className="text-xs font-medium">{label}</p>
      {children}
      {hint && <p className="text-[11px] text-muted-foreground">{hint}</p>}
    </div>
  )
}
