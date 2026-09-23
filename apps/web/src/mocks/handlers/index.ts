import { http, HttpResponse } from "msw"
import { domainsHandlers } from "./domains"
import { workspaceHandlers } from "./workspace"
import { streamHandlers } from "./streams"
import { authHandlers } from "./auth"
import { orgsHandlers } from "./orgs"
import { nodesHandlers } from "./nodes"
import { projectsHandlers } from "./projects"
import { servicesHandlers } from "./services"
import { jobsHandlers } from "./jobs"
import { volumesHandlers } from "./volumes"
import { stacksHandlers } from "./stacks"
import { templatesHandlers } from "./templates"
import { routesHandlers } from "./routes"
import { clusterHandlers } from "./cluster"
import { systemHandlers } from "./system"

export const handlers = [
  // Ahead of the generic CRUD, which would otherwise answer the domain list and
  // delete without the rules the real API enforces.
  ...domainsHandlers,
  ...workspaceHandlers,
  ...streamHandlers,
  ...authHandlers,
  ...orgsHandlers,
  ...nodesHandlers,
  ...projectsHandlers,
  ...servicesHandlers,
  ...jobsHandlers,
  ...volumesHandlers,
  ...stacksHandlers,
  ...templatesHandlers,
  ...routesHandlers,
  ...clusterHandlers,
  ...systemHandlers,
  http.all("/api/*", ({ request }) => HttpResponse.json({ detail: `Demo endpoint not implemented: ${request.method} ${new URL(request.url).pathname}` }, { status: 501 })),
]
