import { test, expect, loginAsDemo, goto } from "./fixtures"

// Base domains are managed here; custom domains belong to a route and are only
// listed. A new domain does nothing until it is verified, and under NS
// delegation the order of the DNS steps is the part people get wrong.

// The demo restores its sample workspace on a full page load, on purpose. A
// test that changes something and then looks elsewhere has to navigate inside
// the app, or it checks a freshly reset demo and passes for the wrong reason.
async function toDomains(page) {
  await page.getByRole("navigation").getByRole("link", { name: "Domains", exact: true }).first().click()
  await expect(page.getByRole("heading", { name: "Domains", exact: true })).toBeVisible({ timeout: 10_000 })
}

async function toDomain(page, name: string) {
  await toDomains(page)
  await page.getByRole("link", { name, exact: true }).click()
  await expect(page.getByRole("heading", { name })).toBeVisible({ timeout: 10_000 })
}

async function toNewRoute(page) {
  await page.getByRole("navigation").getByRole("link", { name: "Projects", exact: true }).first().click()
  await page.getByRole("link", { name: /Demo Project/ }).first().click()
  await page.getByRole("link", { name: /New resource/ }).first().click()
  await page.getByRole("button", { name: "Route", exact: true }).first().click()
}

test.describe("Domains", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("lists base domains, primary first, and custom domains read-only", async ({ page }) => {
    await goto(page, "/domains")
    await expect(page.getByRole("heading", { name: "Domains", exact: true })).toBeVisible({ timeout: 10_000 })

    const bases = page.getByRole("heading", { name: "Base domains" }).locator("xpath=ancestor::section[1]")
    const names = bases.locator(".font-mono")
    await expect(names.first()).toHaveText("demo.example.com")
    await expect(bases.getByText("Primary")).toBeVisible()
    await expect(bases.getByText("Not verified")).toBeVisible()

    // The custom domain links to its route rather than offering anything here.
    const custom = page.getByRole("heading", { name: "Custom domains" }).locator("xpath=ancestor::section[1]")
    await expect(custom.getByRole("link", { name: "api.demo.meshploy.app" })).toBeVisible()
    await page.screenshot({ path: "test-results/domains-list.png", fullPage: true })
  })

  test("an unverified domain puts the ownership proof before the NS change", async ({ page }) => {
    await goto(page, "/domains")
    await page.getByRole("link", { name: /shop\.example\.net/ }).click()
    await expect(page.getByRole("heading", { name: "shop.example.net" })).toBeVisible({ timeout: 10_000 })

    // Records render twice - stacked for a phone, a table otherwise - with
    // one hidden, so ask for the one on screen.
    await expect(page.getByText("_meshploy-verify.shop.example.net").locator("visible=true")).toBeVisible()
    await expect(page.getByText(/before changing its NS record/)).toBeVisible()
    await expect(page.getByText("after step 1")).toBeVisible()

    // First check fails as a real one would before DNS propagates, the second passes.
    await page.getByRole("button", { name: "Check now" }).click()
    await expect(page.getByText(/not yet propagated/)).toBeVisible()
    await page.screenshot({ path: "test-results/domain-unverified.png", fullPage: true })
    await page.getByRole("button", { name: "Check now" }).click()
    await expect(page.getByRole("button", { name: "Check now" })).toHaveCount(0)
  })

  test("switching DNS mode names the transition and what is yours to change", async ({ page }) => {
    await goto(page, "/domains")
    await page.getByRole("link", { name: /demo\.example\.com/ }).click()
    await expect(page.getByRole("heading", { name: "demo.example.com" })).toBeVisible({ timeout: 10_000 })

    await page.getByRole("radio", { name: /On-demand TLS/ }).click()
    const dialog = page.getByRole("dialog")
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText("Yours to do")).toBeVisible()
    await expect(dialog.getByText(/replace the NS record/)).toBeVisible()
    await expect(dialog.getByText(/own authority/)).toBeVisible()
    await page.screenshot({ path: "test-results/domain-mode-switch.png", fullPage: true })

    await dialog.getByRole("button", { name: "Cancel" }).click()
    await expect(page.getByRole("radio", { name: /NS delegation/ })).toHaveAttribute("aria-checked", "true")
  })

  test("the primary serves the platform subdomains and cannot be removed", async ({ page }) => {
    await goto(page, "/domains")
    await page.getByRole("link", { name: /demo\.example\.com/ }).click()
    await expect(page.getByText("console.demo.example.com")).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText("Serving").first()).toBeVisible()
    await expect(page.getByRole("heading", { name: "Remove this domain" })).toHaveCount(0)
    await page.screenshot({ path: "test-results/domain-primary.png", fullPage: true })
  })

  test("a domain holding routes lists them, and is retired rather than removed", async ({ page }) => {
    await goto(page, "/domains")
    await page.getByRole("link", { name: /apps\.example\.org/ }).click()
    await expect(page.getByText("blog.apps.example.org")).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText("Reserved").first()).toBeVisible()
    await expect(page.getByRole("button", { name: "Start retiring" })).toBeVisible()
    await expect(page.getByRole("button", { name: "Remove", exact: true })).toHaveCount(0)
  })

  test("adding a domain goes straight to its records", async ({ page }) => {
    await goto(page, "/domains")
    await page.getByRole("button", { name: "Add base domain" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByLabel("Domain").fill("new.example.com")
    await dialog.getByRole("radio", { name: /On-demand TLS/ }).click()
    await dialog.getByRole("button", { name: "Add base domain" }).click()

    await expect(page.getByRole("heading", { name: "new.example.com" })).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText("_meshploy-verify.new.example.com").locator("visible=true")).toBeVisible()
    await expect(page.getByText("*.new.example.com").locator("visible=true")).toBeVisible()
  })

  test("a name that is not a domain is refused in the dialog", async ({ page }) => {
    await goto(page, "/domains")
    await page.getByRole("button", { name: "Add base domain" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByLabel("Domain").fill("localhost")
    await dialog.getByRole("button", { name: "Add base domain" }).click()
    await expect(dialog.getByText(/is not a domain name/)).toBeVisible()
  })
})

// A custom hostname is proved on its own, from the route's page. The API check
// existed long before anything called it, so a custom-domain route created in
// the console could never get a certificate.
test.describe("Custom domain ownership", () => {
  const DEMO_PROJECT_ID = "00000000-0000-0000-0000-000000000003"

  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test("an unproved hostname shows the record, and says why TLS fails", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/routes/00000000-0000-0000-0000-0000000000e9`)
    await expect(page.getByText("No certificate until ownership is proved")).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText("_meshploy-verify.store.customer.example").locator("visible=true")).toBeVisible()
    await expect(page.getByText("7d2e9a0c4b1f8e3a6c5d0b9f2e7a4c1d").locator("visible=true")).toBeVisible()
    await expect(page.getByText("Not verified")).toBeVisible()
    await page.screenshot({ path: "test-results/custom-domain-unproved.png", fullPage: true })

    // Fails the way a real check does before DNS propagates, then passes.
    await page.getByRole("button", { name: "Check now" }).click()
    await expect(page.getByText(/not yet propagated/)).toBeVisible()
    await page.getByRole("button", { name: "Check now" }).click()
    await expect(page.getByText("No certificate until ownership is proved")).toHaveCount(0)
    await expect(page.getByText("Verified", { exact: true })).toBeVisible()
  })

  test("a route on a base domain has nothing to prove", async ({ page }) => {
    await goto(page, `/projects/${DEMO_PROJECT_ID}/routes/00000000-0000-0000-0000-0000000000e1`)
    await expect(page.getByText("shop.demo.example.com").first()).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText("No certificate until ownership is proved")).toHaveCount(0)
    await expect(page.getByText("Ownership")).toHaveCount(0)
  })

  test("the custom domain appears on the Domains page as not verified", async ({ page }) => {
    await goto(page, "/domains")
    const custom = page.getByRole("heading", { name: "Custom domains" }).locator("xpath=ancestor::section[1]")
    const row = custom.getByRole("row", { name: /store\.customer\.example/ })
    await expect(row.getByText("Not verified")).toBeVisible({ timeout: 10_000 })
  })
})

// A domain that has served routes goes by being seen through: retire it, clear
// what holds it, then remove it. Retiring is reversible and switches nothing off.
test.describe("Retiring a domain", () => {
  const openDomain = toDomain

  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
    await goto(page, "/domains")
  })

  test("the primary cannot be retired, and a verified domain is not removed with one click", async ({ page }) => {
    await openDomain(page, "demo.example.com")
    await expect(page.getByRole("button", { name: "Start retiring" })).toHaveCount(0)

    await openDomain(page, "apps.example.org")
    await expect(page.getByRole("heading", { name: "Retire this domain" })).toBeVisible()
    await expect(page.getByRole("button", { name: "Remove", exact: true })).toHaveCount(0)
  })

  test("an unverified domain skips retiring and can be removed straight away", async ({ page }) => {
    await openDomain(page, "shop.example.net")
    await expect(page.getByText(/never verified, so nothing was ever served/)).toBeVisible()
    await expect(page.getByRole("button", { name: "Remove", exact: true })).toBeEnabled()
  })

  test("retiring says nothing stops serving, and can be undone", async ({ page }) => {
    await openDomain(page, "apps.example.org")
    await page.getByRole("button", { name: "Start retiring" }).click()

    await expect(page.getByText(/^Retiring since/)).toBeVisible()
    await expect(page.getByText(/Everything already on it keeps serving/)).toBeVisible()
    await expect(page.getByRole("heading", { name: "Still on this domain" })).toBeVisible()
    await page.screenshot({ path: "test-results/domain-retiring.png", fullPage: true })

    await page.getByRole("button", { name: "Stop retiring" }).click()
    await expect(page.getByRole("heading", { name: "Retire this domain" })).toBeVisible()
    await expect(page.getByText(/^Retiring since/)).toHaveCount(0)
  })

  test("move the route off with a redirect, delete the redirect, then remove", async ({ page }) => {
    await openDomain(page, "apps.example.org")
    await page.getByRole("button", { name: "Start retiring" }).click()
    const checklist = page.getByRole("heading", { name: "Still on this domain" }).locator("xpath=ancestor::section[1]")
    await expect(page.getByRole("button", { name: "Remove", exact: true })).toBeDisabled()

    // Move: keeps subdomain, previews the new name, offers the redirect.
    await checklist.getByRole("button", { name: "Move" }).click()
    const dialog = page.getByRole("dialog")
    await expect(dialog.getByText("blog.demo.example.com")).toBeVisible()
    await expect(dialog.getByText("Keep the old name redirecting")).toBeVisible()
    await page.screenshot({ path: "test-results/domain-move.png", fullPage: true })
    await dialog.getByRole("button", { name: "Move route" }).click()
    await expect(dialog).toHaveCount(0)

    // The redirect is what is left, and says what it is.
    await expect(checklist.getByText(/Redirects to/)).toBeVisible()
    await expect(checklist.getByText("blog.demo.example.com")).toBeVisible()
    await expect(page.getByRole("button", { name: "Remove", exact: true })).toBeDisabled()

    await checklist.getByRole("button", { name: "Delete redirect" }).click()
    await page.getByRole("dialog").getByRole("button", { name: "Delete redirect" }).click()
    await expect(checklist.getByText(/Nothing holds this domain any more/)).toBeVisible()

    await page.getByRole("button", { name: "Remove", exact: true }).click()
    await page.getByRole("dialog").getByRole("button", { name: "Remove domain" }).click()
    await expect(page.getByRole("heading", { name: "Domains", exact: true })).toBeVisible()
    await expect(page.getByRole("link", { name: /apps\.example\.org/ })).toHaveCount(0)
  })

  test("a retiring domain is no longer offered to new routes", async ({ page }) => {
    await toNewRoute(page)
    const picker = page.getByRole("combobox", { name: "Domain" })
    await picker.click()
    await expect(page.getByRole("option", { name: /apps\.example\.org/ })).toBeVisible()
    await page.keyboard.press("Escape")

    await openDomain(page, "apps.example.org")
    await page.getByRole("button", { name: "Start retiring" }).click()
    await expect(page.getByText(/^Retiring since/)).toBeVisible()

    await toNewRoute(page)
    // The form has loaded its domains once the suffix shows. Only then does
    // an absent picker mean anything: before, it is absent on any form.
    await expect(page.getByText(".demo.example.com").first()).toBeVisible()
    // One usable domain is left - shop.example.net was never verified - so
    // there is nothing to choose between.
    await expect(page.getByRole("combobox", { name: "Domain" })).toHaveCount(0)
  })
})

// Moving the primary moves a pointer, not the platform. The old primary keeps
// serving its console and headscale names until it is retired and removed, and
// it cannot go while nodes still reach the mesh through it.
test.describe("Moving the primary domain", () => {
  const openDomain = toDomain

  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
    await goto(page, "/domains")
  })

  test("the confirmation says what moves and that nothing stops serving", async ({ page }) => {
    await openDomain(page, "apps.example.org")
    await page.getByRole("button", { name: "Make primary" }).click()
    const dialog = page.getByRole("dialog")
    await expect(dialog.getByText("demo.example.com", { exact: true })).toBeVisible()
    await expect(dialog.getByText(/keeps serving its console, API and headscale names/)).toBeVisible()
    await expect(dialog.getByText(/Traffic between nodes is not interrupted/)).toBeVisible()
    await page.screenshot({ path: "test-results/domain-make-primary.png", fullPage: true })
    await dialog.getByRole("button", { name: "Cancel" }).click()
    await expect(page.getByText("Primary", { exact: true })).toHaveCount(0)
  })

  test("the old primary keeps serving, and goes only once its nodes have moved", async ({ page }) => {
    await openDomain(page, "apps.example.org")
    await page.getByRole("button", { name: "Make primary" }).click()
    await page.getByRole("dialog").getByRole("button", { name: "Make primary" }).click()
    await expect(page.getByText("Primary", { exact: true })).toBeVisible()

    await openDomain(page, "demo.example.com")
    await expect(page.getByText("Former primary")).toBeVisible()
    await expect(page.getByText(/Still served here, though this is no longer the primary/)).toBeVisible()
    await expect(page.getByText("Serving").first()).toBeVisible()

    await page.getByRole("button", { name: "Start retiring" }).click()
    const nodesPanel = page.getByRole("heading", { name: "Nodes joined through this domain" }).locator("xpath=ancestor::section[1]")
    await expect(nodesPanel.getByText(/--login-server=https:\/\/headscale\.apps\.example\.org/)).toBeVisible()
    await expect(nodesPanel.getByText(/Move one node first/)).toBeVisible()
    await expect(page.getByRole("button", { name: "Remove", exact: true })).toBeDisabled()
    await page.screenshot({ path: "test-results/domain-former-primary.png", fullPage: true })

    // Marking says what the operator did; it is confirmed, not a click.
    const marks = nodesPanel.getByRole("button", { name: "Mark as moved" })
    const count = await marks.count()
    expect(count).toBeGreaterThan(0)
    for (let i = 0; i < count; i++) {
      await nodesPanel.getByRole("button", { name: "Mark as moved" }).first().click()
      const dialog = page.getByRole("dialog")
      await expect(dialog.getByText(/it does not move it, and it\s+cannot check/)).toBeVisible()
      await dialog.getByRole("button", { name: "It has been moved" }).click()
      await expect(dialog).toHaveCount(0)
    }
    await expect(nodesPanel.getByText(/No node reaches the mesh through/)).toBeVisible()

    // A CI job deploys through this domain's name too. Meshploy only sees where
    // its calls arrive, so a job that is gone is forgotten rather than marked.
    const ci = page.getByRole("heading", { name: "CI jobs deploying through this domain" }).locator("xpath=ancestor::section[1]")
    await expect(ci.getByText(/last called through/)).toBeVisible()
    await expect(ci.getByText("api.demo.example.com")).toBeVisible()
    await ci.getByRole("button", { name: "The job is gone" }).click()
    await expect(page.getByRole("dialog").getByText(/its next call puts it straight back/)).toBeVisible()
    await page.getByRole("dialog").getByRole("button", { name: "Forget it" }).click()
    await expect(ci.getByText(/No CI job deploys through/)).toBeVisible()

    // Git providers were told this domain's api. name too. The push hooks move
    // through the provider's API; the GitHub App's URLs are changed by hand in
    // GitHub, then marked - with the exact new values on screen.
    const providers = page.getByRole("heading", { name: "Git providers calling this domain" }).locator("xpath=ancestor::section[1]")
    await expect(providers.getByText(/the GitHub App's URLs/)).toBeVisible()
    await expect(providers.getByText(/https:\/\/api\.apps\.example\.org\/api\/v1\/webhooks\/github\//).first()).toBeVisible()
    await page.screenshot({ path: "test-results/domain-provider-registrations.png", fullPage: true })
    await expect(page.getByRole("button", { name: "Remove", exact: true })).toBeDisabled()

    // One button per integration with hooks; each move takes its row away.
    const moves = providers.getByRole("button", { name: "Move webhooks" })
    expect(await moves.count()).toBeGreaterThan(0)
    while ((await moves.count()) > 0) {
      const before = await moves.count()
      await moves.first().click()
      await expect(moves).toHaveCount(before - 1)
    }
    await expect(providers.getByText(/push webhooks on/)).toHaveCount(0)

    await providers.getByRole("button", { name: "Mark as updated" }).click()
    const confirm = page.getByRole("dialog")
    await expect(confirm.getByText(/it cannot check/)).toBeVisible()
    await confirm.getByRole("button", { name: "It has been changed" }).click()
    await expect(providers.getByText(/No git provider calls this domain any more/)).toBeVisible()
  })
})
