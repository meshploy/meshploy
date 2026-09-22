import { test, expect, loginAsDemo, goto } from "./fixtures"
import {
  DEMO_PROJECT_ID,
  DEMO_SVC_API,
} from "../src/mocks/data"

test.describe("Projects", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("projects list shows demo project", async ({ page }) => {
    await goto(page, "/projects")
    await expect(page.getByText("Demo Project")).toBeVisible({ timeout: 10_000 })
  })

  test("project services tab shows application services", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/services`)
    await expect(page.getByText("api", { exact: true }).first()).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText("web", { exact: true }).first()).toBeVisible({ timeout: 10_000 })
  })

  test("project databases tab shows database service", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/databases`)
    await expect(page.getByText("postgres", { exact: true }).first()).toBeVisible({ timeout: 10_000 })
  })

  test("service detail overview loads", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/services/${DEMO_SVC_API}/overview`)
    await expect(page.getByText("api").first()).toBeVisible({ timeout: 10_000 })
  })

  test("project jobs tab shows demo job", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/jobs`)
    await expect(page.getByText("db-migrate")).toBeVisible({ timeout: 10_000 })
  })

  test("project volumes tab shows demo volume", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/volumes`)
    await expect(page.getByText("uploads", { exact: true }).first()).toBeVisible({ timeout: 10_000 })
  })
})

// An internal route on a gateway that manages its own DNS gets a certificate
// from Caddy's own CA, because there is no zone to prove control of. The route
// works either way, so this is a notice and not a block - but it belongs
// before the route is made, not at the first certificate warning.
test.describe("Internal routes on a self-managed-DNS gateway", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
    await goto(page, "/projects/00000000-0000-0000-0000-000000000003/new?type=route")
    await expect(page.getByText("Where is this route exposed?")).toBeVisible({ timeout: 10_000 })
  })

  test("says nothing while the route is public", async ({ page }) => {
    await expect(page.getByText(/self-signed certificate/)).toHaveCount(0)
  })

  test("explains the certificate, and why, once it is internal", async ({ page }) => {
    await page.getByRole("button", { name: "Internal", exact: true }).click()

    const notice = page.getByText(/Internal routes on this server use a self-signed certificate/)
    await expect(notice).toBeVisible()

    const panel = page.locator("div").filter({ hasText: /^Internal routes on this server/ }).first()
    const text = (await panel.textContent()) ?? ""
    // The reason, not just the fact.
    // Named the way install.sh and the docs name it, so it matches the choice
    // the operator actually made.
    expect(text).toContain("NS delegation")
    // Both halves of why, since either alone sounds like a defect.
    expect(text).toMatch(/no\s+DNS zone here to prove control of/)
    expect(text).toMatch(/does not answer from the internet/)
    // And what it does and does not cost.
    expect(text).toMatch(/still encrypted/)
    expect(text).toMatch(/public routes are\s+unaffected/)
  })
})
