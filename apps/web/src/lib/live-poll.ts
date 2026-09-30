/**
 * Polling intervals for views that show a status.
 *
 * Statuses change without the browser doing anything: a rollout finishes, a
 * reconciler notices a pod has gone, a build fails. So a status view always
 * polls - fast while something is visibly in motion, slowly otherwise - or a
 * settled status would stay stale until the page was reloaded.
 */
export const LIVE_ACTIVE_MS = 3_000
export const LIVE_SETTLED_MS = 15_000

/**
 * livePoll builds a TanStack Query `refetchInterval` from a predicate that says
 * whether the data is currently in motion.
 *
 * Returns the settled interval when there is no data yet, so a view polls from
 * the start rather than waiting for a first successful load.
 */
export function livePoll<T>(inMotion: (data: T) => boolean) {
  return (query: { state: { data?: T } }): number => {
    const data = query.state.data
    if (data === undefined) return LIVE_SETTLED_MS
    return inMotion(data) ? LIVE_ACTIVE_MS : LIVE_SETTLED_MS
  }
}
