import { createFileRoute, redirect } from "@tanstack/react-router"

// Agents and their keys live on the Connectors page now.
export const Route = createFileRoute("/_app/agents/")({
  beforeLoad: () => {
    throw redirect({ to: "/connectors" })
  },
})
