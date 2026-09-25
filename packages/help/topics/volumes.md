---
id: volumes
title: Volumes
summary: Persistent storage for a service, where it lives, and how it is backed up.
pages: [volumes]
---

## Volumes {#volumes}

A **volume** is storage that outlives a service's containers: uploads, a SQLite file, anything written to disk that must survive a restart or a redeploy. It has a size, and is **mounted** into one service at a path, such as `/data`.

## Where a volume lives {#placement}

A volume is stored on one node's own disk. By default the node is chosen when the service first mounts it; you can also **pin** it to a node. Once it has been created on a node it stays there, and the service that mounts it runs on that node too.

## Backups {#backups}

A volume can be backed up on a schedule to a **storage integration** (S3 or compatible) and kept for as many days as you choose. Databases have their own backups, taken with the database's own tools; see [Databases](/concepts/databases#backups).

## Terms {#terms}

- **Mount path** {#mount-path -> volumes}: Where the volume appears inside the service's containers, such as /data.
- **Pinned** {#pinned -> placement}: Stored on a node you chose. A volume stays on the node it was created on, and its service runs there.
