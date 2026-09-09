import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ShieldAlert, X } from "lucide-react"
import { NOTICE_HOST_EXPOSURE, system as systemApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"

/**
 * Reports a gateway installed onto a host with no firewall, and what Meshploy
 * publishes as a result.
 *
 * This is an advisory, not a verdict, and the copy says so. Meshploy does not
 * manage the operator's firewall — a cloud security group, a NAT gateway or an
 * upstream appliance is invisible from the host and may already cover every
 * port listed here. That is why it can be dismissed rather than nagging: the
 * operator knows things the installer cannot see.
 *
 * Renders nothing unless the installer actually recorded "no firewall", so a
 * dev machine and a firewalled gateway both stay silent.
 */
export function ExposureNotice() {
  const token = useAuthStore((s) => s.token)!
  const qc = useQueryClient()

  const { data } = useQuery({
    queryKey: ["system-exposure"],
    queryFn: () => systemApi.exposure(token),
    enabled: !!token,
    // A point-in-time fact recorded at install; polling it would be noise.
    staleTime: Infinity,
  })

  const dismiss = useMutation({
    mutationFn: () => systemApi.dismissNotice(NOTICE_HOST_EXPOSURE, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["system-exposure"] }),
  })

  if (!data || data.dismissed || data.firewall_state !== "none" || data.ports.length === 0) {
    return null
  }

  const checked = data.checked_at
    ? new Date(data.checked_at).toLocaleDateString(undefined, {
        year: "numeric",
        month: "short",
        day: "numeric",
      })
    : null

  return (
    <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 overflow-hidden">
      <div className="px-4 py-3 border-b border-amber-500/20 flex items-start gap-2.5">
        <ShieldAlert className="h-4 w-4 text-amber-400 shrink-0 mt-0.5" />
        <div className="flex-1 min-w-0">
          <p className="text-xs font-medium text-amber-400">
            No firewall was running on this server when Meshploy was installed
          </p>
          <p className="text-[11px] text-muted-foreground/70 mt-0.5">
            Meshploy publishes the ports below on every network interface, so on this host nothing
            in the operating system is restricting who can reach them. If your provider&rsquo;s
            firewall or security group already covers them, this is nothing to act on — Meshploy
            cannot see those, which is why it is only telling you.
          </p>
        </div>
        <button
          type="button"
          onClick={() => dismiss.mutate()}
          disabled={dismiss.isPending}
          aria-label="Dismiss this notice"
          className="shrink-0 rounded p-1 text-muted-foreground/60 hover:text-foreground hover:bg-amber-500/10 disabled:opacity-50"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="divide-y divide-amber-500/10">
        {data.ports.map((p) => (
          <div key={p.port} className="px-4 py-2.5 flex items-center gap-3">
            <code className="text-xs font-mono text-foreground w-14 shrink-0">{p.port}</code>
            <span className="text-xs text-foreground/80 shrink-0">{p.service}</span>
            {p.note && (
              <span className="text-[11px] text-muted-foreground/70 min-w-0 truncate">{p.note}</span>
            )}
          </div>
        ))}
      </div>

      {checked && (
        <div className="px-4 py-2 border-t border-amber-500/20">
          <p className="text-[11px] text-muted-foreground/60">
            Checked when Meshploy was installed, on {checked}. Re-running the installer checks
            again — it is not watching live.
          </p>
        </div>
      )}
    </div>
  )
}
