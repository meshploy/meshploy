---
id: integrations
title: Integrations
summary: The outside services Meshploy connects to: git providers, container registries, storage for backups, notifications and email.
pages: [integrations]
---

## Integrations {#integrations}

An **integration** is a connection to a service outside Meshploy, made once for the whole workspace and used wherever it is needed. Credentials are stored encrypted.

## Git {#git}

A **git connection** lets services build from private repositories and deploy on push. GitHub connects through the Meshploy GitHub App; GitLab, Gitea and Bitbucket through a token, which Meshploy also uses to add the push webhook to a repository.

## Registries {#registries}

A **registry** is where built images are pushed and pulled from. Every workspace has a **built-in** registry on the gateway; connecting another, such as Docker Hub, GitHub's or a cloud provider's, lets services pull private images from it or build to it.

## Storage {#storage}

A **storage integration** is an S3-compatible bucket, where database, volume and system backups are written.

## Notifications and email {#notifications}

**Notification channels** (Slack, Discord, email, or a signed webhook) are told about deployments, failures and other events you choose. **Email** is the SMTP server Meshploy sends invitations and notifications through.

## Terms {#terms}

- **Built-in registry** {#builtin-registry -> registries}: The registry on the gateway every workspace starts with, where images are built to by default.
- **Storage integration** {#storage -> storage}: An S3-compatible bucket where backups are written.
