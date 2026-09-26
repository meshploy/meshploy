import { test, expect, loginAsDemo, goto } from "./fixtures"
import { DEMO_ORG_ID, DEMO_PROJECT_ID, DEMO_SVC_API } from "../src/mocks/data"

// A build that found no start command and a FastAPI app loading torch leaves
// advice on the overview; following it opens the service with each fix, and
// taking one saves it and offers the redeploy that applies it.
test("advice from a build leads from the overview to its fix", async ({ page }) => {
  test.setTimeout(60_000)
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}/services`)
  await page.evaluate(async ([org, project, svc]) => {
    await fetch(`/api/v1/orgs/${org}/projects/${project}/services/${svc}/__stack`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ languages: ["python"], framework: "fastapi", entry: "app.main:app", heavy: ["torch"], no_start_command: true, dockerfile: "Dockerfile", dockerfile_cmd: true }),
    })
  }, [DEMO_ORG_ID, DEMO_PROJECT_ID, DEMO_SVC_API])

  // Client-side navigation keeps the demo's state.
  await page.getByRole("link", { name: "Overview" }).first().click()
  const attention = page.getByRole("region", { name: /Needs attention/ })
  await expect(attention).toContainText("api: no start command and likely to need more memory", { timeout: 15_000 })
  await attention.getByRole("link", { name: /api: no start command/ }).click()

  const panel = page.getByTestId("hints-panel")
  await expect(panel.getByTestId("hint-start_command")).toContainText("It looks like a FastAPI app in app/main.py")
  await expect(panel.getByRole("button", { name: "Build with its Dockerfile" })).toBeVisible()

  // The start command opens prefilled, to be checked before it is saved.
  await panel.getByRole("button", { name: "Set start command" }).click()
  await expect(panel.getByRole("textbox", { name: "Start command" })).toHaveValue("uvicorn app.main:app --host 0.0.0.0 --port $PORT")
  await panel.getByRole("button", { name: "Save" }).click()
  await expect(panel.getByTestId("hint-start_command")).toHaveCount(0, { timeout: 15_000 })
  await expect(panel.getByRole("status")).toContainText("Saved")
  await expect(panel.getByRole("button", { name: "Redeploy now" })).toBeVisible()

  // Memory: one click.
  await panel.getByRole("button", { name: "Set limit to 2 GiB" }).click()
  await expect(panel.getByTestId("hint-memory")).toHaveCount(0, { timeout: 15_000 })
})

// Dismissing sets the advice aside.
test("a hint can be dismissed", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}/services`)
  await page.evaluate(async ([org, project, svc]) => {
    await fetch(`/api/v1/orgs/${org}/projects/${project}/services/${svc}/__stack`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ heavy: ["puppeteer"] }),
    })
  }, [DEMO_ORG_ID, DEMO_PROJECT_ID, DEMO_SVC_API])
  await page.getByRole("link", { name: /^api port/ }).click()
  const hint = page.getByTestId("hint-memory")
  await expect(hint).toContainText("It depends on puppeteer, which usually needs 1 GiB or more; its memory limit is 512 MiB.", { timeout: 15_000 })
  await hint.getByRole("button", { name: /Dismiss/ }).click()
  await expect(hint).toHaveCount(0, { timeout: 15_000 })
})
