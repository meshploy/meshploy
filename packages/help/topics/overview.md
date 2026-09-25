---
id: overview
title: Workspace overview
summary: What the overview checks for you, what each count and number means, and how delivery is measured.
pages: [workspace-overview]
---

## Needs attention {#attention}

The top of the overview lists what needs someone, most urgent first, each linking to where it is dealt with:

- **Red**, something is down: a service or database that is failing, a node that is offline.
- **Amber**, something that will hurt later: a last deploy or job run that failed, a database with no backups (grouped per project), a domain that is not verified.
- **Blue**, something waiting on a person: a level with newer images than the one above, ready to promote, or a level running a hotfix built there.

Everything here is read from what the platform already knows, so an item goes away by itself once it is dealt with. With nothing to show it reads "Nothing needs attention". Domains are shown to admins only; everyone sees only the projects they were given.

## The counts {#counts}

The cards count what exists across every project you can see, **every environment level included**, and break each count down: services by status, routes by kind. The Services card says how many of them are copies in levels, since the project list counts production only.

## Delivery {#delivery}

Delivery shows how the workspace has shipped over the **last 14 days**. The chart has one day per group: deploys (succeeded and failed) and job runs. The four numbers are measured on **production** only:

- **Deploys a week**: successful deploys to production.
- **Change failure rate**: the share of production deploys that failed. Green below 15%, amber below 30%, red above.
- **Time to recover**: the median time from a failed production deploy to the next successful deploy of that service.
- **Staging to production**: the median time from an image being built in a lower level to its promotion reaching production.

A number with nothing to measure yet reads "no data", never a zero that would look like a result.

## Activity and projects {#activity}

**Recent activity** lists deploys and job runs, newest first, with the level each happened in and where each image came from: built from a branch, or promoted from another level.

The **Projects** list shows each project's levels, lowest first, a badge when a promotion is waiting, and a small bar of its deploys per day over the last 14 days.

## Terms {#terms}

- **Change failure rate** {#change-failure-rate -> delivery}: The share of production deploys in the last 14 days that failed.
- **Time to recover** {#time-to-recover -> delivery}: The median time from a failed production deploy to the next successful one of that service.
- **Staging to production** {#staging-to-production -> delivery}: The median time from an image's build in a lower level to its promotion reaching production.
- **Deploys a week** {#deploys-a-week -> delivery}: Successful deploys to production, averaged over the last 14 days.
