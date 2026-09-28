import { test, expect, loginAsDemo, goto } from "./fixtures"

const PROJECT = "00000000-0000-0000-0000-000000000003"
const API = "00000000-0000-0000-0000-000000000006"

// Any deployment whose image is still kept can be rolled back to; one whose
// image was removed says so and offers nothing, and neither does the one that
// runs now.
test("rollback is offered wherever the image is still kept", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${PROJECT}/services/${API}/deployments`)
  await expect(page.getByText("image removed")).toHaveCount(1, { timeout: 10_000 })
  const removed = page.locator(".execution-history-row", { hasText: "image removed" })
  await expect(removed.getByTitle("Roll back to this deployment")).toHaveCount(0)
  const successes = page.locator(".execution-history-row", { hasText: "success" })
  const count = await successes.count()
  // Every success but the one running now and the removed one.
  await expect(page.getByTitle("Roll back to this deployment")).toHaveCount(count - 2)
  await expect(page.getByText("Images kept")).toBeVisible()
})

// A level with services made before keeping the last three was the default
// offers to move them, and the offer goes once taken.
test("services keeping every image are offered their last three", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${PROJECT}/services`)
  const offer = page.getByTestId("keep-images-offer")
  await expect(offer).toBeVisible({ timeout: 10_000 })
  await expect(offer).toContainText("keeps every image")
  await offer.getByRole("button", { name: "Keep the last 3" }).click()
  await expect(offer).toHaveCount(0)
})
