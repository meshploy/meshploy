import { useEffect, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Loader2, ScanSearch } from "lucide-react"
import { gitIntegrations, type ApiDetection } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { TermInfo } from "@/help/term"

/** What the settings are filled in from, so the form can say it. */
export interface DetectionApplied {
  builder?: string
  port?: number
  startCommand?: string
  memoryLimit?: string
}

/**
 * Looks at the chosen repository and branch before the first build, and asks
 * the form to fill in what fits the app: the builder, the port, a start
 * command, the memory. Waits for typing to settle, since a public repository
 * is typed in by hand.
 */
export function useStackDetection(orgId: string | undefined, source: { gitIntegrationId: string; gitRepo: string; gitBranch: string; dockerfilePath: string }) {
  const token = useAuthStore((s) => s.token)!
  const key = [source.gitIntegrationId, source.gitRepo.trim(), source.gitBranch.trim()] as const
  const [settled, setSettled] = useState(key)
  useEffect(() => {
    const t = setTimeout(() => setSettled(key), 600)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key[0], key[1], key[2]])
  const [integration, repo, branch] = settled
  return useQuery({
    queryKey: ["detect-stack", orgId, integration, repo, branch],
    queryFn: () =>
      gitIntegrations.detect(orgId!, { git_integration_id: integration || undefined, repo, branch }, token),
    enabled: !!orgId && !!repo && !!branch,
    staleTime: 5 * 60_000,
    retry: false,
  })
}

/** The line under the repository: what it is, what was filled in, and why. */
export function StackDetection({ query, applied }: { query: ReturnType<typeof useStackDetection>; applied: DetectionApplied }) {
  const { data, isFetching, error } = query
  if (isFetching && !data) {
    return (
      <p className="flex items-center gap-2 text-xs text-muted-foreground" role="status">
        <Loader2 className="size-3.5 animate-spin" />
        Looking at the repository…
      </p>
    )
  }
  if (error) {
    return (
      <p className="text-xs text-muted-foreground" role="status">
        Could not look at the repository ({(error as Error).message}). The build works out what it can.
      </p>
    )
  }
  if (!data) return null
  const filled = describe(data, applied)
  return (
    <div className="rounded-md border border-sky-500/25 bg-sky-500/[0.05] px-3 py-2.5" data-testid="stack-detection" role="status">
      <p className="flex items-center gap-2 text-sm">
        <ScanSearch className="size-4 shrink-0 text-sky-400" />
        <span className="text-muted-foreground">Detected</span>
        <span className="font-medium text-sky-200">{data.summary || "nothing Meshploy recognises; the build works it out"}</span>
        <TermInfo id="services.detected" />
      </p>
      {filled.length > 0 && (
        <p className="mt-1 pl-6 text-xs text-muted-foreground">
          Filled in: {filled.map((f, i) => (
            <span key={f}>{i > 0 && " · "}<span className="text-foreground/90">{f}</span></span>
          ))}
        </p>
      )}
      {data.notes && data.notes.length > 0 && (
        <ul className="mt-1 space-y-0.5 pl-6 text-xs text-muted-foreground">
          {data.notes.map((n) => <li key={n}>{n}</li>)}
        </ul>
      )}
    </div>
  )
}

function describe(d: ApiDetection, a: DetectionApplied) {
  const out: string[] = []
  if (a.builder && d.builder) out.push(d.builder === "dockerfile" ? "Dockerfile builder" : "Railpack builder")
  if (a.port) out.push(`port ${a.port}`)
  if (a.startCommand) out.push("start command")
  if (a.memoryLimit) out.push(`memory limit ${a.memoryLimit.replace(/Gi$/, " GiB").replace(/Mi$/, " MiB")}`)
  return out
}
