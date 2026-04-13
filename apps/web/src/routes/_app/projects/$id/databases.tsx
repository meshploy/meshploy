import { createFileRoute } from "@tanstack/react-router"
import { Database } from "lucide-react"
import { ComingSoonTab } from "./-components"

export const Route = createFileRoute("/_app/projects/$id/databases")({
  component: () => (
    <ComingSoonTab
      icon={Database}
      title="Databases"
      description="Provision managed PostgreSQL, MySQL, Redis, and MongoDB instances as K8s workloads within this project."
    />
  ),
})
