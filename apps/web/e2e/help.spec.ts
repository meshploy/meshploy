import { test, expect, loginAsDemo, goto } from "./fixtures"
import { DEMO_PROJECT_ID } from "../src/mocks/data"

// Help is one text (packages/help) shown three ways in the console: a page's
// "?" opens its explanation beside it, an ⓘ explains a word and links into
// that explanation, and a first meeting with a feature teaches it.
test.describe("Help", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("a page's ? opens its explanation beside it, and the ? key toggles it", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}`)
    await page.getByRole("button", { name: "How environments work" }).first().click({ timeout: 10_000 })
    const drawer = page.getByTestId("help-drawer")
    await expect(drawer).toContainText("Projects & environments")
    await expect(drawer).toContainText("Only what is newer moves")
    // The page stays usable beside it.
    await expect(page.getByRole("region", { name: "Environments" })).toBeVisible()

    await page.keyboard.press("Escape")
    await expect(drawer).toHaveCount(0)
    await page.keyboard.press("?")
    await expect(page.getByTestId("help-drawer")).toContainText("Projects & environments")
  })

  test("an ⓘ explains a word, and Learn more opens the section behind it", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}`)
    await page.getByRole("button", { name: "What is promotion group?" }).first().click({ timeout: 10_000 })
    const popover = page.getByTestId("term-popover")
    await expect(popover).toContainText("Services that move up the levels together")
    await popover.getByRole("button", { name: /Learn more/ }).click()
    const drawer = page.getByTestId("help-drawer")
    await expect(drawer.locator('[data-section="groups"]')).toBeInViewport()
  })

  test("search finds a section in any topic", async ({ page }) => {
    await goto(page, "/")
    await page.keyboard.press("?")
    const drawer = page.getByTestId("help-drawer")
    await drawer.getByLabel("Search help").fill("hotfix")
    await drawer.getByRole("button", { name: /Hotfixes/ }).click()
    await expect(drawer.locator('[data-section="hotfixes"]')).toBeInViewport()
  })

  test("a project with production alone teaches what levels are", async ({ page }) => {
    await goto(page, "/projects/00000000-0000-0000-0000-000000000040")
    const empty = page.getByTestId("no-levels")
    await expect(empty).toContainText("A project is its own production", { timeout: 10_000 })
    await empty.getByRole("button", { name: "How environments work" }).click()
    await expect(page.getByTestId("help-drawer")).toContainText("Projects & environments")
  })

  test("the top bar's ? opens the topic for the page you are on, and follows the page", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/routes`)
    await page.getByRole("button", { name: "Help: Routes" }).click({ timeout: 10_000 })
    const drawer = page.getByTestId("help-drawer")
    await expect(drawer).toContainText("How a hostname or a port reaches a service")
    await page.getByRole("complementary", { name: "Project navigation" }).getByRole("link", { name: /Variable groups/ }).click()
    await expect(drawer).toContainText("Sets of variables shared between services and jobs")
  })

  test("a form field explains its term", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/new?type=route`)
    await page.getByRole("button", { name: "What is internal zone?" }).click({ timeout: 10_000 })
    await expect(page.getByTestId("term-popover")).toContainText("answered only on your mesh")
  })

  test("a link to another topic opens it in the drawer, at the section", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/databases`)
    await page.getByRole("button", { name: "Help: Databases" }).click({ timeout: 10_000 })
    const drawer = page.getByTestId("help-drawer")
    await drawer.getByRole("button", { name: "Projects & environments" }).click()
    await expect(drawer).toContainText("Levels such as staging and production")
    await expect(drawer.locator('[data-section="databases"]')).toBeInViewport()
  })

  test("every page with a topic opens it from beside its title", async ({ page }) => {
    // Eight full page loads: more than the default limit allows on a busy machine.
    test.setTimeout(90_000)
    const pages: [string, string, string][] = [
      [`/projects/${DEMO_PROJECT_ID}/stacks`, "How stacks work", "A group of services, volumes, routes and files"],
      [`/projects/${DEMO_PROJECT_ID}/jobs`, "How jobs work", "Containers that run to completion"],
      [`/projects/${DEMO_PROJECT_ID}/volumes`, "How volumes work", "Persistent storage for a service"],
      [`/projects/${DEMO_PROJECT_ID}/config-files`, "How config files work", "Files written into a service's containers"],
      ["/discovery", "How discovery works", "already running on your machines outside Meshploy"],
      ["/templates", "How templates work", "One-click apps"],
      ["/integrations/git", "How integrations work", "The outside services Meshploy connects to"],
      ["/users", "How users and access work", "Workspace members, their roles"],
    ]
    for (const [path, button, summary] of pages) {
      await goto(page, path)
      await page.getByRole("button", { name: button }).click({ timeout: 10_000 })
      await expect(page.getByTestId("help-drawer")).toContainText(summary)
      await page.keyboard.press("Escape")
    }
  })

  test("the overview and migration pages explain themselves", async ({ page }) => {
    await goto(page, "/")
    await page.getByRole("button", { name: "What is change failure rate?" }).click({ timeout: 10_000 })
    await expect(page.getByTestId("term-popover")).toContainText("production deploys in the last 14 days that failed")
    await page.keyboard.press("Escape")
    await page.getByRole("button", { name: "What the overview shows" }).click()
    await expect(page.getByTestId("help-drawer")).toContainText("how delivery is measured")
    await page.keyboard.press("Escape")
    await goto(page, "/migration")
    await page.getByRole("button", { name: "How migration works" }).click({ timeout: 10_000 })
    await expect(page.getByTestId("help-drawer")).toContainText("every step reversible until the last")
  })
})
