import { FormLayout } from "@/components/layout/form-layout"
import { ResourceIntro } from "@/components/layout/resource-workbench"
import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Eraser, Loader2, RotateCcw, Save, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { services as servicesApi, projects as projectsApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { inputCls, Section, Field } from "@/components/services/form-primitives"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/settings"
)({
  component: SettingsTab,
})

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

  // Who loses variables when this goes: shown before the name is typed.
  const { data: dependents = [] } = useQuery({
    queryKey: ["service-dependents", orgId, projectId, serviceId],
    queryFn: () => servicesApi.dependents(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })

  const deleteMutation = useMutation({
    mutationFn: () => servicesApi.delete(orgId, projectId, serviceId, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["services", orgId, projectId] })
      navigate({ to: "/projects/$id/services", params: { id: projectId } })
    },
  })

  const clearCacheMutation = useMutation({
    mutationFn: () => projectsApi.clearBuildCache(orgId, projectId, token),
  })

  const resetDbMutation = useMutation({
    mutationFn: () => servicesApi.reset(orgId, projectId, serviceId, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["service", orgId, projectId, serviceId] })
      queryClient.invalidateQueries({ queryKey: ["deployments", orgId, projectId, serviceId] })
    },
  })

  const [resetConfirm, setResetConfirm] = useState(false)

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-40">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  const canDelete = deleteConfirm === service?.name

  return (
    <div className="console-page"><ResourceIntro title="Service settings" description="Manage identity and the service lifecycle." /><FormLayout><div className="space-y-6">
      {/* ── Rename ─────────────────────────────────────────────── */}
      <Section title="Service name" subtitle="Changing the name does not affect the K8s workload name.">
        <Field label="Name">
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Service name"
            className={inputCls}
          />
        </Field>
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
      </Section>

      {/* ── Build cache (application only) ─────────────────────── */}
      {service?.type !== "database" && (
        <Section
          title="Build cache"
          subtitle="The build cache belongs to the project: clearing it here clears it for every service in the project. It is trimmed to the server's limit after each build; clear it to free the space now, or to force a clean build."
        >
          {clearCacheMutation.isError && (
            <p className="text-xs text-destructive">{(clearCacheMutation.error as Error).message}</p>
          )}
          {clearCacheMutation.isSuccess && (
            <p className="text-xs text-emerald-400">Cache cleared — next build starts fresh.</p>
          )}
          <Button
            variant="outline"
            size="sm"
            className="gap-1.5"
            disabled={clearCacheMutation.isPending}
            onClick={() => clearCacheMutation.mutate()}
          >
            {clearCacheMutation.isPending
              ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
              : <Eraser className="h-3.5 w-3.5" />
            }
            Clear build cache
          </Button>
        </Section>
      )}

      {/* ── Danger zone ────────────────────────────────────────── */}
      <Section title="Danger zone" subtitle="Permanent actions that cannot be undone." danger>
        {service?.type === "database" && (
          <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 space-y-3">
            <div>
              <p className="text-sm font-medium">Reset database</p>
              <p className="text-xs text-muted-foreground mt-0.5">
                Deletes the persistent volume and re-provisions the database. All data is permanently lost.
              </p>
            </div>
            {resetDbMutation.isError && (
              <p className="text-xs text-destructive">{(resetDbMutation.error as Error).message}</p>
            )}
            {!resetConfirm ? (
              <Button variant="destructive" size="sm" className="gap-1.5" onClick={() => setResetConfirm(true)}>
                <RotateCcw className="h-3.5 w-3.5" /> Reset database
              </Button>
            ) : (
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" onClick={() => setResetConfirm(false)} disabled={resetDbMutation.isPending}>
                  Cancel
                </Button>
                <Button
                  variant="destructive"
                  size="sm"
                  className="gap-1.5"
                  disabled={resetDbMutation.isPending}
                  onClick={() => { resetDbMutation.mutate(); setResetConfirm(false) }}
                >
                  {resetDbMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RotateCcw className="h-3.5 w-3.5" />}
                  Yes, wipe and reset
                </Button>
              </div>
            )}
          </div>
        )}
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 space-y-4">
          <div>
            <p className="text-sm font-medium">Delete service</p>
            <p className="text-xs text-muted-foreground mt-0.5">
              Deletes the service and its deployments, and removes it from the cluster.
            </p>
          </div>
          {dependents.length > 0 && (
            <div className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-300/90" data-testid="dependents">
              <p className="font-medium">
                {dependents.length === 1 ? "1 thing reads" : `${dependents.length} things read`} {service?.name}&apos;s variables, and
                lose them on their next deploy or run:
              </p>
              <ul className="mt-1.5 list-disc space-y-0.5 pl-4">
                {dependents.map((d) => (
                  <li key={`${d.kind}-${d.level}-${d.name}`}>
                    {d.kind === "job" ? "job " : ""}
                    <span className="font-medium">{d.name}</span> in {d.level}
                  </li>
                ))}
              </ul>
            </div>
          )}
          <Field label={`Type "${service?.name}" to confirm`}>
            <input
              value={deleteConfirm}
              onChange={(e) => setDeleteConfirm(e.target.value)}
              placeholder={service?.name}
              className={inputCls}
            />
          </Field>
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
      </Section>
    </div></FormLayout></div>
  )
}
