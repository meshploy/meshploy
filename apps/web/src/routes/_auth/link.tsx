import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useEffect, useRef, useState } from "react"
import { Loader2, LinkIcon } from "lucide-react"
import { auth, orgs, ApiError } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { safeNext } from "@/lib/safe-next"

/**
 * A one-time sign-in link: /link#<token>, made by whoever holds the server's
 * sign-in link key (Cloud's Open button). The token rides in the fragment so
 * no server on the way logs it, and it is taken out of the address bar before
 * anything else happens.
 */
export const Route = createFileRoute("/_auth/link")({
  validateSearch: (search: Record<string, unknown>): { next?: string } => ({ next: safeNext(search.next) }),
  component: SignInLinkPage,
})

const SPENT = "It was used already, or it has expired: a link works once, for five minutes. Ask for a new one where you got it, or sign in."
const NO_TOKEN = "This address carries no sign-in link. Open it again from where you got it, or sign in."

function SignInLinkPage() {
  const navigate = useNavigate()
  const { next } = Route.useSearch()
  const setAuth = useAuthStore((s) => s.setAuth)
  const setOrgs = useOrgStore((s) => s.setOrgs)
  const [error, setError] = useState<string | null>(null)
  // A link is spent once: React's development double effect must not try twice.
  const started = useRef(false)

  useEffect(() => {
    if (started.current) return
    started.current = true
    const token = window.location.hash.slice(1)
    window.history.replaceState(null, "", window.location.pathname + window.location.search)
    ;(async () => {
      try {
        if (!token) throw new ApiError(400, NO_TOKEN)
        const { token: session } = await auth.spendSignInLink(token)
        const payload = JSON.parse(atob(session.split(".")[1]))
        setAuth(session, payload.uid as string)
        const orgList = await orgs.list(session)
        setOrgs(orgList.map((o) => ({ id: o.id, name: o.name, slug: o.slug })))
        if (next) navigate({ href: next, replace: true })
        else navigate({ to: "/", replace: true })
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) setError(SPENT)
        else setError(err instanceof ApiError ? err.detail : "Something went wrong signing you in. Try the link again, or sign in.")
      }
    })()
  }, [next, navigate, setAuth, setOrgs])

  if (error) {
    return (
      <div className="rounded-xl border border-border/60 bg-card p-6 space-y-4">
        <div className="flex items-center gap-2.5">
          <LinkIcon className="h-5 w-5 text-muted-foreground" />
          <h2 className="text-base font-semibold text-foreground">This link can't sign you in</h2>
        </div>
        <p className="text-sm text-muted-foreground">{error}</p>
        <Button render={<Link to="/login" search={next ? { next } : {}} />} className="w-full h-9">
          Sign in
        </Button>
      </div>
    )
  }

  return (
    <div className="rounded-xl border border-border/60 bg-card p-6 flex items-center justify-center gap-2.5 text-sm text-muted-foreground">
      <Loader2 className="h-4 w-4 animate-spin" />
      Signing you in
    </div>
  )
}
