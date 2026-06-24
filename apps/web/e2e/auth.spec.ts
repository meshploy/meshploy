import { test, expect, loginAsDemo, goto, DEMO_EMAIL } from "./fixtures"

test.describe("Demo auth", () => {
  test("login form is pre-filled with demo credentials", async ({ page }) => {
    await goto(page, "/login")
    await expect(page.locator('input[type="email"]')).toHaveValue(DEMO_EMAIL)
    await expect(page.locator('input[type="password"]')).toHaveValue("demo")
  })

  test("login succeeds and redirects to dashboard", async ({ page }) => {
    await loginAsDemo(page)
    await expect(page).not.toHaveURL(/login/)
  })

  test("authenticated app layout renders after login", async ({ page }) => {
    await loginAsDemo(page)
    // Sidebar nav contains the main app navigation links
    await expect(page.getByRole("navigation").first()).toBeVisible({ timeout: 10_000 })
  })
})
