import { useState } from "react"
import { useNavigate } from "@tanstack/react-router"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Check, Lightbulb, Loader2, RotateCw, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { services as servicesApi, buildConfigs, type ApiHint, type ApiHintFix, type ApiService } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"

/**
 * Advice from the service's last build, each with its fix: a start command
 * the builder could not work out, more memory than the limit for what the app
 * loads, a port the app does not listen on. Taking a fix saves the setting;
 * it reaches the running app on the next deploy, which the panel offers.
 */
export function HintsPanel({ hints, service, orgId, projectId }: { hints: ApiHint[]; service: ApiService; orgId: string; projectId: string }) {
  const token = useAuthStore((s) => s.token)!
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [editing, setEditing] = useState<{ kind: ApiHint["kind"]; value: string } | null>(null)
  // What was just saved, so the panel can offer the deploy that applies it.
  const [saved, setSaved] = useState<"redeploy" | "build" | null>(null)

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ["project-health", orgId, projectId] })
    queryClient.invalidateQueries({ queryKey: ["service", orgId, projectId, service.id] })
    queryClient.invalidateQueries({ queryKey: ["build-config", orgId, projectId, service.id] })
    queryClient.invalidateQueries({ queryKey: ["overview", orgId] })
  }

  const fix = useMutation({
    mutationFn: async ({ action, value }: { action: ApiHintFix["action"]; value: string }) => {
      switch (action) {
        case "start_command":
          return servicesApi.update(orgId, projectId, service.id, { start_command: value.trim() }, token)
        case "memory":
          return servicesApi.update(orgId, projectId, service.id, { memory_limit: value }, token)
        case "port": {
          const primary = service.ports.find((p) => p.is_primary) ?? service.ports[0]
          const ports = service.ports.map((p) => ({
            name: p.name, port: p === primary ? Number(value) : p.port,
            is_http: p.is_http, is_primary: p.is_primary, is_public: p.is_public,
          }))
          return servicesApi.update(orgId, projectId, service.id, { ports }, token)
        }
        case "builder":
          return buildConfigs.update(orgId, projectId, service.id, { builder: value as "dockerfile" }, token)
      }
    },
    onSuccess: (_, { action }) => {
      setEditing(null)
      setSaved(action === "builder" || !service.image ? "build" : "redeploy")
      refresh()
    },
  })

  const dismiss = useMutation({
    mutationFn: (kind: ApiHint["kind"]) => servicesApi.dismissHint(orgId, projectId, service.id, kind, token),
    onSuccess: refresh,
  })

  const redeploy = useMutation({
    mutationFn: () => servicesApi.redeploy(orgId, projectId, service.id, token),
    onSuccess: (d) => {
      setSaved(null)
      queryClient.invalidateQueries({ queryKey: ["deployments", orgId, projectId, service.id] })
      navigate({ to: "/projects/$id/services/$serviceId/deployments/$deploymentId", params: { id: projectId, serviceId: service.id, deploymentId: d.id } })
    },
  })

  if (hints.length === 0 && !saved) return null

  return (
    <section aria-label="Suggestions from the last build" className="mb-3 overflow-hidden rounded-md border border-sky-500/25 bg-sky-500/[0.05]" data-testid="hints-panel">
      <ul className="divide-y divide-sky-500/15">
        {hints.map((h) => (
          <li key={h.kind} className="flex items-start gap-3 px-3 py-2.5" data-testid={`hint-${h.kind}`}>
            <Lightbulb className="mt-0.5 size-4 shrink-0 text-sky-400" />
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium text-sky-200">{h.title}</p>
              <p className="text-xs text-muted-foreground">{h.detail}</p>
              {editing?.kind === h.kind ? (
                <form
                  className="mt-2 flex flex-wrap items-center gap-2"
                  onSubmit={(e) => {
                    e.preventDefault()
                    if (editing.value.trim()) fix.mutate({ action: "start_command", value: editing.value })
                  }}
                >
                  <Input
                    autoFocus
                    aria-label="Start command"
                    value={editing.value}
                    onChange={(e) => setEditing({ kind: h.kind, value: e.target.value })}
                    placeholder="e.g. uvicorn app.main:app --host 0.0.0.0 --port $PORT"
                    className="h-7 min-w-[16rem] flex-1 font-mono text-xs"
                  />
                  <Button type="submit" size="sm" className="h-7 text-xs" disabled={!editing.value.trim() || fix.isPending}>
                    {fix.isPending ? <Loader2 className="size-3 animate-spin" /> : <Check className="size-3" />}
                    Save
                  </Button>
                  <Button type="button" size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setEditing(null)}>Cancel</Button>
                </form>
              ) : (
                h.fixes && h.fixes.length > 0 && (
                  <div className="mt-2 flex flex-wrap items-center gap-2">
                    {h.fixes.map((f) => (
                      <Button
                        key={f.action + f.value}
                        size="sm"
                        variant="outline"
                        className="h-7 border-sky-500/30 text-xs text-sky-200 hover:bg-sky-500/10"
                        disabled={fix.isPending}
                        title={f.action === "start_command" && f.value ? f.value : undefined}
                        onClick={() =>
                          f.action === "start_command"
                            ? setEditing({ kind: h.kind, value: f.value })
                            : fix.mutate({ action: f.action, value: f.value })
                        }
                      >
                        {f.label}
                      </Button>
                    ))}
                  </div>
                )
              )}
              {fix.isError && editing?.kind === h.kind && (
                <p className="mt-1 text-xs text-red-400">{(fix.error as Error).message}</p>
              )}
            </div>
            <button
              type="button"
              aria-label={`Dismiss: ${h.title}`}
              title="Dismiss for everyone"
              onClick={() => dismiss.mutate(h.kind)}
              className="rounded p-0.5 text-muted-foreground hover:bg-secondary hover:text-foreground"
            >
              <X className="size-3.5" />
            </button>
          </li>
        ))}
        {saved && (
          <li className="flex flex-wrap items-center gap-x-3 gap-y-2 px-3 py-2.5" role="status">
            <Check className="size-4 shrink-0 text-emerald-400" />
            <p className="min-w-0 flex-1 text-xs text-muted-foreground">
              {saved === "redeploy"
                ? "Saved. It reaches the running app on the next deploy."
                : "Saved. Deploy to build with it."}
            </p>
            {saved === "redeploy" && (
              <Button size="sm" className="h-7 text-xs" onClick={() => redeploy.mutate()} disabled={redeploy.isPending}>
                {redeploy.isPending ? <Loader2 className="size-3 animate-spin" /> : <RotateCw className="size-3" />}
                Redeploy now
              </Button>
            )}
            <button type="button" aria-label="Close" onClick={() => setSaved(null)} className="rounded p-0.5 text-muted-foreground hover:bg-secondary hover:text-foreground">
              <X className="size-3.5" />
            </button>
          </li>
        )}
      </ul>
    </section>
  )
}
