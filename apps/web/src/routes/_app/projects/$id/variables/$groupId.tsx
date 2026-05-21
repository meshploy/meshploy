import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { ChevronLeft, Eye, EyeOff, Loader2, Lock, Plus, Save, Trash2, X } from "lucide-react"
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
import { inputCls } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

export const Route = createFileRoute("/_app/projects/$id/variables/$groupId")({
  component: GroupDetailPage,
})

function ItemRow({
  item,
  onDelete,
  isDeleting,
}: {
  item: ApiVariableGroupItem
  onDelete: () => void
  isDeleting: boolean
}) {
  const [revealed, setRevealed] = useState(false)

  return (
    <div className="flex items-center gap-3 px-3 py-2.5 border-b border-border/30 last:border-0">
      <div className="flex-1 min-w-0 flex items-center gap-2">
        <code className="text-xs font-mono text-foreground shrink-0">{item.key}</code>
        <span className="text-muted-foreground/30 text-xs">=</span>
        {item.is_secret ? (
          <div className="flex items-center gap-1 flex-1 min-w-0">
            <code className="text-xs font-mono text-muted-foreground truncate">
              {revealed ? (item.value ?? "••••••••") : "••••••••"}
            </code>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setRevealed((v) => !v)}
              className="text-muted-foreground/30 hover:text-muted-foreground shrink-0"
            >
              {revealed ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
            </Button>
          </div>
        ) : (
          <code className="text-xs font-mono text-muted-foreground truncate flex-1">{item.value}</code>
        )}
      </div>
      <div className="flex items-center gap-1 shrink-0">
        {item.is_secret && (
          <span className="text-[9px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-400">secret</span>
        )}
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onDelete}
          disabled={isDeleting}
          className="text-muted-foreground/30 hover:text-destructive transition-colors"
        >
          {isDeleting ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
        </Button>
      </div>
    </div>
  )
}

