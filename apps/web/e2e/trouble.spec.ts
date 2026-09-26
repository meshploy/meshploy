import { test, expect, loginAsDemo, goto } from "./fixtures"
import { DEMO_ORG_ID, DEMO_PROJECT_ID, DEMO_SVC_API } from "../src/mocks/data"

// A service that keeps dying says so wherever it shows: its page, with the
// place to fix it; the services list; the board; and the workspace overview.
test("an out-of-memory service says so everywhere it shows", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}/services`)
  await page.evaluate(async ([org, project, svc]) => {
    await fetch(`/api/v1/orgs/${org}/projects/${project}/services/${svc}/__trouble`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ kind: "out_of_memory", restarts: 6, memory_limit: "512Mi", last_at: new Date().toISOString() }),
    })
  }, [DEMO_ORG_ID, DEMO_PROJECT_ID, DEMO_SVC_API])

  // Client-side navigation keeps the demo's state.
  const nav = page.getByRole("complementary", { name: "Project navigation" })
  await nav.getByRole("link", { name: /Overview/ }).click()
  await expect(page.getByRole("region", { name: "Environments" }).getByText("out of memory")).toBeVisible({ timeout: 15_000 })

  await nav.getByRole("link", { name: /Services/ }).click()
  await expect(page.getByTestId("trouble-tag").first()).toContainText("out of memory", { timeout: 15_000 })

  await page.getByRole("link", { name: /^api port/ }).click()
  const banner = page.getByTestId("trouble-banner")
  await expect(banner).toContainText("Killed for using too much memory")
  await expect(banner).toContainText("Its memory limit is 512Mi")
  await banner.getByRole("link", { name: "Raise memory limit" }).click()
  await expect(page).toHaveURL(/\/config/)

  await page.getByRole("link", { name: "Overview" }).first().click()
  await expect(page.getByRole("region", { name: /Needs attention/ })).toContainText("api keeps running out of memory", { timeout: 15_000 })
})
