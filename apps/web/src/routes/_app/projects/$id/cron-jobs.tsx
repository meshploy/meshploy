import { createFileRoute } from "@tanstack/react-router"
import { Clock } from "lucide-react"
import { ComingSoonTab } from "./-components"

export const Route = createFileRoute("/_app/projects/$id/cron-jobs")({
  component: () => (
    <ComingSoonTab
      icon={Clock}
      title="Cron Jobs"
      description="Schedule recurring tasks on a cron expression. Nightly backups, cleanup scripts, scheduled reports."
    />
  ),
})
