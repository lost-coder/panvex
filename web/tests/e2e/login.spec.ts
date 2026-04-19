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
    await mockApi(page);
    await mockLoginSuccess(page);

    await page.goto("/login");
    await expect(page.getByLabel(/username/i)).toBeVisible();

    await page.getByLabel(/username/i).fill("operator");
    await page.getByLabel(/password/i).fill("correcthorse");

    authed = true;
    await page.getByRole("button", { name: /sign in|log in/i }).click();

    await expect(page).toHaveURL(/\/(dashboard)?$/);
  });

  test("surfaces an error on invalid credentials without navigating", async ({ page }) => {
    await page.route("**/api/auth/me", (route) =>
      route.fulfill({ status: 401, body: "{}" }),
    );
    await mockApi(page);
    await mockLoginFailure(page);

    await page.goto("/login");
    await page.getByLabel(/username/i).fill("operator");
    await page.getByLabel(/password/i).fill("wrong");
    await page.getByRole("button", { name: /sign in|log in/i }).click();

    await expect(page.getByText(/invalid|credentials/i)).toBeVisible();
    await expect(page).toHaveURL(/\/login/);
  });
});
