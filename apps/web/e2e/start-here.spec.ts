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

