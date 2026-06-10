import { expect, test } from "@playwright/test";

import { mockApi } from "./helpers/mock-api";

/**
 * Smoke: OfflineBanner surfaces when the browser reports offline and
 * disappears when connectivity returns. Uses Playwright's
 * `context.setOffline()` which flips `navigator.onLine` and fires the
 * matching DOM events.
 */
test.describe("Offline detection", () => {
  test("banner appears on offline and disappears on reconnect", async ({ page, context }) => {
    await mockApi(page);
    await page.goto("/");
    // Wait for the shell to mount before flipping connectivity —
    // otherwise the banner may not yet be in the tree when we toggle
    // offline.
    await page.waitForLoadState("networkidle");

    await context.setOffline(true);
    await expect(
      page.getByText(/connection lost/i, { exact: false }),
    ).toBeVisible();

    await context.setOffline(false);
    await expect(
      page.getByText(/connection lost/i, { exact: false }),
    ).toBeHidden();
  });
});
