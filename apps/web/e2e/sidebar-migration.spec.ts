import { test, expect, loginAsDemo, goto } from "./fixtures"

// The sidebar card says what happens next in a migration, the way the page
// does. A group that cannot move does not hold the cutover up there, so it is
// not counted as work left here either - otherwise the bar waits on a step
// that never comes.
test.describe("The sidebar migration card", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
    await goto(page, "/")
  })

  test("counts only the groups that can still move", async ({ page }) => {
    // The demo: prepared, three groups - one moved, one waiting, one that cannot move.
    const card = page.getByRole("link", { name: /^Migration:/ })
    await expect(card).toHaveAttribute("aria-label", "Migration: Moving groups · 1/2", { timeout: 10_000 })

    const bar = card.getByRole("progressbar")
    // prepare + two movable groups + cutover + finish; prepared and one moved.
    await expect(bar).toHaveAttribute("aria-valuemax", "5")
    await expect(bar).toHaveAttribute("aria-valuenow", "2")
  })

  // One segment per stage, so cut over and finish read as stages of their own.
  test("shows each stage as its own segment, the groups filling theirs", async ({ page }) => {
    const bar = page.getByRole("link", { name: /^Migration:/ }).getByRole("progressbar")
    const stage = (name: string) => bar.locator(`[data-stage="${name}"] > span`)
    await expect(bar.locator("[data-stage]")).toHaveCount(4, { timeout: 10_000 })
    await expect(stage("prepare")).toHaveAttribute("style", /width: 100%/)
    await expect(stage("groups")).toHaveAttribute("style", /width: 50%/)
    await expect(stage("cutover")).toHaveAttribute("style", /width: 0%/)
    await expect(stage("finish")).toHaveAttribute("style", /width: 0%/)
    await expect(bar.locator('[data-stage="groups"]')).toHaveAttribute("data-current", "true")
  })

  // Cut over and finish are the long steps. Without this they sit still until
  // done, and a step running looks like one waiting to be started.
  test("pulses the stage in hand while its step runs", async ({ page }) => {
    const card = page.getByRole("link", { name: /^Migration:/ })
    const groups = card.locator('[data-stage="groups"]')
    await expect(groups).not.toHaveClass(/animate-pulse/, { timeout: 10_000 })

    await card.click()
    await page.getByRole("button", { name: "Move", exact: true }).first().click()

    await expect(card).toHaveAttribute("aria-label", "Migration: Working…")
    await expect(groups).toHaveClass(/animate-pulse/)
    // Only the stage in hand: the others stay still.
    await expect(card.locator('[data-stage="cutover"]')).not.toHaveClass(/animate-pulse/)
  })
})
