import { createFileRoute, redirect } from "@tanstack/react-router"

export const Route = createFileRoute("/_app/projects/$id/")({
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/projects/$id/services", params, replace: true })
  },
})
