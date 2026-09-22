import { test, expect, loginAsDemo, goto } from "./fixtures"

// Changing a node's role tells the cluster two separate things - whether build
// jobs may select it, and whether a NoSchedule taint keeps services off - and
// each has a consequence worth naming before it happens.
test.describe("Changing a node's role", () => {
  const openNode = async (page, name: string) => {
    await goto(page, "/nodes")
    await page.getByText(name).first().click()
    await expect(page.getByRole("heading", { name: "Node role" })).toBeVisible({ timeout: 10_000 })
  }

  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("asks nothing when the role is already the one chosen", async ({ page }) => {
    await openNode(page, "worker-1")
    const current = page.getByRole("radio", { checked: true })
    await current.click()
    await expect(page.getByRole("dialog")).toHaveCount(0)
  })

  test("names the transition and what it does, not a generic warning", async ({ page }) => {
    await openNode(page, "worker-1")
    await page.getByRole("radio", { name: /Builds only/ }).click()

    const dialog = page.getByRole("dialog")
    await expect(dialog).toBeVisible()
    // From and to, both named.
    await expect(dialog.getByText("Services only")).toBeVisible()
    await expect(dialog.getByText("Builds only")).toBeVisible()
    // The two things the cluster is actually told.
    await expect(dialog.getByText(/Build jobs can be placed here/)).toBeVisible()
    await expect(dialog.getByText(/NoSchedule taint/)).toBeVisible()
    // And the part people get wrong: a taint does not evict.
    await expect(dialog.getByText(/keeps running and moves only when it is next scheduled/)).toBeVisible()
  })

  test("cancelling leaves the role alone", async ({ page }) => {
    await openNode(page, "worker-1")
    await page.getByRole("radio", { name: /Builds only/ }).click()
    await page.getByRole("button", { name: "Cancel" }).click()

    await expect(page.getByRole("dialog")).toHaveCount(0)
    await expect(page.getByRole("radio", { name: /Services only/ })).toHaveAttribute("aria-checked", "true")
  })

  // The sharpest line in the dialog is about the rest of the cluster, not this
  // machine: taking builds off the last build node breaks every Git deploy.
  //
  // The gateway has a switch rather than the picker, and it makes exactly this
  // change - which is why it goes through the same confirmation. On a
  // single-server install it is the likeliest way to end up with nowhere to
  // build.
  test("warns when this was the only node that can build", async ({ page }) => {
    await goto(page, "/nodes")
    await page.getByText("gateway").first().click()
    await expect(page.getByRole("heading", { name: "Build node" })).toBeVisible({ timeout: 10_000 })

    await page.getByRole("switch").first().click()

    const dialog = page.getByRole("dialog")
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText(/only node that can build/)).toBeVisible()
    await expect(dialog.getByText(/Deploying from Git will fail/)).toBeVisible()
    // And it says what still works, so the warning is usable rather than scary.
    await expect(dialog.getByText(/deploying an existing image still works/)).toBeVisible()
  })

  test("turning the gateway's builds back on asks nothing", async ({ page }) => {
    await goto(page, "/nodes")
    await page.getByText("gateway").first().click()
    await expect(page.getByRole("heading", { name: "Build node" })).toBeVisible({ timeout: 10_000 })

    // Off, through the dialog.
    await page.getByRole("switch").first().click()
    await page.getByRole("button", { name: "Change the role" }).click()
    await expect(page.getByRole("dialog")).toHaveCount(0)

    // And on again: nothing is lost by adding a capability, so nothing is asked.
    await page.getByRole("switch").first().click()
    await expect(page.getByRole("dialog")).toHaveCount(0)
  })
})
