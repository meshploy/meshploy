import { createFileRoute } from "@tanstack/react-router"
import { Zap } from "lucide-react"
import { ComingSoonTab } from "./-components"

export const Route = createFileRoute("/_app/projects/$id/jobs")({
  component: () => (
    <ComingSoonTab
      icon={Zap}
      title="Jobs"
      description="Run one-off tasks like migrations, exports, or seed scripts against your project's services."
    />
  ),
})
