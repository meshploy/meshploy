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
