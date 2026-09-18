import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Check, Copy, Loader2, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { buildConfigs as buildConfigsApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"

/**
 * The URL that builds one service, whoever calls it.
 *
 * Shown wherever someone would go looking for it: in the build configuration
 * beside deploy-on-push, and on the deployments tab, which is where you are
 * standing when you want to re-deploy from somewhere else. It is the same URL
 * and the same token in both places - one component, so they cannot drift.
 */
export function DeployWebhookURL({
  orgId,
  projectId,
  serviceId,
  deployToken,
  description,
}: {
  orgId: string
  projectId: string
  serviceId: string
  deployToken: string
  /** Shown above the URL where there is room to explain it. */
  description?: string
}) {
  const token = useAuthStore((s) => s.token)!
  const qc = useQueryClient()
  const [copied, setCopied] = useState(false)

  // Whole, the way it has to be pasted: a path alone is no use in a provider's
  // webhook box or a curl.
  const url = `${window.location.origin}/api/v1/webhooks/deploy/${serviceId}?token=${deployToken}`

  const regenerate = useMutation({
    mutationFn: () => buildConfigsApi.regenerateDeployToken(orgId, projectId, serviceId, token),
    onSuccess: (updated) => {
      qc.setQueryData(["build-config", orgId, projectId, serviceId], updated)
    },
  })

  return (
    <div className="flex flex-col gap-2">
      {description && <p className="text-xs text-muted-foreground">{description}</p>}
      <div className="flex items-center gap-2">
        <code className="flex-1 min-w-0 text-[11px] font-mono bg-muted/30 border border-border/40 rounded px-2.5 py-1.5 text-foreground/70 truncate">
          POST {url}
        </code>
        <Button
          size="icon-sm"
          variant="outline"
          title="Copy the URL"
          onClick={() => {
            navigator.clipboard.writeText(url)
            setCopied(true)
            setTimeout(() => setCopied(false), 2000)
          }}
        >
          {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
        </Button>
        <Button
          size="icon-sm"
          variant="outline"
          onClick={() => regenerate.mutate()}
          disabled={regenerate.isPending}
          title="Issue a new token — the current URL stops working"
        >
          {regenerate.isPending
            ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
            : <RefreshCw className="h-3.5 w-3.5" />}
        </Button>
      </div>
      {regenerate.isError && (
        <p className="text-xs text-destructive">{(regenerate.error as Error).message}</p>
      )}
    </div>
  )
}
