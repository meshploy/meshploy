import { createFileRoute, redirect } from "@tanstack/react-router"

export const Route = createFileRoute("/_app/projects/$id/stacks/$stackId/")({
  beforeLoad: ({ params }) =>
    redirect({ to: "/projects/$id/stacks/$stackId/services", params }),
})
