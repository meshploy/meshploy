import { test, expect, loginAsDemo, goto } from "./fixtures"

test.describe("Nodes page", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("nodes page loads and lists mock nodes", async ({ page }) => {
    await goto(page, "/nodes")
    await expect(page.getByText("gateway")).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText("worker-1")).toBeVisible({ timeout: 10_000 })
  })

  test("nodes table has two rows", async ({ page }) => {
    await goto(page, "/nodes")
    const rows = page.locator("table tbody tr")
    await expect(rows).toHaveCount(2, { timeout: 10_000 })
  })

  test("node detail page loads", async ({ page }) => {
    await goto(page, "/nodes")
    await page.getByText("gateway").first().click()
    await expect(page.getByText("100.64.0.1")).toBeVisible({ timeout: 10_000 })
  })
})
