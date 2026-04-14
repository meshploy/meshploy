import { createFileRoute, redirect } from "@tanstack/react-router"

export const Route = createFileRoute("/_app/projects/$id/services/$serviceId/")({
  beforeLoad: ({ params }) =>
    redirect({ to: "/projects/$id/services/$serviceId/deployments", params }),
})
