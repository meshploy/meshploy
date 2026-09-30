import { test, expect, loginAsDemo, goto } from "./fixtures"
import { DEMO_PROJECT_ID, DEMO_STACK_ID } from "../src/mocks/data"

// A stack's rollout settings sit above its rollouts and are saved with the
// page's floating bar: turning a switch over moves nothing on the page, and
// nothing is saved until Save.
test("a stack's rollout settings are saved from the floating bar", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}/stacks/${DEMO_STACK_ID}/rollouts`)
  const builds = page.getByTestId("stack-builds-setting")
  await expect(builds).toBeVisible({ timeout: 10_000 })
  const before = await builds.boundingBox()

  await builds.getByRole("switch", { name: "Limit parallel builds" }).click()
  const after = await builds.boundingBox()
  expect(after?.height).toBe(before?.height)
  await expect(page.getByRole("region", { name: "Unsaved configuration changes" })).toBeVisible()

  await builds.getByRole("button", { name: "More" }).click()
  await expect(builds.getByLabel("Builds at once")).toHaveText("3 builds")
  await page.getByRole("region", { name: "Unsaved configuration changes" }).getByRole("button", { name: "Save" }).click()
  await expect(page.getByRole("region", { name: "Unsaved configuration changes" })).toHaveCount(0)
  await expect(builds.getByLabel("Builds at once")).toHaveText("3 builds")
})
