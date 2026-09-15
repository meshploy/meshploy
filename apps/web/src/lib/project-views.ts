import type { QueryClient } from "@tanstack/react-query"

/**
 * Refresh every list an apply, a sync or a template deploy can change.
 *
 * Those create services, routes and volumes as a side effect, and each list
 * has its own query. Invalidating only what the caller was looking at left the
 * rest stale: a stack apply that created a route showed 7 in the sidebar, which
 * polls, and 6 on the routes page, which does not, until a reload.
 */
export function invalidateProjectViews(qc: QueryClient, orgId: string | undefined, projectId: string) {
  for (const key of ["project", "routes", "services", "volumes", "stacks", "domains"]) {
    qc.invalidateQueries({ queryKey: [key, orgId, projectId] })
  }
}
