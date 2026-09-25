import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { projects as projectsApi, services as servicesApi, ApiError, type ApiDeployment } from "@/lib/api"

/**
 * Deploy, asked first where a group promotes into the service's level.
 *
 * There the service takes its images from the level below, so Deploy building
 * from git makes production run something staging never had, and staging's
 * next promotion will not replace it until staging builds something newer.
 * Still allowed (it is how a hotfix goes out), but said first, next to the
 * usual reason for pressing Deploy there: running the current image again
 * with changed variables, which needs no build.
 */
/**
 * Whether a group promotes into this service's level, and from where: the
 * level below it on the group's path, and that level's copy of the service.
 */
export function useReceiving(orgId: string | undefined, projectId: string, serviceId: string, token: string) {
  const { data: board } = useQuery({
    queryKey: ["board", orgId, projectId],
    queryFn: () => projectsApi.board(orgId!, projectId, token),
    enabled: !!orgId,
  })
  const group = board?.groups.find((g) => g.cells.some((c) => c.service_id === serviceId))
  const at = group ? group.path.indexOf(projectId) : -1
  if (!board || !group || at <= 0) return { board, receiving: null }
  const from = board.levels.find((l) => l.project_id === group.path[at - 1])
  const here = board.levels.find((l) => l.project_id === projectId)
  const cell = group.cells.find((c) => c.service_id === serviceId)
  const fromCell = group.cells.find((c) => c.level_id === from?.project_id && c.lineage_id === cell?.lineage_id)
  return { board, receiving: { group, from, here, cell, fromCell } }
}

export function useDeployGuard({
  orgId,
  projectId,
  serviceId,
  token,
  build,
  onRedeployed,
}: {
  orgId: string | undefined
  projectId: string
  serviceId: string
  token: string
  /** The ordinary deploy: builds from git, or deploys the configured image. */
  build: () => void
  onRedeployed?: (deployment: ApiDeployment) => void
}) {
  const [open, setOpen] = useState(false)
  const { receiving } = useReceiving(orgId, projectId, serviceId, token)
  const here = receiving?.here
  const cell = receiving?.cell

  const qc = useQueryClient()
  const redeploy = useMutation({
    mutationFn: () => servicesApi.redeploy(orgId!, projectId, serviceId, token),
    onSuccess: (deployment) => {
      qc.invalidateQueries({ queryKey: ["deployments", orgId, projectId, serviceId] })
      qc.invalidateQueries({ queryKey: ["service", orgId, projectId, serviceId] })
      qc.invalidateQueries({ queryKey: ["board", orgId] })
      setOpen(false)
      onRedeployed?.(deployment)
    },
  })

  const request = () => (receiving ? setOpen(true) : build())
  const dialog = open && receiving && (
    <Dialog open onOpenChange={(o) => !o && setOpen(false)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            Deploy {cell?.service_name ?? "this service"} in {here?.name}?
          </DialogTitle>
          <DialogDescription>
            It takes its images from {receiving.from?.name ?? "the level below"} (group {receiving.group.name}).
          </DialogDescription>
        </DialogHeader>
        <ul className="list-disc space-y-1.5 pl-5 text-sm text-muted-foreground">
          <li>
            <span className="text-foreground">Redeploy the current image</span> runs what {here?.name} runs now again, for
            changed variables. Nothing is built.
          </li>
          <li>
            <span className="text-foreground">Build here anyway</span> makes a new image in {here?.name}, skipping{" "}
            {receiving.from?.name}: a hotfix. {receiving.from?.name}&apos;s next promotion will not replace it until{" "}
            {receiving.from?.name} builds something newer.
          </li>
        </ul>
        {redeploy.error && (
          <p className="text-xs text-destructive">
            {redeploy.error instanceof ApiError ? redeploy.error.message : "Could not redeploy."}
          </p>
        )}
        <DialogFooter className="gap-2">
          <Button variant="ghost" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button variant="outline" onClick={() => redeploy.mutate()} disabled={!cell?.image || redeploy.isPending}>
            {redeploy.isPending && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
            Redeploy the current image
          </Button>
          <Button
            variant="destructive"
            onClick={() => {
              setOpen(false)
              build()
            }}
          >
            Build here anyway
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
  return { request, dialog, receiving }
}
