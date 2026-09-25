---
id: environments
title: Projects & environments
summary: Levels such as staging and production, how services move up through them as the same image, and what a level uses from the one above.
pages: [project-overview]
---

## Levels {#levels}

A project is its own **production**. Below it you can add **levels**, such as staging or dev, each one a place to run the project's services before production does.

::diagram level-chain | dev → staging → production

Each level has its own namespace in the cluster, so what runs in staging never touches production: its services, routes, volumes and variables are its own. Levels are ordered, and the order is the way changes travel, from the lowest level up to production.

Add a level from the level switcher under the project's name, placed above or below an existing one. Switching level keeps the tab you are on, so "what does staging have here" is one click.

## What a level holds, and what it uses from above {#borrowing}

A new level starts **empty**. It holds only what you put in it, and anything it does not have it **uses from the nearest level above**.

- A **service** a level does not have is reached in the level above: staging's web talks to production's api until staging has an api of its own.
- **Variables** a service publishes, such as a database's connection details, are resolved at every deploy to the nearest copy at or above the level. Give a level its own database and its services use it on their next deploy.
- A level **never** uses anything from **below** it. Production does not read staging's variables or connect to staging's database. A deploy or promotion that would is refused, and says what the level needs of its own first.

## Promotion groups {#groups}

A **promotion group** is a set of services that move up together, along a **path** of levels that ends at production. Different groups can take different paths: the web and api of an app might go dev → staging → production while a worker goes straight from staging to production.

The lowest level on a group's path is its **entry level**, and it is the only one where the group's services **build**: from git, on push if auto-deploy is on. Every level above receives images instead of building them, so auto-deploy is turned off there when the group is made.

Copying one service down to a level with **Copy to** makes a group of just that service. It can join a named group later, which absorbs it.

## Promote {#promote}

**Promote** takes the images a level runs and deploys them, as they are, to the next level on the group's path. Nothing is rebuilt: production runs the exact image staging tested, with production's own variables.

Only what is **newer** moves. For each service in the group, Promote compares when each level's image was **built** (not when it was last deployed, so a rollback does not make an old image newer), and moves the service only if the level below has a newer image. The rest stay, and the dialog says why:

- **unchanged**: the level above already runs this image.
- **older**: the image here was built before the one above.
- **never built**: nothing has been built in this level yet.
- **not here**: this level does not have the service.
- **needs its own first**: its variables would come from below the level above; the dialog names what that level needs.

A service the level above does not have yet is **created** there by the promotion, with its own variables copied as they are. The dialog marks it as new, so you can review those variables before they reach production.

## Databases {#databases}

Databases are **never promoted**: data does not move up with code. A level either:

- **uses the database from the level above**, which means its writes change that level's data. The board marks this in amber; or
- has **its own copy**, empty or cloned from the latest backup of the one above. Choose **Own copy** on the board.

Once a level has its own, its services use it on their next deploy.

## Routes in a level {#routes}

A route in a level takes the level's name in its hostname, so `app` in staging is `app-staging`, and only production has the real names. A subdomain that already ends like a level's name is refused, so the two can never collide.

When a service is copied into a level, its routes come with it under the level's names, **paused until its first deploy** there succeeds. The board shows them as "after first deploy" until then. Renaming a level renames its hostnames; the old names stop answering.

## Hotfixes {#hotfixes}

**Deploy** on a level that receives promotions still works, and asks first. It offers two choices:

- **Redeploy the current image**, for changed variables, which builds nothing; or
- **Build here anyway**, which builds from git in that level: a **hotfix**.

A hotfix is marked **built here** on the board. It is newer than what the level below built, so a normal promotion will not replace it until the level below builds something newer. The Promote dialog then reminds you to make sure that build includes the fix. To replace the hotfix sooner, **Overwrite** moves the level below's image up even though it is older; the dialog names what is replaced.

## Moving services between levels {#moving}

From a service's menu on the board:

- **Copy to** a lower level, to start working on it there, as a group of its own.
- **Add to group**, so it moves with others.
- **Remove from group**, or **Ungroup**: its copies stay and keep running, but no longer move together. The levels above the entry build on push again, as the entry did.
- **Bring down** a higher level's image to a lower one, to reproduce there exactly what, say, production runs.
- **Remove from staging** deletes that level's copy; the level then uses the one above. A service that started in a level has no copy above it, so there the choice reads **Delete**.

## Where an image came from {#provenance}

Every deployment records where its image came from: **built** from a branch and commit, **promoted** or **brought down** from another level, **rolled back**, **redeployed**, or deployed from an **image**. The board's cards and the top of each service's page show it, with the commit's message, so "what is production running, and from where" is answered at a glance.

## Deleting a level {#deleting}

Deleting a level removes everything in it, from the cluster too: its services, routes, volumes, jobs, data and namespace. Groups that passed through it skip it from then on. A group that built there builds at the next level on its path instead, and a group left with nothing between its entry and production is dissolved. You confirm by typing the level's name.

## Common tasks {#how-to}

- **Test a change in staging before production.** Add a staging level below production. Put the services into a group whose path is staging → production. Deploy in staging, then press Promote.
- **Give staging its own database.** On the board, choose Own copy on the database's amber row, empty or cloned from production's latest backup.
- **Ship a hotfix straight to production.** Deploy on the production service, choose Build here anyway, then make sure the fix reaches staging's branch.
- **See what production runs and where it came from.** Look at the production card on the board, or the top of the service's page.

## Terms {#terms}

- **Level** {#level -> levels}: A place to run a project's services before production, such as staging. Each has its own namespace; production is the project itself.
- **Entry level** {#entry-level -> groups}: The lowest level on a group's path, and the only one where the group's services build. Levels above it receive images by promotion.
- **Promotion group** {#promotion-group -> groups}: Services that move up the levels together, along a path of their own that ends at production.
- **Just this service** {#single-group -> groups}: A group made by copying one service down. It can join a named group later, which absorbs it.
- **Uses the level above** {#borrowed -> borrowing}: This level does not have its own copy, so it uses the nearest level above's. It never uses anything from below.
- **Uses production's database** {#borrowed-database -> databases}: Writes in this level change the level above's data. Choose Own copy to give it its own, empty or cloned from a backup.
- **Built here** {#built-here -> hotfixes}: Built in this level instead of promoted to it: a hotfix. A normal promotion will not replace it until the level below builds something newer.
- **After first deploy** {#after-first-deploy -> routes}: A route copied into this level with the service, paused until the service's first successful deploy here.
- **Promote** {#promote -> promote}: Deploys the images this level runs, as they are, to the next level on the group's path. Only newer images move; the rest stay, with the reason.
