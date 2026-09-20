import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Layers, Loader2, Lock, Plus, Trash2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { variableGroups as groupsApi, type ApiVariableGroup } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Section } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"

/** What the groups are attached to. A service and a job attach the same way. */
export type GroupOwner = { kind: "service" | "job"; id: string }

// ─── Variable groups section ──────────────────────────────────────────────────

function GroupAttachmentRow({
  group, last, detachable, onDetach, isDetaching,
}: {
  group: ApiVariableGroup
  last: boolean
  detachable: boolean
  onDetach: () => void
  isDetaching: boolean
}) {
  const varCount = group.items.filter((i) => !i.is_secret).length
  const secretCount = group.items.filter((i) => i.is_secret).length

  return (
    <div className={cn("flex items-center gap-3 px-3 py-2.5", !last && "border-b border-border/40")}>
      {group.system_managed
        ? <Lock className="h-3 w-3 text-muted-foreground/40 shrink-0" />
        : <Layers className="h-3 w-3 text-muted-foreground/40 shrink-0" />
      }
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-medium text-foreground truncate">{group.name}</span>
          {group.system_managed && (
            <span className="text-[11px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded bg-muted text-muted-foreground border border-border/60 shrink-0">auto</span>
          )}
        </div>
        <p className="text-[11px] text-muted-foreground/60 mt-0.5">
          {varCount > 0 && `${varCount} var${varCount !== 1 ? "s" : ""}`}
          {varCount > 0 && secretCount > 0 && " · "}
          {secretCount > 0 && `${secretCount} secret${secretCount !== 1 ? "s" : ""}`}
          {group.items.length === 0 && "empty"}
        </p>
      </div>
      {detachable && (
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onDetach}
          disabled={isDetaching}
          className="text-muted-foreground/30 hover:text-destructive transition-colors disabled:opacity-40 shrink-0"
          title="Detach"
        >
          {isDetaching ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
        </Button>
      )}
    </div>
  )
}

/**
 * The groups attached to a service or a job.
 *
 * A service re-applies when this changes, so a running container sees it. A job
 * has nothing running: the next run reads the new values, which is why nothing
 * is redeployed here.
 */
