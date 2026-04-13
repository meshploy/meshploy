import { createFileRoute } from "@tanstack/react-router"
import { Settings2 } from "lucide-react"
import { ComingSoonTab } from "./-components"

export const Route = createFileRoute("/_app/projects/$id/settings")({
  component: () => (
    <ComingSoonTab
      icon={Settings2}
      title="Settings"
      description="Manage project name, slug, environment variables, and danger zone actions."
    />
  ),
})
