---
id: databases
title: Databases
summary: Managed databases in a project, how services connect to them, and how they are backed up and restored.
pages: [databases]
---

## Databases {#databases}

A **database** is a managed service in a project: Postgres, MySQL, MongoDB, Redis, Dragonfly or ClickHouse. Meshploy runs it with a volume for its data, creates its user and password, and keeps the password encrypted.

## Connecting to one {#connecting}

A database **publishes its connection details** as a variable group, such as `PRIMARY_DB_URL`, host, port, user and password. Attach that group to a service and refer to it, as `DATABASE_URL=${PRIMARY_DB_URL}`, and the service always has the current values.

There are three ways to reach a database:

- **Internal**: from services in the same project, by its name in the cluster. The usual way.
- **Mesh**: from any machine on your mesh, such as your laptop, through a port published on the mesh.
- **Public**: through a TCP route on the gateway, for when something outside the mesh must connect. Keep it to an allowlist.

The **Explorer** runs queries against it from the console, and shows its tables.

## Backups {#backups}

A database is backed up on a schedule to a **storage integration** (S3 or compatible), kept for as many days as you choose. Any backup can be **restored**, over the database it came from.

A database with no backups has nothing to restore from if its volume is lost; the workspace overview lists those under Needs attention.

## In environment levels {#levels}

Databases are never promoted. A level either uses the database of the level above (its writes change that data) or has its own copy, empty or cloned from the latest backup of the one above. See [Projects & environments](/concepts/environments#databases).

## Terms {#terms}

- **Published variables** {#published -> connecting}: The connection details a database publishes as a variable group, for services to attach and refer to.
- **Mesh address** {#mesh -> connecting}: Where machines on your mesh reach the database, through a port published on the mesh.
- **Storage integration** {#storage -> backups}: The S3-compatible bucket backups are written to.