function GroupDetailPage() {
  const { id: projectId, groupId } = useParams({ from: "/_app/projects/$id/variables/$groupId" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const navigate = useNavigate()
  const qc = useQueryClient()

  const invalidate = () => qc.invalidateQueries({ queryKey: ["variable-group", orgId, projectId, groupId] })

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
      // Load all services and check which are attached to this group
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

  // Add item form
  const [showAdd, setShowAdd] = useState(false)
  const [addKey, setAddKey] = useState("")
  const [addValue, setAddValue] = useState("")
  const [addIsSecret, setAddIsSecret] = useState(false)

  // Attach service form
  const [showAttach, setShowAttach] = useState(false)
  const [attachServiceId, setAttachServiceId] = useState("")

  const upsertMut = useMutation({
    mutationFn: () => groupsApi.upsertItem(orgId, projectId, groupId, { key: addKey.trim(), value: addValue, is_secret: addIsSecret }, token),
    onSuccess: () => { invalidate(); setShowAdd(false); setAddKey(""); setAddValue(""); setAddIsSecret(false) },
  })

  const deleteMut = useMutation({
    mutationFn: (itemId: string) => groupsApi.deleteItem(orgId, projectId, groupId, itemId, token),
    onSuccess: invalidate,
  })

  const attachMut = useMutation({
    mutationFn: (serviceId: string) => groupsApi.attach(orgId, projectId, serviceId, groupId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["service-variable-groups-for-group", orgId, projectId, groupId] })
      setShowAttach(false)
      setAttachServiceId("")
    },
  })

  const detachMut = useMutation({
    mutationFn: (serviceId: string) => groupsApi.detach(orgId, projectId, serviceId, groupId, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["service-variable-groups-for-group", orgId, projectId, groupId] }),
  })

  if (isLoading || !group) {
    return <div className="p-6 flex items-center gap-2 text-muted-foreground text-sm"><Loader2 className="h-4 w-4 animate-spin" /> Loading…</div>
  }

  const attachedServiceIds = new Set(attachedGroups)
  const availableServices = allServices.filter((s) => !attachedServiceIds.has(s.id))

  return (
    <div className="p-6 max-w-2xl space-y-6">
      {/* Back + header */}
      <div className="flex items-center gap-2">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate({ to: "/projects/$id/variables", params: { id: projectId } })}
          className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
        >
          <ChevronLeft className="h-3.5 w-3.5" /> Variables
        </Button>
        <span className="text-muted-foreground/40">/</span>
        <span className="text-sm font-medium">{group.name}</span>
        {group.system_managed && (
          <span className="flex items-center gap-1 text-[10px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded bg-muted text-muted-foreground border border-border/60">
            <Lock className="h-2.5 w-2.5" /> auto
          </span>
        )}
      </div>

      {group.description && (
        <p className="text-xs text-muted-foreground">{group.description}</p>
      )}

      {/* Items */}
      <div className="rounded-lg border border-border/60 bg-card overflow-hidden">
        <div className="px-4 py-3 border-b border-border/40 flex items-center justify-between">
          <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Items</p>
          {!group.system_managed && (
            <Button variant="ghost" size="sm" className="gap-1.5 text-xs h-6" onClick={() => setShowAdd(true)}>
              <Plus className="h-3 w-3" /> Add
            </Button>
          )}
        </div>

        {group.items.length === 0 && !showAdd && (
          <div className="px-4 py-8 text-center text-sm text-muted-foreground/50">
            {group.system_managed ? "No variables generated yet — deploy the service first." : "No items yet"}
          </div>
        )}

        {group.items.map((item) => (
          <ItemRow
            key={item.id}
            item={item}
            onDelete={() => deleteMut.mutate(item.id)}
            isDeleting={deleteMut.isPending && deleteMut.variables === item.id}
          />
        ))}

        {showAdd && !group.system_managed && (
          <div className="p-3 border-t border-border/40 space-y-2">
            <div className="grid grid-cols-2 gap-2">
              <input
                autoFocus
                value={addKey}
                onChange={(e) => setAddKey(e.target.value)}
                placeholder="KEY"
                className={cn(inputCls, "font-mono text-xs uppercase")}
              />
              <input
                value={addValue}
                onChange={(e) => setAddValue(e.target.value)}
                placeholder="value"
                type={addIsSecret ? "password" : "text"}
                className={cn(inputCls, "font-mono text-xs")}
              />
            </div>
            <div className="flex items-center gap-3">
              <label className="flex items-center gap-2 text-xs text-muted-foreground select-none cursor-pointer">
                <Switch checked={addIsSecret} onCheckedChange={setAddIsSecret} size="sm" />
                Secret (masked)
              </label>
              <div className="ml-auto flex gap-2">
                <Button size="sm" onClick={() => upsertMut.mutate()} disabled={!addKey.trim() || upsertMut.isPending} className="gap-1.5 h-7 text-xs">
                  {upsertMut.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Save className="h-3 w-3" />}
                  Save
                </Button>
                <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => { setShowAdd(false); setAddKey(""); setAddValue("") }}>
                  <X className="h-3 w-3" />
                </Button>
              </div>
            </div>
            {upsertMut.isError && <p className="text-xs text-destructive">{(upsertMut.error as Error).message}</p>}
          </div>
        )}
      </div>

      {/* Attached services */}
      <div className="rounded-lg border border-border/60 bg-card overflow-hidden">
        <div className="px-4 py-3 border-b border-border/40 flex items-center justify-between">
          <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Attached services</p>
          {!showAttach && availableServices.length > 0 && (
            <Button variant="ghost" size="sm" className="gap-1.5 text-xs h-6" onClick={() => setShowAttach(true)}>
              <Plus className="h-3 w-3" /> Attach
            </Button>
          )}
        </div>

        {attachedServiceIds.size === 0 && !showAttach && (
          <div className="px-4 py-6 text-center text-sm text-muted-foreground/50">Not attached to any service</div>
        )}

        {allServices.filter((s) => attachedServiceIds.has(s.id)).map((svc) => (
          <div key={svc.id} className="flex items-center justify-between px-3 py-2.5 border-b border-border/30 last:border-0">
            <div className="flex items-center gap-2">
              <span className={cn("h-1.5 w-1.5 rounded-full shrink-0", svc.status === "running" ? "bg-emerald-400" : "bg-muted-foreground/30")} />
              <span className="text-sm font-medium">{svc.name}</span>
              {svc.status !== "running" && (
                <span className="text-[10px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-400">not deployed</span>
              )}
            </div>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => detachMut.mutate(svc.id)}
              disabled={detachMut.isPending && detachMut.variables === svc.id}
              className="text-muted-foreground/30 hover:text-destructive"
            >
              {detachMut.isPending && detachMut.variables === svc.id ? <Loader2 className="h-3 w-3 animate-spin" /> : <X className="h-3.5 w-3.5" />}
            </Button>
          </div>
        ))}

        {showAttach && (
          <div className="p-3 border-t border-border/40 flex gap-2">
            <Select value={attachServiceId} onValueChange={(v) => setAttachServiceId(v ?? "")}>
              <SelectTrigger className="flex-1 h-8 text-xs bg-background border-border/60">
                <SelectValue placeholder={availableServices.length === 0 ? "All services already attached" : "Select a service…"} />
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
            <Button size="sm" className="h-8" onClick={() => attachMut.mutate(attachServiceId)} disabled={!attachServiceId || attachMut.isPending}>
              {attachMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : "Attach"}
            </Button>
            <Button size="sm" variant="ghost" className="h-8" onClick={() => { setShowAttach(false); setAttachServiceId("") }}>
              <X className="h-3.5 w-3.5" />
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
