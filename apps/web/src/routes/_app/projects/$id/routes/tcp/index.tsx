import { createFileRoute, redirect } from "@tanstack/react-router"

// TCP ports are listed with the HTTPS routes; this path exists so the
// breadcrumb above a TCP route leads somewhere.
export const Route = createFileRoute("/_app/projects/$id/routes/tcp/")({
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/projects/$id/routes", params: { id: params.id }, replace: true })
  },
})
