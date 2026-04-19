import { expect, test } from "@playwright/test";

/**
 * Integration smoke: login against a real control-plane binary.
 *
 * Preconditions (see ../README.md):
 *   • `admin/e2e-secret` account bootstrapped via
 *     `control-plane bootstrap-admin`
 *   • control-plane listening on $PANVEX_E2E_URL (default :18080)
 *   • SQLite driver, in-memory or temp file, clean state
 *
 * This test is representative, not exhaustive. It proves the
 * auth-middleware + session-store wiring works end-to-end; the
 * mocked smoke suite already covers UX branches.
 */
test("operator logs in and reaches the dashboard", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel(/username/i).fill("admin");
  await page.getByLabel(/password/i).fill("e2e-secret");
  await page.getByRole("button", { name: /sign in|log in/i }).click();

  await expect(page).toHaveURL(/\/(dashboard)?$/, { timeout: 15_000 });
  // Sanity: /auth/me succeeded — the operator greeting or avatar
  // should be visible. Exact selector is intentionally loose so a
  // design tweak doesn't break the integration gate.
  await expect(page.getByText(/admin|dashboard/i).first()).toBeVisible();
});
