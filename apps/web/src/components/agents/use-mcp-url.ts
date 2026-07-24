import { useQuery } from "@tanstack/react-query"
import { domains as domainsApi } from "@/lib/api"

/**
 * useMcpUrl resolves the org's remote MCP connect endpoint. The console lives at
 * `console.<base_domain>`, and the MCP server is mounted at `/mcp`. When no domain
 * is configured yet we fall back to the current origin so the value is still usable
 * in local/dev setups.
 */
export function useMcpUrl(orgId: string | undefined, token: string): string {
  const { data: domains = [] } = useQuery({
    queryKey: ["domains", orgId],
    queryFn: () => domainsApi.list(orgId!, token),
    enabled: !!orgId,
  })

  const base = domains.find((d) => d.verified)?.base_domain ?? domains[0]?.base_domain
  if (base) return `https://console.${base}/mcp`
  return `${window.location.origin}/mcp`
}
