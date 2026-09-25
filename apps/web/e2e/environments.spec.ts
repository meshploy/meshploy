import { test, expect, loginAsDemo, goto } from "./fixtures"
import { DEMO_ORG_ID, DEMO_PROJECT_ID, DEMO_ROUTE_ID, DEMO_SVC_API, DEMO_SVC_WEB } from "../src/mocks/data"

// A project's environment levels. Each level is a project of its own
// underneath, so the console's job is to keep them presented as one project:
// one entry in the list, and a switcher that moves between levels on the tab
// you are on.
test.describe("Environment levels", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("the projects list shows the project, not its levels", async ({ page }) => {
    await goto(page, "/projects")
    await expect(page.getByText("Demo Project")).toHaveCount(1, { timeout: 10_000 })
    await expect(page.getByText("demo-project-staging")).toHaveCount(0)
  })

  test("switching keeps the tab, and a level says which it is", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/routes`)
    const switcher = page.getByRole("button", { name: "Environment: production" })
    await expect(switcher).toBeVisible({ timeout: 10_000 })
    await expect(page.getByRole("status").filter({ hasText: "You are in" })).toHaveCount(0)

    await switcher.click()
    await page.getByRole("menuitem", { name: /staging/ }).click()

    await expect(page).toHaveURL(/\/projects\/[^/]+\/routes/)
    await expect(page).not.toHaveURL(new RegExp(DEMO_PROJECT_ID))
    await expect(page.getByRole("button", { name: "Environment: staging" })).toBeVisible()
    await expect(page.getByRole("status").filter({ hasText: "You are in staging" })).toBeVisible()
  })

  test("a new level goes where it is placed, and is where you land", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/services`)
    await page.getByRole("button", { name: "Environment: production" }).click({ timeout: 10_000 })
    await page.getByRole("menuitem", { name: "New level" }).click()

    const dialog = page.getByRole("dialog")
    await dialog.getByPlaceholder("staging").fill("qa")
    // Above staging: between it and production.
    await dialog.getByRole("button", { name: "Above", exact: true }).click()
    const chain = dialog.getByRole("list", { name: "The chain, lowest first" })
    await expect(chain).toHaveText(/staging.*qa.*production/)

    await dialog.getByRole("button", { name: "Add level" }).click()
    await expect(page.getByRole("button", { name: "Environment: qa" })).toBeVisible()

    await page.getByRole("button", { name: "Environment: qa" }).click()
    const items = page.getByRole("menuitem")
    await expect(items.nth(0)).toContainText("production")
    await expect(items.nth(1)).toContainText("qa")
    await expect(items.nth(2)).toContainText("staging")
  })

  test("production has nothing above it", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/services`)
    await page.getByRole("button", { name: "Environment: production" }).click({ timeout: 10_000 })
    await page.getByRole("menuitem", { name: "New level" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("combobox", { name: "Level to place it next to" }).click()
    await page.getByRole("option", { name: "production" }).click()
    await expect(dialog.getByRole("button", { name: "Above", exact: true })).toHaveCount(0)
  })
})

// The board: levels as columns, promotion groups as rows, and Promote moving
// the image a level runs to the next level on the group's path.
test.describe("Environment board", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
    await goto(page, `/projects/${DEMO_PROJECT_ID}`)
    await expect(page.getByRole("region", { name: "Environments" })).toBeVisible({ timeout: 10_000 })
  })

  test("promotes what staging runs to production, and then there is nothing to promote", async ({ page }) => {
    const board = page.getByRole("region", { name: "Environments" })
    await expect(board.getByText("sha-4a1b9c2")).toHaveCount(1)
    const promote = board.getByRole("button", { name: /Promote.*production/ })
    await expect(promote).toBeEnabled()
    await promote.click()

    const dialog = page.getByRole("dialog")
    await expect(dialog).toContainText("Promote app to production?")
    await expect(dialog).toContainText("sha-4a1b9c2")
    await dialog.getByRole("button", { name: "Promote to production" }).click()

    await expect(board.getByText("sha-4a1b9c2")).toHaveCount(2)
    await expect(board.getByRole("button", { name: /Nothing newer for.*production/ })).toBeDisabled()
  })

  test("each database a level uses from above says whose, and what that means", async ({ page }) => {
    const board = page.getByRole("region", { name: "Environments" })
    await expect(board.getByText(/postgres: uses production's\. Writes here change production's data\./)).toBeVisible()
  })

  test("a level can be given its own copy of a database, cloned when there is a backup", async ({ page }) => {
    const board = page.getByRole("region", { name: "Environments" })
    const row = board.locator("div").filter({ hasText: /^postgres: uses production's/ }).first()
    await row.getByRole("button", { name: "Own copy" }).click()

    const dialog = page.getByRole("dialog")
    await expect(dialog).toContainText("Give staging its own postgres")
    await dialog.getByRole("radio", { name: /Cloned from production/ }).check()
    await expect(dialog).toContainText("A clone copies real users' data into staging")
    await dialog.getByRole("button", { name: "Create and clone" }).click()

    await expect(board.getByText(/postgres: uses production's/)).toHaveCount(0)
  })

  test("a database with no backup cannot be cloned, and says why", async ({ page }) => {
    const board = page.getByRole("region", { name: "Environments" })
    const row = board.locator("div").filter({ hasText: /^mysql: uses production's/ }).first()
    await row.getByRole("button", { name: "Own copy" }).click()
    const dialog = page.getByRole("dialog")
    await expect(dialog.getByRole("radio", { name: /Cloned from production/ })).toBeDisabled()
    await expect(dialog).toContainText("has no backup yet")
  })

  test("a service a level does not have says whose copy it uses", async ({ page }) => {
    await expect(page.getByRole("region", { name: "Environments" }).getByText("api: uses production's")).toBeVisible()
  })

  test("so does the level itself, on every tab", async ({ page }) => {
    await page.getByRole("button", { name: "Environment: production" }).click()
    await page.getByRole("menuitem", { name: /staging/ }).click()
    await expect(page.getByRole("status").filter({ hasText: "You are in staging" })).toContainText(
      "staging has no database of its own, so it uses production's: writes here change production's data."
    )
  })

  test("a new group enters at the lowest level it passes through", async ({ page }) => {
    await page.getByRole("button", { name: "Group", exact: true }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByPlaceholder("frontend").fill("backend")
    await dialog.getByRole("checkbox", { name: "api" }).check()
    await expect(dialog).toContainText("Builds in staging, then production")
    await dialog.getByRole("button", { name: "Create group" }).click()

    const board = page.getByRole("region", { name: "Environments" })
    await expect(board.getByText("backend", { exact: true })).toBeVisible()
    // Copied into staging, not built there yet.
    await expect(board.getByRole("button", { name: /Nothing built for.*production/ })).toBeDisabled()
  })
})

// A route in a level carries the level's name, so only production has the
// real hostnames. The form says so before the route exists.
test("a route created in a level shows, and gets, the level's name", async ({ page }) => {
  await loginAsDemo(page)
  // The demo's seeded staging level.
  await goto(page, "/projects/00000000-0000-0000-0000-000000000041/new?type=route")
  await expect(page.getByText("Where is this route exposed?")).toBeVisible({ timeout: 10_000 })
  await page.getByPlaceholder("api or *.my-app").fill("shop")
  await expect(page.getByText(/^-staging\./).first()).toBeVisible()
  await expect(page.getByText(/shop-staging\./).first()).toBeVisible()
})

// Editing what exists: level names, groups, and moving single services.
test.describe("Changing levels and groups", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
    await goto(page, `/projects/${DEMO_PROJECT_ID}`)
    await expect(page.getByRole("region", { name: "Environments" })).toBeVisible({ timeout: 10_000 })
  })

  test("renaming a level says what changes, and what cannot", async ({ page }) => {
    await page.getByRole("button", { name: "Environment: production" }).click()
    await page.getByRole("menuitem", { name: /staging/ }).click()
    await page.getByRole("button", { name: "Environment: staging" }).click()
    await page.getByRole("menuitem", { name: "Rename staging" }).click()

    const dialog = page.getByRole("dialog")
    await expect(dialog).toContainText("links to the old names stop working")
    await expect(dialog).toContainText("demo-project-staging, cannot be renamed")
    await dialog.getByRole("textbox", { name: "New name" }).fill("qa")
    await dialog.getByRole("button", { name: "Rename" }).click()
    await expect(page.getByRole("button", { name: "Environment: qa" })).toBeVisible()
  })

  test("a service copied down is a group of its own, until it joins one", async ({ page }) => {
    const board = page.getByRole("region", { name: "Environments" })
    await board.getByRole("button", { name: "More for api" }).click()
    await page.getByRole("menuitem", { name: "staging" }).click()
    await expect(board.getByText("api · just this service")).toBeVisible()

    // Its copy in staging can join the app group, which absorbs it.
    await board.getByRole("button", { name: "More for api" }).first().click()
    await page.getByRole("menuitem", { name: "app" }).click()
    await expect(board.getByText("api · just this service")).toHaveCount(0)
  })

  test("a group's services can be added and removed", async ({ page }) => {
    const board = page.getByRole("region", { name: "Environments" })
    await board.getByRole("button", { name: "Edit" }).first().click()
    let dialog = page.getByRole("dialog")
    await dialog.getByRole("checkbox", { name: "api" }).check()
    await dialog.getByRole("button", { name: "Save" }).click()
    await expect(dialog).toHaveCount(0)

    await board.getByRole("button", { name: "Edit" }).first().click()
    dialog = page.getByRole("dialog")
    await expect(dialog).toContainText("keeps running in every level")
    const apiRow = dialog.locator("div").filter({ hasText: /^api/ }).last()
    await apiRow.getByRole("button", { name: "Remove" }).click()
    await expect(dialog.getByRole("checkbox", { name: "api" })).toHaveCount(0)
  })

  test("bringing a service down runs production's image below", async ({ page }) => {
    const board = page.getByRole("region", { name: "Environments" })
    await expect(board.getByText("web:latest")).toHaveCount(1)
    // Staging's web card comes first, production's second.
    await board.getByRole("button", { name: "More for web" }).nth(1).click()
    await page.getByRole("menuitem", { name: "staging" }).click()
    await expect(board.getByText("web:latest")).toHaveCount(2)
  })
})

test("a level's copy can be removed, and the level then uses the one above", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}`)
  const board = page.getByRole("region", { name: "Environments" })
  await expect(board).toBeVisible({ timeout: 10_000 })

  // staging's web is the first web card; production's is the second.
  await board.getByRole("button", { name: "More for web" }).first().click()
  await page.getByRole("menuitem", { name: "Remove from staging" }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog).toContainText("production's web is untouched, and staging uses it from then on")
  await expect(dialog).toContainText("also leaves the app group")
  await dialog.getByRole("button", { name: "Remove from staging" }).click()

  await expect(board.getByText("web:sha-4a1b9c2")).toHaveCount(0)
  await expect(board.getByText("web:latest")).toHaveCount(1)
})

