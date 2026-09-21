# Introduction

Meshploy is an open-source, self-hosted application platform that connects your servers over a private WireGuard mesh. It brings application deployment, builds, routing, databases, and operations into one platform that you can use through the console, CLI, API, or an AI agent.

You bring the machines and choose where work runs. A cloud VM, a supported machine in your office, and a server on your own hardware can participate in the same platform, with different roles.

## Your machines, working together

Imagine an application whose source lives in Git. You want to build it on your own hardware, run it on a cloud server, and reach it through your domain. You also have an existing service on another machine that you want to make accessible without redeploying it.

Meshploy connects these pieces through a shared private network:

| Part | Its role |
| --- | --- |
| Git source | Supplies the application source for a build. |
| Build node | Builds a container image and publishes it to the registry. |
| Workload node | Runs the application as a Kubernetes workload. |
| Gateway | Receives public traffic and routes it to the configured target. |
| Mesh-only node | Connects an existing machine without scheduling Kubernetes workloads on it. |

These roles do not require a separate machine each. Start small, then add supported nodes and separate responsibilities as your needs grow.

### Build here. Run there.

Connect a Git provider, configure a service, and choose how to build it. Meshploy supports Dockerfile builds, Nixpacks, Railpack, and deployment from an existing container image.

Source builds run as Kubernetes Jobs on eligible build nodes. A service can pin its builds to a specific builder, while its application runs on workload nodes. The built-in registry provides a place to store the resulting images for deployment.

Your own machine can participate when it meets the supported Linux node requirements and is configured for builds. Running the CLI on a laptop alone does not make that laptop a build node; the configured builder must be online and reachable when builds run.

### Reach services you already run

You can connect a machine as a mesh-only node and route to a port on it. That makes Meshploy useful for applications you already operate outside its deployment system, as well as new applications you deploy through it.

A route determines how traffic reaches a target. Publishing a route and running a service are separate operations: connecting a machine to the mesh does not by itself publish its applications to the internet.

### Grow across machines

Meshploy uses K3s to schedule application workloads across connected workers. You can add capacity and configure service replicas as your application grows.

Replicas still need an application designed to run in multiple instances. Persistent storage, database availability, and gateway recovery require their own planning; adding workers alone does not make the entire platform highly available.

## Choose how you operate

The interfaces work with the same platform resources, although their operation coverage differs.

| Interface | Use it for |
| --- | --- |
| Console | Configure resources, inspect deployments and logs, and manage integrations visually. |
| CLI | Work from your terminal and automate supported operations in scripts. |
| REST API | Connect your own tools and workflows to Meshploy. |
| MCP | Let a compatible AI agent inspect and operate platform resources. |

For agent access, choose an identity deliberately. Local `meshploy mcp` uses your saved CLI credentials and acts as your user. Remote MCP uses an agent token with the project and resource permissions you grant it. Use a dedicated agent when it needs narrower access than your account.

Meshploy provides the operational interface for an agent to deploy and manage applications on your infrastructure. You still decide its access and review policy; confirmation behavior depends on the agent client and its configuration.

## Organize an application in a project

A project groups the resources that belong together: services, databases, stacks, routes, volumes, variable groups, configuration files, and jobs. Deployments record changes to services, while logs and status help you inspect the result.

Integrations connect the platform to Git providers, container registries, object storage for backups, and notification destinations. Configure the integrations needed by your workflow, then use them with your project resources.

## What you own and operate

Meshploy is Apache-2.0 licensed and runs on infrastructure you provide. You remain responsible for host security, capacity, availability, and recovery.

The private mesh encrypts communication between connected machines. It does not replace host firewalls or application authorization. Follow the installation guide's port and firewall requirements, and publish only the routes you intend to expose.

The gateway is a central dependency for public traffic and platform management. Plan backups and recovery for the platform as well as for application databases and persistent data.

## Get started

1. [Install Meshploy](/self-hosting/) on a supported server and configure DNS.
2. Create the owner account and open the console.
3. [Deploy your first application](/guides/deploy-first-application/) and verify its public route.
4. [Choose node roles and build placement](/guides/node-roles-and-build-placement/) or [connect an existing service](/guides/route-existing-service/).
5. [Configure domains and TLS](/guides/domains-and-tls/), [connect a scoped agent](/guides/scoped-agent-access/), and [plan database recovery](/guides/database-backup-and-restore/) as your workflow requires.

For the mechanisms behind these steps, read [How Meshploy works](/architecture/how-it-works/). For automation, use the [CLI reference](/cli/reference/) or [API reference](/api/reference/).
