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

  // With an integration, the repository and branch are picked from what it
  // can reach, as a new service's are; one it does not list says so.
  test("choosing git access turns repository and branch into pickers", async ({ page }) => {
    await goto(page, "/projects/00000000-0000-0000-0000-000000000003/stacks/00000000-0000-0000-0000-0000000000c2/editor")
    await page.getByRole("button", { name: "Edit" }).click({ timeout: 10_000 })
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("combobox").first().click()
    await page.getByRole("option", { name: /Demo GitLab/ }).click()
    await expect(dialog.getByRole("textbox")).toHaveCount(1) // only the compose file path is typed
    await dialog.getByRole("combobox").nth(2).click()
    await expect(page.getByRole("option", { name: "develop" })).toBeVisible()
    await page.getByRole("option", { name: "develop" }).click()
    await dialog.getByRole("button", { name: "Save" }).click()
    await expect(dialog).toHaveCount(0)
  })

  test("a pasted stack is applied from its header, on any tab", async ({ page }) => {
    await goto(page, "/projects/00000000-0000-0000-0000-000000000003/stacks/00000000-0000-0000-0000-00000000000c/services")
    await expect(page.getByRole("button", { name: "Sync" })).toHaveCount(0, { timeout: 10_000 })
    await page.getByRole("button", { name: "Apply" }).click()
    // An apply opens its rollout, where it is followed and kept.
    await expect(page.getByRole("heading", { name: /^Apply / })).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText("Done").first()).toBeVisible()
    await page.getByRole("link", { name: "All rollouts" }).click()
    await expect(page.getByRole("heading", { name: "Rollouts" })).toBeVisible()
    await expect(page.getByRole("link", { name: /Apply/ })).toHaveCount(1)

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

// A service whose latest build failed still runs, and says it is not on the
// latest build, beside its status, wherever it is listed.
test("a service running an earlier build says its latest failed", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, "/projects/00000000-0000-0000-0000-000000000003/services")
  await expect(page.getByTestId("not-latest-tag")).toHaveCount(1, { timeout: 10_000 })
  await page.getByTestId("not-latest-tag").click()
  // The service page says it too, beside its status.
  await expect(page.getByTestId("not-latest-tag")).toBeVisible({ timeout: 10_000 })
  await expect(page).toHaveURL(/\/services\/[0-9a-f-]+/)
})
