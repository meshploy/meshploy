import { test, expect, loginAsDemo, goto } from "./fixtures"

// The ports can be taken while groups are still to move: the old edge keeps
// serving them, and Finish waits until they have moved.
test.describe("Taking the edge first", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
    await goto(page, "/migration")
  })

  test("says what changes, then holds Finish until the groups have moved", async ({ page }) => {
    // The demo has a group that can still move, so a plain cutover waits.
    await expect(page.getByRole("button", { name: "Cut over" })).toBeDisabled({ timeout: 10_000 })
    await page.getByRole("button", { name: "Take the edge first" }).click()

    const dialog = page.getByRole("dialog")
    await expect(dialog).toContainText("keeps serving every domain that has not moved")
    await expect(dialog).toContainText("A domain added on the old platform from now on is not reached")
    await dialog.getByRole("button", { name: "Take the edge" }).click()

    await expect(page.getByText("Meshploy's edge holds the ports.")).toBeVisible({ timeout: 10_000 })
    await expect(page.getByRole("button", { name: "Take the edge first" })).toHaveCount(0)
    await expect(page.getByText("Available once every group that can move has")).toBeVisible()
  })
})
