---
id: variables
title: Variable groups
summary: Sets of variables shared between services and jobs, the ones services publish about themselves, and how references between them work.
pages: [variables]
---

## Variable groups {#groups}

A **variable group** is a named set of variables that services and jobs **attach**, so one value is set in one place: an API key, a feature flag, a region. Change it once and every service that attaches it gets the new value on its next deploy. Values marked secret are stored encrypted and hidden in the console.

## Published groups {#published}

Every service **publishes** a group about itself, so others can reach it: its host, port and URL, and for a database how to connect. Its name follows the service's, so a database called Primary DB publishes `PRIMARY_DB_URL`. Meshploy keeps these up to date after every deploy; you attach them, you do not edit them.

A service is never given its own published group: it already knows its own address, and the names could clash with its own settings.

## References {#references}

A value can refer to another variable with `${NAME}`: `DATABASE_URL=${PRIMARY_DB_URL}`. The reference is filled in at every deploy, so the service keeps working when the database's password is reset. A name the service does not have, or references that go round in a loop, are left as written and noted in the deploy's log.

## In environment levels {#levels}

A published group is resolved at every deploy to the nearest copy of its service **at or above** the consumer's level, so staging's web uses staging's database when it has one, and production's otherwise. A level never uses variables from a level below it. See [Projects & environments](/concepts/environments#borrowing).

## Terms {#terms}

- **Published group** {#published -> published}: The variables a service publishes about itself, such as a database's connection URL, kept current by Meshploy.
- **Reference** {#reference -> references}: ${NAME} in a value, filled in with that variable at every deploy.
- **Secret** {#secret -> groups}: Stored encrypted and hidden in the console once saved.
