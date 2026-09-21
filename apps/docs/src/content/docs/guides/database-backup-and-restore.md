---
title: Back up and restore databases
description: Configure object storage, verify database backups, and understand the supported restore paths.
---

A database backup is useful only when you can restore and verify it. This guide covers application database backups; the platform database backup in Settings is a separate operation.

## Check engine support

The current backup executor supports:

| Engine | Backup | Built-in restore |
| --- | --- | --- |
| PostgreSQL | Compressed logical dump | Yes |
| MySQL | Compressed logical dump | Yes |
| MongoDB | Compressed archive | Yes |
| Redis / Dragonfly | Compressed RDB export | No |
| ClickHouse | Not supported by this executor | No |

Do not assume that every provisionable database engine has the same backup and restore capabilities. Redis and Dragonfly recovery needs an engine-specific procedure outside the built-in restore action.

## 1. Connect object storage

Under **Integrations → Object storage**, configure an S3-compatible storage connection with the endpoint and credentials for your provider.

Prepare the bucket and permissions required to list, upload, read, and delete backup objects. Deletion is needed for retention cleanup. Confirm that the Meshploy API host can reach the storage endpoint.

Use a backup destination whose availability does not depend entirely on the database's own machine.

## 2. Configure a database backup

Open the database resource's **Backups** tab and add a backup configuration.

Choose the storage integration and destination, then set the schedule, retention, and enabled state. Check the destination prefix so that you can identify this database's objects later.

The scheduler parses standard cron expressions. For example:

```text
0 2 * * *   every day at 02:00
```

Confirm the scheduler's effective timezone on your installation rather than assuming the browser's local timezone. Verify the first scheduled run against the expected time.

Retention is age-based and deletes eligible old objects. It is cleanup, not a promise to preserve a minimum number of successful restore points. Choose a window long enough for your recovery needs.

## 3. Run and verify a manual backup

Trigger a backup before relying on the schedule.

1. Inspect its reported status and any errors.
2. Open the configuration's restore-point list.
3. Confirm a new object with the expected timestamp and nonzero size exists in storage.
4. Record which database and configuration produced it.

An object existing in the bucket does not prove the dump can be restored. Rehearse recovery using disposable data before relying on this for production.

## 4. Prepare for a restore

The restore action imports into the database associated with that backup configuration. It does not create an isolated replacement database.

Before proceeding:

- Confirm the database, engine, and selected backup object.
- Stop or coordinate application writes.
- Take a fresh backup of current data if it needs to be preserved.
- Plan for conflicts with existing tables, documents, and data.
- Allow time to validate the result before resuming the application.

The current implementation invokes the engine's import tools. It does not automatically reset the destination to an empty database or guarantee an atomic replacement. Restoring over populated data can fail or produce a result that requires manual reconciliation.

## 5. Restore and validate

For a supported engine, choose the intended restore point and confirm the restore action.

The operation runs asynchronously. An accepted request is not a completed restore. Check completion notifications and API logs for the outcome.

Afterward:

1. Connect to the database and check schema and representative records.
2. Verify counts and application-specific invariants.
3. Run an application read/write smoke test.
4. Resume normal writes only after validating the result.

If the restore fails, preserve the error and inspect the destination state before retrying. A failed import may already have changed data.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Upload or listing fails | Endpoint, bucket, credentials, permissions, and connectivity. |
| Backup cannot read the database | Database readiness, configured credentials, and engine support. |
| No scheduled runs | Enabled state, cron expression, scheduler logs, timezone, and previous run status. |
| Old objects disappear | Retention settings and any independent bucket lifecycle policy. |
| Restore fails on existing objects | Destination state and engine-specific import behavior. |
| Restore action accepted but data unchanged | Background completion/error logs; acceptance alone is not success. |

## What this does not recover

Application database dumps do not restore your entire installation, uploaded files on unrelated volumes, or all configuration and encryption keys.

Configure platform database backups separately under **Settings → Backups**, and maintain a recovery plan for persistent volumes and host configuration. Restoring Meshploy's own database is an operator action with a different scope from restoring one application database.
