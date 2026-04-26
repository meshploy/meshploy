import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router"
import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { ChevronLeft, Eye, EyeOff, Info, Loader2, Plus, Trash2 } from "lucide-react"
import { secrets as secretsApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { inputCls } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/projects/$id/secrets/new")({
  component: NewSecretsPage,
})

type SecretRow = { id: number; name: string; value: string; showValue: boolean }

let rowId = 0
const mkRow = (): SecretRow => ({ id: ++rowId, name: "", value: "", showValue: false })

function NewSecretsPage() {
  const { id: projectId } = useParams({ from: "/_app/projects/$id/secrets/new" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const navigate = useNavigate()

  const [rows, setRows] = useState<SecretRow[]>([mkRow()])
  const [errors, setErrors] = useState<string[]>([])

  const { data: existing = [] } = useQuery({
    queryKey: ["secrets", orgId, projectId],
    queryFn: () => secretsApi.list(orgId, projectId, token),
    enabled: !!orgId,
  })

  const patch = (id: number, field: Partial<SecretRow>) =>
    setRows((prev) => prev.map((r) => (r.id === id ? { ...r, ...field } : r)))

  const addRow = () => setRows((prev) => [...prev, mkRow()])

  const removeRow = (id: number) =>
    setRows((prev) => prev.length > 1 ? prev.filter((r) => r.id !== id) : prev)

  const saveMut = useMutation({
    mutationFn: async () => {
      const valid = rows.filter((r) => r.name.trim() && r.value)
      if (valid.length === 0) throw new Error("Add at least one secret with a name and value.")
      const errs: string[] = []
      await Promise.all(
        valid.map((r) =>
          secretsApi.create(orgId, projectId, { name: r.name.trim(), value: r.value }, token)
            .catch((e: Error) => { errs.push(`${r.name}: ${e.message}`) })
        )
      )
      if (errs.length) throw new Error(errs.join("\n"))
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["secrets", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["project", orgId, projectId] })
      navigate({ to: "/projects/$id/secrets", params: { id: projectId } })
    },
    onError: (e: Error) => setErrors(e.message.split("\n")),
  })

  const filledCount = rows.filter((r) => r.name.trim() && r.value).length

  return (
    <div className="min-h-screen bg-background flex flex-col">
      {/* Top bar */}
      <div className="sticky top-0 z-10 border-b border-border/40 bg-background/90 backdrop-blur-sm">
        <div className="h-14 flex items-center gap-3 px-6">
          <button
            onClick={() => navigate({ to: "/projects/$id/secrets", params: { id: projectId } })}
            className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <ChevronLeft className="h-4 w-4" />
            Secrets
          </button>
          <span className="text-muted-foreground/40">/</span>
          <span className="text-sm font-medium">New secrets</span>
        </div>
      </div>

      <div className="flex-1 max-w-2xl mx-auto w-full px-6 py-8 space-y-6">
        <div>
          <h1 className="text-base font-semibold">Add secrets</h1>
          <p className="text-xs text-muted-foreground mt-1">
            Encrypted at rest with AES-256. Attach secrets to services to inject them as environment variables at deploy time.
          </p>
        </div>

        {/* Existing count note */}
        {existing.length > 0 && (
          <div className="flex items-start gap-2 text-xs text-muted-foreground/70 bg-muted/20 rounded-md px-3 py-2.5">
            <Info className="h-3.5 w-3.5 shrink-0 mt-0.5" />
            <span>
              This project already has <strong className="text-foreground">{existing.length} secret{existing.length !== 1 ? "s" : ""}</strong>.
              {" "}New secrets will be added alongside them — existing secrets are never overwritten.
            </span>
          </div>
        )}

        {/* Rows */}
        <div className="space-y-2">
          <div className="grid grid-cols-[1fr_1fr_32px] gap-2 px-1">
            <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Name</span>
            <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Value</span>
            <span />
          </div>

          {rows.map((row) => (
            <div key={row.id} className="grid grid-cols-[1fr_1fr_32px] gap-2 items-center">
              <input
                className={inputCls}
                placeholder="e.g. DATABASE_URL"
                value={row.name}
                onChange={(e) => patch(row.id, { name: e.target.value })}
              />
              <div className="relative">
                <input
                  className={cn(inputCls, "pr-8")}
                  type={row.showValue ? "text" : "password"}
                  placeholder="Secret value"
                  value={row.value}
                  onChange={(e) => patch(row.id, { value: e.target.value })}
                />
                <button
                  type="button"
                  onClick={() => patch(row.id, { showValue: !row.showValue })}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground/50 hover:text-muted-foreground"
                >
                  {row.showValue ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </button>
              </div>
              <button
                onClick={() => removeRow(row.id)}
                disabled={rows.length === 1}
                className="flex items-center justify-center text-muted-foreground/30 hover:text-destructive transition-colors disabled:opacity-20"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}

          <button
            onClick={addRow}
            className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors mt-1"
          >
            <Plus className="h-3.5 w-3.5" /> Add another
          </button>
        </div>

        {errors.length > 0 && (
          <div className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2 space-y-1">
            {errors.map((e, i) => <p key={i} className="text-xs text-destructive">{e}</p>)}
          </div>
        )}

        <div className="flex gap-3">
          <Button
            onClick={() => saveMut.mutate()}
            disabled={saveMut.isPending || filledCount === 0}
            className="gap-1.5"
          >
            {saveMut.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            Save {filledCount > 0 ? `${filledCount} secret${filledCount !== 1 ? "s" : ""}` : "secrets"}
          </Button>
          <Button
            variant="ghost"
            onClick={() => navigate({ to: "/projects/$id/secrets", params: { id: projectId } })}
          >
            Cancel
          </Button>
        </div>
      </div>
    </div>
  )
}
