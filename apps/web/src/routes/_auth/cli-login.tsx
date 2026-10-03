import { createFileRoute, redirect } from "@tanstack/react-router"
import { useAuthStore } from "@/store/auth-store"
import { CliLoginApproval, cliLoginHref } from "@/components/auth/cli-login-approval"

// Signing in first, two-factor included, happens on the console's own login,
// which sends people back here.

export const Route = createFileRoute("/_auth/cli-login")({
  validateSearch: (search: Record<string, unknown>): { code?: string } => ({
    code: typeof search.code === "string" && search.code ? search.code : undefined,
  }),
  beforeLoad: ({ search }) => {
    if (!useAuthStore.getState().token) throw redirect({ to: "/login", search: { next: cliLoginHref("/cli-login", search.code) } })
  },
  component: CliLoginPage,
})

function CliLoginPage() {
  const { code } = Route.useSearch()
  return <CliLoginApproval code={code} path="/cli-login" signIn="/login" />
}
