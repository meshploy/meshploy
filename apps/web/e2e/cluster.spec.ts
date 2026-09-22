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
    // The command appears twice: on its own, and again inside the agent
    // prompt. The first is the one to read.
    const cmd = page.getByText(/curl -fsSL .*\/install\.sh/).first()
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
  test("offers the four roles a node can have, each saying what it means", async ({ page }) => {
    const group = page.getByRole("radiogroup", { name: "What this node is for" })
    await expect(group.getByRole("radio")).toHaveCount(4)

    for (const label of ["Services and builds", "Services only", "Builds only", "Mesh only"]) {
      await expect(group.getByRole("radio", { name: new RegExp(label) })).toBeVisible()
    }
    // The default is the one that runs both, and it is marked as chosen.
    await expect(group.getByRole("radio", { name: /Services and builds/ })).toHaveAttribute(
      "aria-checked",
      "true"
    )

    await group.getByRole("radio", { name: /Mesh only/ }).click()
    await expect(group.getByRole("radio", { name: /Mesh only/ })).toHaveAttribute("aria-checked", "true")
  })

  // The same install, handed to an agent instead of a terminal. Shown beside
  // the command rather than folded away: it is one of the two ways to do this.
  test("shows an agent prompt beside the command", async ({ page }) => {
    const prompt = page.getByText(/Add a machine to my Meshploy cluster/)
    await expect(prompt).toBeVisible({ timeout: 10_000 })

    const text = (await prompt.textContent()) ?? ""
    expect(text).toContain("--token=mprov-")
    // It carries a credential into somebody's agent history, and says so.
    expect(text).toMatch(/single-use/)
    expect(text).toMatch(/[Dd]o not echo/)
  })

  // An agent has none of what a person supplies by being present: which
  // machine, whether it is already spoken for, permission before changing it,
  // and what to do when something goes wrong.
  test("the prompt stops the agent guessing, acting, or retrying", async ({ page }) => {
    const prompt = page.getByText(/Add a machine to my Meshploy cluster/)
    await expect(prompt).toBeVisible({ timeout: 10_000 })
    const text = (await prompt.textContent()) ?? ""

    // It looks in my SSH config before asking me anything.
    expect(text).toContain("~/.ssh/config")
    expect(text).toMatch(/one question, not four/)
    // BatchMode is the difference between a clean refusal and a hung agent on
    // a password-only server.
    expect(text).toContain("BatchMode=yes")
    expect(text).toMatch(/do not use sshpass/)
    // Already a node: stop rather than register it twice.
    expect(text).toContain("/etc/meshploy/node.conf")
    expect(text).toMatch(/wait for a yes/)
    // A spent token cannot be fixed by trying again.
    expect(text).toMatch(/Do not retry/)
    // And it says what the machine is about to become.
    expect(text).toMatch(/Tailscale/)
  })

  // The role is minted into the token, so a token on screen belongs to the
  // role that was chosen when it was made. Changing the choice afterwards must
  // not leave a command that quietly does something else.
  test("drops a token that no longer matches the chosen role", async ({ page }) => {
    await expect(page.getByText(/curl -fsSL .*\/install\.sh/).first()).toBeVisible({ timeout: 10_000 })

    const group = page.getByRole("radiogroup", { name: "What this node is for" })
    await group.getByRole("radio", { name: /Builds only/ }).click()

    await expect(page.getByText(/curl -fsSL .*\/install\.sh/)).toHaveCount(0)
    await expect(page.getByText(/made for the role you had chosen before/)).toBeVisible()

    // And generating again gives a command for the role now chosen.
    await page.getByRole("button", { name: /New token|Generate token/ }).click()
    await expect(page.getByText(/curl -fsSL .*\/install\.sh/).first()).toBeVisible()
    await expect(group.getByRole("radio", { name: /Builds only/ })).toHaveAttribute(
      "aria-checked",
      "true"
    )
  })

  test("says how long the token is good for", async ({ page }) => {
    await expect(page.getByText(/Good for one machine, for an hour/)).toBeVisible({ timeout: 10_000 })
  })
})

// Narrow screens: a panel that cannot shrink pushes the page sideways, and a
// control that only exists on hover does not exist on a touch screen.
test.describe("Add a node, narrow", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 430, height: 900 })
    await loginAsDemo(page)
    await goto(page, "/cluster")
    await expect(page.getByText("Add a node")).toBeVisible({ timeout: 10_000 })
    await page.getByRole("button", { name: /Generate token/ }).click()
  })

  test("does not push the page sideways", async ({ page }) => {
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    )
    expect(overflow).toBe(0)
  })

  test("shows its copy buttons without a hover to offer", async ({ page }) => {
    const opacities = await page.evaluate(() =>
      [...document.querySelectorAll("button.absolute")]
        .filter((b) => b.getBoundingClientRect().width > 0)
        .map((b) => parseFloat(getComputedStyle(b).opacity))
    )
    expect(opacities.length).toBeGreaterThan(0)
    for (const o of opacities) expect(o).toBeGreaterThan(0)
  })
})
