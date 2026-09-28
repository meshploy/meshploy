import { test, expect, loginAsDemo, goto } from "./fixtures"

const PROJECT = "00000000-0000-0000-0000-000000000003"
const API = "00000000-0000-0000-0000-000000000006"

// Resources are stepped through sizes, not typed: + and - move along the
// ladder, and the value between them opens the whole list.
test("a service's resources are stepped, not typed", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${PROJECT}/services/${API}/config`)
  await page.getByRole("button", { name: "Resource limits" }).click()

  const limit = page.getByRole("combobox", { name: "Memory limit", exact: true })
  await expect(limit).toBeVisible({ timeout: 10_000 })
  const before = (await limit.innerText()).replace(/[^\w. ]/g, "").trim()
  const box = limit.locator("xpath=..")
  await box.getByRole("button", { name: "Larger" }).click()
  await expect(limit).not.toContainText(before)
  await box.getByRole("button", { name: "Smaller" }).click()
  await expect(limit).toContainText(before)

  // No box to type a size into.
  const limits = page.locator("#service-resource-limits")
  await expect(limits.getByRole("textbox")).toHaveCount(0)
  await expect(limits.getByRole("spinbutton")).toHaveCount(0)

  // Replicas count up and down.
  const replicas = page.getByLabel("Replicas", { exact: true })
  await expect(replicas).toHaveText("2 replicas")
  await replicas.locator("xpath=..").getByRole("button", { name: "More" }).click()
  await expect(replicas).toHaveText("3 replicas")
})
