import { test, expect, loginAsDemo, goto } from "./fixtures"

test.describe("Cluster page", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("cluster page loads", async ({ page }) => {
    await goto(page, "/cluster")
    await expect(page.locator("main")).toBeVisible({ timeout: 10_000 })
  })
})

// Adding a node is one command with nothing in it but a token. Everything the
// machine would otherwise be asked - where the mesh is, a key to join it, what
// the node is for - travels with that token.
test.describe("Add a node", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
    await goto(page, "/cluster")
    await expect(page.getByText("Add a node")).toBeVisible({ timeout: 10_000 })
    await page.getByRole("button", { name: /Generate token/ }).click()
  })

  test("gives one command, carrying only the token", async ({ page }) => {
    const cmd = page.getByText(/curl -fsSL .*\/install\.sh/)
    await expect(cmd).toBeVisible({ timeout: 10_000 })

    const text = (await cmd.textContent()) ?? ""
    expect(text).toContain("--token=mprov-")
    // The things a worker used to be asked for must not be in it.
    expect(text).not.toContain("MESHPLOY_TOKEN=")
    expect(text).not.toContain("HEADSCALE")
    expect(text).not.toContain("preauth")
  })

  // The role is chosen when the token is minted, so the machine is asked
  // nothing at all - which is the only way a one-liner can be one line.
  test("offers the four roles a node can have", async ({ page }) => {
    // SegmentedControl renders plain buttons, so scope by the panel rather
    // than by a role it does not have.
    const panel = page.locator("div").filter({ hasText: /^Add a node/ }).first()
    for (const label of ["Both", "Workloads", "Builds", "Mesh only"]) {
      await expect(panel.getByRole("button", { name: label, exact: true })).toBeVisible()
    }
    await panel.getByRole("button", { name: "Mesh only", exact: true }).click()
    await expect(page.getByText(/Joins the mesh but not the cluster/)).toBeVisible()
  })

  // The same install, handed to an agent instead of a terminal. Folded away,
  // because the command above is what most people came for.
  test("hides an agent prompt behind a disclosure", async ({ page }) => {
    await expect(page.getByText(/Add a Meshploy worker to my cluster/)).toHaveCount(0)

    await page.getByRole("button", { name: /hand it to an agent/ }).click()
    const prompt = page.getByText(/Add a Meshploy worker to my cluster/)
    await expect(prompt).toBeVisible()

    const text = (await prompt.textContent()) ?? ""
    expect(text).toContain("--token=mprov-")
    // It carries a credential into somebody's agent history, and says so.
    expect(text).toMatch(/single-use/)
    expect(text).toMatch(/do not echo it back/)
  })
})
