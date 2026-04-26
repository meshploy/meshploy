import { createFileRoute } from "@tanstack/react-router"
import { KeyRound } from "lucide-react"
import { ComingSoonTab } from "./-components"

export const Route = createFileRoute("/_app/projects/$id/secrets")({
  component: () => (
    <ComingSoonTab
      icon={KeyRound}
      title="Secrets"
      description="Manage environment secrets and inject them into your services."
    />
  ),
})
