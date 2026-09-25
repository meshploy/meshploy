---
id: config-files
title: Config files
summary: Files written into a service's containers at a path you choose, kept encrypted and shared between services.
pages: [config-files]
---

## Config files {#files}

A **config file** is a file Meshploy writes into a service's containers at a path you choose, such as `/etc/nginx/conf.d/app.conf` or `/app/config.json`. It sits beside whatever the image already has in that folder, without replacing it. Its content is stored encrypted, so it can hold credentials.

## Attaching {#attaching}

A file is **attached** to one or more services. Editing it re-applies every service it is attached to, so they pick up the new content. A file that is still attached cannot be deleted: detach it from each service first.

A file can be up to 256 KB.

## Terms {#terms}

- **Path** {#path -> files}: Where the file appears inside the service's containers. It sits beside the image's own files in that folder.
- **Attached** {#attached -> attaching}: Mounted into a service. Editing the file re-applies every service it is attached to.
