---
id: jobs
title: Jobs
summary: Containers that run to completion, once or on a schedule, with their own history and logs.
pages: [jobs]
---

## Jobs {#jobs}

A **job** runs a container until it finishes, instead of keeping it running: a database migration, a nightly report, a cleanup. It runs an image with an optional command, and has its own variables and resource limits like a service.

- A **one-off** job runs when you press **Run**.
- A **scheduled** job runs on a cron schedule, such as `0 2 * * *` for 02:00 every night.

## Variables {#variables}

A job attaches variable groups like a service does, so a migration reads the database's published connection details with `DATABASE_URL=${PRIMARY_DB_URL}` instead of a copy of the password. If its variables cannot be read, the run fails and says why, rather than running without them.

## Runs {#runs}

Every run is kept with its status and log, up to the job's **history limit**. For a scheduled job, the **concurrency policy** decides what happens when a run is due while the last is still going:

- **Allow**: both run.
- **Forbid**: the new run is skipped.
- **Replace**: the running one is stopped and the new one starts.

## Terms {#terms}

- **Schedule** {#schedule -> jobs}: When a scheduled job runs, in cron form: 0 2 * * * is 02:00 every night.
- **Concurrency policy** {#concurrency -> runs}: What happens when a run is due while the last is still going: allow both, skip the new one, or replace the old one.
- **History limit** {#history -> runs}: How many finished runs are kept, with their logs.
