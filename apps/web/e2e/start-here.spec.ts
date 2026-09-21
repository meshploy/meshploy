import { test, expect, loginAsDemo, goto } from "./fixtures"

// The getting-started panel hides itself once a workspace has a service with a
// route, which the demo workspace has. `?start=1` is how it is looked at
// anyway - by whoever is building it, and by anyone answering a question about
// it - so that is what these check.
test.describe("Start here", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("stays out of the way on a workspace that is past it", async ({ page }) => {
    await goto(page, "/")
    await expect(page.getByRole("heading", { name: "Workspace overview" })).toBeVisible({
      timeout: 10_000,
    })
    await expect(page.getByRole("heading", { name: "Start here" })).toHaveCount(0)
  })

  test("renders on request, with its readiness lines and a next step", async ({ page }) => {
    await goto(page, "/?start=1")
    await expect(page.getByRole("heading", { name: "Start here" })).toBeVisible({
      timeout: 10_000,
    })

    // The three things that make a first deployment fail. The demo workspace is
    // healthy, so each reads as the green case.
    await expect(page.getByText("Somewhere to run it")).toBeVisible()
    await expect(page.getByText("Somewhere to build it")).toBeVisible()
    await expect(page.getByText("Your domain points here")).toBeVisible()
    await expect(page.getByText("demo.example.com")).toBeVisible()

    // And a way on, whichever step this workspace is at.
    await expect(page.getByRole("link", { name: /A template/ })).toBeVisible()
    await expect(page.getByRole("link", { name: /first-deployment guide/ })).toBeVisible()
  })
})

// The overview's largest panel used to be the mesh diagram, which says the same
// thing every day and says it again in the node list below. It now answers what
// a dashboard is opened for.
test.describe("Overview", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("leads with recent activity, not the mesh diagram", async ({ page }) => {
    await goto(page, "/")
    await expect(page.getByText("Recent activity")).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText("Mesh topology")).toHaveCount(0)
    // A row links back to the record it is reporting.
    await expect(page.getByText("in Demo Project").first()).toBeVisible()
  })

  // Two things run in Meshploy and both belong here. A cron that failed at 3am
  // is the entry this panel exists for.
  test("carries job runs beside deployments", async ({ page }) => {
    await goto(page, "/")
    const panel = page.locator("div.quiet-surface").filter({ hasText: "Recent activity" }).first()
    await expect(panel).toBeVisible({ timeout: 10_000 })

    await expect(panel.getByText(/Deployed|Deploy failed|Building|Rolling out/).first()).toBeVisible()
    await expect(panel.getByText(/Ran|Run failed|Running|Waiting for its schedule/).first()).toBeVisible()

    // A job run opens the job's runs, not a deployment page.
    const runRow = panel.getByRole("link").filter({ hasText: /Ran|Run failed|Waiting/ }).first()
    await expect(runRow).toHaveAttribute("href", /\/jobs\/.+\/runs$/)
  })

  test("reports what the mesh is actually using", async ({ page }) => {
    await goto(page, "/")
    // The sidebar has a group with the same word, so this is the panel's heading.
    await expect(page.getByRole("heading", { name: "Infrastructure" })).toBeVisible({
      timeout: 10_000,
    })
    await expect(page.getByText(/Memory/).first()).toBeVisible()
    await expect(page.getByText(/Disk/).first()).toBeVisible()
    await expect(page.getByText("38%").first()).toBeVisible()
  })

  // Both lists grow on their own - more deployments, more projects - and a row
  // of panels that changes shape as the workspace fills reads as broken.
  test("holds both panels to one height, and scrolls inside them", async ({ page }) => {
    await goto(page, "/")
    const activity = page.locator("div").filter({ hasText: /^Recent activity/ }).first()
    await expect(activity).toBeVisible({ timeout: 10_000 })

    const heights = await page.evaluate(() => {
      const panel = (matches: (head: string) => boolean) => {
        const el = Array.from(document.querySelectorAll("div.quiet-surface")).find((e) =>
          matches(e.firstElementChild?.textContent ?? "")
        )
        if (!el) return null
        return {
          height: Math.round(el.getBoundingClientRect().height),
          // The body is the second child; it is the part that scrolls.
          scrolls: getComputedStyle(el.children[1]).overflowY,
        }
      }
      return [
        panel((h) => h.startsWith("Recent activity")),
        // "All →" tells this header apart from anything else saying Projects.
        panel((h) => h.startsWith("Projects") && h.includes("All")),
      ].filter((p) => p !== null)
    })

    expect(heights).toHaveLength(2)
    expect(heights[0].height).toBe(heights[1].height)
    expect(heights[0].scrolls).toBe("auto")
    expect(heights[1].scrolls).toBe("auto")
  })

  test("the mesh diagram still has a home on the cluster page", async ({ page }) => {
    await goto(page, "/cluster")
    await expect(page.getByText("gateway").first()).toBeVisible({ timeout: 10_000 })
  })
})
