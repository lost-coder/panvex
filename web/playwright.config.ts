import { defineConfig, devices } from "@playwright/test";

/**
 * Phase 8 — Playwright E2E baseline.
 *
 * Tests target a local Vite dev server that is spawned by Playwright
 * (`webServer` block). API calls are intercepted in each test via
 * `page.route()` so the suite stays hermetic — no backend, no database,
 * no shared state between runs.
 *
 * Why Chromium only? Operators run either Chrome or Edge on corporate
 * machines; adding Firefox/WebKit doubles the test-matrix cost for a
 * smoke suite. CI can widen once the suite settles.
 */
export default defineConfig({
  testDir: "./tests/e2e",
  timeout: 30_000,
  expect: { timeout: 5_000 },
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI ? [["github"], ["list"]] : "list",
  use: {
    baseURL: "http://localhost:5173",
    trace: "on-first-retry",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
    // 8.ext: widen the matrix once the Chromium smoke stays green.
    // Firefox covers Gecko quirks (form validation, focus-visible);
    // WebKit covers Safari which a subset of operators actually use.
    // Both stay on the same smoke suite — no Safari-only spec needed.
    {
      name: "firefox",
      use: { ...devices["Desktop Firefox"] },
    },
    {
      name: "webkit",
      use: { ...devices["Desktop Safari"] },
    },
  ],
  webServer: {
    command: "npm run dev",
    url: "http://localhost:5173",
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
    stdout: "pipe",
    stderr: "pipe",
  },
});