test("a service can leave its group from its card, and keeps running", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}`)
  const board = page.getByRole("region", { name: "Environments" })
  await expect(board).toBeVisible({ timeout: 10_000 })

  await board.getByRole("button", { name: "More for web" }).first().click()
  await page.getByRole("menuitem", { name: "Remove from app" }).click()

  // The group went with its only service; web still runs in both levels.
  await expect(board.getByRole("button", { name: /Promote.*production/ })).toHaveCount(0)
  await expect(board.getByText("web:sha-4a1b9c2")).toHaveCount(1)
  await expect(board.getByText("web:latest")).toHaveCount(1)
})

// A service copied down brings its routes under the level's name, waiting
// for its first deploy there.
test("a copied service's routes come with it, named for the level", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}`)
  const board = page.getByRole("region", { name: "Environments" })
  await expect(board).toBeVisible({ timeout: 10_000 })
  await board.getByRole("button", { name: "More for api" }).click()
  await page.getByRole("menuitem", { name: "staging" }).click()
  await expect(board.getByText("api · just this service")).toBeVisible()

  await page.getByRole("button", { name: "Environment: production" }).click()
  await page.getByRole("menuitem", { name: /staging/ }).click()
  await page.getByRole("link", { name: /^Routes/ }).first().click()
  await expect(page.getByText("api-staging.demo.meshploy.app")).toBeVisible()
  await expect(page.getByText("waits for first deploy").first()).toBeVisible()
})

