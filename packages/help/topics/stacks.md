---
id: stacks
title: Stacks
summary: A group of services, volumes, routes and files defined together in one Compose file, applied and removed as one.
pages: [stacks]
---

## Stacks {#stacks}

A **stack** is a set of resources defined together in one **Docker Compose** file: several services, the volumes they keep data on, the routes that reach them, and the config files they read. Applying the stack creates or updates all of them at once; each is then an ordinary service, volume or route you can open, marked as managed by the stack.

## Where the spec comes from {#sources}

- **Written here**: edit the Compose file in the stack's editor.
- **From git**: a Compose file in a repository, fetched on its own or with the whole repository cloned around it. **Sync** pulls the latest from the branch.
- **From a template**: a one-click template creates its app as a stack. See [Templates](/concepts/templates).

A stack's **variables** fill in `${NAME}` in its spec, so one spec serves with different values.

## Apply and destroy {#apply}

**Apply** makes the cluster match the spec: new services are created and changed ones updated. A service taken out of the spec is **unlinked** from the stack, not deleted: it keeps running as a service of its own, for you to keep or delete.

**Destroy** tears the stack's services down but keeps the stack and its spec, so applying again brings them back. Its volumes and routes are removed only if you choose to: a volume holds data, and a route holds a name someone may be using.

## How Compose maps {#compose}

A stack runs a Compose file the way Docker Compose would, with Kubernetes underneath:

- **Names.** Each service is reached by its Compose name on any port it listens on, as on a Compose network, and by its `container_name` and network `aliases` too. No `ports:` are needed for one service to reach another.
- **Builds.** A `build:` section builds from its Dockerfile: `dockerfile:` if given, else `Dockerfile` in the context, with its `args:`. A monorepo whose services share one Dockerfile and differ by a build argument works as written.
- **Run once.** A service another one waits for with `condition: service_completed_successfully` (a migration, a setup step) runs to completion on each deploy instead of being kept up, and shows as **completed**. `restart: on-failure:N` retries it N times. The services waiting for it start alongside it rather than after it, so they should retry until it has run.
- **Published ports.** A port published everywhere (`5432:5432`) is reachable on the mesh, never on the internet unless you add a TCP route. One published on a single address of the host (`100.81.6.12:5433:5432`) gets a TCP route bound to that address, created by its first deploy. One published on `127.0.0.1` stays inside the cluster.
- **Bind mounts** of the repository's own files (`./migrations`, `./scripts/setup.sh`) arrive where Compose put them, read-only, up to 1 MB per service in all. A bind mount of a path on the host, such as `/var/run/docker.sock`, is left out, and the apply says so.

## Terms {#terms}

- **Apply** {#apply -> apply}: Makes the cluster match the stack's spec. A service taken out of the spec is unlinked, not deleted.
- **Destroyed** {#destroyed -> apply}: The stack's services are torn down; the stack and its spec are kept, and applying again recreates them. Volumes and routes go only if you chose so.
- **Run once** {#run-once -> compose}: A service that does its work and exits, run to completion on each deploy. Compose marks it by another service waiting for it to complete.
- **Stack variables** {#variables -> sources}: Values filled in for ${NAME} in the stack's spec.