export function GroupAttachments({ owner, projectId }: { owner: GroupOwner; projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const isJob = owner.kind === "job"

  const [showAdd, setShowAdd] = useState(false)
  const [selectedGroupId, setSelectedGroupId] = useState("")

  const attachedKey = [isJob ? "job-variable-groups" : "service-variable-groups", orgId, projectId, owner.id]

  const { data: attached = [], isLoading } = useQuery<ApiVariableGroup[]>({
    queryKey: attachedKey,
    queryFn: () =>
      isJob
        ? groupsApi.listForJob(orgId, projectId, owner.id, token)
        : groupsApi.listForService(orgId, projectId, owner.id, token),
    enabled: !!orgId,
  })

  const { data: allGroups = [] } = useQuery<ApiVariableGroup[]>({
    queryKey: ["variable-groups", orgId, projectId],
    queryFn: () => groupsApi.list(orgId, projectId, token),
    enabled: !!orgId && showAdd,
  })

  const invalidate = () => qc.invalidateQueries({ queryKey: attachedKey })

  const attachMut = useMutation({
    mutationFn: () =>
      isJob
        ? groupsApi.attachToJob(orgId, projectId, owner.id, selectedGroupId, token)
        : groupsApi.attach(orgId, projectId, owner.id, selectedGroupId, token),
    onSuccess: () => { setShowAdd(false); setSelectedGroupId(""); invalidate() },
  })

  const detachMut = useMutation({
    mutationFn: (groupId: string) =>
      isJob
        ? groupsApi.detachFromJob(orgId, projectId, owner.id, groupId, token)
        : groupsApi.detach(orgId, projectId, owner.id, groupId, token),
    onSuccess: invalidate,
  })

  const attachedIds = new Set(attached.map((g) => g.id))
  // A service never offers its own generated group -- it always has it. A job
  // owns none, so every group in the project is on offer.
  const availableGroups = allGroups.filter(
    (g) => !attachedIds.has(g.id) && !(g.system_managed && !isJob && g.service_id === owner.id)
  )

  return (
    <Section
      title="Variable groups"
      subtitle={
        isJob
          ? "Attach groups of variables and secrets. Every item reaches the next run as an env var, and ${NAME} in the job's own variables resolves against them."
          : "Attach groups of variables and secrets — all items inject as env vars on next deploy."
      }
    >
      {isLoading ? (
        <div className="flex items-center gap-2 text-muted-foreground py-2">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span className="text-xs">Loading…</span>
        </div>
      ) : (
        <div className="space-y-2">
          {attached.length > 0 && (
            <div className="rounded-lg border border-border/60 overflow-hidden">
              {attached.map((g, i) => (
                <GroupAttachmentRow
                  key={g.id}
                  group={g}
                  last={i === attached.length - 1}
                  detachable={isJob || !g.system_managed}
                  onDetach={() => detachMut.mutate(g.id)}
                  isDetaching={detachMut.isPending && detachMut.variables === g.id}
                />
              ))}
            </div>
          )}

          {showAdd ? (
            <div className="rounded-lg border border-border/60 bg-card p-3 space-y-3">
              <Select value={selectedGroupId} onValueChange={(v) => setSelectedGroupId(v ?? "")}>
                <SelectTrigger className="w-full! h-8 text-xs bg-muted/20 border-border/60">
                  {/* Children, or the trigger shows the id it holds - which is
                      what somebody picking a group by name least wants to see. */}
                  <SelectValue placeholder={availableGroups.length === 0 ? "No groups available" : "Select a variable group…"}>
                    {availableGroups.find((g) => g.id === selectedGroupId)?.name}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {availableGroups.map((g) => (
                    <SelectItem key={g.id} value={g.id}>{g.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <div className="flex gap-2">
                <Button
                  onClick={() => attachMut.mutate()}
                  disabled={!selectedGroupId || attachMut.isPending || availableGroups.length === 0}
                  className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-md bg-primary text-primary-foreground disabled:opacity-40 transition-opacity"
                >
                  {attachMut.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Plus className="h-3 w-3" />}
                  Attach
                </Button>
                <Button
                  variant="ghost"
                  onClick={() => { setShowAdd(false); setSelectedGroupId("") }}
                  className="flex items-center gap-1 text-xs px-3 py-1.5 rounded-md text-muted-foreground hover:text-foreground transition-colors"
                >
                  <X className="h-3 w-3" /> Cancel
                </Button>
              </div>
              {attachMut.isError && (
                <p className="text-xs text-destructive">{(attachMut.error as Error).message}</p>
              )}
            </div>
          ) : (
            <Button
              variant="ghost"
              onClick={() => setShowAdd(true)}
              className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              <Plus className="h-3.5 w-3.5" /> Attach group
            </Button>
          )}
        </div>
      )}
    </Section>
  )
}

/**
 * The same list, before the thing that owns it exists.
 *
 * A create form can hold the choice but not make it: an attachment needs an id.
 * The caller attaches what is selected once the resource is created, the way
 * the service form attaches a volume.
 */
export function StagedGroupPicker({ projectId, selected, onChange }: {
  projectId: string
  selected: string[]
  onChange: (ids: string[]) => void
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const [showAdd, setShowAdd] = useState(false)
  const [groupId, setGroupId] = useState("")

  const { data: allGroups = [] } = useQuery<ApiVariableGroup[]>({
    queryKey: ["variable-groups", orgId, projectId],
    queryFn: () => groupsApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })

  const chosen = selected.map((id) => allGroups.find((g) => g.id === id)).filter(Boolean) as ApiVariableGroup[]
  const available = allGroups.filter((g) => !selected.includes(g.id))

  return (
    <Section
      title="Variable groups"
      subtitle="Attach groups of variables and secrets. Every item reaches each run as an env var, and ${NAME} above resolves against them."
    >
      <div className="space-y-2">
        {chosen.length > 0 && (
          <div className="rounded-lg border border-border/60 overflow-hidden">
            {chosen.map((g, i) => (
              <GroupAttachmentRow
                key={g.id}
                group={g}
                last={i === chosen.length - 1}
                detachable
                onDetach={() => onChange(selected.filter((id) => id !== g.id))}
                isDetaching={false}
              />
            ))}
          </div>
        )}

        {showAdd ? (
          <div className="rounded-lg border border-border/60 bg-card p-3 space-y-3">
            <Select value={groupId} onValueChange={(v) => setGroupId(v ?? "")}>
              <SelectTrigger className="w-full! h-8 text-xs bg-muted/20 border-border/60">
                <SelectValue placeholder={available.length === 0 ? "No groups available" : "Select a variable group…"}>
                  {available.find((g) => g.id === groupId)?.name}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {available.map((g) => (
                  <SelectItem key={g.id} value={g.id}>{g.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <div className="flex gap-2">
              <Button
                onClick={() => { onChange([...selected, groupId]); setGroupId(""); setShowAdd(false) }}
                disabled={!groupId}
                className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-md bg-primary text-primary-foreground disabled:opacity-40 transition-opacity"
              >
                <Plus className="h-3 w-3" /> Attach
              </Button>
              <Button
                variant="ghost"
                onClick={() => { setShowAdd(false); setGroupId("") }}
                className="flex items-center gap-1 text-xs px-3 py-1.5 rounded-md text-muted-foreground hover:text-foreground transition-colors"
              >
                <X className="h-3 w-3" /> Cancel
              </Button>
            </div>
          </div>
        ) : (
          <Button
            variant="ghost"
            onClick={() => setShowAdd(true)}
            className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            <Plus className="h-3.5 w-3.5" /> Attach group
          </Button>
        )}
      </div>
    </Section>
  )
}
