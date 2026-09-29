import { test, expect, loginAsDemo, goto } from "./fixtures"

const DEMO = "00000000-0000-0000-0000-000000000003"
const EXPERIMENTS = "00000000-0000-0000-0000-000000000040"

const box = (page: import("@playwright/test").Page, name: string) =>
  page.locator(".react-flow__node").filter({ has: page.getByText(name, { exact: true }) }).first()

// A database that is down: what waits on it says so rather than failing on
// its own account, and following it keeps its neighbours and fades the rest,
// steadily. Hovering once rebuilt the nodes, which React Flow hides to measure,
// which ended the hover: a flicker without end.
test("the map traces what waits on a failure, and following it is steady", async ({ page }) => {
  await loginAsDemo(page)
  // Drawn from one read, not one per service.
  const perService: string[] = []
  page.on("request", (r) => { if (/\/services\/[^/]+\/variable-groups/.test(r.url())) perService.push(r.url()) })
  await goto(page, `/projects/${EXPERIMENTS}/map`)
  await expect(box(page, "orders-db")).toContainText("Runs out of memory", { timeout: 10_000 })
  expect(perService).toHaveLength(0)
  await expect(box(page, "checkout")).toContainText("Waiting on orders-db")

  await box(page, "orders-db").hover()
  const faded = box(page, "storefront").locator("> div")
  for (let i = 0; i < 8; i++) {
    await expect(faded).toHaveClass(/opacity-30/)
    await expect(box(page, "orders-db")).toBeVisible()
    await page.waitForTimeout(120)
  }
  await expect(box(page, "checkout").locator("> div")).not.toHaveClass(/opacity-30/)
})

// A service's panel offers what the board does: Promote into its level, and
// Roll back to the image it ran before.
test("a service's panel offers promote and roll back", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${DEMO}/map`)
  await box(page, "web").click({ timeout: 10_000 })
  const panel = page.getByTestId("map-panel")
  await expect(panel.getByRole("button", { name: /Promote staging.*production/ })).toBeEnabled()
  await expect(panel.getByRole("button", { name: /Roll back to/ })).toBeVisible()
})

// The workspace map is the overview read another way, and the choice sticks.
test("the overview switches to the workspace map", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, "/")
  await page.getByRole("tab", { name: "map" }).click()
  await expect(box(page, "worker-1")).toBeVisible({ timeout: 10_000 })
  await expect(box(page, "worker-1")).toContainText("mem")
  await goto(page, "/")
  await expect(box(page, "gateway")).toBeVisible({ timeout: 10_000 })
})

// Taking a node out as a what-if says, per service, whether it would keep
// running, move, or stay down and why; nothing is stopped.
test("a node taken out says what would move and what would stay down", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, "/?view=map")
  await box(page, "worker-1").click({ timeout: 10_000 })
  await page.getByRole("button", { name: /What if worker-1 went down/ }).click()
  await expect(page.getByTestId("what-if-banner")).toContainText("What if worker-1 went down")
  const forecast = page.getByTestId("what-if-forecast")
  await expect(forecast).toContainText("stays down: it is pinned to this node")
  await expect(forecast).toContainText("moves to gateway")
  await expect(forecast).toContainText("keeps running on gateway")
  await expect(forecast).toContainText("its volume's data is on this node")
})

// Taking a service out of a project's map makes what depends on it wait, and
// says it is a what-if; putting it back clears it.
test("a service taken out shows what waits on it", async ({ page }) => {
  await loginAsDemo(page)
  await goto(page, `/projects/${EXPERIMENTS}/map`)
  await box(page, "redis").click({ timeout: 10_000 })
  await page.getByRole("button", { name: /What if redis went down/ }).click()
  await expect(page.getByTestId("what-if-banner")).toContainText("What if redis went down")
  await expect(box(page, "redis")).toContainText("Taken out (what if)")
  await page.getByRole("button", { name: "Clear" }).click()
  await expect(page.getByTestId("what-if-banner")).toHaveCount(0)
})
