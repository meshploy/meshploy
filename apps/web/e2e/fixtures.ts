import { test as base, Page } from "@playwright/test"

export const DEMO_EMAIL = "demo@meshploy.com"
export const DEMO_PASSWORD = "demo"

async function blockExternalResources(page: Page) {
  await page.route("https://fonts.googleapis.com/**", (route) => route.abort())
  await page.route("https://fonts.gstatic.com/**", (route) => route.abort())
  // config.js is optional runtime config — serve as empty JS so it doesn't block parsing
  await page.route("**/config.js", (route) =>
    route.fulfill({ contentType: "application/javascript", body: "" })
  )
}

export async function goto(page: Page, path: string) {
  await blockExternalResources(page)
  await page.goto(path, { waitUntil: "domcontentloaded" })
  // Wait until the React root has rendered (has children)
  await page.waitForFunction(
    () => (document.getElementById("root")?.children.length ?? 0) > 0,
    { timeout: 30_000 }
  )
}

export async function loginAsDemo(page: Page) {
  await goto(page, "/login")
  // Wait for the login form specifically (email input = React mounted and MSW active)
  await page.waitForSelector('input[type="email"]', { state: "visible", timeout: 20_000 })
  await page.getByRole("button", { name: "Sign in" }).click()
  await page.waitForURL((url) => !url.pathname.includes("/login"), { timeout: 15_000 })
}

export const test = base.extend<{ goto: (path: string) => Promise<void> }>({
  goto: async ({ page }, use) => {
    await use((path: string) => goto(page, path))
  },
})

export { expect } from "@playwright/test"
