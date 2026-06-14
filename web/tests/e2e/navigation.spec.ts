import { expect, test } from "@playwright/test";

import { mockApi } from "./helpers/mock-api";

/**
 * Smoke: vim-style navigation shortcuts and the `?` help overlay.
 *
 * Validates that ShortcutsOverlay binds, the leader sequence
 * `g s` / `g c` routes to /servers and /clients, and pressing `?`
 * surfaces the help dialog.
 */
// Helper: wait until the dashboard is rendered AND focus is on the
// document body so global shortcuts are picked up. Without it, the
// fast-path tests pressed `g` before the keydown handler mounted.
async function loadDashboard(page: import("@playwright/test").Page) {
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  await page.locator("body").click();
}

test.describe("Keyboard navigation", () => {
  test("g s jumps to Servers", async ({ page }) => {
    await mockApi(page);
    await loadDashboard(page);
    await expect(page).toHaveURL(/\/(dashboard)?$/);

    await page.keyboard.press("g");
    await page.keyboard.press("s");
    await expect(page).toHaveURL(/\/servers/);
  });

  test("g c jumps to Clients", async ({ page }) => {
    await mockApi(page);
    await loadDashboard(page);

    await page.keyboard.press("g");
    await page.keyboard.press("c");
    await expect(page).toHaveURL(/\/clients/);
  });

  test("? toggles the shortcuts overlay", async ({ page }) => {
    await mockApi(page);
    await loadDashboard(page);

    await page.keyboard.press("?");
    // English default locale: the overlay title is "Keyboard shortcuts"
    // (a stale Russian assertion here is the same locale drift the offline
    // smoke test had — see 73d8b6dc).
    const dialog = page.getByRole("dialog", { name: /keyboard shortcuts/i });
    await expect(dialog).toBeVisible();

    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
  });
});
