import { test, expect, loginAsDemo, goto } from "./fixtures"

test.describe("Cluster page", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("cluster page loads", async ({ page }) => {
    await goto(page, "/cluster")
    await expect(page.locator("main")).toBeVisible({ timeout: 10_000 })
  })
})
