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
