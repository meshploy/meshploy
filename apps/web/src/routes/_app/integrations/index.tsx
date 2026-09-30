import { createFileRoute, redirect } from "@tanstack/react-router"

// Each category is its own route. A provider's connect flow may still return
// here, so its parameters are carried on to the git tab.
export const Route = createFileRoute("/_app/integrations/")({
  beforeLoad: ({ search }) => {
    throw redirect({ to: "/integrations/git", search, replace: true })
  },
})
