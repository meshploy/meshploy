import { createFileRoute } from "@tanstack/react-router"
import { GitSourcesTab } from "@/components/integrations/sections"

export const Route = createFileRoute("/_app/integrations/_tabs/git")({
  component: GitSourcesTab,
})
