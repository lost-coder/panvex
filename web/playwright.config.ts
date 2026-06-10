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
  // Integration specs under tests/e2e/integration/ require a real
  // control-plane backend on :18080 and have their own playwright
  // config (playwright.integration.config.ts). The smoke runner stays
  // hermetic — page.route() stubs cover every endpoint — so this
  // ignore keeps the two suites independent. visual.spec.ts is also
  // skipped by default: the toHaveScreenshot baselines are not
  // committed yet, so the first run (and every CI run) would fail.
  // Run `npm run test:e2e -- --grep @visual --update-snapshots` to
  // mint baselines, then commit the resulting *-snapshots/ folder
  // and remove the testIgnore entry below.
  testIgnore: [
    "**/integration/**",
    "**/visual.spec.ts",
  ],
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
