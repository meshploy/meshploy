---
id: migration
title: Migration
summary: Moving a server's workloads from another platform onto Meshploy a group at a time, with every step reversible until the last.
pages: [migration]
---

## Migration {#migration}

When Meshploy is installed beside another platform on the same server, setup can plan a **migration**: moving that platform's apps and databases onto Meshploy without a big switch-over. Only the workspace owner sees it.

Every step runs on the server itself, through the **host agent** on the gateway. The console queues the request, and the step's progress arrives on the page as it runs. If the host agent is not reporting, nothing runs; the page says how to start it.

## The steps {#steps}

1. **Prepare** builds the Meshploy side of the plan with everything **stopped** and every route **paused**. It touches nothing of the old platform's, and can be run again.
2. **Move** each **group** when you are ready. A group is what must move together so its data never lives in two places: a database with every app that uses it. Moving one stops the old copies, brings the new ones up with their data, and switches the group's domains.
3. **Cut over** hands the web ports to Meshploy: it carries the certificates across, stops the old edge, and starts Meshploy's on 80 and 443. Expect a few seconds of refused connections. Every group that can move must have moved first.
4. **Finish** removes what is left of the old platform.

## Undoing {#rollback}

Until **Finish**, everything can be put back. **Roll back** a single group, or **Put everything back**, which returns every group to the old platform and gives it back the ports. Data written into Meshploy since a group moved stays in Meshploy.

**Finish** cannot be undone. By default it leaves the old platform's volumes on the disk, holding their data; tick **Remove its volumes too** only when you are sure nothing on them is needed.

## Compose apps {#compose}

A compose app becomes a stack. The plan reads its compose file first and says what will happen to each service: built ones move on the image they run now and build from their Dockerfile on the next deploy, one-shot steps run once per deploy, repository files and published addresses come across. A service that reaches into the machine itself, with host networking, privileges, devices, the Docker socket or a folder of the host, asks before the app moves: move it without that, or leave the app on the old platform.

## Groups that cannot move {#stuck}

A group Meshploy cannot move keeps running on the old platform. At cut over it loses its domains, because the ports change hands; the console says which domains before you confirm. Finish leaves it running.

## Terms {#terms}

- **Group** {#group -> steps}: What must move together so its data never lives in two places: a database with every app that uses it.
- **Cut over** {#cutover -> steps}: Hands ports 80 and 443 to Meshploy, with the certificates. Every group that can move must have moved first.
- **Finish** {#finish -> rollback}: Removes what is left of the old platform. The one step that cannot be undone.
