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
]
