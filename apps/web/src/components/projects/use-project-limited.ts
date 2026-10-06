import { useQuery } from "@tanstack/react-query"
import { projects as projectsApi } from "@/lib/api"

/**
 * Whether you see this project limited to what you were granted inside it.
 * Then nothing is created in it from here: that needs the project itself.
 * Shares the project layout's query, so it costs no request of its own.
 */
export function useProjectLimited(orgId: string | undefined, projectId: string, token: string) {
  const { data } = useQuery({
    queryKey: ["project", orgId, projectId],
    queryFn: () => projectsApi.get(orgId!, projectId, token),
    enabled: !!orgId,
  })
  return !!data?.limited
}
