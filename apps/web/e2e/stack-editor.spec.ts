import { test, expect, loginAsDemo, goto } from "./fixtures"

// The editor tab says where a stack's file comes from and what Sync does with
// it, in the same panels as every other tab.
test.describe("A stack's compose file", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("a git stack names its source and has not been synced yet", async ({ page }) => {
    await goto(page, "/projects/00000000-0000-0000-0000-000000000003/stacks/00000000-0000-0000-0000-0000000000c2/editor")
    await expect(page.getByRole("heading", { name: "Compose file" })).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText("acme/power-insight")).toBeVisible()
    await expect(page.getByText("The whole repository: files the compose file mounts come from it too")).toBeVisible()
    await expect(page.getByText("Not synced yet: the file below is the one stored with the stack")).toBeVisible()
    // The stack's actions are in its header, beside its name.
    await expect(page.getByRole("button", { name: "Sync" })).toBeVisible()
    await expect(page.getByRole("button", { name: "Apply" })).toBeVisible()
    await expect(page.getByRole("button", { name: "Destroy" })).toBeVisible()
  })

  // Moved from another platform, it arrives with no git access - that
  // platform's connection stays behind - and says so, with where to fix it.
  test("a git stack without git access says so, and the source is edited", async ({ page }) => {
    await goto(page, "/projects/00000000-0000-0000-0000-000000000003/stacks/00000000-0000-0000-0000-0000000000c2/editor")
    await expect(page.getByText("None: Sync and builds cannot read this repository.")).toBeVisible({ timeout: 10_000 })
    await page.getByRole("button", { name: "Edit" }).click()
    const dialog = page.getByRole("dialog")
    await expect(dialog.getByRole("heading", { name: "Edit source" })).toBeVisible()
    await expect(dialog.getByText("owner/repo names no server: choose the integration it is on.")).toBeVisible()
    await expect(dialog.getByRole("button", { name: "Save" })).toBeDisabled()
    await dialog.getByRole("textbox").first().fill("https://gitlab.com/acme/power-insight.git")
    await dialog.getByRole("button", { name: "Save" }).click()
    await expect(dialog).toHaveCount(0)
  })

  test("a pasted stack is applied from its header, on any tab", async ({ page }) => {
    await goto(page, "/projects/00000000-0000-0000-0000-000000000003/stacks/00000000-0000-0000-0000-00000000000c/services")
    await expect(page.getByRole("button", { name: "Sync" })).toHaveCount(0, { timeout: 10_000 })
    await page.getByRole("button", { name: "Apply" }).click()
    await expect(page.getByText("Applied", { exact: true })).toBeVisible({ timeout: 10_000 })

    await page.getByRole("link", { name: "Editor" }).click()
    await expect(page.getByRole("heading", { name: "Source" })).toHaveCount(0)
    await expect(page.getByRole("button", { name: "Save" })).toBeDisabled()
  })

  test("Destroy asks first, with the extra removal opt-in", async ({ page }) => {
    await goto(page, "/projects/00000000-0000-0000-0000-000000000003/stacks/00000000-0000-0000-0000-00000000000c")
    await page.getByRole("button", { name: "Destroy" }).click({ timeout: 10_000 })
    const dialog = page.getByRole("dialog")
    await expect(dialog.getByRole("heading", { name: "Destroy this stack?" })).toBeVisible()
    await expect(dialog.getByRole("switch")).toHaveCount(2)
    await dialog.getByRole("button", { name: "Cancel" }).click()
    await expect(dialog).toHaveCount(0)
  })
})
