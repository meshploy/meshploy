import type { ApiService } from "@/lib/api"

// Beside a service's status when its latest deployment failed: it still
// runs, on what it ran before, and "running" alone read as if the new build
// were live. A tag, not a link, as the trouble tag beside it is: it sits on
// cards that open the service, whose Deployments tab has the failure's log.
export function NotLatestTag({ service }: { service: Pick<ApiService, "latest_deploy_failed" | "status"> }) {
  if (!service.latest_deploy_failed || service.status === "deploying") return null
  return (
    <span title="The latest deployment failed: it runs what it ran before, not the latest build"
      data-testid="not-latest-tag"
      className="inline-flex shrink-0 items-center rounded-full border border-amber-500/35 bg-amber-500/10 px-1.5 text-[10px] font-medium text-amber-300">
      latest build failed
    </span>
  )
}