// A group moves what is newer and leaves the rest: web was rebuilt in
// staging, api was never built there, and the confirmation says both.
test("promote moves only what is newer, and says why the rest stays", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}`)
  const board = page.getByRole("region", { name: "Environments" })
  await expect(board).toBeVisible({ timeout: 10_000 })

  // api joins app, and is copied into staging unbuilt.
  await board.getByRole("button", { name: "Edit" }).first().click()
  const edit = page.getByRole("dialog")
  await edit.getByRole("checkbox", { name: "api" }).check()
  await edit.getByRole("button", { name: "Save" }).click()
  await expect(edit).toHaveCount(0)

  await board.getByRole("button", { name: /Promote.*production/ }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog).toContainText("Moves to production")
  await expect(dialog).toContainText("web")
  await expect(dialog).toContainText("never built in staging")
  await dialog.getByRole("button", { name: "Promote to production" }).click()
  await expect(board.getByText("web:sha-4a1b9c2")).toHaveCount(2)
  await expect(board.getByText("api:latest")).toHaveCount(1)
  // Production's web now says it came up from staging, built from develop.
  await expect(board.getByTestId("origin-line").filter({ hasText: "Promoted from staging" }).filter({ hasText: "4a1b9c2" })).toBeVisible()
})

test("a card and the service page say where the running image came from", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}`)
  const board = page.getByRole("region", { name: "Environments" })
  const lines = board.getByTestId("origin-line")
  await expect(lines.filter({ hasText: "Built from develop" }).filter({ hasText: "4a1b9c2" }).filter({ hasText: "Add the dark checkout page" })).toBeVisible({ timeout: 10_000 })
  await expect(lines.filter({ hasText: "Promoted from staging" }).filter({ hasText: "develop" }).filter({ hasText: "9e3f210" }).filter({ hasText: "Cache product images" })).toBeVisible()

  await goto(page, `/projects/${DEMO_PROJECT_ID}/services/${DEMO_SVC_WEB}/overview`)
  const strip = page.getByTestId("origin-strip")
  await expect(strip).toContainText("Promoted from staging", { timeout: 10_000 })
  await expect(strip).toContainText("develop")
  await expect(strip).toContainText("9e3f210")
  await expect(strip).toContainText("Cache product images")
})

