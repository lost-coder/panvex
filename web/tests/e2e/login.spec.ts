import { expect, test } from "@playwright/test";

import { mockApi, mockLoginFailure, mockLoginSuccess } from "./helpers/mock-api";

/**
 * Smoke: login — happy-path and invalid-credentials flows.
 *
 * /auth/me returns 401 initially so the router redirects to /login;
 * after the form submits, mockLoginSuccess switches the fixture to
 * return the default operator and the router mounts ProtectedShell.
 */
test.describe("Login flow", () => {
  test("logs in with valid credentials and lands on the dashboard", async ({ page }) => {
    // Register the baseline mocks first; the dynamic /auth/me handler
    // below has to run AFTER mockApi so Playwright resolves it before
    // the always-authenticated default. Last-registered handler wins.
    await mockApi(page);
    await mockLoginSuccess(page);

    let authed = false;
    await page.route("**/api/auth/me", (route) => {
      if (!authed) {
        return route.fulfill({ status: 401, body: "{}" });
      }
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ id: "u1", username: "operator", role: "admin", totp_enabled: false }),
      });
    });

    await page.goto("/login");
    await expect(page.getByLabel(/username/i)).toBeVisible();

    await page.getByLabel(/username/i).fill("operator");
    // Anchored: the show/hide-password toggle's aria-label also contains
    // "password", so a loose /password/i matches two elements.
    await page.getByLabel(/^password$/i).fill("correcthorse");

    authed = true;
    await page.getByRole("button", { name: /sign in|log in/i }).click();

    await expect(page).toHaveURL(/\/(dashboard)?$/);
  });

  test("surfaces an error on invalid credentials without navigating", async ({ page }) => {
    await mockApi(page);
    await mockLoginFailure(page);
    await page.route("**/api/auth/me", (route) =>
      route.fulfill({ status: 401, body: "{}" }),
    );

    await page.goto("/login");
    await page.getByLabel(/username/i).fill("operator");
    await page.getByLabel(/^password$/i).fill("wrong");
    await page.getByRole("button", { name: /sign in|log in/i }).click();

    await expect(page.getByText(/invalid|credentials/i)).toBeVisible();
    await expect(page).toHaveURL(/\/login/);
  });
});
