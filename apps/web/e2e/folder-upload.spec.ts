import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { test, expect, loginAsDemo, goto } from "./fixtures"
import { DEMO_PROJECT_ID } from "../src/mocks/data"

/** An app folder as it sits on someone's disk: dependencies installed, a
 *  secret in .env, build output its .gitignore names, scratch files a
 *  subfolder's .gitignore names. */
function appFolder(): string {
  const root = join(mkdtempSync(join(tmpdir(), "meshploy-e2e-")), "Todo App")
  const files: Record<string, string> = {
    "package.json": '{"name":"todo","scripts":{"start":"node src/index.js"}}',
    "src/index.js": "require('http').createServer((q, s) => s.end('ok')).listen(process.env.PORT)",
    "node_modules/left-pad/index.js": "module.exports = 1",
    ".env": "SECRET=do-not-send",
    "dist/bundle.js": "built",
    ".gitignore": "dist/\n",
    "src/.gitignore": "*.tmp\n",
    "src/scratch.tmp": "scratch",
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
  await expect(drop).toContainText("4 files")
  await expect(page.getByTestId("folder-skipped")).toContainText("node_modules")
  await expect(page.getByTestId("folder-skipped")).toContainText(".env")
  await expect(page.getByPlaceholder("my-api")).toHaveValue("todo-app")

  await page.getByRole("button", { name: /^Create/ }).click()
  await page.waitForURL(/\/services\/[^/]+\/deployments/, { timeout: 15_000 })
  await expect(page.getByText("Built from a folder").first()).toBeVisible({ timeout: 15_000 })

  await page.getByRole("link", { name: "Configuration", exact: true }).click()
  await expect(page.getByTestId("current-upload")).toContainText("Built from Todo App, 4 files", { timeout: 10_000 })
  await expect(page.getByRole("button", { name: "Folder", exact: true })).toHaveClass(/text-primary/)
})

// A dropped folder is walked by the browser one entry at a time, so what is
// skipped or ignored is never opened: a Next.js app's .next, or a folder a
// subfolder's .gitignore names, costs nothing however big it is.
test("a dropped folder never opens what it leaves out", async ({ page }) => {
  await loginAsDemo(page)
  const result = await page.evaluate(async () => {
    const tree: Record<string, string> = {
      ".gitignore": "**/.next/\n",
      "package.json": "{}",
      "apps/web/page.js": "x",
      "apps/web/.next/cache/a.sst": "cache",
      "apps/api/.gitignore": ".medusa\n",
      "apps/api/main.ts": "x",
      "apps/api/.medusa/server.js": "built",
      "apps/api/node_modules/a/index.js": "dep",
      "apps/api/.env": "SECRET=1",
    }
    const opened: string[] = []
    const dir = (path: string): FileSystemDirectoryEntry => {
      const name = path.split("/").pop() || "shop"
      const children = new Map<string, FileSystemEntry>()
      for (const [p, body] of Object.entries(tree)) {
        if (path && !p.startsWith(path + "/")) continue
        const rest = path ? p.slice(path.length + 1) : p
        const head = rest.split("/")[0]
        const full = path ? `${path}/${head}` : head
        if (children.has(head)) continue
        children.set(head, rest.includes("/") ? dir(full) : ({
          name: head, isFile: true, isDirectory: false,
          file: (ok: (f: File) => void) => ok(new File([body], head)),
        } as unknown as FileSystemEntry))
      }
      return {
        name, isFile: false, isDirectory: true,
        createReader: () => {
          opened.push(path || ".")
          let done = false
          return { readEntries: (ok: (e: FileSystemEntry[]) => void) => { ok(done ? [] : [...children.values()]); done = true } }
        },
      } as unknown as FileSystemDirectoryEntry
    }
    const root = dir("")
    const items = [{ kind: "file", webkitGetAsEntry: () => root }] as unknown as DataTransferItemList
    const { filesFromDrop } = await import("/src/lib/pack-folder.ts" as string)
    const got = await filesFromDrop(items)
    return { files: got.files.map((f: { path: string }) => f.path).sort(), skipped: got.skipped.sort(), opened: opened.sort() }
  })
  expect(result.files).toEqual([".gitignore", "apps/api/.gitignore", "apps/api/main.ts", "apps/web/page.js", "package.json"])
  expect(result.skipped).toEqual(["apps/api/.env", "apps/api/node_modules"])
  expect(result.opened).toEqual([".", "apps", "apps/api", "apps/web"])
})