test("a card opens what its level serves, and a route waiting for a deploy is not a link", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}`)
  const board = page.getByRole("region", { name: "Environments" })
  await expect(board.getByRole("link", { name: "Open app-staging.demo.example.com" })).toHaveAttribute("href", "https://app-staging.demo.example.com", { timeout: 10_000 })
  await expect(board.getByRole("link", { name: "Open app.demo.example.com" })).toBeVisible()
})

test("a service page opens its address, or offers to add one", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}/services/${DEMO_SVC_WEB}/overview`)
  await expect(page.getByRole("link", { name: "Open", exact: true })).toHaveAttribute("href", "https://app.demo.example.com", { timeout: 10_000 })

  // Without its route, api's header offers one, aimed at api.
  await goto(page, `/projects/${DEMO_PROJECT_ID}/services`)
  await page.evaluate(async ([org, project, route]) => {
    await fetch(`/api/v1/orgs/${org}/projects/${project}/routes/${route}`, { method: "DELETE" })
  }, [DEMO_ORG_ID, DEMO_PROJECT_ID, DEMO_ROUTE_ID])
  await page.getByRole("link", { name: /^api port/ }).click()
  await page.getByRole("link", { name: "Add route" }).first().click()
  await expect(page).toHaveURL(new RegExp(`type=route.*service=${DEMO_SVC_API}`))
})

test("on a phone the board shows one level at a time, chosen from tabs", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 })
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}`)
  const board = page.getByRole("region", { name: "Environments" })
  await expect(board.getByRole("tab", { name: /staging/ })).toHaveAttribute("aria-selected", "true", { timeout: 10_000 })
  await expect(board.getByText("app-staging.demo.example.com")).toBeVisible()
  await expect(board.getByText("app.demo.example.com", { exact: true })).toHaveCount(0)

  await board.getByRole("tab", { name: /production/ }).click()
  await expect(board.getByText("app.demo.example.com", { exact: true })).toBeVisible()
  await expect(board.getByText("app-staging.demo.example.com")).toHaveCount(0)
})

test("a level can be deleted by name, and its group then has nothing to promote between", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}`)
  await page.getByRole("button", { name: "Environment: production" }).click({ timeout: 10_000 })
  await page.getByRole("menuitem", { name: /staging/ }).click()
  await page.getByRole("button", { name: "Environment: staging" }).click()
  await page.getByRole("menuitem", { name: "Delete staging" }).click()

  const dialog = page.getByRole("dialog")
  await expect(dialog).toContainText("demo-project-staging")
  const confirm = dialog.getByRole("button", { name: "Delete staging" })
  await expect(confirm).toBeDisabled()
  await dialog.getByLabel("Level name").fill("staging")
  await confirm.click()

  await expect(page).toHaveURL(new RegExp(DEMO_PROJECT_ID))
  await page.getByRole("button", { name: "Environment: production" }).click()
  await expect(page.getByRole("menuitem", { name: /staging/ })).toHaveCount(0)
  await expect(page.getByRole("menuitem", { name: /Delete/ })).toHaveCount(0)
})

test("on a phone the level switcher sits beside the section picker", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 })
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}/services`)
  await page.getByRole("button", { name: "Environment: production" }).click({ timeout: 10_000 })
  await page.getByRole("menuitem", { name: /staging/ }).click()
  await expect(page).toHaveURL(/\/services/)
  await expect(page.getByRole("button", { name: "Environment: staging" })).toBeVisible()
})

test("the overview's resource cards break their counts down", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}`)
  const resources = page.getByRole("region", { name: "Resources" })
  await expect(resources.getByTestId("stats-services")).toContainText("2 running", { timeout: 10_000 })
  await expect(resources.getByTestId("stats-routes")).toContainText("HTTPS")
  await expect(resources.getByTestId("stats-routes")).toContainText("TCP")
})
