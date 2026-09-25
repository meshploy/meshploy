---
id: services
title: Services & builds
summary: What a service is, how Meshploy builds it from git or runs an image, and how a deploy, a rollback and a redeploy differ.
pages: [services]
---

## Services {#services}

A **service** is one long-running workload in a project: a web app, an API, a worker. It runs from a container image, either one you name (`nginx:1.27`, `ghcr.io/you/app:v3`) or one Meshploy **builds from your git repository**.

A service has its own variables, ports, replicas and resource limits. Its page shows what it runs now and where that came from, its deployments, its pods and their logs, and a terminal into a running pod.

## Building from git {#building}

Meshploy clones the branch you choose and builds an image with one of three builders:

- **Railpack** and **Nixpacks** look at the code and work out how to build it, with no Dockerfile needed.
- **Dockerfile** builds your own Dockerfile, from the path you give.

A build runs as a job on a **build node**, pushes the image to your registry (the built-in one, or a registry you connected), then deploys it. The build log streams on the deployment's page, and the deployment records the branch and commit it built.

## Deploying on push {#auto-deploy}

With **auto-deploy** on, a push to the tracked branch builds and deploys by itself. A GitHub connection reports every push through the Meshploy GitHub App; GitLab, Gitea and Bitbucket use a webhook on the repository, which Meshploy adds when its token allows. **Watch paths** narrow it to pushes that change certain folders, for a repository that holds more than one service.

The **deploy webhook URL** builds the service whenever it is called, from a CI job, a cron, or a public repository with no connection. The token in it is the only thing guarding it: treat it as a credential.

## Variables {#variables}

A service's own variables are set on its Configuration tab. It can also attach **variable groups**: shared ones, and the ones other services publish, such as a database's connection details. Its own variables win on a clash.

A value can refer to another with `${NAME}`, so `DATABASE_URL=${PRIMARY_DB_URL}` reads the database's published URL instead of a copy that goes stale. A reference to a name the service does not have is left as written and noted in the deploy's log. **Build variables** are a separate block, given to the builder only.

## Deploy, rollback and redeploy {#deploys}

- **Deploy** builds from git, or deploys the configured image.
- **Rollback** runs an earlier deployment's image again, as it was. Images are kept for it while **image retention** allows.
- **Redeploy** runs the current image again, for changed variables, and builds nothing.

A deploy fails at once when no online node can build, and after a few minutes when no node can take the service.

## Common tasks {#how-to}

- **Deploy from a repository.** New resource, choose Service, pick the git connection, repository and branch, then Deploy.
- **Connect a database.** Attach the database's variable group on the service's Configuration tab, then use `${<NAME>_URL}` in a variable, and redeploy.
- **Go back to the last good version.** Deployments tab, choose an earlier successful deployment, Rollback.

## Terms {#terms}

- **Build node** {#build-node -> building}: A node that runs image builds. The gateway is one by default; any node can be made one.
- **Auto-deploy** {#auto-deploy -> auto-deploy}: Build and deploy on every push to the tracked branch.
- **Watch paths** {#watch-paths -> auto-deploy}: Only pushes that change these folders deploy; for a repository holding more than one service.
- **Deploy webhook** {#deploy-webhook -> auto-deploy}: A URL that builds this service whenever it is called. Its token is the only guard: treat it as a credential.
- **Redeploy** {#redeploy -> deploys}: Runs the image the service runs now again, for changed variables, without building.
- **Rollback** {#rollback -> deploys}: Runs an earlier deployment's image again, as it was.
- **Image retention** {#image-retention -> deploys}: How many built images are kept, which is how far back a rollback can go.
