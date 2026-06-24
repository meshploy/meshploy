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
