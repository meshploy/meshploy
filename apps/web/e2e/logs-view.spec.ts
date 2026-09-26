import { test, expect, loginAsDemo, goto } from "./fixtures"
import { DEMO_PROJECT_ID, DEMO_SVC_WEB } from "../src/mocks/data"

// A deployment's log opens on its latest line, and stays there as lines
// arrive, unless the reader scrolls up.
test("a deployment log opens at its latest line", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}/services/${DEMO_SVC_WEB}/deployments/00000000-0000-0000-0000-000000000044`)
  const last = page.getByText("Build complete: registry/demo-web:4a1b9c2")
  await expect(last).toBeInViewport({ timeout: 15_000 })
  // Ordinary lines carry no badge; the warning is marked in the gutter.
  const warning = page.locator('[data-level="warning"]').first()
  await expect(warning).toContainText("!")
  await expect(page.getByText("info", { exact: true })).toHaveCount(0)
})
