---
title: Deploy your first application
description: Deploy an image, verify its workload, and publish an HTTPS route before moving to Git builds.
---

This walkthrough starts with a small HTTP application from an existing container image. It separates deployment from routing so you can verify each step.

## Before you start

- Complete [installation and owner setup](/self-hosting/).
- Have a healthy workload-capable node with capacity to run the application.
- Sign in with permission to create and deploy project resources.
- Check that your installation domain resolves to the gateway.

Use a disposable project for this first walkthrough.

## 1. Create a project and service

Create a project named **first-app** in the console. Inside it, choose **New resource** and create a service using an existing container image.

For a simple HTTP example, use:

| Setting | Value |
| --- | --- |
| Name | hello |
| Image | nginx:stable-alpine |
| Container port | 80 |
| Protocol | HTTP |
| Replicas | 1 |

The image serves the standard Nginx welcome page on port 80. This example uses a mutable tag for convenience; pin an image digest when you need a reproducible production deployment.

Configure port 80 as available for routing. A port kept internal cannot be used as the public HTTP route target. No registry credentials are needed for this public image; a private image needs a registry integration.

## 2. Deploy and inspect the result

Trigger a deployment and open the service's **Deployments** tab. Wait for the workload to become ready, then inspect its logs.

For an image deployment, there is no source build. Meshploy pulls the image and starts the workload on an eligible node.

Check these before configuring a hostname:

- The image was pulled successfully.
- The container stays running.
- The configured port matches the application's listener.
- The node has enough CPU, memory, and disk space.

You can also inspect the service from an authenticated CLI:

```bash
meshploy service list -p first-app
meshploy service deployments hello -p first-app
meshploy service logs hello -p first-app
```

## 3. Publish an HTTPS route

In the project's **Routes**, create an HTTP route under the installation domain. Choose the **Service** target, select **hello**, and use its HTTP port 80 with path `/`.

If the service creation flow already created a route, inspect and use that route instead of creating a duplicate. Make sure it is published.

Open the hostname shown by the route. You should see the Nginx welcome page over HTTPS. Certificate issuance can take time on the first request when using on-demand TLS.

See [domains and TLS](/guides/domains-and-tls/) if DNS or certificate validation fails.

## 4. Move to your own source

Once the image-to-route path works, connect your Git provider under **Integrations → Git sources** and create a service from your repository.

Configure the repository, branch, application root directory, and builder:

- **Dockerfile:** use the path to the Dockerfile in your build context.
- **Nixpacks or Railpack:** use the supported automatic build workflow.
- Set the application port to the actual listener inside the container.
- Supply build-time values separately from runtime environment variables.

For an initial Dockerfile-based example, a repository can contain:

```dockerfile
FROM nginx:stable-alpine
COPY index.html /usr/share/nginx/html/index.html
```

```html
<!doctype html>
<html lang="en">
  <title>Hello from Meshploy</title>
  <h1>Built from Git. Running on my infrastructure.</h1>
</html>
```

Deploy the service. Inspect the build logs, then the rollout and runtime logs. Source builds require an eligible builder; follow [node roles and placement](/guides/node-roles-and-build-placement/) to build on different hardware from your application.

## If something fails

| Symptom | Check |
| --- | --- |
| Image pull fails | Image name/tag, private registry credentials, node registry connectivity. |
| Build does not start | Builder availability, placement selection, and requested resources. |
| Container exits repeatedly | Runtime logs, startup command, required environment variables. |
| Deployment runs but route fails | HTTP port, published route, target readiness, and gateway connectivity. |
| Hostname does not resolve | DNS records and propagation, before investigating the application. |
| HTTPS certificate fails | DNS mode, hostname verification, and gateway reachability on 80/443. |

## Change and recover

Make a small change, deploy again, and inspect the new deployment record. Rollback selects an older deployment image; it still requires the image to exist and the rollout to succeed. It does not undo database migrations or restore persistent data.

When finished, remove the test route and service. Review any persistent resources separately before deleting them.
