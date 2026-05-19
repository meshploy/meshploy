import { createFileRoute, redirect } from "@tanstack/react-router"

export const Route = createFileRoute("/_app/projects/$id/jobs/$jobId/")({
  beforeLoad: ({ params }) =>
    redirect({ to: "/projects/$id/jobs/$jobId/runs", params }),
})
