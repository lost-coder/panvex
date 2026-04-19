import { expect, test } from "@playwright/test";

import { mockApi } from "./helpers/mock-api";

/**
 * Visual regression baseline using Playwright's built-in
 * `toHaveScreenshot()`. Baselines live alongside the spec in
 * `tests/e2e/visual.spec.ts-snapshots/` and are committed to git —
 * the first CI run writes them, subsequent runs diff.
 *
 * This stays cheap compared to Chromatic/Percy (no external service,
 * no API key) at the cost of owning the baselines in-tree. Regenerate
 * with `npm run test:e2e -- --update-snapshots` when a legitimate UI
 * change lands.
 *
 * Scope is intentionally narrow: 1 shot per primary route, desktop
 * viewport only. Wider coverage goes in a separate PR once the
 * baselines prove stable.
 */
test.describe("Visual regression", () => {
  test.beforeEach(async ({ page }) => {
    await mockApi(page);
  });

  test("dashboard matches snapshot", async ({ page }) => {
    await page.goto("/");
    // Wait for the skeleton → real content transition; in the mocked
    // scenario this is synchronous but we still give the frame one
    // microtask to settle so animations finish.
    await page.waitForLoadState("networkidle");
    await expect(page).toHaveScreenshot("dashboard.png", {
      fullPage: false,
      animations: "disabled",
    });
  });

  test("servers list matches snapshot", async ({ page }) => {
    await page.goto("/servers");
    await page.waitForLoadState("networkidle");
    await expect(page).toHaveScreenshot("servers.png", {
      fullPage: false,
      animations: "disabled",
    });
  });

  test("clients list matches snapshot", async ({ page }) => {
    await page.goto("/clients");
    await page.waitForLoadState("networkidle");
    await expect(page).toHaveScreenshot("clients.png", {
      fullPage: false,
      animations: "disabled",
    });
  });
});
