import { test, expect, loginAsDemo, goto } from "./fixtures"
import { DEMO_PROJECT_ID } from "../src/mocks/data"

// Choosing a repository looks at it and fills in what fits the app, without
// touching what was typed by hand; another repository replaces the first
// one's suggestions.
test("a new service is filled in from what its repository is", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}/new?type=service`)
  await page.getByRole("button", { name: "Public", exact: true }).click({ timeout: 10_000 })
  await page.getByPlaceholder("https://github.com/owner/repo").fill("https://github.com/demo/api")
  await page.getByPlaceholder("main").fill("main")

  const detected = page.getByTestId("stack-detection")
  await expect(detected).toContainText("Python · FastAPI (app/main.py)", { timeout: 10_000 })
  await expect(detected).toContainText("Filled in: port 8000 · start command · memory limit 2 GiB")
  await expect(page.getByTestId("start-command")).toHaveValue("uvicorn app.main:app --host 0.0.0.0 --port $PORT")
  await expect(page.getByPlaceholder("port")).toHaveValue("8000")

  // A start command typed by hand stays; the rest follows the new repository.
  await page.getByTestId("start-command").fill("python serve.py")
  await page.getByPlaceholder("https://github.com/owner/repo").fill("https://github.com/demo/web")
  await expect(detected).toContainText("Node.js · Dockerfile", { timeout: 10_000 })
  await expect(detected).toContainText("Filled in: Dockerfile builder · port 8080")
  await expect(page.getByText("The install and build steps are the Dockerfile's own.")).toBeVisible()
  await expect(page.getByPlaceholder("port")).toHaveValue("8080")
  await expect(page.getByTestId("start-command")).toHaveValue("python serve.py")
})

// A monorepo: the look lists its apps, and choosing one looks at that folder's
// app and fills the form in from it.
test("a monorepo's app is chosen by its folder", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}/new?type=service`)
  await page.getByRole("button", { name: "Public", exact: true }).click({ timeout: 10_000 })
  await page.getByPlaceholder("https://github.com/owner/repo").fill("https://github.com/demo/shop")
  await page.getByPlaceholder("main").fill("main")

  const detected = page.getByTestId("stack-detection")
  await expect(detected).toContainText("This repository holds 2 apps", { timeout: 10_000 })
  const folder = page.getByTestId("app-folder")
  await expect(folder).toContainText("pnpm workspaces")
  await expect(folder).toContainText("Repository root")

  await folder.getByRole("combobox").click()
  await page.getByRole("option", { name: /apps\/api/ }).click()
  await expect(detected).toContainText("Python · FastAPI (app/main.py)", { timeout: 10_000 })
  await expect(detected).not.toContainText("This repository holds 2 apps")
  await expect(page.getByTestId("start-command")).toHaveValue("uvicorn app.main:app --host 0.0.0.0 --port $PORT")
  await expect(page.getByPlaceholder("port")).toHaveValue("8000")
})

// A service for each of a monorepo's apps, each named after its folder and
// made from what its own folder holds.
test("a monorepo makes a service for each app", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}/new?type=service`)
  await page.getByRole("button", { name: "Public", exact: true }).click({ timeout: 10_000 })
  await page.getByPlaceholder("https://github.com/owner/repo").fill("https://github.com/demo/shop")
  await page.getByPlaceholder("main").fill("main")
  await expect(page.getByTestId("stack-detection")).toContainText("This repository holds 2 apps", { timeout: 10_000 })

  await page.getByRole("button", { name: "Create a service for each" }).click()
  const dialog = page.getByTestId("create-each")
  await expect(dialog.getByLabel("Name for apps/api")).toHaveValue("api")
  // The demo has an api already; a name in use is the one thing that stops it.
  await dialog.getByLabel("Name for apps/api").fill("shop-api")
  await dialog.getByLabel("Name for apps/web").fill("storefront")
  await page.getByRole("button", { name: "Create 2 services" }).click()

  await expect(page).toHaveURL(new RegExp(`/projects/${DEMO_PROJECT_ID}/services$`), { timeout: 15_000 })
  await expect(page.getByText("storefront").first()).toBeVisible({ timeout: 10_000 })
  await expect(page.getByText("shop-api").first()).toBeVisible()
})
