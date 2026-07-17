import { defineConfig, devices } from "@playwright/test";

/**
 * Phase 8 — Playwright E2E baseline.
 *
 * Tests target a local Vite dev server that is spawned by Playwright
 * (`webServer` block). API calls are intercepted in each test via
 * `page.route()` so the suite stays hermetic — no backend, no database,
 * no shared state between runs.
 *
 * Why Chromium only in CI? Operators run either Chrome or Edge on
 * corporate machines; adding Firefox/WebKit doubles the test-matrix
 * cost for a smoke suite. CI (`.github/workflows/ci.yml`) installs and
 * runs only the `chromium` project — Firefox/WebKit are declared below
 * but gated behind `PW_ALL_BROWSERS` so the config stops advertising a
 * matrix CI never actually exercises. Run the full matrix locally with
 * `PW_ALL_BROWSERS=1 npx playwright test` (after
 * `npm run test:e2e:install:all` to fetch Firefox/WebKit).
 */
export default defineConfig({
  testDir: "./tests/e2e",
  // Integration specs under tests/e2e/integration/ require a real
  // control-plane backend on :18080 and have their own playwright
  // config (playwright.integration.config.ts). The smoke runner stays
  // hermetic — page.route() stubs cover every endpoint — so this
  // ignore keeps the two suites independent.
  testIgnore: [
    "**/integration/**",
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
    // Firefox covers Gecko quirks (form validation, focus-visible);
    // WebKit covers Safari which a subset of operators actually use.
    // Both stay on the same smoke suite — no Safari-only spec needed.
    // Neither runs in CI (chromium is the official gate); opt in locally
    // with `PW_ALL_BROWSERS=1` so the declared matrix matches what's
    // actually exercised anywhere.
    ...(process.env.PW_ALL_BROWSERS
      ? [
          {
            name: "firefox",
            use: { ...devices["Desktop Firefox"] },
          },
          {
            name: "webkit",
            use: { ...devices["Desktop Safari"] },
          },
        ]
      : []),
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
