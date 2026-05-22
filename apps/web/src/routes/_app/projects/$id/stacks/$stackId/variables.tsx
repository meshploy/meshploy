import { createFileRoute, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState, useEffect } from "react"
import { Loader2, Plus, Trash2, Save } from "lucide-react"
import { Button } from "@/components/ui/button"
import { stacks as stacksApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Section, inputCls } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/stacks/$stackId/variables")({
  component: StackVariablesTab,
})

type VarRow = { key: string; value: string }

function StackVariablesTab() {
  const { id: projectId, stackId } = useParams({ from: "/_app/projects/$id/stacks/$stackId/variables" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const qc = useQueryClient()
  const queryKey = ["stack", orgId, projectId, stackId]

  const { data: stack, isLoading } = useQuery({
    queryKey,
    queryFn: () => stacksApi.get(orgId!, projectId, stackId, token),
    enabled: !!orgId,
  })

  const [rows, setRows] = useState<VarRow[]>([])
  const [dirty, setDirty] = useState(false)

  useEffect(() => {
    if (stack && !dirty) {
      const entries = Object.entries(stack.variables ?? {})
      setRows(entries.length > 0 ? entries.map(([key, value]) => ({ key, value })) : [])
    }
  }, [stack, dirty])

  const saveMutation = useMutation({
    mutationFn: () => {
      const variables: Record<string, string> = {}
      for (const { key, value } of rows) {
        if (key.trim()) variables[key.trim()] = value
      }
      return stacksApi.update(orgId!, projectId, stackId, { variables }, token)
    },
    onSuccess: (updated) => {
      qc.setQueryData(queryKey, updated)
      setDirty(false)
    },
  })

  const addRow = () => {
    setRows((r) => [...r, { key: "", value: "" }])
    setDirty(true)
  }

  const removeRow = (i: number) => {
    setRows((r) => r.filter((_, idx) => idx !== i))
    setDirty(true)
  }

  const updateRow = (i: number, field: "key" | "value", val: string) => {
    setRows((r) => r.map((row, idx) => idx === i ? { ...row, [field]: val } : row))
    setDirty(true)
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-40">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <Section
        title="Stack Variables"
        subtitle='Define values substituted into ${KEY} placeholders in your spec.'
      >
        <div className="space-y-2">
            {rows.length > 0 && (
              <div className="grid grid-cols-[1fr_1fr_auto] gap-2 text-[11px] text-muted-foreground px-0.5 mb-1">
                <span>Key</span>
                <span>Value</span>
                <span />
              </div>
            )}
            {rows.map((row, i) => (
              <div key={i} className="grid grid-cols-[1fr_1fr_auto] gap-2 items-center">
                <input
                  value={row.key}
                  onChange={(e) => updateRow(i, "key", e.target.value)}
                  placeholder="VARIABLE_NAME"
                  className={cn(inputCls, "font-mono text-xs")}
                />
                <input
                  value={row.value}
                  onChange={(e) => updateRow(i, "value", e.target.value)}
                  placeholder="value"
                  className={cn(inputCls, "font-mono text-xs")}
                />
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => removeRow(i)}
                  className="text-muted-foreground hover:text-destructive"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            ))}

            <Button variant="outline" size="sm" className="gap-1.5 mt-1" onClick={addRow}>
              <Plus className="h-3.5 w-3.5" /> Add variable
            </Button>
          </div>
      </Section>

      {saveMutation.isError && (
        <p className="text-xs text-destructive">
          {(saveMutation.error as Error)?.message ?? "Failed to save"}
        </p>
      )}

      <div className="flex items-center gap-3">
        <Button
          size="sm"
          className="gap-1.5"
          onClick={() => saveMutation.mutate()}
          disabled={saveMutation.isPending || !dirty}
        >
          {saveMutation.isPending
            ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
            : <Save className="h-3.5 w-3.5" />
          }
          Save variables
        </Button>
        {dirty && <span className="text-[11px] text-amber-400/80 font-mono">unsaved changes</span>}
      </div>
    </div>
  )
}
