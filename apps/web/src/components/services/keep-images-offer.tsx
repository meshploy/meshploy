import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { HardDrive, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { imageRetention } from "@/lib/api"

// Services made before keeping the last three images was the default keep
// every image they build. Offered once per level, not done at upgrade: moving
// them removes their older images, which cannot be rolled back to afterwards.
export function KeepImagesOffer({ orgId, projectId, token }: { orgId: string; projectId: string; token: string }) {
  const qc = useQueryClient()
  const dismissKey = `keep-images-offer:${projectId}`
  const [dismissed, setDismissed] = useState(() => {
    try { return localStorage.getItem(dismissKey) === "1" } catch { return false }
  })
  const { data } = useQuery({
    queryKey: ["image-retention", orgId, projectId],
    queryFn: () => imageRetention.list(orgId, projectId, token),
    enabled: !dismissed,
  })
  const keep = useMutation({
    mutationFn: () => imageRetention.keepLast(orgId, projectId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["image-retention", orgId, projectId] })
      qc.invalidateQueries({ queryKey: ["build-config", orgId, projectId] })
    },
  })

  const services = data?.services ?? []
  if (dismissed || services.length === 0) return null
  const images = services.reduce((n, s) => n + s.images_kept, 0)
  const names = services.map((s) => s.name)
  const shown = names.length > 3 ? `${names.slice(0, 3).join(", ")} and ${names.length - 3} more` : names.join(", ")

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border/60 bg-muted/10 px-4 py-3"
      data-testid="keep-images-offer">
      <div className="flex items-start gap-2.5 min-w-0">
        <HardDrive className="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
        <div className="min-w-0">
          <p className="text-sm">
            {services.length === 1 ? "1 service keeps" : `${services.length} services keep`} every image {services.length === 1 ? "it builds" : "they build"} ({images} now)
          </p>
          <p className="text-xs text-muted-foreground">
            {shown}. Keep the last {data?.keep ?? 3} of each for rollback, as a new service does; the older ones are removed now and cannot be rolled back to.
          </p>
          {keep.isError && <p className="text-xs text-destructive mt-1">{(keep.error as Error).message}</p>}
        </div>
      </div>
      <div className="flex items-center gap-2 shrink-0">
        <Button size="sm" variant="ghost" onClick={() => {
          try { localStorage.setItem(dismissKey, "1") } catch { /* private window: shown again next time */ }
          setDismissed(true)
        }}>Not now</Button>
        <Button size="sm" onClick={() => keep.mutate()} disabled={keep.isPending}>
          {keep.isPending && <Loader2 className="size-4 animate-spin" />}Keep the last {data?.keep ?? 3}
        </Button>
      </div>
    </div>
  )
}
