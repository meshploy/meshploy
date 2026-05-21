import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Eye, EyeOff, Layers, Loader2, Lock, Plus, Server, Trash2, X } from "lucide-react"
import { useState } from "react"
import {
  variableGroups as groupsApi,
  services as servicesApi,
  type ApiVariableGroupItem,
  type ApiService,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { inputCls, Section } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { DetailPageHeader } from "@/components/layout/detail-page-header"

export const Route = createFileRoute("/_app/projects/$id/variables/$groupId")({
  component: GroupDetailPage,
})

const sanitizeKey = (v: string) =>
  v.toUpperCase().replace(/ /g, "_").replace(/[^A-Z0-9_]/g, "")

// ─── Item row ─────────────────────────────────────────────────────────────────

function ItemRow({ item, onDelete, isDeleting }: { item: ApiVariableGroupItem; onDelete: () => void; isDeleting: boolean }) {
  const [revealed, setRevealed] = useState(false)

  return (
    <div className="rounded-md border border-border/60 bg-muted/5 px-3 py-2.5 flex items-center gap-3">
      <code className="text-xs font-mono text-foreground shrink-0">{item.key}</code>
      <span className="text-muted-foreground/40 text-xs shrink-0">=</span>
      {item.is_secret ? (
        <div className="flex items-center gap-1 flex-1 min-w-0">
          <code className="text-xs font-mono text-muted-foreground truncate">
            {revealed ? (item.value ?? "••••••••") : "••••••••"}
          </code>
          <Button variant="ghost" size="icon-sm" onClick={() => setRevealed((v) => !v)} className="text-muted-foreground/30 hover:text-muted-foreground shrink-0">
            {revealed ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
          </Button>
        </div>
      ) : (
        <code className="text-xs font-mono text-muted-foreground truncate flex-1">{item.value}</code>
      )}
      {item.is_secret && (
        <span className="text-[9px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-400 border border-amber-500/20 shrink-0">secret</span>
      )}
      <Button variant="ghost" size="icon-sm" onClick={onDelete} disabled={isDeleting} className="text-muted-foreground/30 hover:text-destructive transition-colors shrink-0 disabled:opacity-40">
        {isDeleting ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
      </Button>
    </div>
  )
}

// ─── Add item form ────────────────────────────────────────────────────────────

function AddItemForm({ onSave, onCancel, isPending, error }: {
  onSave: (key: string, value: string, isSecret: boolean) => void
  onCancel: () => void
  isPending: boolean
  error?: string
}) {
  const [key, setKey] = useState("")
  const [value, setValue] = useState("")
  const [isSecret, setIsSecret] = useState(false)

  return (
    <div className="rounded-md border border-border/60 bg-muted/10 p-3 space-y-3">
      <div className="grid grid-cols-2 gap-2">
        <input
          autoFocus
          value={key}
          onChange={(e) => setKey(sanitizeKey(e.target.value))}
          placeholder="KEY"
          className={cn(inputCls, "font-mono text-xs")}
        />
        <input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="value"
          type={isSecret ? "password" : "text"}
          className={cn(inputCls, "font-mono text-xs")}
        />
      </div>
      <div className="flex items-center gap-3">
        <label className="flex items-center gap-2 text-xs text-muted-foreground select-none cursor-pointer">
          <Switch checked={isSecret} onCheckedChange={setIsSecret} size="sm" />
          Secret (masked)
        </label>
        <div className="ml-auto flex gap-2">
          <Button size="sm" onClick={() => onSave(key, value, isSecret)} disabled={!key.trim() || isPending} className="gap-1.5 h-7 text-xs">
            {isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : null}
            Save
          </Button>
          <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={onCancel}>
            <X className="h-3 w-3" />
          </Button>
        </div>
      </div>
      {error && <p className="text-xs text-destructive">{error}</p>}
    </div>
  )
}

// ─── Service row ──────────────────────────────────────────────────────────────

function ServiceRow({ svc, onDetach, isDetaching }: { svc: ApiService; onDetach: () => void; isDetaching: boolean }) {
  return (
    <div className="rounded-md border border-border/60 bg-muted/5 px-3 py-2.5 flex items-center gap-3">
      <span className={cn("h-1.5 w-1.5 rounded-full shrink-0", svc.status === "running" ? "bg-emerald-400" : "bg-muted-foreground/30")} />
      <span className="text-xs font-medium flex-1 truncate">{svc.name}</span>
      {svc.status !== "running" && (
        <span className="text-[9px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-400 border border-amber-500/20 shrink-0">not deployed</span>
      )}
      <Button variant="ghost" size="icon-sm" onClick={onDetach} disabled={isDetaching} className="text-muted-foreground/30 hover:text-destructive transition-colors shrink-0 disabled:opacity-40">
        {isDetaching ? <Loader2 className="h-3 w-3 animate-spin" /> : <X className="h-3.5 w-3.5" />}
      </Button>
    </div>
  )
}

// ─── Attach service form ──────────────────────────────────────────────────────

function AttachServiceForm({ availableServices, onAttach, onCancel, isPending, error }: {
  availableServices: ApiService[]
  onAttach: (serviceId: string) => void
  onCancel: () => void
  isPending: boolean
  error?: string
}) {
  const [serviceId, setServiceId] = useState("")

  return (
    <div className="rounded-md border border-border/60 bg-muted/10 p-3 space-y-3">
      <div className="flex items-center gap-2">
        <Select value={serviceId} onValueChange={(v) => setServiceId(v ?? "")}>
          <SelectTrigger className="flex-1 h-9 text-sm bg-background border-border/60">
            <SelectValue placeholder={availableServices.length === 0 ? "All services already attached" : "Select a service…"}>
              {availableServices.find((s) => s.id === serviceId)?.name}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {availableServices.map((s) => (
              <SelectItem key={s.id} value={s.id}>
                {s.name}
                {s.status !== "running" && <span className="ml-2 text-muted-foreground text-xs">(not running)</span>}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button variant="ghost" size="icon-sm" onClick={onCancel} className="text-muted-foreground hover:text-destructive transition-colors shrink-0">
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>
      {error && <p className="text-xs text-destructive">{error}</p>}
      <Button size="sm" className="gap-1.5" onClick={() => onAttach(serviceId)} disabled={!serviceId || isPending || availableServices.length === 0}>
        {isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
        Attach service
      </Button>
    </div>
  )
}

// ─── Page ─────────────────────────────────────────────────────────────────────

function GroupDetailPage() {
  const { id: projectId, groupId } = useParams({ from: "/_app/projects/$id/variables/$groupId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const qc = useQueryClient()

  const [showAddItem, setShowAddItem] = useState(false)
  const [showAttach, setShowAttach] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const invalidateGroup = () => qc.invalidateQueries({ queryKey: ["variable-group", orgId, projectId, groupId] })

  const { data: group, isLoading } = useQuery({
    queryKey: ["variable-group", orgId, projectId, groupId],
    queryFn: () => groupsApi.get(orgId, projectId, groupId, token),
    enabled: !!orgId,
  })

  const { data: allServices = [] } = useQuery<ApiService[]>({
    queryKey: ["services", orgId, projectId],
    queryFn: () => servicesApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })

  const { data: attachedGroups = [] } = useQuery({
    queryKey: ["service-variable-groups-for-group", orgId, projectId, groupId],
    queryFn: async () => {
      const all = await servicesApi.list(orgId, projectId, token)
      const results: string[] = []
      await Promise.all(all.map(async (svc) => {
        const gs = await groupsApi.listForService(orgId, projectId, svc.id, token)
        if (gs.some((g) => g.id === groupId)) results.push(svc.id)
      }))
      return results
    },
    enabled: !!orgId && !!group && !group.system_managed,
  })

  const upsertMut = useMutation({
    mutationFn: ({ key, value, isSecret }: { key: string; value: string; isSecret: boolean }) =>
      groupsApi.upsertItem(orgId, projectId, groupId, { key, value, is_secret: isSecret }, token),
    onSuccess: () => { invalidateGroup(); setShowAddItem(false) },
  })

  const deleteMut = useMutation({
    mutationFn: (itemId: string) => groupsApi.deleteItem(orgId, projectId, groupId, itemId, token),
    onSuccess: invalidateGroup,
  })

  const attachMut = useMutation({
    mutationFn: (serviceId: string) => groupsApi.attach(orgId, projectId, serviceId, groupId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["service-variable-groups-for-group", orgId, projectId, groupId] })
      setShowAttach(false)
    },
  })

  const detachMut = useMutation({
    mutationFn: (serviceId: string) => groupsApi.detach(orgId, projectId, serviceId, groupId, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["service-variable-groups-for-group", orgId, projectId, groupId] }),
  })

  const deleteGroupMut = useMutation({
    mutationFn: () => groupsApi.delete(orgId, projectId, groupId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["variable-groups", orgId, projectId] })
      navigate({ to: "/projects/$id/variables", params: { id: projectId } })
    },
  })

  if (isLoading || !group) {
    return (
      <div className="flex items-center justify-center h-32">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  const attachedServiceIds = new Set(attachedGroups)
  const attachedServices = allServices.filter((s) => attachedServiceIds.has(s.id))
  const availableServices = allServices.filter((s) => !attachedServiceIds.has(s.id))

  return (
    <div className="flex flex-col min-h-full">
      <DetailPageHeader
        backTo="/projects/$id/variables"
        backLabel="Back to variables"
        backParams={{ id: projectId }}
        icon={group.system_managed
          ? <Server className="h-4 w-4 text-muted-foreground" />
          : <Layers className="h-4 w-4 text-muted-foreground" />
        }
        name={group.name}
        badge={group.system_managed
          ? (
            <span className="flex items-center gap-1 text-[10px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded bg-muted text-muted-foreground border border-border/60">
              <Lock className="h-2.5 w-2.5" /> auto
            </span>
          )
          : undefined
        }
        subtitle={group.description || undefined}
      />

      <div className="p-6 space-y-6 max-w-2xl">

        {/* Items */}
        <Section title="Items" subtitle="Key-value pairs injected as environment variables on next deploy.">
          <div className="space-y-2">
            {group.items.length === 0 && !showAddItem && (
              <p className="text-xs text-muted-foreground/60 py-2">
                {group.system_managed ? "No variables generated yet — deploy the service first." : "No items yet."}
              </p>
            )}
            {group.items.map((item) => (
              <ItemRow
                key={item.id}
                item={item}
                onDelete={() => deleteMut.mutate(item.id)}
                isDeleting={deleteMut.isPending && deleteMut.variables === item.id}
              />
            ))}

            {showAddItem && !group.system_managed && (
              <AddItemForm
                onSave={(key, value, isSecret) => upsertMut.mutate({ key, value, isSecret })}
                onCancel={() => setShowAddItem(false)}
                isPending={upsertMut.isPending}
                error={(upsertMut.error as Error | null)?.message}
              />
            )}

            {!showAddItem && !group.system_managed && (
              <Button
                variant="ghost"
                onClick={() => setShowAddItem(true)}
                className="w-full flex items-center justify-center gap-1.5 py-2 text-xs text-muted-foreground hover:text-foreground border border-dashed border-border/60 rounded-md hover:border-border transition-colors"
              >
                <Plus className="h-3 w-3" />
                Add another item
              </Button>
            )}
          </div>
        </Section>

        {/* Attached services — hidden for system-managed groups */}
        {!group.system_managed && (
          <Section title="Attached services" subtitle="Services that will receive these variables on next deploy.">
            <div className="space-y-2">
              {attachedServices.length === 0 && !showAttach && (
                <p className="text-xs text-muted-foreground/60 py-2">Not attached to any service.</p>
              )}
              {attachedServices.map((svc) => (
                <ServiceRow
                  key={svc.id}
                  svc={svc}
                  onDetach={() => detachMut.mutate(svc.id)}
                  isDetaching={detachMut.isPending && detachMut.variables === svc.id}
                />
              ))}

              {showAttach && (
                <AttachServiceForm
                  availableServices={availableServices}
                  onAttach={(serviceId) => attachMut.mutate(serviceId)}
                  onCancel={() => setShowAttach(false)}
                  isPending={attachMut.isPending}
                  error={(attachMut.error as Error | null)?.message}
                />
              )}

              {!showAttach && (
                <Button
                  variant="ghost"
                  onClick={() => setShowAttach(true)}
                  className="w-full flex items-center justify-center gap-1.5 py-2 text-xs text-muted-foreground hover:text-foreground border border-dashed border-border/60 rounded-md hover:border-border transition-colors"
                >
                  <Plus className="h-3 w-3" />
                  Attach a service
                </Button>
              )}
            </div>
          </Section>
        )}

        {/* Danger zone */}
        {!group.system_managed && (
          <Section title="Danger zone" subtitle="Permanent actions that cannot be undone." danger>
            <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 flex items-center justify-between gap-4">
              <div>
                <p className="text-sm font-medium">Delete group</p>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Removes this variable group and detaches it from all services.
                </p>
              </div>
              {!confirmDelete ? (
                <Button variant="destructive" size="sm" className="shrink-0 gap-1.5" onClick={() => setConfirmDelete(true)}>
                  <Trash2 className="h-3.5 w-3.5" /> Delete
                </Button>
              ) : (
                <div className="flex items-center gap-2 shrink-0">
                  <Button variant="outline" size="sm" onClick={() => setConfirmDelete(false)} disabled={deleteGroupMut.isPending}>
                    Cancel
                  </Button>
                  <Button variant="destructive" size="sm" className="gap-1.5" onClick={() => deleteGroupMut.mutate()} disabled={deleteGroupMut.isPending}>
                    {deleteGroupMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                    Confirm delete
                  </Button>
                </div>
              )}
            </div>
          </Section>
        )}

      </div>
    </div>
  )
}
