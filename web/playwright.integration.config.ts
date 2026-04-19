import { defineConfig, devices } from "@playwright/test";

/**
 * Integration E2E config — hits a real control-plane binary instead of
 * intercepting /api/*. See tests/e2e/integration/README.md for the
 * local run recipe.
 *
 * Differences from the smoke config:
 *   • testDir: tests/e2e/integration (separate suite, tagged)
 *   • baseURL: points at a backend on :18080, not the vite dev server
 *   • no webServer block — the caller is expected to start the
 *     control-plane themselves (or via docker-compose in CI)
 *   • longer timeouts — cold-start SQLite + bootstrap takes seconds
 */
export default defineConfig({
  testDir: "./tests/e2e/integration",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["list"]] : "list",
  use: {
    baseURL: process.env.PANVEX_E2E_URL ?? "http://localhost:18080",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
