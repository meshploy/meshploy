import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { test, expect, loginAsDemo, goto } from "./fixtures"
import { DEMO_PROJECT_ID } from "../src/mocks/data"

/** An app folder as it sits on someone's disk: dependencies installed, a
 *  secret in .env, build output its .gitignore names. */
function appFolder(): string {
  const root = join(mkdtempSync(join(tmpdir(), "meshploy-e2e-")), "Todo App")
  const files: Record<string, string> = {
    "package.json": '{"name":"todo","scripts":{"start":"node src/index.js"}}',
    "src/index.js": "require('http').createServer((q, s) => s.end('ok')).listen(process.env.PORT)",
    "node_modules/left-pad/index.js": "module.exports = 1",
    ".env": "SECRET=do-not-send",
    "dist/bundle.js": "built",
    ".gitignore": "dist/\n",
  }
  for (const [name, body] of Object.entries(files)) {
    mkdirSync(join(root, name, ".."), { recursive: true })
    writeFileSync(join(root, name), body)
  }
  return root
}

// A folder from this computer becomes a service: packed in the browser with
// what should not leave it left out, sent, built and deployed; its config
// then says what it was built from and takes a new version.
test("a service is deployed from a folder on this computer", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO_PROJECT_ID}/new?type=service`)
  await page.getByRole("button", { name: "Folder", exact: true }).click({ timeout: 10_000 })

  await page.getByTestId("folder-input").setInputFiles(appFolder())
  await expect(page.getByTestId("folder-name")).toHaveText("Todo App", { timeout: 10_000 })
  const drop = page.getByTestId("folder-drop")
  await expect(drop).toContainText("3 files")
  await expect(page.getByTestId("folder-skipped")).toContainText("node_modules")
  await expect(page.getByTestId("folder-skipped")).toContainText(".env")
  await expect(page.getByPlaceholder("my-api")).toHaveValue("todo-app")

  await page.getByRole("button", { name: /^Create/ }).click()
  await page.waitForURL(/\/services\/[^/]+\/deployments/, { timeout: 15_000 })
  await expect(page.getByText("Built from a folder").first()).toBeVisible({ timeout: 15_000 })

  await page.getByRole("link", { name: "Configuration", exact: true }).click()
  await expect(page.getByTestId("current-upload")).toContainText("Built from Todo App, 3 files", { timeout: 10_000 })
  await expect(page.getByRole("button", { name: "Folder", exact: true })).toHaveClass(/text-primary/)
})
