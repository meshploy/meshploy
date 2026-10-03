/**
 * Where to go after signing in: a path on this console, never another site.
 * A page that needs a session sends people here with it, so they come back.
 */
export function safeNext(next: unknown): string | undefined {
  if (typeof next !== "string" || !next.startsWith("/") || next.startsWith("//") || next.startsWith("/\\")) return undefined
  return next
}
