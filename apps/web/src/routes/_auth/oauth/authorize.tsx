import { createFileRoute, redirect } from "@tanstack/react-router"
import { useAuthStore } from "@/store/auth-store"
import { OAuthApproval } from "@/components/auth/oauth-approval"
import type { OAuthRequest } from "@/lib/api"

// OAuth's authorization endpoint for MCP clients. Signing in first, two-factor
// included, happens on the console's own login, which sends people back here
// with the client's request intact.

type Search = Partial<OAuthRequest> & { response_type?: string }

const str = (v: unknown) => (typeof v === "string" && v ? v : undefined)

export const Route = createFileRoute("/_auth/oauth/authorize")({
  validateSearch: (s: Record<string, unknown>): Search => ({
    response_type: str(s.response_type),
    client_id: str(s.client_id),
    redirect_uri: str(s.redirect_uri),
    code_challenge: str(s.code_challenge),
    code_challenge_method: str(s.code_challenge_method),
    state: str(s.state),
  }),
  beforeLoad: () => {
    if (!useAuthStore.getState().token) {
      throw redirect({ to: "/login", search: { next: window.location.pathname + window.location.search } })
    }
  },
  component: AuthorizePage,
})

function AuthorizePage() {
  const s = Route.useSearch()
  if (s.response_type !== "code" || !s.client_id || !s.redirect_uri) {
    return (
      <div className="rounded-xl border border-border/60 bg-card p-6">
        <h2 className="text-base font-semibold">This connection request is not valid</h2>
        <p className="mt-0.5 text-sm text-muted-foreground">It is missing what an application sends when it asks to connect. Start again from the application.</p>
      </div>
    )
  }
  const request: OAuthRequest = {
    client_id: s.client_id, redirect_uri: s.redirect_uri, state: s.state,
    code_challenge: s.code_challenge ?? "", code_challenge_method: s.code_challenge_method ?? "",
  }
  return <OAuthApproval request={request} href={window.location.pathname + window.location.search} signIn="/login" />
}
