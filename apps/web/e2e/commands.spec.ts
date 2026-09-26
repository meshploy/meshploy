import { test, expect, loginAsDemo, goto } from "./fixtures"
import { DEMO_PROJECT_ID, DEMO_SVC_API, DEMO_SVC_WEB } from "../src/mocks/data"

// Install, build and start commands: empty leaves them to the builder and the
// image, as before; set, they are kept and shown again.
test.describe("Commands", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("a service's commands are saved from its configuration", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/services/${DEMO_SVC_API}/config`)
    const install = page.getByRole("textbox", { name: "Install command" })
    await expect(install).toHaveAttribute("placeholder", "Worked out by Railpack", { timeout: 10_000 })
    await expect(install).toHaveValue("")
    await install.fill("pnpm install --frozen-lockfile")
    await page.getByRole("textbox", { name: "Build command" }).fill("pnpm build")
    await page.getByTestId("start-command").fill("node dist/server.js")
    await page.getByRole("region", { name: "Unsaved configuration changes" }).getByRole("button", { name: "Save" }).click()
    await expect(page.getByRole("region", { name: "Unsaved configuration changes" })).toHaveCount(0)

    // Read back after leaving the page.
    const tabs = page.locator("nav.detail-tabs")
    await tabs.getByRole("link", { name: "Overview", exact: true }).click()
    await tabs.getByRole("link", { name: "Configuration" }).click()
    await expect(page.getByRole("textbox", { name: "Install command" })).toHaveValue("pnpm install --frozen-lockfile")
    await expect(page.getByRole("textbox", { name: "Build command" })).toHaveValue("pnpm build")
    await expect(page.getByTestId("start-command")).toHaveValue("node dist/server.js")
  })

  test("a Dockerfile build says its steps are its own, and a new service offers a start command", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/services/${DEMO_SVC_WEB}/config`)
    await expect(page.getByText("The install and build steps are the Dockerfile's own.")).toBeVisible({ timeout: 10_000 })
    await expect(page.getByRole("textbox", { name: "Install command" })).toHaveCount(0)
    await goto(page, `/projects/${DEMO_PROJECT_ID}/new?type=service`)
    await expect(page.getByTestId("start-command")).toHaveAttribute("placeholder", "The image's own", { timeout: 10_000 })
  })

  test("a new service builds to the built-in registry unless another is chosen", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/new?type=service`)
    await expect(page.getByRole("combobox").filter({ hasText: "Meshploy registry" })).toBeVisible({ timeout: 10_000 })
  })
})

test("a project's build cache can be cleared from its own settings", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}/settings`)
  await page.getByRole("button", { name: "Clear build cache" }).click({ timeout: 10_000 })
  await expect(page.getByText("Cleared. The next build starts fresh.")).toBeVisible()
})
