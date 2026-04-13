import { createFileRoute } from "@tanstack/react-router"
import { Workflow } from "lucide-react"
import { ComingSoonTab } from "./-components"

export const Route = createFileRoute("/_app/projects/$id/pipelines")({
  component: () => (
    <ComingSoonTab
      icon={Workflow}
      title="Pipelines"
      description="Orchestrate multi-step deployments — build, migrate, deploy, verify — as a single atomic workflow."
    />
  ),
})
