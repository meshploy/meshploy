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

## Terms {#terms}

- **Apply** {#apply -> apply}: Makes the cluster match the stack's spec. A service taken out of the spec is unlinked, not deleted.
- **Destroyed** {#destroyed -> apply}: The stack's services are torn down; the stack and its spec are kept, and applying again recreates them. Volumes and routes go only if you chose so.
- **Stack variables** {#variables -> sources}: Values filled in for ${NAME} in the stack's spec.
