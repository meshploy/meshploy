import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, Save, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { services as servicesApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/settings"
)({
  component: SettingsTab,
})

const inputCls =
  "w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow"

function SettingsTab() {
  const { id: projectId, serviceId } = useParams({
    from: "/_app/projects/$id/services/$serviceId/settings",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const { data: service, isLoading } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => servicesApi.get(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const [name, setName] = useState("")
  const [deleteConfirm, setDeleteConfirm] = useState("")

  useEffect(() => {
    if (service) setName(service.name)
  }, [service])

  const renameMutation = useMutation({
    mutationFn: () => servicesApi.update(orgId, projectId, serviceId, { name }, token),
    onSuccess: (updated) => {
      queryClient.setQueryData(["service", orgId, projectId, serviceId], updated)
      queryClient.invalidateQueries({ queryKey: ["services", orgId, projectId] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: () => servicesApi.delete(orgId, projectId, serviceId, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["services", orgId, projectId] })
      navigate({ to: "/projects/$id/services", params: { id: projectId } })
    },
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-40">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  const canDelete = deleteConfirm === service?.name

  return (
    <div className="p-6 max-w-xl space-y-8">
      {/* ── Rename ─────────────────────────────────────────────── */}
      <div className="space-y-4">
        <div className="border-b border-border/40 pb-2">
          <p className="text-sm font-medium">Service name</p>
          <p className="text-xs text-muted-foreground mt-0.5">
            Changing the name does not affect the K8s workload name.
          </p>
        </div>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          className={inputCls}
          placeholder="Service name"
        />
        {renameMutation.isError && (
          <p className="text-xs text-destructive">{(renameMutation.error as Error).message}</p>
        )}
        {renameMutation.isSuccess && (
          <p className="text-xs text-emerald-400">Renamed successfully.</p>
        )}
        <div className="flex justify-end">
          <Button
            size="sm"
            className="gap-1.5"
            disabled={!name.trim() || name === service?.name || renameMutation.isPending}
            onClick={() => renameMutation.mutate()}
          >
            {renameMutation.isPending
              ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
              : <Save className="h-3.5 w-3.5" />
            }
            Save
          </Button>
        </div>
      </div>

      {/* ── Danger zone ────────────────────────────────────────── */}
      <div className="space-y-4">
        <div className="border-b border-destructive/30 pb-2">
          <p className="text-sm font-medium text-destructive">Danger zone</p>
          <p className="text-xs text-muted-foreground mt-0.5">
            Permanent actions that cannot be undone.
          </p>
        </div>

        <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 space-y-4">
          <div>
            <p className="text-sm font-medium">Delete service</p>
            <p className="text-xs text-muted-foreground mt-0.5">
              Deletes the service record and all associated deployments.
              The K8s workload is not automatically removed.
            </p>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs text-muted-foreground">
              Type <span className="font-mono text-foreground">{service?.name}</span> to confirm
            </label>
            <input
              value={deleteConfirm}
              onChange={(e) => setDeleteConfirm(e.target.value)}
              placeholder={service?.name}
              className={inputCls}
            />
          </div>

          {deleteMutation.isError && (
            <p className="text-xs text-destructive">{(deleteMutation.error as Error).message}</p>
          )}

          <Button
            variant="destructive"
            size="sm"
            className="gap-1.5 w-full"
            disabled={!canDelete || deleteMutation.isPending}
            onClick={() => deleteMutation.mutate()}
          >
            {deleteMutation.isPending
              ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
              : <Trash2 className="h-3.5 w-3.5" />
            }
            Delete service
          </Button>
        </div>
      </div>
    </div>
  )
}
